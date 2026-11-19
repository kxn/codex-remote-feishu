package orchestrator

import (
	"fmt"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

func TestGoalCommandTextAndCardDelivery(t *testing.T) {
	commands := []struct {
		text string
		kind agentproto.CommandKind
	}{
		{"/goal", agentproto.CommandThreadGoalGet},
		{"/goal new 完成联调", agentproto.CommandThreadGoalSet},
		{"/goal edit 更新联调目标", agentproto.CommandThreadGoalSet},
		{"/goal pause", agentproto.CommandThreadGoalSet},
		{"/goal resume", agentproto.CommandThreadGoalSet},
		{"/goal clear --confirm", agentproto.CommandThreadGoalClear},
	}
	for _, command := range commands {
		for _, card := range []bool{false, true} {
			for _, failure := range []string{"", "dispatch", "backend"} {
				t.Run(fmt.Sprintf("%s/card=%t/failure=%s", command.text, card, failure), func(t *testing.T) {
					svc, surfaceID, instanceID := goalInterlockTestSetup(t)
					action := control.Action{
						Kind: control.ActionGoalCommand, SurfaceSessionID: surfaceID,
						ChatID: "chat-1", ActorUserID: "user-1",
						MessageID: "om-source", Text: command.text,
					}
					if card {
						payload := svc.goalScopedPayload(svc.root.Surfaces[surfaceID], command.text, map[string]any{})
						action.GoalOperationID, _ = payload[frontstagecontract.CardActionPayloadKeyGoalOperationID].(string)
						action.InstanceID = instanceID
						action.ThreadID = "thread-1"
						action.CatalogFamilyID = control.FeishuCommandGoal
						action.CatalogVariantID = "goal.codex.normal"
						action.CatalogBackend = agentproto.BackendCodex
						action.Inbound = &control.ActionInboundMeta{CardDaemonLifecycleID: "lifecycle-1"}
					}
					events := svc.ApplySurfaceAction(action)
					request := findAgentCommand(events, command.kind)
					if request == nil {
						t.Fatalf("expected Goal command, got %#v", events)
					}
					if loading := findPageEvent(events); loading != nil {
						assertGoalDeliveryTarget(t, loading, card)
					}
					var page *control.FeishuPageView
					if failure == "dispatch" {
						page = findPageEvent(svc.HandleCommandDispatchFailure(surfaceID, request.CommandID, fmt.Errorf("send failed")))
					} else {
						result := agentproto.Event{
							Kind:      agentproto.EventThreadGoalCommandResult,
							CommandID: request.CommandID, ThreadID: "thread-1",
							ThreadGoal: &agentproto.ThreadGoalUpdate{ThreadID: "thread-1", Objective: "联调", Status: "paused"},
						}
						if failure == "backend" {
							result.ErrorMessage = "Goal 操作被拒绝"
						}
						page = findPageEvent(svc.ApplyAgentEvent(instanceID, result))
					}
					if page == nil {
						t.Fatal("expected Goal result page")
					}
					assertGoalDeliveryTarget(t, page, card)
				})
			}
		}
	}
}

func assertGoalDeliveryTarget(t *testing.T, page *control.FeishuPageView, card bool) {
	t.Helper()
	if card {
		if page.MessageID != "om-source" || !page.Patchable {
			t.Fatalf("card command must update its original card, got message=%q patchable=%t", page.MessageID, page.Patchable)
		}
	} else if page.MessageID != "" || page.Patchable {
		t.Fatalf("text command must never patch the user's text message, got message=%q patchable=%t", page.MessageID, page.Patchable)
	}
}

func TestGoalQueryMissingClearsCachedGoal(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionGoalCommand,
		SurfaceSessionID: surfaceID, ChatID: "chat-1", ActorUserID: "user-1", Text: "/goal"})
	request := findAgentCommand(events, agentproto.CommandThreadGoalGet)
	result := svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind: agentproto.EventThreadGoalCommandResult, CommandID: request.CommandID,
		ThreadID: "thread-1", GoalMissing: true,
	})
	if goal := svc.root.Instances[instanceID].Threads["thread-1"].ThreadGoal; goal != nil {
		t.Fatalf("confirmed missing Goal must clear old cached Goal, got %#v", goal)
	}
	page := findPageEvent(result)
	if page == nil || !containsSectionLine(page, "当前会话没有 Goal") {
		t.Fatalf("confirmed missing Goal must show no-Goal page, got %#v", page)
	}
}

func TestGoalClearWithoutConfirmationDoesNotClaimCleared(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionGoalCommand,
		SurfaceSessionID: surfaceID, ChatID: "chat-1", ActorUserID: "user-1", Text: "/goal clear --confirm"})
	request := findAgentCommand(events, agentproto.CommandThreadGoalClear)
	result := svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind: agentproto.EventThreadGoalCommandResult, CommandID: request.CommandID, ThreadID: "thread-1",
	})
	page := findPageEvent(result)
	if page == nil || page.Title == "Goal 已清除" || containsSectionLine(page, "当前会话没有 Goal") {
		t.Fatalf("unconfirmed clear must not claim success, got %#v", page)
	}
	if svc.root.Instances[instanceID].Threads["thread-1"].ThreadGoal == nil {
		t.Fatal("unconfirmed clear must retain cached Goal")
	}
}

func TestGoalResultAfterThreadSwitchDoesNotRenderStaleControls(t *testing.T) {
	for _, dispatchFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(dispatchFailure), func(t *testing.T) {
			svc, surfaceID, instanceID := goalInterlockTestSetup(t)
			events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionGoalCommand,
				SurfaceSessionID: surfaceID, ChatID: "chat-1", ActorUserID: "user-1", Text: "/goal pause"})
			request := findAgentCommand(events, agentproto.CommandThreadGoalSet)
			svc.root.Surfaces[surfaceID].SelectedThreadID = "thread-2"
			if dispatchFailure {
				events = svc.HandleCommandDispatchFailure(surfaceID, request.CommandID, fmt.Errorf("send failed"))
			} else {
				events = svc.ApplyAgentEvent(instanceID, agentproto.Event{
					Kind: agentproto.EventThreadGoalCommandResult, CommandID: request.CommandID, ThreadID: "thread-1",
					ThreadGoal: &agentproto.ThreadGoalUpdate{ThreadID: "thread-1", Objective: "旧会话目标", Status: "paused"},
				})
			}
			if page := findPageEvent(events); page != nil {
				t.Fatalf("old thread result must not expose controls for the newly selected thread, got %#v", page)
			}
			if len(svc.goalUserCommands) != 0 {
				t.Fatal("finished command must still be removed from pending map")
			}
		})
	}
}
