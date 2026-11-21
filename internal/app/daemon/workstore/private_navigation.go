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

func (t *Tx) GetPrivateNavigation(scopeID string) (work.PrivateNavigation, error) {
	var v work.PrivateNavigation
	var data string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM private_navigation WHERE scope_id=?", scopeID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(data), &v)
	return v, err
}
func (t *Tx) putPrivateNavigation(v work.PrivateNavigation, expected int64) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if v.Revision != expected+1 {
		return ErrConflict
	}
	scope, err := t.GetScope(v.PersonalScopeID)
	if err != nil {
		return err
	}
	if scope.Kind != work.ScopePersonal || scope.PrincipalID != v.PrincipalID || scope.RoleID != v.RoleID {
		return ErrScopeDenied
	}
	front, err := t.GetContext(v.FrontContextID)
	if err != nil {
		return err
	}
	if front.Purpose == work.PurposeReview || front.Purpose == work.PurposeControl || front.ScopeID != scope.ID || front.RoleID != scope.RoleID || front.ProjectViewScopeID != v.ProjectScopeID || front.ProjectContextID != v.ProjectContextID {
		return fmt.Errorf("navigation front owner/view mismatch")
	}
	if v.Mode == work.PrivateProject {
		if err = t.RequireProjectPrincipalAccess(v.ProjectScopeID, v.PrincipalID, v.UpdatedAt); err != nil {
			return err
		}
		context, e := t.GetContext(v.ProjectContextID)
		if e != nil {
			return e
		}
		if context.ScopeID != v.ProjectScopeID || context.RoleID != v.RoleID {
			return ErrScopeDenied
		}
	}
	var result sql.Result
	if expected == 0 {
		result, err = t.tx.ExecContext(t.ctx, "INSERT INTO private_navigation(scope_id,revision,payload) VALUES(?,?,?) ON CONFLICT DO NOTHING", scope.ID, v.Revision, mustJSON(v))
	} else {
		result, err = t.tx.ExecContext(t.ctx, "UPDATE private_navigation SET revision=?,payload=? WHERE scope_id=? AND revision=?", v.Revision, mustJSON(v), scope.ID, expected)
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

// EnsurePrivateNavigation is the ingress-only default. It has no project grant
// and never moves a previous context or a running worker to another scope.
func (t *Tx) EnsurePrivateNavigation(scopeID, ingressID string, now time.Time) (work.PrivateNavigation, error) {
	existing, err := t.GetPrivateNavigation(scopeID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return existing, err
	}
	scope, err := t.GetScope(scopeID)
	if err != nil {
		return existing, err
	}
	record, err := t.GetIngress(ingressID)
	if err != nil {
		return existing, err
	}
	if scope.Kind != work.ScopePersonal || record.Envelope.ChatType != "p2p" || record.Envelope.SenderType != "user" || record.Gateway.RoleID != scope.RoleID {
		return existing, ErrScopeDenied
	}
	principal, _, err := t.ResolvePrincipal(work.IdentityEvidence{Tenant: record.Envelope.Tenant, AppID: record.Envelope.AppID, Generation: record.Gateway.BotGeneration, UnionDomain: record.Gateway.UnionDomain, IDs: record.Envelope.SenderIDs, ObservationRef: record.ID})
	if err != nil {
		return existing, err
	}
	if principal.ID != scope.PrincipalID {
		return existing, ErrScopeDenied
	}
	context := work.WorkContext{ID: "private-front:" + scope.ID, ScopeID: scope.ID, RoleID: scope.RoleID, Workstream: "personal-default", Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err = t.PutContext(context, 0); err != nil {
		return existing, err
	}
	navigation := work.PrivateNavigation{PersonalScopeID: scope.ID, PrincipalID: scope.PrincipalID, RoleID: scope.RoleID, Mode: work.PrivatePersonal, FrontContextID: context.ID, Revision: 1, Source: work.SourceRef{Kind: "sdk_private_ingress", ID: record.ID, Version: strconv.FormatInt(record.Revision, 10), EvidenceRef: record.ID}, UpdatedAt: now}
	err = t.putPrivateNavigation(navigation, 0)
	return navigation, err
}

type ChangePrivateNavigationRequest struct {
	Source           PrivateActionSource
	ExpectedRevision int64
	Mode             work.PrivateNavigationMode
	ProjectScopeID   string
	FrontContextID   string
	ProjectContextID string
	Now              time.Time
}

func (t *Tx) ChangePrivateNavigation(input ChangePrivateNavigationRequest) (navigation work.PrivateNavigation, err error) {
	err = t.atomic(func() error {
		scope, source, e := t.ValidatePrivateAction(input.Source)
		if e != nil {
			return e
		}
		old, e := t.GetPrivateNavigation(scope.ID)
		if e != nil {
			return e
		}
		if old.Revision != input.ExpectedRevision {
			return ErrConflict
		}
		navigation = work.PrivateNavigation{PersonalScopeID: scope.ID, PrincipalID: scope.PrincipalID, RoleID: scope.RoleID, Mode: input.Mode, ProjectScopeID: input.ProjectScopeID, FrontContextID: input.FrontContextID, ProjectContextID: input.ProjectContextID, Revision: old.Revision + 1, Source: source, UpdatedAt: input.Now}
		return t.putPrivateNavigation(navigation, old.Revision)
	})
	return
}

// FreezePrivateIngressNavigation runs in the receive transaction before ACK.
// It is the sole allowed initialization of a raw personal routing snapshot.
func (t *Tx) FreezePrivateIngressNavigation(id string, navigation work.PrivateNavigation) error {
	record, err := t.GetIngress(id)
	if err != nil {
		return err
	}
	if record.State != work.IngressPending || record.Route.Kind != work.ScopePersonal || record.Route.ScopeID != "" {
		return ErrConflict
	}
	if record.Gateway.RoleID != navigation.RoleID {
		return ErrScopeDenied
	}
	navigation, record.Route.ReplyRoutingError, err = t.privateReplyNavigation(record, navigation)
	if err != nil {
		return err
	}
	record.Route.ScopeID = navigation.PersonalScopeID
	record.Route.ContextID = navigation.FrontContextID
	record.Route.NavigationRevision = navigation.Revision
	record.Route.ProjectViewScopeID = navigation.ProjectScopeID
	record.Route.ProjectContextID = navigation.ProjectContextID
	expected := record.Revision
	record.Revision++
	return t.record("raw_ingress", record.ID, record.Revision, expected, record)
}
