package workservice

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

func TestReviewCreateProjectCannotSilentlyReplaceActiveMainBinding(t *testing.T) {
	service, _, _ := runningService(t)
	ctx := t.Context()
	if err := service.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := tx.PutTeam(work.Team{ID: "review-team", Name: "Review team", LeaderRoleID: "leader", Revision: 1}, 0); err != nil {
			return err
		}
		return tx.PutTeamMember(work.TeamMember{TeamID: "review-team", RoleID: "leader", Active: true, Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	create := func(key, name string, generation int64) (Result, error) {
		token, err := service.signer.Issue(workauth.Claims{Purpose: "bootstrap", RoleID: "leader", BotGeneration: 1, Tenant: "tenant", ChatID: "review-chat", PrincipalID: "human", OperationID: key}, time.Hour)
		if err != nil {
			return Result{}, err
		}
		return service.CreateProject(ctx, token, CreateProjectRequest{Key: key, Tenant: "tenant", ChatID: "review-chat", PrincipalID: "human", ExpectedBindingGeneration: generation, Name: name, TeamID: "review-team", LeaderRoleID: "leader", InitialRequest: "Create this project"})
	}
	first, err := create("first-establishment", "Existing project", 0)
	if err != nil {
		t.Fatal(err)
	}
	// An ordinary create authorization has no separate target/impact decision for
	// replacing another current project's main channel.
	if _, err = create("second-establishment", "Unconfirmed replacement", 1); err == nil {
		t.Error("ordinary bootstrap create silently replaced an active project's main binding")
	}
	if err = service.store.Within(ctx, func(tx *workstore.Tx) error {
		current, err := tx.GetChannelProject("tenant", "review-chat")
		if err != nil {
			return err
		}
		if current.ID != first.ObjectID {
			t.Error("failed/unconfirmed creation changed current project")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestReviewFailedProjectBindingCASRollsBackCreatedScopeAndProject(t *testing.T) {
	service, _, _ := runningService(t)
	ctx := t.Context()
	if err := service.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := tx.PutTeam(work.Team{ID: "rollback-team", Name: "Rollback team", LeaderRoleID: "leader", Revision: 1}, 0); err != nil {
			return err
		}
		return tx.PutTeamMember(work.TeamMember{TeamID: "rollback-team", RoleID: "leader", Active: true, Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	token, err := service.signer.Issue(workauth.Claims{Purpose: "bootstrap", RoleID: "leader", BotGeneration: 1, Tenant: "tenant", ChatID: "unbound-chat", PrincipalID: "human", OperationID: "bad-binding"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CreateProject(ctx, token, CreateProjectRequest{Key: "bad-binding", Tenant: "tenant", ChatID: "unbound-chat", PrincipalID: "human", ExpectedBindingGeneration: 99, Name: "Must roll back", TeamID: "rollback-team", LeaderRoleID: "leader", InitialRequest: "Establish"})
	if err == nil {
		t.Fatal("missing binding accepted a stale expected generation")
	}
	if err = service.store.Within(ctx, func(tx *workstore.Tx) error {
		scopes, err := tx.ListScopes()
		if err != nil {
			return err
		}
		projects, err := tx.ListProjects()
		if err != nil {
			return err
		}
		if len(scopes) != 1 || len(projects) != 0 {
			t.Fatalf("partial creation escaped rollback scopes=%d projects=%d", len(scopes), len(projects))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
