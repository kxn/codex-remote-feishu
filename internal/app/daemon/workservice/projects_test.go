package workservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestProjectReservationIsScopedIdempotentAndDoesNotPretendRemoteReady(t *testing.T) {
	s, _, _ := runningService(t)
	ctx := context.Background()
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := tx.PutTeam(work.Team{ID: "team", Name: "Team", LeaderRoleID: "leader", Revision: 1}, 0); err != nil {
			return err
		}
		return tx.PutTeamMember(work.TeamMember{TeamID: "team", RoleID: "leader", Active: true, Revision: 1}, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.signer.Issue(workauth.Claims{Purpose: "bootstrap", RoleID: "leader", BotGeneration: 1, Tenant: "tenant", ChatID: "chat", PrincipalID: "human", OperationID: "create"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req := CreateProjectRequest{Key: "create", Tenant: "tenant", ChatID: "chat", PrincipalID: "human", Name: "Search", TeamID: "team", LeaderRoleID: "leader", InitialRequest: "Design and build search"}
	first, err := s.CreateProject(ctx, token, req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "accepted" {
		t.Fatalf("remote provisioning falsely complete: %+v", first)
	}
	replay, err := s.CreateProject(ctx, token, req)
	if err != nil || !replay.Replay || replay.ObjectID != first.ObjectID {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	req.Name = "Changed"
	if _, err = s.CreateProject(ctx, token, req); !errors.Is(err, workstore.ErrIdempotencyConflict) {
		t.Fatalf("changed body got %v", err)
	}
	req.ChatID = "other-chat"
	if _, err = s.CreateProject(ctx, token, req); !errors.Is(err, workauth.ErrUnauthorized) {
		t.Fatalf("cross-chat got %v", err)
	}
	err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		project, err := tx.GetChannelProject("tenant", "chat")
		if err != nil {
			return err
		}
		if project.ID != first.ObjectID || project.State != work.ProjectProvisioning {
			t.Fatalf("project=%+v", project)
		}
		member, err := tx.GetProjectMember(project.ID, "leader")
		if err != nil {
			return err
		}
		if member.State != work.ProjectMemberInvited {
			t.Fatalf("membership fabricated: %+v", member)
		}
		op, err := tx.GetOperation(first.OperationID)
		if err != nil {
			return err
		}
		if op.RequestRef == "" || len(op.Steps) != 5 {
			t.Fatalf("missing durable saga: %+v", op)
		}
		_, err = tx.GetContent(project.ScopeID, op.RequestRef)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
