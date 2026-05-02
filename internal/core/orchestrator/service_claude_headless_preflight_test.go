package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func newClaudeHeadlessPreflightService(t *testing.T) (*Service, *state.SurfaceConsoleRecord, *state.InstanceRecord) {
	t.Helper()
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	inst := &state.InstanceRecord{
		InstanceID:            "inst-claude-1",
		DisplayName:           "repo",
		WorkspaceRoot:         "/data/dl/repo",
		WorkspaceKey:          "/data/dl/repo",
		ShortName:             "repo",
		Backend:               agentproto.BackendClaude,
		ClaudeProfileID:       "devseek",
		ClaudeReasoningEffort: "high",
		Source:                "headless",
		Managed:               true,
		Online:                true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID: "thread-1",
				Name:     "主线程",
				CWD:      "/data/dl/repo",
				Loaded:   true,
			},
		},
	}
	svc.UpsertInstance(inst)
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionStatus,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	surface := svc.root.Surfaces["surface-1"]
	surface.Backend = agentproto.BackendClaude
	surface.ClaudeProfileID = "devseek"
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       inst.InstanceID,
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-1",
	})
	return svc, surface, inst
}

func TestDispatchNextRestartsClaudeHeadlessForQueuedReasoningMismatch(t *testing.T) {
	svc, surface, inst := newClaudeHeadlessPreflightService(t)
	surface.PromptOverride = state.ModelConfigRecord{ReasoningEffort: "medium"}
	surface.QueueItems["queue-1"] = &state.QueueItemRecord{
		ID:                    "queue-1",
		SurfaceSessionID:      surface.SurfaceSessionID,
		ActorUserID:           surface.ActorUserID,
		SourceKind:            state.QueueItemSourceUser,
		SourceMessageID:       "msg-1",
		SourceMessagePreview:  "继续处理",
		ReplyToMessageID:      "msg-1",
		ReplyToMessagePreview: "继续处理",
		Inputs:                []agentproto.Input{{Type: agentproto.InputText, Text: "继续处理"}},
		FrozenThreadID:        "thread-1",
		FrozenCWD:             "/data/dl/repo",
		FrozenExecutionMode:   agentproto.PromptExecutionModeResumeExisting,
		FrozenOverride: state.ModelConfigRecord{
			ReasoningEffort: "low",
			AccessMode:      agentproto.AccessModeFullAccess,
		},
		FrozenPlanMode:     state.PlanModeSettingOff,
		RouteModeAtEnqueue: state.RouteModePinned,
		Status:             state.QueueItemQueued,
	}
	surface.QueuedQueueItemIDs = []string{"queue-1"}

	events := svc.dispatchNext(surface)

	if surface.ActiveQueueItemID != "" {
		t.Fatalf("expected queue item to stay queued during restart preflight, got active %q", surface.ActiveQueueItemID)
	}
	if len(surface.QueuedQueueItemIDs) != 1 || surface.QueuedQueueItemIDs[0] != "queue-1" {
		t.Fatalf("expected queue order to stay intact, got %#v", surface.QueuedQueueItemIDs)
	}
	if surface.PendingHeadless == nil || surface.PendingHeadless.Purpose != state.HeadlessLaunchPurposePromptDispatchRestart {
		t.Fatalf("expected prompt dispatch restart pending headless, got %#v", surface.PendingHeadless)
	}
	if surface.PendingHeadless.ClaudeReasoningEffort != "low" {
		t.Fatalf("expected pending headless to carry queued reasoning, got %#v", surface.PendingHeadless)
	}
	if surface.AttachedInstanceID != "" {
		t.Fatalf("expected surface to wait for restarted instance, got attached %q", surface.AttachedInstanceID)
	}
	if surface.PromptOverride.ReasoningEffort != "medium" {
		t.Fatalf("expected surface override to remain untouched, got %#v", surface.PromptOverride)
	}
	if len(events) != 3 || events[0].Notice == nil || events[1].DaemonCommand == nil || events[2].DaemonCommand == nil {
		t.Fatalf("expected restart notice + kill + start, got %#v", events)
	}
	if events[1].DaemonCommand.Kind != control.DaemonCommandKillHeadless || events[1].DaemonCommand.InstanceID != inst.InstanceID {
		t.Fatalf("unexpected kill command: %#v", events[1].DaemonCommand)
	}
	if events[2].DaemonCommand.Kind != control.DaemonCommandStartHeadless || events[2].DaemonCommand.ClaudeReasoningEffort != "low" {
		t.Fatalf("unexpected start command: %#v", events[2].DaemonCommand)
	}
}

func TestApplyInstanceConnectedAfterClaudePromptRestartDispatchesQueuedItem(t *testing.T) {
	svc, surface, _ := newClaudeHeadlessPreflightService(t)
	surface.QueueItems["queue-1"] = &state.QueueItemRecord{
		ID:                    "queue-1",
		SurfaceSessionID:      surface.SurfaceSessionID,
		ActorUserID:           surface.ActorUserID,
		SourceKind:            state.QueueItemSourceUser,
		SourceMessageID:       "msg-1",
		SourceMessagePreview:  "继续处理",
		ReplyToMessageID:      "msg-1",
		ReplyToMessagePreview: "继续处理",
		Inputs:                []agentproto.Input{{Type: agentproto.InputText, Text: "继续处理"}},
		FrozenThreadID:        "thread-1",
		FrozenCWD:             "/data/dl/repo",
		FrozenExecutionMode:   agentproto.PromptExecutionModeResumeExisting,
		FrozenOverride: state.ModelConfigRecord{
			ReasoningEffort: "low",
			AccessMode:      agentproto.AccessModeFullAccess,
		},
		FrozenPlanMode:     state.PlanModeSettingOff,
		RouteModeAtEnqueue: state.RouteModePinned,
		Status:             state.QueueItemQueued,
	}
	surface.QueuedQueueItemIDs = []string{"queue-1"}
	preflight := svc.dispatchNext(surface)
	if len(preflight) == 0 || surface.PendingHeadless == nil {
		t.Fatalf("expected preflight restart, got events=%#v pending=%#v", preflight, surface.PendingHeadless)
	}

	restarted := &state.InstanceRecord{
		InstanceID:            surface.PendingHeadless.InstanceID,
		DisplayName:           "repo",
		WorkspaceRoot:         "/data/dl/repo",
		WorkspaceKey:          "/data/dl/repo",
		ShortName:             "repo",
		Backend:               agentproto.BackendClaude,
		ClaudeProfileID:       "devseek",
		ClaudeReasoningEffort: "low",
		Source:                "headless",
		Managed:               true,
		Online:                true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "主线程", CWD: "/data/dl/repo", Loaded: true},
		},
	}
	svc.UpsertInstance(restarted)

	events := svc.ApplyInstanceConnected(restarted.InstanceID)

	if surface.PendingHeadless != nil {
		t.Fatalf("expected pending headless to clear after connect, got %#v", surface.PendingHeadless)
	}
	if surface.AttachedInstanceID != restarted.InstanceID {
		t.Fatalf("expected surface to reattach to restarted instance, got %q", surface.AttachedInstanceID)
	}
	if surface.ActiveQueueItemID != "queue-1" {
		t.Fatalf("expected queued item to dispatch after reattach, got active %q", surface.ActiveQueueItemID)
	}
	var command *agentproto.Command
	for _, event := range events {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			command = event.Command
			break
		}
	}
	if command == nil {
		t.Fatalf("expected prompt dispatch after restart attach, got %#v", events)
	}
	if command.Overrides.ReasoningEffort != "low" || command.Target.ThreadID != "thread-1" {
		t.Fatalf("unexpected resumed prompt command: %#v", command)
	}
}

func TestAutoContinueRestartsClaudeHeadlessForReasoningMismatch(t *testing.T) {
	svc, surface, inst := newClaudeHeadlessPreflightService(t)
	surface.AutoContinue.Enabled = true
	surface.AutoContinue.Episode = &state.PendingAutoContinueEpisodeRecord{
		EpisodeID:                  "autocontinue-1",
		InstanceID:                 inst.InstanceID,
		ThreadID:                   "thread-1",
		FrozenCWD:                  "/data/dl/repo",
		FrozenExecutionMode:        agentproto.PromptExecutionModeResumeExisting,
		FrozenSurfaceBindingPolicy: agentproto.SurfaceBindingPolicyFollowExecutionThread,
		FrozenOverride: state.ModelConfigRecord{
			ReasoningEffort: "low",
			AccessMode:      agentproto.AccessModeFullAccess,
		},
		FrozenPlanMode:            state.PlanModeSettingOff,
		FrozenRouteMode:           state.RouteModePinned,
		RootReplyToMessageID:      "msg-1",
		RootReplyToMessagePreview: "msg-1",
		State:                     state.AutoContinueEpisodeScheduled,
		PendingDueAt:              svc.now(),
		TriggerKind:               state.AutoContinueTriggerKindEligibleFailure,
	}

	events := svc.maybeDispatchPendingAutoContinue(surface, svc.now())

	if len(events) != 3 || events[1].DaemonCommand == nil || events[2].DaemonCommand == nil {
		t.Fatalf("expected restart notice + kill + start for autocontinue, got %#v", events)
	}
	if surface.ActiveQueueItemID != "" {
		t.Fatalf("expected autocontinue not to occupy active queue before restart completes, got %q", surface.ActiveQueueItemID)
	}
	if surface.AutoContinue.Episode == nil || surface.AutoContinue.Episode.State != state.AutoContinueEpisodeScheduled {
		t.Fatalf("expected autocontinue episode to stay scheduled, got %#v", surface.AutoContinue.Episode)
	}
	if surface.PendingHeadless == nil || surface.PendingHeadless.Purpose != state.HeadlessLaunchPurposePromptDispatchRestart {
		t.Fatalf("expected autocontinue to reuse prompt restart owner, got %#v", surface.PendingHeadless)
	}
	if surface.PendingHeadless.ClaudeReasoningEffort != "low" {
		t.Fatalf("expected autocontinue restart to carry frozen reasoning, got %#v", surface.PendingHeadless)
	}
}
