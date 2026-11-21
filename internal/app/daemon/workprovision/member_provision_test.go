package workprovision

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func queueNewMember(t *testing.T, f *provisionFixture) string {
	t.Helper()
	id := "new-member-operation"
	binding := work.RoleBotBinding{RoleID: "new-role", GatewayRef: "new-gateway", AppID: "new-app", IdentityGeneration: 1}
	f.world.platforms[binding.AppID] = &fakePlatform{world: f.world, binding: binding, messages: map[string]string{}, pins: map[string]bool{}}
	if err := f.store.Within(context.Background(), func(tx *workstore.Tx) error {
		if err := tx.PutRole(work.Role{ID: binding.RoleID, Name: "New role", Description: "Support", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutRoleBotBinding(binding, 0); err != nil {
			return err
		}
		project, err := tx.GetProject(f.projectID)
		if err != nil {
			return err
		}
		if err = tx.PutTeamMember(work.TeamMember{TeamID: project.TeamID, RoleID: binding.RoleID, Active: true, Revision: 1}, 0); err != nil {
			return err
		}
		if err = tx.PutProjectMember(work.ProjectMember{ProjectID: project.ID, RoleID: binding.RoleID, State: work.ProjectMemberInvited, BotGeneration: 1, Revision: 1}, 0); err != nil {
			return err
		}
		request := workservice.ProjectMemberProvisionIntent{ProjectID: project.ID, Role: binding, Inviter: f.leader.binding, MemberRevision: 1, Tenant: "tenant", ChatID: "project-chat", ChannelGeneration: 1, SourceMessageID: f.sourceID}
		raw, err := json.Marshal(request)
		if err != nil {
			return err
		}
		body := work.NewContent("new-member-request", project.ScopeID, "application/json", raw, f.now)
		if err = tx.PutContent(body); err != nil {
			return err
		}
		_, _, err = tx.PutOperation(work.Operation{ID: id, ScopeID: project.ScopeID, Actor: work.Actor{Kind: work.ActorRole, ID: "leader"}, Kind: "project.member.provision", TargetID: project.ID, TargetGeneration: 1, IdempotencyKey: id, RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationPrepared, Revision: 1, Steps: []work.OperationStep{{ID: "members", State: work.OperationPrepared}, {ID: "execution", State: work.OperationPrepared}}}, 0)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return id
}
func TestNewMemberUsesOwnSagaWithoutRecreatingProject(t *testing.T) {
	f := newFixture(t)
	if _, err := f.service.Run(context.Background(), f.operationID); err != nil {
		t.Fatal(err)
	}
	id := queueNewMember(t, f)
	baselineInvites := len(f.world.inviteCalls)
	baselinePrepare := f.prepared
	report, err := f.service.Run(context.Background(), id)
	if err != nil || report.State != string(work.OperationConfirmed) {
		t.Fatalf("member report %#v %v", report, err)
	}
	if f.leader.documentCalls != 1 || f.leader.folderCalls != 1 || f.leader.tabCalls != 1 || f.leader.publishCalls != 1 || f.prepared != baselinePrepare+1 {
		t.Fatal("new member repeated whole project establishment")
	}
	if len(f.world.inviteCalls) != baselineInvites+1 || f.world.inviteCalls[baselineInvites][0] != "new-app" {
		t.Fatalf("wrong membership invitation %#v", f.world.inviteCalls)
	}
	checks := 0
	for _, app := range f.world.selfChecks {
		if app == "new-app" {
			checks++
		}
	}
	if checks < 2 {
		t.Fatal("new bot was not verified using its own bound credential")
	}
	if _, err = f.service.Run(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if f.prepared != baselinePrepare+1 || len(f.world.inviteCalls) != baselineInvites+1 {
		t.Fatal("member replay repeated effects")
	}
}
func TestLateRemovedMemberIsNotRevivedByMembershipReconciliation(t *testing.T) {
	f := newFixture(t)
	if _, err := f.service.Run(context.Background(), f.operationID); err != nil {
		t.Fatal(err)
	}
	id := queueNewMember(t, f)
	if err := f.store.Within(context.Background(), func(tx *workstore.Tx) error {
		member, err := tx.GetProjectMember(f.projectID, "new-role")
		if err != nil {
			return err
		}
		member.State = work.ProjectMemberRemoved
		member.Revision++
		return tx.PutProjectMember(member, 1)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Run(context.Background(), id); !errors.Is(err, ErrStale) {
		t.Fatalf("removed member saga still authorized: %v", err)
	}
	if err := f.service.RefreshMembers(context.Background(), f.projectID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Within(context.Background(), func(tx *workstore.Tx) error {
		member, err := tx.GetProjectMember(f.projectID, "new-role")
		if err != nil {
			return err
		}
		if member.State != work.ProjectMemberRemoved {
			t.Fatal("background reconciliation revived removed role")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestMembershipRefreshDoesNotResumePausedProject(t *testing.T) {
	f := newFixture(t)
	if _, err := f.service.Run(context.Background(), f.operationID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Within(context.Background(), func(tx *workstore.Tx) error {
		project, err := tx.GetProject(f.projectID)
		if err != nil {
			return err
		}
		old := project.Revision
		project.State = work.ProjectPausing
		project.Revision++
		if err = tx.PutProject(project, old); err != nil {
			return err
		}
		old = project.Revision
		project.State = work.ProjectPaused
		project.Revision++
		return tx.PutProject(project, old)
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.RefreshMembers(context.Background(), f.projectID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Within(context.Background(), func(tx *workstore.Tx) error {
		project, err := tx.GetProject(f.projectID)
		if err != nil {
			return err
		}
		if project.State != work.ProjectPaused {
			t.Fatalf("membership update resumed project: %s", project.State)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
