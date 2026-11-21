package workstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"time"
)

const SchemaMenuInteractionsV1 = `CREATE TABLE menu_interactions(id TEXT PRIMARY KEY,revision INTEGER NOT NULL CHECK(revision>0),payload_hash TEXT NOT NULL,payload TEXT NOT NULL CHECK(json_valid(payload)),state TEXT GENERATED ALWAYS AS(json_extract(payload,'$.state')) STORED NOT NULL);CREATE INDEX pending_menu_interactions ON menu_interactions(state,id);`

type MenuInteraction struct {
	ID                 string                       `json:"id"`
	PrincipalID        string                       `json:"principal_id"`
	ScopeID            string                       `json:"scope_id"`
	ContextID          string                       `json:"context_id"`
	NavigationRevision int64                        `json:"navigation_revision"`
	Gateway            work.IngressGateway          `json:"gateway"`
	Envelope           work.MenuInteractionEnvelope `json:"envelope"`
	State              string                       `json:"state"`
	DestinationTextRef string                       `json:"destination_text_ref,omitempty"`
	DestinationState   work.OperationState          `json:"destination_state,omitempty"`
	UUID               string                       `json:"uuid"`
	ChatID             string                       `json:"chat_id,omitempty"`
	MessageID          string                       `json:"message_id,omitempty"`
	ResultRef          string                       `json:"result_ref,omitempty"`
	LastError          string                       `json:"last_error,omitempty"`
	Revision           int64                        `json:"revision"`
}

func (t *Tx) GetMenuInteraction(id string) (MenuInteraction, error) {
	var v MenuInteraction
	var body string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM menu_interactions WHERE id=?", id).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal([]byte(body), &v)
	}
	return v, err
}
func (t *Tx) updateMenuInteraction(v MenuInteraction, expected int64) error {
	if v.Revision != expected+1 {
		return ErrConflict
	}
	result, err := t.tx.ExecContext(t.ctx, "UPDATE menu_interactions SET revision=?,payload=? WHERE id=? AND revision=?", v.Revision, mustJSON(v), v.ID, expected)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}
func (t *Tx) AcceptMenuInteraction(raw work.MenuInteractionEnvelope, gateway work.IngressGateway) (v MenuInteraction, replay bool, err error) {
	err = t.atomic(func() error {
		if gateway.Validate() != nil || raw.GatewayRef != gateway.GatewayRef || raw.AppID != gateway.AppID || raw.EventID == "" || raw.Tenant == "" || raw.EventKey == "" || raw.ReceivedAt.IsZero() {
			return fmt.Errorf("menu requires actual SDK identity and event")
		}
		binding, e := t.GetRoleBotBinding(gateway.RoleID)
		if e != nil {
			return e
		}
		if binding.GatewayRef != gateway.GatewayRef || binding.AppID != gateway.AppID || binding.IdentityGeneration != gateway.BotGeneration {
			return ErrConflict
		}
		canonical := raw
		canonical.ReceivedAt = time.Time{}
		hashBytes := sha256.Sum256([]byte(mustJSON(struct {
			Envelope work.MenuInteractionEnvelope
			Gateway  work.IngressGateway
		}{canonical, gateway})))
		hash := hex.EncodeToString(hashBytes[:])
		key := sha256.Sum256([]byte(mustJSON([]any{raw.AppID, gateway.BotGeneration, raw.EventID})))
		id := "menu:" + hex.EncodeToString(key[:])
		var prior string
		e = t.tx.QueryRowContext(t.ctx, "SELECT payload_hash FROM menu_interactions WHERE id=?", id).Scan(&prior)
		if e == nil {
			if prior != hash {
				return ErrIdempotencyConflict
			}
			v, e = t.GetMenuInteraction(id)
			replay = true
			return e
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		principal, e := t.resolveMenuPrincipal(raw, gateway, id)
		if e != nil {
			return e
		}
		scope, e := t.FindPersonalScope(principal.ID, gateway.RoleID)
		if errors.Is(e, ErrNotFound) {
			sum := sha256.Sum256([]byte(mustJSON([]string{principal.ID, gateway.RoleID})))
			scope = work.WorkScope{ID: "personal-menu:" + hex.EncodeToString(sum[:]), Kind: work.ScopePersonal, PrincipalID: principal.ID, RoleID: gateway.RoleID, Revision: 1}
			if e = t.PutScope(scope, 0); e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
		nav, e := t.GetPrivateNavigation(scope.ID)
		if errors.Is(e, ErrNotFound) {
			context := work.WorkContext{ID: "private-front:" + scope.ID, ScopeID: scope.ID, RoleID: scope.RoleID, Workstream: "personal-default", Revision: 1, CreatedAt: raw.ReceivedAt, UpdatedAt: raw.ReceivedAt}
			if e = t.PutContext(context, 0); e != nil {
				return e
			}
			nav = work.PrivateNavigation{PersonalScopeID: scope.ID, PrincipalID: principal.ID, RoleID: scope.RoleID, Mode: work.PrivatePersonal, FrontContextID: context.ID, Revision: 1, Source: work.SourceRef{Kind: "sdk_menu", ID: id, Version: "1", EvidenceRef: id}, UpdatedAt: raw.ReceivedAt}
			if e = t.putPrivateNavigation(nav, 0); e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
		v = MenuInteraction{ID: id, PrincipalID: principal.ID, ScopeID: scope.ID, ContextID: nav.FrontContextID, NavigationRevision: nav.Revision, Gateway: gateway, Envelope: raw, State: "pending", UUID: hex.EncodeToString(key[:20]), Revision: 1}
		_, e = t.tx.ExecContext(t.ctx, "INSERT INTO menu_interactions(id,revision,payload_hash,payload) VALUES(?,?,?,?)", v.ID, v.Revision, hash, mustJSON(v))
		return e
	})
	return
}
func (t *Tx) resolveMenuPrincipal(raw work.MenuInteractionEnvelope, gateway work.IngressGateway, evidence string) (work.Principal, error) {
	aliases := identityAliases(work.IdentityEvidence{Tenant: raw.Tenant, AppID: raw.AppID, Generation: gateway.BotGeneration, UnionDomain: gateway.UnionDomain, IDs: raw.SenderIDs})
	if len(aliases) == 0 {
		return work.Principal{}, fmt.Errorf("menu sender lacks stable typed SDK identity")
	}
	owner := ""
	for _, alias := range aliases {
		current, e := t.GetExternalIdentity(alias)
		if e == nil {
			if owner != "" && owner != current.PrincipalID {
				return work.Principal{}, ErrIdentityConflict
			}
			owner = current.PrincipalID
		} else if !errors.Is(e, ErrNotFound) {
			return work.Principal{}, e
		}
	}
	if owner == "" {
		sum := sha256.Sum256([]byte(mustJSON(aliases[0])))
		owner = "principal:" + hex.EncodeToString(sum[:])
		if e := t.PutPrincipal(work.Principal{ID: owner, DisplayName: aliases[0].ExternalID, Revision: 1}, 0); e != nil {
			return work.Principal{}, e
		}
	}
	for _, alias := range aliases {
		if _, e := t.tx.ExecContext(t.ctx, "INSERT INTO external_identities(tenant,domain,id_type,external_id,generation,principal_id,evidence_ref) VALUES(?,?,?,?,?,?,?) ON CONFLICT DO NOTHING", alias.Tenant, alias.AppIdentityDomain, alias.IDType, alias.ExternalID, alias.Generation, owner, evidence); e != nil {
			return work.Principal{}, e
		}
	}
	return t.GetPrincipal(owner)
}
