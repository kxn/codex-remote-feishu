package workprovision

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestMembershipHealthDetectsRemovedBotWithoutInvitingOrStoppingOtherRole(t *testing.T) {
	f := newFixture(t)
	if _, err := f.service.Run(t.Context(), f.operationID); err != nil {
		t.Fatal(err)
	}
	invitations := len(f.world.inviteCalls)
	f.world.present["app-expert"] = false
	if err := f.service.CheckMembershipHealth(t.Context(), f.projectID); err != nil {
		t.Fatal(err)
	}
	if len(f.world.inviteCalls) != invitations {
		t.Fatal("health check silently reinvited removed bot")
	}
	err := f.store.Within(t.Context(), func(tx *workstore.Tx) error {
		expert, err := tx.GetProjectMember(f.projectID, "expert")
		if err != nil {
			return err
		}
		leader, err := tx.GetProjectMember(f.projectID, "leader")
		if err != nil {
			return err
		}
		project, err := tx.GetProject(f.projectID)
		if err != nil {
			return err
		}
		if expert.State != work.ProjectMemberUnavailable || expert.AvailabilityEvidence == nil || leader.State != work.ProjectMemberReady || project.State != work.ProjectDegraded {
			t.Fatalf("incorrect availability expert%+v leader%+v project%+v", expert, leader, project)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	f.world.present["app-expert"] = true
	if err = f.service.CheckMembershipHealth(t.Context(), f.projectID); err != nil {
		t.Fatal(err)
	}
	if err = f.store.Within(t.Context(), func(tx *workstore.Tx) error {
		member, err := tx.GetProjectMember(f.projectID, "expert")
		if member.State != work.ProjectMemberUnavailable {
			t.Fatal("health promoted unavailable member without provisioning")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
func TestMembershipChatDeletedKeepsHumanGateAndRejectsOldBinding(t *testing.T) {
	f := newFixture(t)
	if _, err := f.service.Run(t.Context(), f.operationID); err != nil {
		t.Fatal(err)
	}
	err := f.store.Within(t.Context(), func(tx *workstore.Tx) error {
		if err := tx.PutPauseGate(work.PauseGate{ID: "human-gate", ScopeID: f.scopeID, Source: work.GateProject, SourceID: "human-action", State: work.GateClosed, Revision: 1}, 0); err != nil {
			return err
		}
		channels, err := tx.ListProjectChannelBindings(f.projectID)
		if err != nil {
			return err
		}
		channel := channels[0]
		binding := f.leader.binding
		raw, _ := json.Marshal(map[string]string{"kind": "chat.deleted"})
		id, _, err := tx.AcceptPlatformObservation(workstore.PlatformObservation{Gateway: work.IngressGateway{RoleID: binding.RoleID, GatewayRef: binding.GatewayRef, AppID: binding.AppID, BotGeneration: binding.IdentityGeneration}, Tenant: channel.Tenant, ChatID: channel.ChatID, Kind: "chat.deleted", EventID: "chat-deleted-event", Facts: raw, ReceivedAt: f.service.now()})
		if err != nil {
			return err
		}
		observation := workstore.ProjectMembershipObservation{ProjectID: f.projectID, Binding: binding, ChannelGeneration: channel.BindingGeneration, Tenant: channel.Tenant, ChatID: channel.ChatID, ChatDeleted: true, Source: work.SourceRef{Kind: "platform_observation", ID: id, Version: "1", EvidenceRef: id}}
		old := observation
		old.ChannelGeneration++
		if err = tx.ApplyProjectMembershipObservation(old); err == nil {
			t.Fatal("old channel observation admitted")
		}
		if err = tx.ApplyProjectMembershipObservation(observation); err != nil {
			return err
		}
		for _, role := range []string{"leader", "expert"} {
			member, err := tx.GetProjectMember(f.projectID, role)
			if err != nil {
				return err
			}
			if member.State != work.ProjectMemberUnavailable {
				t.Fatal("deleted chat left member ready")
			}
		}
		gate, err := tx.GetPauseGate("human-gate")
		if err != nil {
			return err
		}
		if gate.State != work.GateClosed || gate.Revision != 1 {
			t.Fatal("independent human gate changed")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

type failedMembershipRead struct{ Platform }

func (failedMembershipRead) IsCallerInChat(context.Context, string) (bool, error) {
	return false, fmt.Errorf("network unavailable")
}
func TestMembershipHealthNetworkErrorDoesNotMeanAbsent(t *testing.T) {
	f := newFixture(t)
	if _, err := f.service.Run(t.Context(), f.operationID); err != nil {
		t.Fatal(err)
	}
	options := f.options
	options.Resolver = ResolverFunc(func(_ context.Context, binding work.RoleBotBinding) (Platform, error) {
		return failedMembershipRead{f.world.platforms[binding.AppID]}, nil
	})
	service, err := New(f.store, options)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.CheckMembershipHealth(t.Context(), f.projectID); err == nil {
		t.Fatal("network error not reported")
	}
	if err = f.store.Within(t.Context(), func(tx *workstore.Tx) error {
		members, err := tx.ListProjectMembers(f.projectID)
		for _, member := range members {
			if member.State != work.ProjectMemberReady {
				t.Fatal("network failure interpreted as absence")
			}
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
