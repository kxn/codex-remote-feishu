package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestCompactCommandRequiresBoundThread(t *testing.T) {
	now := time.Date(2026, 4, 14, 18, 0, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	svc.root.Surfaces["surface-1"].SelectedThreadID = ""

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCompact,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "compact_requires_thread" {
		t.Fatalf("expected compact_requires_thread notice, got %#v", events)
	}
}

func TestCompactCommandDispatchesThreadCompactStart(t *testing.T) {
	now := time.Date(2026, 4, 14, 18, 5, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCompact,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	catalog, command := requireCompactStartEvents(t, events)
	if catalog.Title != "正在压缩上下文" || catalog.ThemeKey != "progress" || !catalog.Patchable || catalog.TrackingKey == "" {
		t.Fatalf("unexpected compact owner card: %#v", catalog)
	}
	if catalog.Sealed {
		t.Fatalf("expected dispatching compact card to stay interactive lifecycle-wise, got %#v", catalog)
	}
	if len(catalog.BodySections) != 1 || catalog.BodySections[0].Label != "当前会话" {
		t.Fatalf("expected compact card to keep current thread in body area, got %#v", catalog.BodySections)
	}
	if len(catalog.NoticeSections) != 1 || !strings.Contains(strings.Join(catalog.NoticeSections[0].Lines, "\n"), "正在向本地 Codex 发起上下文压缩请求。") {
		t.Fatalf("expected compact card to put dispatch notice in notice area, got %#v", catalog.NoticeSections)
	}
	if command.Target.ThreadID != "thread-1" {
		t.Fatalf("unexpected compact target: %#v", command.Target)
	}
	binding := svc.turns.compactTurns["inst-1"]
	if binding == nil || binding.SurfaceSessionID != "surface-1" || binding.ThreadID != "thread-1" || binding.Status != compactTurnStatusDispatching {
		t.Fatalf("unexpected compact binding: %#v", binding)
	}
	if binding.FlowID != catalog.TrackingKey {
		t.Fatalf("expected compact binding flow to match owner card tracking key, binding=%#v catalog=%#v", binding, catalog)
	}
	if flow := svc.activeOwnerCardFlow(svc.root.Surfaces["surface-1"]); flow == nil || flow.Kind != ownerCardFlowKindCompact || flow.FlowID != binding.FlowID || flow.Phase != ownerCardFlowPhaseLoading {
		t.Fatalf("unexpected compact owner flow: %#v", flow)
	}
}

func TestCompactMenuActionBindsOwnerFlowToCurrentCard(t *testing.T) {
	now := time.Date(2026, 4, 19, 8, 0, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCompact,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-menu-compact-1",
		Inbound:          &control.ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
	})
	catalog, _ := requireCompactStartEvents(t, events)
	if catalog.MessageID != "om-menu-compact-1" {
		t.Fatalf("expected compact menu flow to target current card, got %#v", catalog)
	}
	if catalog.TrackingKey != "" {
		t.Fatalf("expected compact menu flow not to require detached tracking key, got %#v", catalog)
	}
	flow := svc.activeOwnerCardFlow(svc.root.Surfaces["surface-1"])
	if flow == nil || flow.MessageID != "om-menu-compact-1" {
		t.Fatalf("expected compact owner flow to bind current card, got %#v", flow)
	}
}

func TestCompactCommandRejectsWhileRegularTurnRunning(t *testing.T) {
	now := time.Date(2026, 4, 14, 18, 10, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})
	svc.root.Instances["inst-1"].ActiveThreadID = "thread-1"
	svc.root.Instances["inst-1"].ActiveTurnID = "turn-1"

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCompact,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "compact_busy" {
		t.Fatalf("expected compact_busy notice, got %#v", events)
	}
}

func TestCompactPendingQueuesLaterMessageUntilTurnCompletes(t *testing.T) {
	now := time.Date(2026, 4, 14, 18, 15, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})

	first := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCompact,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	requireCompactStartEvents(t, first)
	svc.BindPendingRemoteCommand("surface-1", "cmd-compact-1")

	queued := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-after-compact",
		Text:             "整理完以后继续",
	})
	if len(queued) != 1 || queued[0].PendingInput == nil || queued[0].PendingInput.Status != string(state.QueueItemQueued) {
		t.Fatalf("expected queued follow-up input, got %#v", queued)
	}
	for _, event := range queued {
		if event.Command != nil {
			t.Fatalf("expected compact pending to block immediate dispatch, got %#v", queued)
		}
	}

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-compact-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})
	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-compact-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})

	dispatched := false
	for _, event := range completed {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			dispatched = true
		}
	}
	if !dispatched {
		t.Fatalf("expected queued input to dispatch after compact completion, got %#v", completed)
	}
	if svc.turns.compactTurns["inst-1"] != nil {
		t.Fatalf("expected compact binding to be cleared, got %#v", svc.turns.compactTurns["inst-1"])
	}
}

func TestCompactRunningBlocksThreadSwitchAndNewThread(t *testing.T) {
	now := time.Date(2026, 4, 14, 18, 17, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})

	first := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCompact,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	requireCompactStartEvents(t, first)
	svc.BindPendingRemoteCommand("surface-1", "cmd-compact-3")
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-compact-3",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})

	switchEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-2",
	})
	if len(switchEvents) != 1 || switchEvents[0].Notice == nil || switchEvents[0].Notice.Code != "thread_switch_compacting" {
		t.Fatalf("expected thread_switch_compacting notice, got %#v", switchEvents)
	}

	newEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionNewThread,
		SurfaceSessionID: "surface-1",
	})
	if len(newEvents) != 1 || newEvents[0].Notice == nil || newEvents[0].Notice.Code != "new_thread_blocked_compacting" {
		t.Fatalf("expected new_thread_blocked_compacting notice, got %#v", newEvents)
	}
}

func TestDetachDuringCompactWaitsForCompactCompletion(t *testing.T) {
	now := time.Date(2026, 4, 14, 18, 18, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})

	first := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCompact,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	requireCompactStartEvents(t, first)
	svc.BindPendingRemoteCommand("surface-1", "cmd-compact-4")
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-compact-4",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})

	detachEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionDetach,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(detachEvents) != 1 || detachEvents[0].Notice == nil || detachEvents[0].Notice.Code != "detach_pending" {
		t.Fatalf("expected detach_pending notice, got %#v", detachEvents)
	}
	if !svc.root.Surfaces["surface-1"].Abandoning {
		t.Fatalf("expected surface to enter abandoning during compact detach")
	}

	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-compact-4",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})
	gotDetached := false
	for _, event := range completed {
		if event.Notice != nil && event.Notice.Code == "detached" {
			gotDetached = true
			break
		}
	}
	if !gotDetached {
		t.Fatalf("expected detached notice after compact completion, got %#v", completed)
	}
	if svc.root.Surfaces["surface-1"].AttachedInstanceID != "" {
		t.Fatalf("expected surface to detach after compact completion, got %#v", svc.root.Surfaces["surface-1"])
	}
}

func TestCompactStartFailureRestoresQueuedDispatch(t *testing.T) {
	now := time.Date(2026, 4, 14, 18, 20, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})

	first := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCompact,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	requireCompactStartEvents(t, first)
	svc.BindPendingRemoteCommand("surface-1", "cmd-compact-2")

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-after-failed-compact",
		Text:             "失败后继续",
	})

	events := svc.ApplyAgentEvent("inst-1", agentproto.NewSystemErrorEvent(agentproto.ErrorInfo{
		Code:             "compact_start_failed",
		Layer:            "server",
		Stage:            "command_response",
		Operation:        "thread.compact.start",
		Message:          "Codex 拒绝了这次上下文整理请求。",
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-1",
	}))
	dispatched := false
	gotFailedCard := false
	for _, event := range events {
		if catalog, ok := eventCommandCatalog(event); ok && catalog.Title == "上下文压缩失败" && catalog.ThemeKey == "error" {
			gotFailedCard = true
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			dispatched = true
		}
	}
	if !gotFailedCard || !dispatched {
		t.Fatalf("expected compact failure owner card plus queued dispatch, got %#v", events)
	}
	if svc.turns.compactTurns["inst-1"] != nil {
		t.Fatalf("expected compact binding to clear after failure, got %#v", svc.turns.compactTurns["inst-1"])
	}
}

func TestCompactDisconnectClearsBindingAndAllowsRetryAfterReconnect(t *testing.T) {
	now := time.Date(2026, 4, 14, 18, 25, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	startCompactDispatching(t, svc)

	svc.ApplyInstanceDisconnected("inst-1")
	if svc.turns.compactTurns["inst-1"] != nil {
		t.Fatalf("expected disconnect to clear compact binding, got %#v", svc.turns.compactTurns["inst-1"])
	}

	svc.ApplyInstanceConnected("inst-1")
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-1",
	})

	retry := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCompact,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	requireCompactStartEvents(t, retry)
}

func TestCompactTransportDegradedClearsBindingAndAllowsRetryAfterReconnect(t *testing.T) {
	now := time.Date(2026, 4, 14, 18, 26, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	startCompactDispatching(t, svc)

	svc.ApplyInstanceTransportDegraded("inst-1", false)
	if svc.turns.compactTurns["inst-1"] != nil {
		t.Fatalf("expected transport degraded to clear compact binding, got %#v", svc.turns.compactTurns["inst-1"])
	}

	svc.ApplyInstanceConnected("inst-1")
	retry := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCompact,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	requireCompactStartEvents(t, retry)
}

func TestCompactRemoveInstanceClearsBinding(t *testing.T) {
	now := time.Date(2026, 4, 14, 18, 27, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	startCompactDispatching(t, svc)

	svc.RemoveInstance("inst-1")
	if svc.turns.compactTurns["inst-1"] != nil {
		t.Fatalf("expected remove instance to clear compact binding, got %#v", svc.turns.compactTurns["inst-1"])
	}
}

func startCompactDispatching(t *testing.T, svc *Service) {
	t.Helper()
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-1",
	})

	first := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCompact,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	requireCompactStartEvents(t, first)
	svc.BindPendingRemoteCommand("surface-1", "cmd-compact-audit")
}

func TestCompactTurnLifecycleKeepsUpdatingSameOwnerCard(t *testing.T) {
	now := time.Date(2026, 4, 14, 18, 28, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})

	start := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCompact,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	catalog, _ := requireCompactStartEvents(t, start)
	svc.RecordOwnerCardFlowMessage("surface-1", catalog.TrackingKey, "om-compact-1")
	svc.BindPendingRemoteCommand("surface-1", "cmd-compact-5")

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-compact-5",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})
	if len(started) != 1 {
		t.Fatalf("expected one running owner-card update, got %#v", started)
	}
	running := commandCatalogFromEvent(t, started[0])
	if running.MessageID != "om-compact-1" || running.Title != "正在压缩上下文" || running.ThemeKey != "progress" {
		t.Fatalf("unexpected running owner-card update: %#v", running)
	}
	if running.Sealed {
		t.Fatalf("expected running compact card to remain unsealed, got %#v", running)
	}
	if len(running.BodySections) != 1 || running.BodySections[0].Label != "当前会话" {
		t.Fatalf("expected running compact card to preserve business body, got %#v", running.BodySections)
	}
	if len(running.NoticeSections) != 1 {
		t.Fatalf("expected running compact card to keep a single notice section, got %#v", running.NoticeSections)
	}
	if summary := commandCatalogSummaryText(running); !strings.Contains(summary, "正在压缩当前会话的上下文。") || strings.Contains(summary, "压缩期间普通输入会排队") {
		t.Fatalf("expected running compact owner card to keep only the primary ongoing line, got %#v", running)
	}

	completedItem := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-compact-5",
		ItemID:   "compact-5",
		ItemKind: "context_compaction",
	})
	if len(completedItem) != 1 {
		t.Fatalf("expected one completion owner-card update, got %#v", completedItem)
	}
	completedCatalog := commandCatalogFromEvent(t, completedItem[0])
	if completedCatalog.MessageID != "om-compact-1" || completedCatalog.Title != "上下文已压缩" || completedCatalog.ThemeKey != "success" {
		t.Fatalf("unexpected completion owner-card update: %#v", completedCatalog)
	}
	if !completedCatalog.Sealed {
		t.Fatalf("expected completion compact card to be sealed, got %#v", completedCatalog)
	}
	if len(completedCatalog.BodySections) != 1 || completedCatalog.BodySections[0].Label != "当前会话" {
		t.Fatalf("expected completion compact card to preserve body state, got %#v", completedCatalog.BodySections)
	}
	if len(completedCatalog.NoticeSections) != 1 || !strings.Contains(strings.Join(completedCatalog.NoticeSections[0].Lines, "\n"), "当前会话的上下文已压缩完成。") {
		t.Fatalf("expected completion compact card to expose terminal notice, got %#v", completedCatalog.NoticeSections)
	}

	completedTurn := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-compact-5",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})
	for _, event := range completedTurn {
		if _, ok := eventCommandCatalog(event); ok {
			t.Fatalf("expected completion item to avoid duplicate terminal compact card, got %#v", completedTurn)
		}
	}
}

func TestCompactTurnCompletedWithoutCompactionItemFallsBackToTerminalOwnerCard(t *testing.T) {
	now := time.Date(2026, 4, 14, 18, 29, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})

	start := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCompact,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	catalog, _ := requireCompactStartEvents(t, start)
	svc.RecordOwnerCardFlowMessage("surface-1", catalog.TrackingKey, "om-compact-2")
	svc.BindPendingRemoteCommand("surface-1", "cmd-compact-6")

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-compact-6",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})
	completedTurn := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-compact-6",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})
	if len(completedTurn) != 1 {
		t.Fatalf("expected one fallback terminal owner-card update, got %#v", completedTurn)
	}
	completedCatalog := commandCatalogFromEvent(t, completedTurn[0])
	if completedCatalog.MessageID != "om-compact-2" || completedCatalog.Title != "上下文已压缩" || completedCatalog.ThemeKey != "success" {
		t.Fatalf("unexpected fallback terminal owner-card update: %#v", completedCatalog)
	}
	if !completedCatalog.Sealed {
		t.Fatalf("expected fallback terminal compact card to be sealed, got %#v", completedCatalog)
	}
}

func requireCompactStartEvents(t *testing.T, events []control.UIEvent) (*control.FeishuPageView, *agentproto.Command) {
	t.Helper()
	if len(events) != 2 {
		t.Fatalf("expected compact owner card plus agent command, got %#v", events)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if events[1].Command == nil || events[1].Command.Kind != agentproto.CommandThreadCompactStart {
		t.Fatalf("expected compact agent command as second event, got %#v", events)
	}
	return catalog, events[1].Command
}

func newCompactServiceFixture(now *time.Time) *Service {
	svc := newServiceForTest(now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
			"thread-2": {ThreadID: "thread-2", Name: "另一个会话", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	return svc
}
