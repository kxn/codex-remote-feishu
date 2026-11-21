package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"reflect"
	"strings"
	"time"
)

type MaintenanceState string

const (
	MaintenanceQueued    MaintenanceState = "queued"
	MaintenanceSending   MaintenanceState = "sending"
	MaintenanceUnknown   MaintenanceState = "unknown"
	MaintenanceCompleted MaintenanceState = "completed"
	MaintenanceFailed    MaintenanceState = "failed"
)

type MaintenanceProof struct {
	NativeWriteWatermark      int64          `json:"native_write_watermark"`
	CanonicalCommandWatermark int64          `json:"canonical_command_watermark"`
	InspectionCommandID       string         `json:"inspection_command_id"`
	Source                    work.SourceRef `json:"source"`
	ObservedAt                time.Time      `json:"observed_at"`
	NativeStopped             bool           `json:"native_stopped"`
	ContinuationStopped       bool           `json:"continuation_stopped"`
	BackgroundWorkStopped     bool           `json:"background_work_stopped"`
}
type Maintenance struct {
	ID                 string                 `json:"id"`
	ScopeID            string                 `json:"scope_id"`
	ContextID          string                 `json:"context_id"`
	WorkerID           string                 `json:"worker_id"`
	WorkerGeneration   int64                  `json:"worker_generation"`
	CarrierIncarnation string                 `json:"carrier_incarnation"`
	InstanceID         string                 `json:"instance_id"`
	InstanceGeneration int64                  `json:"instance_generation"`
	ProcessGeneration  int64                  `json:"process_generation"`
	LocationID         string                 `json:"location_id,omitempty"`
	LocationGeneration int64                  `json:"location_generation"`
	NativeSessionID    string                 `json:"native_session_id"`
	Kind               agentproto.CommandKind `json:"kind"`
	Source             PrivateActionSource    `json:"source"`
	State              MaintenanceState       `json:"state"`
	CommandID          string                 `json:"command_id"`
	Preflight          *MaintenanceProof      `json:"preflight,omitempty"`
	Start              *work.SourceRef        `json:"start,omitempty"`
	Terminal           *work.SourceRef        `json:"terminal,omitempty"`
	Completion         *MaintenanceProof      `json:"completion,omitempty"`
	LastError          string                 `json:"last_error,omitempty"`
	Revision           int64                  `json:"revision"`
	CreatedAt          time.Time              `json:"created_at"`
	UpdatedAt          time.Time              `json:"updated_at"`
}

const SchemaMaintenanceV1 = `
CREATE TABLE native_maintenance (id TEXT PRIMARY KEY,worker_id TEXT NOT NULL REFERENCES workers(id),command_id TEXT NOT NULL UNIQUE,revision INTEGER NOT NULL CHECK(revision>0),payload TEXT NOT NULL CHECK(json_valid(payload)),state TEXT GENERATED ALWAYS AS(json_extract(payload,'$.state')) STORED NOT NULL);
CREATE UNIQUE INDEX maintenance_worker ON native_maintenance(worker_id) WHERE state IN('queued','sending','unknown');
CREATE TABLE maintenance_admission_blocks(resource_key TEXT PRIMARY KEY,maintenance_id TEXT NOT NULL REFERENCES native_maintenance(id));
`

func (t *Tx) GetMaintenance(id string) (Maintenance, error) {
	var v Maintenance
	var body string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM native_maintenance WHERE id=?", id).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal([]byte(body), &v)
	}
	return v, err
}
func (t *Tx) GetMaintenanceByCommand(commandID string) (Maintenance, error) {
	var id string
	err := t.tx.QueryRowContext(t.ctx, "SELECT id FROM native_maintenance WHERE command_id=?", commandID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Maintenance{}, ErrNotFound
	}
	if err != nil {
		return Maintenance{}, err
	}
	return t.GetMaintenance(id)
}
func (t *Tx) ListPendingMaintenance() ([]Maintenance, error) {
	rows, err := t.tx.QueryContext(t.ctx, "SELECT payload FROM native_maintenance WHERE state IN('queued','sending','unknown') ORDER BY json_extract(payload,'$.created_at'),id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []Maintenance{}
	for rows.Next() {
		var body string
		if err = rows.Scan(&body); err != nil {
			return nil, err
		}
		var v Maintenance
		if err = json.Unmarshal([]byte(body), &v); err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, rows.Err()
}
func (t *Tx) updateMaintenance(v Maintenance, expected int64) error {
	if v.Revision != expected+1 {
		return ErrConflict
	}
	changed, err := t.tx.ExecContext(t.ctx, "UPDATE native_maintenance SET revision=?,payload=? WHERE id=? AND revision=?", v.Revision, mustJSON(v), v.ID, expected)
	if err != nil {
		return err
	}
	n, err := changed.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}
func (t *Tx) RequestMaintenance(v Maintenance) (canonical Maintenance, created bool, err error) {
	err = t.atomic(func() error {
		if v.ID == "" || v.CommandID == "" || v.NativeSessionID == "" || v.Kind != agentproto.CommandThreadCompactStart || v.State != MaintenanceQueued || v.Revision != 1 || v.CreatedAt.IsZero() {
			return fmt.Errorf("maintenance requires explicit supported intent and frozen session")
		}
		if v.Preflight != nil || v.Start != nil || v.Terminal != nil || v.Completion != nil {
			return fmt.Errorf("new maintenance cannot invent execution evidence")
		}
		scope, _, e := t.ValidatePrivateAction(v.Source)
		if e != nil {
			return e
		}
		if scope.ID != v.ScopeID {
			return ErrScopeDenied
		}
		worker, e := t.GetWorker(v.WorkerID)
		if e != nil {
			return e
		}
		if e = maintenanceMatches(v, worker); e != nil {
			return e
		}
		carrier, e := t.GetCarrier(v.CarrierIncarnation)
		if e != nil {
			return e
		}
		binding, e := t.GetRoleBotBinding(worker.RoleID)
		if e != nil {
			return e
		}
		if carrier.Owner.BotGeneration != binding.IdentityGeneration {
			return fmt.Errorf("worker identity changed; safe rebuilding is required before maintenance")
		}
		context, e := t.GetContext(worker.ContextID)
		if e != nil {
			return e
		}
		if context.Purpose == work.PurposeControl || context.Purpose == work.PurposeReview {
			return ErrScopeDenied
		}
		var sourceContext string
		if v.Source.CommandID != "" {
			command, e := t.GetPrivateCommand(v.Source.CommandID)
			if e != nil {
				return e
			}
			if fields := strings.Fields(command.Text); len(fields) == 0 || strings.ToLower(fields[0]) != "/compact" {
				return ErrScopeDenied
			}
			sourceContext = command.ContextID
		} else if v.Source.MenuInteractionID != "" {
			menu, e := t.GetMenuInteraction(v.Source.MenuInteractionID)
			if e != nil {
				return e
			}
			if menu.Envelope.EventKey != "compact" {
				return ErrScopeDenied
			}
			sourceContext = menu.ContextID
		} else if v.Source.MessageID != "" {
			attempt, e := t.GetAttemptByRunRef(v.Source.RunRef.RunID)
			if e != nil {
				return e
			}
			sourceContext = attempt.ContextID
		} else {
			interaction, e := t.GetHumanInteraction(v.Source.InteractionID)
			if e != nil {
				return e
			}
			viewID, _ := interaction.Envelope.Values["team_view"].(string)
			view, e := t.GetUIView(viewID)
			if e != nil {
				return e
			}
			if e = t.ValidateViewInteraction(view, interaction); e != nil {
				return e
			}
			sourceContext = view.ContextID
		}
		if sourceContext != v.ContextID || (worker.State != work.WorkerReady && worker.State != work.WorkerBusy) || worker.Backend != work.BackendCodex || worker.Controller != work.ControllerAutomatic {
			return ErrScopeDenied
		}
		if old, e := t.GetMaintenance(v.ID); e == nil {
			left, right := old, v
			left.Revision = 1
			left.State = MaintenanceQueued
			left.Preflight = nil
			left.Start = nil
			left.Terminal = nil
			left.Completion = nil
			left.LastError = ""
			left.UpdatedAt = right.UpdatedAt
			left.CreatedAt = right.CreatedAt
			if !reflect.DeepEqual(left, right) {
				return ErrIdempotencyConflict
			}
			canonical = old
			return nil
		} else if !errors.Is(e, ErrNotFound) {
			return e
		}
		if _, e = t.tx.ExecContext(t.ctx, "INSERT INTO native_maintenance(id,worker_id,command_id,revision,payload) VALUES(?,?,?,?,?)", v.ID, v.WorkerID, v.CommandID, v.Revision, mustJSON(v)); e != nil {
			return e
		}
		keys, e := t.maintenanceKeys(v, worker)
		if e != nil {
			return e
		}
		for _, key := range keys {
			if _, e = t.tx.ExecContext(t.ctx, "INSERT INTO maintenance_admission_blocks(resource_key,maintenance_id) VALUES(?,?)", key, v.ID); e != nil {
				return fmt.Errorf("another maintenance owns resource: %w", e)
			}
		}
		canonical = v
		created = true
		return nil
	})
	return
}
func maintenanceMatches(v Maintenance, w work.WorkerSession) error {
	if v.WorkerID != w.ID || v.ScopeID != w.ScopeID || v.ContextID != w.ContextID || v.WorkerGeneration != w.Generation || v.CarrierIncarnation != w.CarrierIncarnation || v.InstanceID != w.InstanceID || v.InstanceGeneration != w.InstanceGeneration || v.ProcessGeneration != w.ProcessGeneration || v.LocationID != w.LocationID || v.LocationGeneration != w.LocationGeneration || v.NativeSessionID != w.NativeSessionID {
		return ErrConflict
	}
	return nil
}
func (t *Tx) maintenanceKeys(v Maintenance, w work.WorkerSession) ([]string, error) {
	keys := []string{"worker:" + w.ID, fmt.Sprintf("carrier:%s:%d:%d", w.InstanceID, w.InstanceGeneration, w.ProcessGeneration)}
	if w.LocationID != "" {
		location, e := t.GetLocation(w.LocationID)
		if e != nil {
			return nil, e
		}
		keys = append(keys, "cwd:"+location.HostID+":"+location.Cwd)
		keys = append(keys, location.ResourceKeys...)
	}
	return keys, nil
}
func (t *Tx) AssertMaintenanceResourceAvailable(key string) error {
	var id string
	err := t.tx.QueryRowContext(t.ctx, "SELECT maintenance_id FROM maintenance_admission_blocks WHERE resource_key=?", key).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("resource reserved for native maintenance %s", id)
}
