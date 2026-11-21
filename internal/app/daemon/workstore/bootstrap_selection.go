package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"slices"
	"strconv"
)

type BootstrapLeaderSelection struct {
	Tenant            string `json:"tenant"`
	ChatID            string `json:"chat_id"`
	BindingGeneration int64  `json:"binding_generation"`
	RoleID            string `json:"role_id"`
	PrincipalID       string `json:"principal_id"`
	IngressID         string `json:"ingress_id"`
	InteractionID     string `json:"interaction_id"`
}
type SelectBootstrapLeaderRequest struct{ IngressID, RoleID, InteractionID string }

func (t *Tx) GetBootstrapLeaderSelection(tenant, chat string, generation int64) (BootstrapLeaderSelection, error) {
	var result BootstrapLeaderSelection
	var payload string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM bootstrap_leader_selections WHERE tenant=? AND chat_id=? AND binding_generation=?", tenant, chat, generation).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal([]byte(payload), &result)
	}
	return result, err
}
func (t *Tx) SelectBootstrapLeader(req SelectBootstrapLeaderRequest) (result BootstrapLeaderSelection, err error) {
	err = t.atomic(func() error {
		raw, e := t.GetIngress(req.IngressID)
		if e != nil {
			return e
		}
		if raw.Route.Kind != work.ScopeBootstrap || raw.Envelope.SenderType != "user" || raw.Envelope.ChatType != "group" {
			return ErrScopeDenied
		}
		first, e := t.FindFirstEstablishmentIngress(raw.Envelope.Tenant, raw.Envelope.ChatID, raw.Route.BindingGeneration)
		if e != nil {
			return e
		}
		principal, _, e := t.ResolvePrincipal(work.IdentityEvidence{Tenant: first.Envelope.Tenant, AppID: first.Envelope.AppID, Generation: first.Gateway.BotGeneration, UnionDomain: first.Gateway.UnionDomain, IDs: first.Envelope.SenderIDs, ObservationRef: first.ID})
		if e != nil {
			return e
		}
		action, e := t.GetHumanInteraction(req.InteractionID)
		if e != nil {
			return e
		}
		if action.PrincipalID != principal.ID || action.Envelope.Tenant != raw.Envelope.Tenant || action.Envelope.ChatID != raw.Envelope.ChatID || action.Envelope.Values["ingress_id"] != raw.ID || action.Envelope.Values["role_id"] != req.RoleID || action.Envelope.Values["binding_generation"] != strconv.FormatInt(raw.Route.BindingGeneration, 10) {
			return ErrScopeDenied
		}

		notice, e := t.GetSelectionNotice(raw.ID)
		if e != nil {
			return e
		}
		if notice.Gateway != action.Gateway || notice.Tenant != action.Envelope.Tenant || notice.ChatID != action.Envelope.ChatID || (notice.ExternalMessageID != "" && notice.ExternalMessageID != action.Envelope.MessageID) || !slices.Contains(notice.CandidateRoleIDs, req.RoleID) {
			return ErrScopeDenied
		}
		if notice.State != work.OperationConfirmed && notice.State != work.OperationUnknown && notice.State != work.OperationSending {
			return ErrScopeDenied
		}
		if action.Envelope.Values["team_setup_ingress"] != raw.ID {
			return ErrScopeDenied
		}
		if notice.State != work.OperationConfirmed {
			previous := notice.Revision
			notice.Revision++
			notice.State = work.OperationConfirmed
			notice.ExternalMessageID = action.Envelope.MessageID
			notice.UpdatedAt = action.Envelope.ReceivedAt
			changed, e := t.tx.ExecContext(t.ctx, "UPDATE ingress_selection_notices SET revision=?,payload=? WHERE ingress_id=? AND revision=?", notice.Revision, mustJSON(notice), raw.ID, previous)
			if e != nil {
				return e
			}
			count, e := changed.RowsAffected()
			if e != nil {
				return e
			}
			if count != 1 {
				return ErrConflict
			}
		}
		binding, e := t.GetRoleBotBinding(req.RoleID)
		if e != nil {
			return e
		}
		if binding.Validate() != nil {
			return ErrScopeDenied
		}
		if current, e := t.GetProjectChannelBinding(raw.Envelope.Tenant, raw.Envelope.ChatID); e == nil {
			if current.Active || current.BindingGeneration != raw.Route.BindingGeneration {
				return ErrConflict
			}
		} else if !errors.Is(e, ErrNotFound) {
			return e
		} else if raw.Route.BindingGeneration != 0 {
			return ErrConflict
		}
		if scope, e := t.FindBootstrapScope(raw.Envelope.Tenant, raw.Envelope.ChatID, raw.Route.BindingGeneration); e == nil && scope.RoleID != req.RoleID {
			return ErrConflict
		} else if e != nil && !errors.Is(e, ErrNotFound) {
			return e
		}
		result = BootstrapLeaderSelection{Tenant: raw.Envelope.Tenant, ChatID: raw.Envelope.ChatID, BindingGeneration: raw.Route.BindingGeneration, RoleID: req.RoleID, PrincipalID: principal.ID, IngressID: first.ID, InteractionID: action.ID}
		prior, e := t.GetBootstrapLeaderSelection(result.Tenant, result.ChatID, result.BindingGeneration)
		if e == nil {
			if prior.RoleID != result.RoleID || prior.PrincipalID != result.PrincipalID {
				return ErrConflict
			}
			result = prior
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		_, e = t.tx.ExecContext(t.ctx, "INSERT INTO bootstrap_leader_selections(tenant,chat_id,binding_generation,payload) VALUES(?,?,?,?)", result.Tenant, result.ChatID, result.BindingGeneration, mustJSON(result))
		if e != nil {
			return e
		}
		return t.WakeEstablishmentIngress(result.Tenant, result.ChatID, result.BindingGeneration, "")
	})
	return
}
