package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

func goalCardPayloadAction(surfaceID string, page *control.FeishuPageView, arg string) control.Action {
	var payload map[string]any
	for _, button := range page.RelatedButtons {
		if button.CallbackValue[frontstagecontract.CardActionPayloadKeyActionArg] == arg {
			payload = button.CallbackValue
			break
		}
	}
	if payload == nil {
		for _, section := range page.Sections {
			for _, entry := range section.Entries {
				if entry.Form != nil {
					payload = entry.Form.SubmitValue
				}
			}
		}
	}
	action := goalCardTestAction(surfaceID, payload[frontstagecontract.CardActionPayloadKeyInstanceID].(string), payload[frontstagecontract.CardActionPayloadKeyThreadID].(string), "/goal "+arg)
	action.GoalOperationID, _ = payload[frontstagecontract.CardActionPayloadKeyGoalOperationID].(string)
	return action
}

func TestGoalOldConfirmationCannotUseAnotherCardsFingerprint(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	opened := svc.ApplySurfaceAction(control.Action{Kind: control.ActionGoalCommand, SurfaceSessionID: surfaceID, Text: "/goal clear"})
	old := goalCardPayloadAction(surfaceID, findPageEvent(opened), "clear --confirm")
	svc.root.Instances[instanceID].Threads["thread-1"].ThreadGoal = &agentproto.ThreadGoalUpdate{ThreadID: "thread-1", Objective: "replacement", Status: "active", CreatedAt: 200}
	current := svc.ApplySurfaceAction(control.Action{Kind: control.ActionGoalCommand, SurfaceSessionID: surfaceID, Text: "/goal clear"})
	if cmd := findAgentCommand(svc.ApplySurfaceAction(old), agentproto.CommandThreadGoalClear); cmd != nil {
		t.Fatal("old confirmation cleared replacement Goal after another card opened")
	}
	confirm := goalCardPayloadAction(surfaceID, findPageEvent(current), "clear --confirm")
	if cmd := findAgentCommand(svc.ApplySurfaceAction(confirm), agentproto.CommandThreadGoalClear); cmd == nil {
		t.Fatal("rejecting the old card must preserve the replacement Goal's own confirmation")
	}
}

func TestGoalOldStatusControlsCannotMutateReplacement(t *testing.T) {
	for _, sub := range []string{"pause", "resume"} {
		t.Run(sub, func(t *testing.T) {
			svc, surfaceID, instanceID := goalInterlockTestSetup(t)
			surface := svc.root.Surfaces[surfaceID]
			goal := svc.root.Instances[instanceID].Threads["thread-1"].ThreadGoal
			if sub == "resume" {
				goal.Status = "paused"
			}
			page := svc.goalStatusPage(surface, goal)
			old := goalCardPayloadAction(surfaceID, &page, sub)
			svc.root.Instances[instanceID].Threads["thread-1"].ThreadGoal = &agentproto.ThreadGoalUpdate{ThreadID: "thread-1", Objective: "replacement", Status: "active", CreatedAt: 200}
			if cmd := findAgentCommand(svc.ApplySurfaceAction(old), agentproto.CommandThreadGoalSet); cmd != nil {
				t.Fatal("old status button mutated replacement Goal")
			}
		})
	}
}

func TestGoalMutationCardWithoutOperationIsRejected(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	action := goalCardTestAction(surfaceID, instanceID, "thread-1", "/goal pause")
	if cmd := findAgentCommand(svc.ApplySurfaceAction(action), agentproto.CommandThreadGoalSet); cmd != nil {
		t.Fatal("legacy mutation card without operation token must not execute")
	}
}

func TestGoalCardOperationCanOnlyBeConsumedOnce(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	surface := svc.root.Surfaces[surfaceID]
	page := svc.goalStatusPage(surface, svc.root.Instances[instanceID].Threads["thread-1"].ThreadGoal)
	action := goalCardPayloadAction(surfaceID, &page, "pause")
	if cmd := findAgentCommand(svc.ApplySurfaceAction(action), agentproto.CommandThreadGoalSet); cmd == nil {
		t.Fatal("current card must execute its first request")
	}
	if cmd := findAgentCommand(svc.ApplySurfaceAction(action), agentproto.CommandThreadGoalSet); cmd != nil {
		t.Fatal("replayed card operation must not execute twice")
	}
}

func TestGoalOldEditFormCannotUseAnotherFormsFingerprint(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	opened := svc.ApplySurfaceAction(control.Action{Kind: control.ActionGoalCommand, SurfaceSessionID: surfaceID, Text: "/goal edit"})
	old := goalCardPayloadAction(surfaceID, findPageEvent(opened), "edit stale objective")
	svc.root.Instances[instanceID].Threads["thread-1"].ThreadGoal = &agentproto.ThreadGoalUpdate{ThreadID: "thread-1", Objective: "replacement", Status: "active", CreatedAt: 200}
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionGoalCommand, SurfaceSessionID: surfaceID, Text: "/goal edit"})
	if cmd := findAgentCommand(svc.ApplySurfaceAction(old), agentproto.CommandThreadGoalSet); cmd != nil {
		t.Fatal("old edit form changed replacement Goal after another form opened")
	}
}

func TestGoalResumeCardRejectsCompletedGoal(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	surface := svc.root.Surfaces[surfaceID]
	goal := svc.root.Instances[instanceID].Threads["thread-1"].ThreadGoal
	goal.Status = "paused"
	page := svc.goalStatusPage(surface, goal)
	old := goalCardPayloadAction(surfaceID, &page, "resume")
	goal.Status = "complete"
	if cmd := findAgentCommand(svc.ApplySurfaceAction(old), agentproto.CommandThreadGoalSet); cmd != nil {
		t.Fatal("old resume button must not reactivate a completed Goal")
	}
}

func TestGoalCardCleanupPreservesExpiredTextConfirmation(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	surface := svc.root.Surfaces[surfaceID]
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionGoalCommand, SurfaceSessionID: surfaceID, Text: "/goal clear"})
	later := svc.now().Add(11 * time.Minute)
	svc.now = func() time.Time { return later }
	svc.goalStatusPage(surface, svc.root.Instances[instanceID].Threads["thread-1"].ThreadGoal)
	if cmd := findAgentCommand(svc.ApplySurfaceAction(control.Action{Kind: control.ActionGoalCommand, SurfaceSessionID: surfaceID, Text: "/goal clear --confirm"}), agentproto.CommandThreadGoalClear); cmd != nil {
		t.Fatal("rendering a card must not discard the expired text confirmation and allow its replay")
	}
}
