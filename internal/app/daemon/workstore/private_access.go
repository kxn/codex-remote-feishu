package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strconv"
	"time"
)

func (t *Tx) GetProjectPrincipalAccess(scopeID, principalID string) (work.ProjectPrincipalAccess, error) {
	var v work.ProjectPrincipalAccess
	var data string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM project_principal_access WHERE scope_id=? AND principal_id=?", scopeID, principalID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(data), &v)
	return v, err
}
func (t *Tx) putProjectPrincipalAccess(v work.ProjectPrincipalAccess, expected int64) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if v.Revision != expected+1 {
		return ErrConflict
	}
	var result sql.Result
	var err error
	if expected == 0 {
		result, err = t.tx.ExecContext(t.ctx, "INSERT INTO project_principal_access(scope_id,principal_id,revision,payload) VALUES(?,?,?,?) ON CONFLICT DO NOTHING", v.ScopeID, v.PrincipalID, v.Revision, mustJSON(v))
	} else {
		result, err = t.tx.ExecContext(t.ctx, "UPDATE project_principal_access SET revision=?,payload=? WHERE scope_id=? AND principal_id=? AND revision=?", v.Revision, mustJSON(v), v.ScopeID, v.PrincipalID, expected)
	}
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrConflict
	}
	return nil
}

// GrantProjectAccessFromIngress uses an actual observed group membership fact,
// never a boolean supplied by a UI/model. Binding changes invalidate this grant.
func (t *Tx) GrantProjectAccessFromIngress(scopeID, principalID, ingressID string, expiresAt time.Time) error {
	record, err := t.GetIngress(ingressID)
	if err != nil {
		return err
	}
	if record.Envelope.SenderType != "user" || record.Envelope.ChatType != "group" || record.Route.Kind != work.ScopeProject || record.Route.ScopeID != scopeID {
		return ErrScopeDenied
	}
	observedAt := record.Envelope.MembershipObservedAt()
	if observedAt.After(record.Envelope.ReceivedAt.Add(5 * time.Minute)) {
		return ErrScopeDenied
	}
	if !expiresAt.After(observedAt) || expiresAt.Sub(observedAt) > 24*time.Hour {
		return fmt.Errorf("membership validity exceeds SDK observation limit")
	}
	principal, _, err := t.ResolvePrincipal(work.IdentityEvidence{Tenant: record.Envelope.Tenant, AppID: record.Envelope.AppID, Generation: record.Gateway.BotGeneration, UnionDomain: record.Gateway.UnionDomain, IDs: record.Envelope.SenderIDs, ObservationRef: record.ID})
	if err != nil {
		return err
	}
	if principal.ID != principalID {
		return ErrScopeDenied
	}
	scope, err := t.GetScope(scopeID)
	if err != nil {
		return err
	}
	binding, err := t.GetProjectChannelBinding(record.Envelope.Tenant, record.Envelope.ChatID)
	if err != nil {
		return err
	}
	if !binding.Active || binding.ProjectID != scope.ProjectID || binding.BindingGeneration != record.Route.BindingGeneration {
		return ErrScopeDenied
	}
	old, err := t.GetProjectPrincipalAccess(scopeID, principalID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if err == nil && !observedAt.After(old.VerifiedAt) {
		return nil
	}
	v := work.ProjectPrincipalAccess{Tenant: record.Envelope.Tenant, ChatID: record.Envelope.ChatID, ScopeID: scopeID, PrincipalID: principalID, State: work.ProjectAccessActive, BindingGeneration: binding.BindingGeneration, Source: work.SourceRef{Kind: "sdk_group_message", ID: record.ID, Version: strconv.FormatInt(binding.BindingGeneration, 10), EvidenceRef: record.ID}, VerifiedAt: observedAt, ExpiresAt: expiresAt, Revision: old.Revision + 1}
	return t.putProjectPrincipalAccess(v, old.Revision)
}
func (t *Tx) RequireProjectPrincipalAccess(scopeID, principalID string, now time.Time) error {
	access, err := t.GetProjectPrincipalAccess(scopeID, principalID)
	if err != nil {
		return err
	}
	if access.State != work.ProjectAccessActive || now.Before(access.VerifiedAt) || !now.Before(access.ExpiresAt) {
		return ErrScopeDenied
	}
	scope, err := t.GetScope(scopeID)
	if err != nil {
		return err
	}
	if scope.Kind != work.ScopeProject {
		return ErrScopeDenied
	}
	channels, err := t.ListProjectChannelBindings(scope.ProjectID)
	if err != nil {
		return err
	}
	for _, channel := range channels {
		if channel.Tenant == access.Tenant && channel.ChatID == access.ChatID && channel.Active && channel.Purpose == "main" && channel.BindingGeneration == access.BindingGeneration {
			return nil
		}
	}
	return ErrScopeDenied
}

// MembershipQuery is the exact request frozen by the trusted SDK verifier.
type MembershipQuery struct {
	ScopeID           string             `json:"scope_id"`
	PrincipalID       string             `json:"principal_id"`
	Tenant            string             `json:"tenant"`
	ChatID            string             `json:"chat_id"`
	BindingGeneration int64              `json:"binding_generation"`
	QueriedIdentity   work.IdentityScope `json:"queried_identity"`
}

func (t *Tx) RecordMembershipCheck(check work.MembershipCheck) error {
	return t.atomic(func() error {
		if err := check.Validate(); err != nil {
			return err
		}
		scope, err := t.GetScope(check.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind != work.ScopeProject {
			return ErrScopeDenied
		}
		binding, err := t.GetProjectChannelBinding(check.Tenant, check.ChatID)
		if err != nil {
			return err
		}
		if !binding.Active || binding.ProjectID != scope.ProjectID || binding.BindingGeneration != check.BindingGeneration {
			return ErrScopeDenied
		}
		gateway, err := t.GetRoleBotBinding(check.Gateway.RoleID)
		if err != nil {
			return err
		}
		if gateway.GatewayRef != check.Gateway.GatewayRef || gateway.AppID != check.Gateway.AppID || gateway.IdentityGeneration != check.Gateway.BotGeneration {
			return ErrScopeDenied
		}
		identity, err := t.GetExternalIdentity(check.QueriedIdentity)
		if err != nil {
			return err
		}
		if identity.PrincipalID != check.PrincipalID || identity.Tenant != check.Tenant {
			return ErrScopeDenied
		}
		operation, err := t.GetOperation(check.OperationID)
		if err != nil {
			return err
		}
		if operation.Kind != "feishu.membership.query" || operation.Actor != (work.Actor{Kind: work.ActorSystem, ID: "membership-verifier"}) || operation.ScopeID != check.ScopeID || operation.TargetID != check.ChatID || operation.TargetGeneration != check.BindingGeneration || operation.State != work.OperationConfirmed || operation.ExternalReceipt != check.ResponseRef {
			return fmt.Errorf("membership source lacks confirmed trusted SDK query")
		}
		request, err := t.GetContent(check.ScopeID, operation.RequestRef)
		if err != nil {
			return err
		}
		if request.SHA256 != operation.RequestHash {
			return ErrIdempotencyConflict
		}
		var frozen MembershipQuery
		if err = json.Unmarshal(request.Data, &frozen); err != nil {
			return err
		}
		if frozen != (MembershipQuery{ScopeID: check.ScopeID, PrincipalID: check.PrincipalID, Tenant: check.Tenant, ChatID: check.ChatID, BindingGeneration: check.BindingGeneration, QueriedIdentity: check.QueriedIdentity}) {
			return ErrScopeDenied
		}
		if _, err = t.GetContent(check.ScopeID, check.ResponseRef); err != nil {
			return err
		}
		if _, err = t.tx.ExecContext(t.ctx, "INSERT INTO membership_checks(id,payload) VALUES(?,?)", check.ID, mustJSON(check)); err != nil {
			return err
		}
		old, err := t.GetProjectPrincipalAccess(check.ScopeID, check.PrincipalID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if err == nil && check.ObservedAt.Before(old.VerifiedAt) {
			return fmt.Errorf("stale membership observation")
		}
		state := work.ProjectAccessActive
		if check.Status == work.MembershipAbsent {
			state = work.ProjectAccessRevoked
		}
		access := work.ProjectPrincipalAccess{Tenant: check.Tenant, ChatID: check.ChatID, ScopeID: check.ScopeID, PrincipalID: check.PrincipalID, State: state, BindingGeneration: check.BindingGeneration, Source: work.SourceRef{Kind: "sdk_membership_check", ID: check.ID, Version: strconv.FormatInt(check.BindingGeneration, 10), EvidenceRef: check.ResponseRef}, VerifiedAt: check.ObservedAt, ExpiresAt: check.ExpiresAt, Revision: old.Revision + 1}
		return t.putProjectPrincipalAccess(access, old.Revision)
	})
}
