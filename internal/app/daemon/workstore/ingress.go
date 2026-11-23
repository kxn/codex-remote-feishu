package workstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"reflect"
	"time"
)

func ingressID(env work.RawIngressEnvelope) string {
	sum := sha256.Sum256([]byte(mustJSON([]string{env.Tenant, env.ChatID, env.MessageID})))
	return "ingress:" + hex.EncodeToString(sum[:])
}
func (t *Tx) AcceptRawIngress(env work.RawIngressEnvelope, gateway work.IngressGateway) (record work.IngressRecord, created bool, err error) {
	err = t.atomic(func() error {
		if err := env.Validate(); err != nil {
			return err
		}
		if err := gateway.Validate(); err != nil {
			return err
		}
		if gateway.GatewayRef != env.GatewayRef || gateway.AppID != env.AppID {
			return fmt.Errorf("gateway event identity mismatch")
		}
		generation := gateway.BotGeneration
		id := ingressID(env)
		existing, e := t.GetIngress(id)
		if e == nil {
			if existing.Envelope.Tenant != env.Tenant || existing.Envelope.ChatID != env.ChatID || existing.Envelope.ChatType != env.ChatType || existing.Envelope.MessageID != env.MessageID || existing.Envelope.RawContent != env.RawContent || existing.Envelope.MessageType != env.MessageType {
				return ErrIdempotencyConflict
			}
			record = existing
		} else if !errors.Is(e, ErrNotFound) {
			return e
		} else {
			route := work.IngressRoute{Kind: work.ScopePersonal}
			if env.ChatType == "group" {
				route.Kind = work.ScopeBootstrap
				binding, e := t.GetProjectChannelBinding(env.Tenant, env.ChatID)
				if e != nil && !errors.Is(e, ErrNotFound) {
					return e
				}
				if e == nil {
					route.BindingGeneration = binding.BindingGeneration
					if binding.Active {
						project, e := t.GetProject(binding.ProjectID)
						if e != nil {
							return e
						}
						route.Kind = work.ScopeProject
						route.ProjectID = project.ID
						route.ScopeID = project.ScopeID
					}
				}
			}
			record = work.IngressRecord{Gateway: gateway, ID: id, Envelope: env, Route: route, State: work.IngressPending, Revision: 1}
			if e = t.record("raw_ingress", id, 1, 0, record); e != nil {
				return e
			}
			created = true
		}
		payload := mustJSON(work.IngressObservation{Envelope: env, Gateway: gateway})
		var priorID, prior string
		e = t.tx.QueryRowContext(t.ctx, "SELECT ingress_id,payload FROM ingress_observations WHERE app_id=? AND generation=? AND event_id=?", env.AppID, generation, env.EventID).Scan(&priorID, &prior)
		if e == nil {
			var previous work.IngressObservation
			if e = json.Unmarshal([]byte(prior), &previous); e != nil {
				return e
			}
			previous.Envelope.ReceivedAt = env.ReceivedAt
			if priorID != id || !reflect.DeepEqual(previous, work.IngressObservation{Envelope: env, Gateway: gateway}) {
				return ErrIdempotencyConflict
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		_, e = t.tx.ExecContext(t.ctx, "INSERT INTO ingress_observations(ingress_id,app_id,generation,event_id,payload) VALUES(?,?,?,?,?)", id, env.AppID, generation, env.EventID, payload)
		return e
	})
	return
}
func (t *Tx) GetIngress(id string) (work.IngressRecord, error) {
	var v work.IngressRecord
	err := t.read("raw_ingress", id, &v)
	return v, err
}
func (t *Tx) ListPendingIngress(limit int) ([]work.IngressRecord, error) {
	return t.ListPendingIngressAt(time.Now(), limit)
}
func (t *Tx) ListPendingIngressAt(now time.Time, limit int) ([]work.IngressRecord, error) {
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("ingress limit must be 1..1000")
	}
	rows, err := t.tx.QueryContext(t.ctx, "SELECT payload FROM raw_ingress WHERE state='pending' AND NOT EXISTS (SELECT 1 FROM personal_migration_fences f WHERE f.scope_id=json_extract(raw_ingress.payload,'$.route.scope_id')) AND (json_extract(payload,'$.next_attempt_at') IS NULL OR julianday(json_extract(payload,'$.next_attempt_at'))<=julianday(?)) ORDER BY sequence LIMIT ?", now.UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []work.IngressRecord{}
	for rows.Next() {
		var payload string
		if err = rows.Scan(&payload); err != nil {
			return nil, err
		}
		var value work.IngressRecord
		if err = json.Unmarshal([]byte(payload), &value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
func (t *Tx) PutIngress(v work.IngressRecord, expected int64) error {
	old, err := t.GetIngress(v.ID)
	if err != nil {
		return err
	}
	if old.Gateway != v.Gateway || !reflect.DeepEqual(old.Envelope, v.Envelope) || old.Route != v.Route {
		return fmt.Errorf("raw envelope and receive-time routing snapshot are immutable")
	}
	if old.State != work.IngressPending && (old.State != v.State || old.MessageID != v.MessageID) {
		return fmt.Errorf("processed ingress cannot be rerouted")
	}
	return t.record("raw_ingress", v.ID, v.Revision, expected, v)
}
func (t *Tx) ListIngressObservations(id string) ([]work.IngressObservation, error) {
	rows, err := t.tx.QueryContext(t.ctx, "SELECT payload FROM ingress_observations WHERE ingress_id=? ORDER BY app_id,event_id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []work.IngressObservation{}
	for rows.Next() {
		var data string
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		var value work.IngressObservation
		if err = json.Unmarshal([]byte(data), &value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
func (t *Tx) PutPrivateCommand(v work.TypedCommandEnvelope, expected int64) error {
	if v.State == "sending" {
		if err := t.AssertPersonalMigrationOpen(v.ScopeID); err != nil {
			return err
		}
	}
	if expected > 0 {
		old, e := t.GetPrivateCommand(v.ID)
		if e != nil {
			return e
		}
		if old.State != v.State && !(old.State == "pending" && v.State == "sending") && !(old.State == "sending" && (v.State == "handled" || v.State == "unknown")) {
			return fmt.Errorf("invalid private command transition")
		}
		if e = immutable(old.IngressID != v.IngressID || old.ScopeID != v.ScopeID || old.PrincipalID != v.PrincipalID || old.RoleID != v.RoleID || old.Text != v.Text || old.GatewayRef != v.GatewayRef || old.ContextID != v.ContextID || old.NavigationRevision != v.NavigationRevision || old.ProjectViewScopeID != v.ProjectViewScopeID || old.ProjectContextID != v.ProjectContextID || !reflect.DeepEqual(old.Raw, v.Raw)); e != nil {
			return e
		}
	}
	return t.record("private_commands", v.ID, v.Revision, expected, v)
}
func (t *Tx) GetPrivateCommand(id string) (work.TypedCommandEnvelope, error) {
	var v work.TypedCommandEnvelope
	err := t.read("private_commands", id, &v)
	return v, err
}
func (t *Tx) ListPendingPrivateCommands() ([]work.TypedCommandEnvelope, error) {
	rows, err := t.tx.QueryContext(t.ctx, "SELECT c.payload FROM private_commands c JOIN raw_ingress r ON r.id=json_extract(c.payload,'$.ingress_id') WHERE c.state='pending' ORDER BY r.sequence,c.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	commands := []work.TypedCommandEnvelope{}
	for rows.Next() {
		var payload string
		if err = rows.Scan(&payload); err != nil {
			return nil, err
		}
		var command work.TypedCommandEnvelope
		if err = json.Unmarshal([]byte(payload), &command); err != nil {
			return nil, err
		}
		commands = append(commands, command)
	}
	return commands, rows.Err()
}
