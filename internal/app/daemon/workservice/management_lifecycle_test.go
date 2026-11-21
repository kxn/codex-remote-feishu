package workservice

import (
	"context"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type managementPolicyFunc func(context.Context, string, string, string, work.SourceRef) error

func (f managementPolicyFunc) AuthorizeManagement(c context.Context, p, k, t string, s work.SourceRef) error {
	return f(c, p, k, t, s)
}
func TestRoleGlobalDescriptionRequiresRealPrivateSourceAndTrustedPolicy(t *testing.T) {
	s, token, run := knowledgeService(t)
	req := RoleDescriptionRequest{Key: "global", RoleID: "leader", Scope: "global", ExpectedRevision: 1, Description: "New explicit responsibilities", SourceMessageID: "human-input"}
	if _, err := s.RoleUpdateDescription(context.Background(), token, run, req); err == nil {
		t.Fatal("nil trusted policy authorized global role change")
	}
	calls := 0
	s = s.WithManagementAuthorizer(managementPolicyFunc(func(ctx context.Context, principal, kind, target string, source work.SourceRef) error {
		calls++
		if principal != "human" || kind != "role.global-description" || target != "leader" || source.ID != "human-input" {
			t.Fatalf("untrusted policy evidence: %s %s %s %#v", principal, kind, target, source)
		}
		bounded, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		return s.store.Within(bounded, func(tx *workstore.Tx) error { _, err := tx.GetRole("leader"); return err })
	}))
	changed, err := s.RoleUpdateDescription(context.Background(), token, run, req)
	if err != nil || changed.ObjectID != "leader" {
		t.Fatalf("change %#v %v", changed, err)
	}
	if calls != 1 {
		t.Fatal("trusted policy was not called")
	}
	if err = s.store.Within(context.Background(), func(tx *workstore.Tx) error {
		role, err := tx.GetRole("leader")
		if err != nil {
			return err
		}
		if role.DescriptionVersion != 2 || role.SettingsRevision != 1 || role.Description != req.Description {
			t.Fatalf("role/model boundary %#v", role)
		}
		op, err := tx.GetOperation(changed.OperationID)
		if err != nil {
			return err
		}
		if op.RequestRef == "" {
			t.Fatal("management source was not durably recorded")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	req.Key = "fake-source"
	req.ExpectedRevision = 2
	req.SourceMessageID = "not-admitted"
	if _, err = s.RoleUpdateDescription(context.Background(), token, run, req); err == nil {
		t.Fatal("fake human source accepted")
	}
	if calls != 1 {
		t.Fatal("fake source reached management authorizer")
	}
}
func TestProjectResponsibilityDoesNotPolluteGlobalRole(t *testing.T) {
	s, token, run := lifecycleProjectService(t, false)
	result, err := s.RoleUpdateDescription(context.Background(), token, run, RoleDescriptionRequest{Key: "local", RoleID: "expert", Scope: "project", ExpectedRevision: 0, Description: "Review this project's accessibility", SourceMessageID: "lifecycle-human"})
	if err != nil || result.Revision != 1 {
		t.Fatalf("local change %#v %v", result, err)
	}
	view, err := s.RoleInspect(context.Background(), token, run, RoleInspectRequest{RoleID: "expert"})
	if err != nil || view.ProjectOverride == nil || view.Role.Description != "Review" || view.EffectiveDescription != "Review this project's accessibility" {
		t.Fatalf("project override %#v %v", view, err)
	}
	if _, err = s.RoleUpdateDescription(context.Background(), token, run, RoleDescriptionRequest{Key: "global-from-group", RoleID: "expert", Scope: "global", ExpectedRevision: 1, Description: "Leaked group role", SourceMessageID: "lifecycle-human"}); err == nil {
		t.Fatal("group instruction became global management authority")
	}
}
func TestBootstrapCanCreateOwnedTeamWhenNoTeamExists(t *testing.T) {
	s, token, run := lifecycleProjectService(t, true)
	teams, err := s.TeamList(context.Background(), token, run, KnowledgeListRequest{})
	if err != nil || len(teams.Items) != 0 {
		t.Fatalf("bootstrap teams %#v %v", teams, err)
	}
	roles, err := s.RoleList(context.Background(), token, run, KnowledgeListRequest{})
	if err != nil || len(roles.Items) != 2 {
		t.Fatalf("discover configured roles %#v %v", roles, err)
	}
	req := TeamCreateRequest{Key: "new-team", Name: "Design", LeaderRoleID: "leader", MemberRoleIDs: []string{"expert"}, SourceMessageID: "lifecycle-human"}
	created, err := s.TeamCreate(context.Background(), token, run, req)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.TeamCreate(context.Background(), token, run, req)
	if err != nil || !replay.Replay || replay.ObjectID != created.ObjectID {
		t.Fatalf("team replay %#v %v", replay, err)
	}
	if err = s.store.Within(context.Background(), func(tx *workstore.Tx) error {
		owner, err := tx.GetTeamOwnership(created.ObjectID)
		if err != nil {
			return err
		}
		if owner.PrincipalID != "human" || owner.Source.ID != "lifecycle-human" {
			t.Fatalf("forged team owner %#v", owner)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.TeamUpdate(context.Background(), token, run, TeamUpdateRequest{Key: "rename", TeamID: created.ObjectID, ExpectedRevision: 1, Name: "Design team", LeaderRoleID: "expert", SourceMessageID: "lifecycle-human"}); err != nil {
		t.Fatalf("actual owner blocked without global policy: %v", err)
	}
}
