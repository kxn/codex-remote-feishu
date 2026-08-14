package orchestrator

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestStartReviewForClaudeQueuesTypedReviewFork(t *testing.T) {
	svc, surface, repoRoot := newReviewSessionService(t)
	surface.Backend = agentproto.BackendClaude
	svc.root.Instances["inst-1"].Backend = agentproto.BackendClaude
	writeReviewSessionRepoFile(t, repoRoot, "docs/guide.md", "pending review change\n")

	events := svc.startReview(surface, reviewStartState{
		Ready:           true,
		ParentThreadID:  "thread-main",
		ThreadCWD:       repoRoot,
		SourceMessageID: "msg-review-claude",
		TargetLabel:     "未提交变更",
		Target:          agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindUncommittedChanges},
	})

	if len(events) == 0 {
		t.Fatal("expected Claude review start events")
	}
	for _, event := range events {
		if event.Command != nil && event.Command.Kind == agentproto.CommandReviewStart {
			t.Fatalf("Claude review must not use Codex-native review/start: %#v", events)
		}
	}
	if len(surface.QueuedQueueItemIDs) != 0 || surface.ActiveQueueItemID == "" {
		t.Fatalf("expected review queue item to dispatch immediately, queued=%#v active=%q", surface.QueuedQueueItemIDs, surface.ActiveQueueItemID)
	}
	item := surface.QueueItems[surface.ActiveQueueItemID]
	if item == nil {
		t.Fatal("expected active review queue item")
	}
	plan := queuedItemPromptDispatchPlan(item)
	if plan.ExecutionMode != agentproto.PromptExecutionModeForkEphemeral ||
		plan.SourceThreadID != "thread-main" ||
		plan.SurfaceBindingPolicy != agentproto.SurfaceBindingPolicyKeepSurfaceSelection ||
		plan.Purpose != agentproto.PromptPurposeReview {
		t.Fatalf("unexpected Claude review dispatch plan: %#v", plan)
	}
	if surface.SelectedThreadID != "thread-main" {
		t.Fatalf("review fork must keep parent selected, got %q", surface.SelectedThreadID)
	}
	if surface.ReviewSession == nil || surface.ReviewSession.Backend != agentproto.BackendClaude || surface.ReviewSession.ExecutorKind != state.ReviewExecutorClaudeForkSession {
		t.Fatalf("unexpected Claude review session: %#v", surface.ReviewSession)
	}
	if len(item.Inputs) != 1 || !strings.Contains(item.Inputs[0].Text, "docs/guide.md") {
		t.Fatalf("review prompt must carry controlled target context: %#v", item.Inputs)
	}
	command := agentCommandFromEvents(events)
	if command == nil || command.Overrides.PlanMode != string(state.PlanModeSettingOn) {
		t.Fatalf("Claude review must keep reviewer permission mode in plan: %#v", command)
	}
}

func TestStartReviewForOpenCodeQueuesTypedReviewFork(t *testing.T) {
	svc, surface, repoRoot := newReviewSessionService(t)
	surface.Backend = agentproto.BackendOpenCode
	svc.root.Instances["inst-1"].Backend = agentproto.BackendOpenCode
	writeReviewSessionRepoFile(t, repoRoot, "docs/guide.md", "pending review change\n")

	events := svc.startReview(surface, reviewStartState{
		Ready:           true,
		ParentThreadID:  "thread-main",
		ThreadCWD:       repoRoot,
		SourceMessageID: "msg-review-opencode",
		TargetLabel:     "未提交变更",
		Target:          agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindUncommittedChanges},
	})

	if len(events) == 0 {
		t.Fatal("expected OpenCode review start events")
	}
	for _, event := range events {
		if event.Command != nil && event.Command.Kind == agentproto.CommandReviewStart {
			t.Fatalf("OpenCode review must not use Codex-native review/start: %#v", events)
		}
	}
	if len(surface.QueuedQueueItemIDs) != 0 || surface.ActiveQueueItemID == "" {
		t.Fatalf("expected review queue item to dispatch immediately, queued=%#v active=%q", surface.QueuedQueueItemIDs, surface.ActiveQueueItemID)
	}
	item := surface.QueueItems[surface.ActiveQueueItemID]
	if item == nil {
		t.Fatal("expected active review queue item")
	}
	plan := queuedItemPromptDispatchPlan(item)
	if plan.ExecutionMode != agentproto.PromptExecutionModeForkEphemeral ||
		plan.SourceThreadID != "thread-main" ||
		plan.SurfaceBindingPolicy != agentproto.SurfaceBindingPolicyKeepSurfaceSelection ||
		plan.Purpose != agentproto.PromptPurposeReview {
		t.Fatalf("unexpected OpenCode review dispatch plan: %#v", plan)
	}
	if surface.SelectedThreadID != "thread-main" {
		t.Fatalf("review fork must keep parent selected, got %q", surface.SelectedThreadID)
	}
	if surface.ReviewSession == nil || surface.ReviewSession.Backend != agentproto.BackendOpenCode || surface.ReviewSession.ExecutorKind != state.ReviewExecutorOpenCodeACPFork {
		t.Fatalf("unexpected OpenCode review session: %#v", surface.ReviewSession)
	}
	if len(item.Inputs) != 1 || !strings.Contains(item.Inputs[0].Text, "docs/guide.md") {
		t.Fatalf("review prompt must carry controlled target context: %#v", item.Inputs)
	}
}

func TestOpenCodeReviewConfigFailureClearsPendingSessionAndKeepsParentSelected(t *testing.T) {
	svc, surface, repoRoot := newReviewSessionService(t)
	surface.Backend = agentproto.BackendOpenCode
	svc.root.Instances["inst-1"].Backend = agentproto.BackendOpenCode
	writeReviewSessionRepoFile(t, repoRoot, "docs/guide.md", "pending review change\n")
	events := svc.startReview(surface, reviewStartState{
		Ready:           true,
		ParentThreadID:  "thread-main",
		ThreadCWD:       repoRoot,
		SourceMessageID: "msg-review-opencode",
		Target:          agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindUncommittedChanges},
	})
	command := agentCommandFromEvents(events)
	if command == nil || surface.ReviewSession == nil || surface.ActiveQueueItemID == "" {
		t.Fatalf("expected pending OpenCode review command, events=%#v session=%#v active=%q", events, surface.ReviewSession, surface.ActiveQueueItemID)
	}
	const commandID = "cmd-opencode-review-config-failed"
	svc.BindPendingRemoteCommand(surface.SurfaceSessionID, commandID)

	problem := agentproto.ErrorInfo{
		Code:             "opencode_prompt_config_invalid",
		SurfaceSessionID: surface.SurfaceSessionID,
		CommandID:        commandID,
		ThreadID:         "ses_review",
		Message:          "review mode unavailable",
	}
	failed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventSystemError,
		CommandID: commandID,
		ThreadID:  "ses_review",
		Problem:   &problem,
	})
	if len(failed) == 0 || surface.ReviewSession != nil || surface.ActiveQueueItemID != "" {
		t.Fatalf("OpenCode review config failure must clear pending state: events=%#v session=%#v active=%q", failed, surface.ReviewSession, surface.ActiveQueueItemID)
	}
	if surface.SelectedThreadID != "thread-main" {
		t.Fatalf("review config failure must keep parent selected, got %q", surface.SelectedThreadID)
	}
}

func TestValidReviewSessionRequiresMatchingRecordedBackend(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	surface.ReviewSession = &state.ReviewSessionRecord{
		Phase:          state.ReviewSessionPhaseReady,
		Backend:        agentproto.BackendClaude,
		ExecutorKind:   state.ReviewExecutorClaudeForkSession,
		ParentThreadID: "thread-main",
		ReviewThreadID: "thread-review",
	}
	if got := svc.validReviewSession(surface); got != nil {
		t.Fatalf("Codex surface must reject Claude review session: %#v", got)
	}
	surface.Backend = agentproto.BackendClaude
	svc.root.Instances["inst-1"].Backend = agentproto.BackendClaude
	if got := svc.validReviewSession(surface); got == nil {
		t.Fatal("matching Claude surface and instance should accept Claude review session")
	}
}

func TestInitialReviewResultAggregatesCompletedAssistantItemsOnce(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	activateReviewSessionForTest(t, svc, surface, "msg-review-start", "turn-review-1")
	for _, item := range []struct {
		id   string
		text string
	}{
		{id: "finding-1", text: "第一条严重问题"},
		{id: "finding-1", text: "第一条严重问题"},
		{id: "finding-2", text: "第二条测试缺口"},
	} {
		svc.ApplyAgentEvent("inst-1", agentproto.Event{
			Kind:      agentproto.EventItemCompleted,
			ThreadID:  "thread-review",
			TurnID:    "turn-review-1",
			ItemID:    item.id,
			ItemKind:  "agent_message",
			Metadata:  map[string]any{"text": item.text},
			Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
		})
	}
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})

	if got, want := surface.ReviewSession.LastReviewText, "第一条严重问题\n\n第二条测试缺口"; got != want {
		t.Fatalf("aggregated review text = %q, want %q", got, want)
	}
	if surface.ReviewSession.PendingReviewText != "" || len(surface.ReviewSession.PendingReviewItemIDs) != 0 {
		t.Fatalf("completed initial turn must clear capture runtime: %#v", surface.ReviewSession)
	}
}

func TestTypedReviewBindingUsesReviewTemporarySessionLabel(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	svc.bindActiveRemoteTurn("inst-1", &remoteTurnBinding{
		InstanceID:       "inst-1",
		SurfaceSessionID: surface.SurfaceSessionID,
		ThreadID:         "thread-review-fork",
		TurnID:           "turn-review-fork",
		DispatchPlan: agentproto.PromptDispatchPlan{
			ExecutionMode:        agentproto.PromptExecutionModeForkEphemeral,
			SourceThreadID:       "thread-main",
			SurfaceBindingPolicy: agentproto.SurfaceBindingPolicyKeepSurfaceSelection,
			Purpose:              agentproto.PromptPurposeReview,
		},
	})
	if got := svc.ResolveTemporarySessionLabel(surface.SurfaceSessionID, "inst-1", "thread-review-fork", "turn-review-fork"); got != reviewTemporarySessionLabel {
		t.Fatalf("typed review label = %q, want %q", got, reviewTemporarySessionLabel)
	}
}

func TestClaudeReviewCommandRejectClearsPendingSession(t *testing.T) {
	svc, surface, repoRoot := newReviewSessionService(t)
	surface.Backend = agentproto.BackendClaude
	svc.root.Instances["inst-1"].Backend = agentproto.BackendClaude
	writeReviewSessionRepoFile(t, repoRoot, "docs/guide.md", "pending review change\n")
	events := svc.startReview(surface, reviewStartState{
		Ready:           true,
		ParentThreadID:  "thread-main",
		ThreadCWD:       repoRoot,
		SourceMessageID: "msg-review-claude",
		Target:          agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindUncommittedChanges},
	})
	command := agentCommandFromEvents(events)
	if command == nil || surface.ReviewSession == nil {
		t.Fatalf("expected pending Claude review command, events=%#v session=%#v", events, surface.ReviewSession)
	}
	const commandID = "cmd-claude-review-rejected"
	svc.BindPendingRemoteCommand(surface.SurfaceSessionID, commandID)
	rejected := svc.HandleCommandRejected("inst-1", agentproto.CommandAck{
		CommandID: commandID,
		Problem:   &agentproto.ErrorInfo{Code: "claude_fork_failed", Message: "fork failed"},
	})
	if noticeCode(rejected, "command_rejected") == "" || surface.ReviewSession != nil || surface.ActiveQueueItemID != "" {
		t.Fatalf("Claude review rejection must clear pending state: events=%#v session=%#v active=%q", rejected, surface.ReviewSession, surface.ActiveQueueItemID)
	}
}

func TestTypedReviewBindingMaterializesReviewThreadSource(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	inst := svc.root.Instances["inst-1"]
	delete(inst.Threads, "thread-review-fork")
	binding := &remoteTurnBinding{DispatchPlan: agentproto.PromptDispatchPlan{
		ExecutionMode:        agentproto.PromptExecutionModeForkEphemeral,
		SourceThreadID:       "thread-main",
		SurfaceBindingPolicy: agentproto.SurfaceBindingPolicyKeepSurfaceSelection,
		Purpose:              agentproto.PromptPurposeReview,
	}}
	thread := svc.materializeRemoteTurnThread(inst, "thread-review-fork", "", binding, nil)
	if thread == nil || !threadIsReview(thread) || thread.ForkedFromID != "thread-main" {
		t.Fatalf("typed review binding must materialize review provenance: %#v", thread)
	}
	if surface.SelectedThreadID != "thread-main" {
		t.Fatalf("materialization must not change selection, got %q", surface.SelectedThreadID)
	}
}

func TestTypedReviewDiscoveryMarksReviewThreadBeforeTurnStarts(t *testing.T) {
	svc, surface, repoRoot := newReviewSessionService(t)
	surface.Backend = agentproto.BackendOpenCode
	svc.root.Instances["inst-1"].Backend = agentproto.BackendOpenCode
	writeReviewSessionRepoFile(t, repoRoot, "docs/guide.md", "pending review change\n")
	events := svc.startReview(surface, reviewStartState{
		Ready:           true,
		ParentThreadID:  "thread-main",
		ThreadCWD:       repoRoot,
		SourceMessageID: "msg-review-discovery",
		Target:          agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindUncommittedChanges},
	})
	if command := agentCommandFromEvents(events); command == nil || surface.ReviewSession == nil || surface.ActiveQueueItemID == "" {
		t.Fatalf("expected pending OpenCode review, events=%#v session=%#v", events, surface.ReviewSession)
	}
	const commandID = "cmd-review-discovery"
	svc.BindPendingRemoteCommand(surface.SurfaceSessionID, commandID)

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventThreadDiscovered,
		CommandID: commandID,
		ThreadID:  "thread-review-fork",
		CWD:       "/tmp/work",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})

	thread := svc.root.Instances["inst-1"].Threads["thread-review-fork"]
	if thread == nil || !threadIsReview(thread) || thread.ForkedFromID != "thread-main" {
		t.Fatalf("typed review discovery must hide fork before prompt starts: %#v", thread)
	}
	if surface.ReviewSession == nil || surface.ReviewSession.Phase != state.ReviewSessionPhasePending || surface.ReviewSession.ReviewThreadID != "" {
		t.Fatalf("discovery must not activate review before turn starts: %#v", surface.ReviewSession)
	}
}

func TestFollowSurfaceIgnoresObservedReviewThreadFocus(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	inst := svc.root.Instances["inst-1"]
	surface.RouteMode = state.RouteModeFollowLocal
	inst.ObservedFocusedThreadID = "thread-review"

	events := svc.reevaluateFollowSurface(surface)

	if len(events) != 0 {
		t.Fatalf("review focus must not produce a follow rebind: %#v", events)
	}
	if surface.SelectedThreadID != "thread-main" {
		t.Fatalf("review focus must keep parent selected, got %q", surface.SelectedThreadID)
	}
	if svc.followLocalWouldRetarget(surface, inst) {
		t.Fatal("review focus must not be reported as a follow retarget")
	}
}

func agentCommandFromEvents(events []eventcontract.Event) *agentproto.Command {
	for i := range events {
		if events[i].Command != nil {
			return events[i].Command
		}
	}
	return nil
}
