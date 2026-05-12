package orchestrator

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPreselectedHeadlessLaunchBlocksNormalInput(t *testing.T) {
	now := time.Date(2026, 4, 8, 10, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-offline",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        false,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})
	if len(events) != 2 || events[1].DaemonCommand == nil || events[1].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected detached /use to start headless launch, got %#v", events)
	}

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.PendingHeadless.InstanceID == "" || snapshot.PendingHeadless.Status != string(state.HeadlessLaunchStarting) {
		t.Fatalf("expected pending preselected headless snapshot, got %#v", snapshot)
	}
	blocked := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	if len(blocked) != 1 || blocked[0].Notice == nil || blocked[0].Notice.Code != "headless_starting" {
		t.Fatalf("expected headless_starting notice while preselected launch pending, got %#v", blocked)
	}
}

func TestDetachedUseFreezesHeadlessLaunchContractIntoPendingAndCommand(t *testing.T) {
	now := time.Date(2026, 5, 1, 13, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessClaudeSurfaceBackendContract("devseek"), "", "")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-offline",
		DisplayName:   "repo",
		WorkspaceRoot: "/data/dl/repo",
		WorkspaceKey:  "/data/dl/repo",
		ShortName:     "repo",
		Backend:       agentproto.BackendClaude,
		Online:        false,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/repo", Loaded: true},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})

	if len(events) != 2 || events[1].DaemonCommand == nil || events[1].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected detached /use to start headless launch, got %#v", events)
	}
	pending := svc.root.Surfaces["surface-1"].PendingHeadless
	if pending == nil {
		t.Fatalf("expected pending headless launch, got %#v", svc.root.Surfaces["surface-1"])
	}
	if pending.Backend != agentproto.BackendClaude || pending.CodexProviderID != "" || pending.ClaudeProfileID != "devseek" {
		t.Fatalf("expected pending launch to freeze claude launch contract, got %#v", pending)
	}
	if got := events[1].DaemonCommand; got.Backend != agentproto.BackendClaude || got.CodexProviderID != "" || got.ClaudeProfileID != "devseek" {
		t.Fatalf("expected daemon command to match frozen launch contract, got %#v", got)
	}
}

func TestDetachCancelsPendingHeadlessLaunch(t *testing.T) {
	now := time.Date(2026, 4, 8, 10, 12, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-offline",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        false,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})

	start := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})
	if len(start) != 2 || start[1].DaemonCommand == nil || start[1].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected detached /use to start headless launch, got %#v", start)
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionDetach,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 2 || events[0].DaemonCommand == nil || events[0].DaemonCommand.Kind != control.DaemonCommandKillHeadless || events[1].Notice == nil || events[1].Notice.Code != "detached" {
		t.Fatalf("expected detach to cancel pending launch and reset surface, got %#v", events)
	}
	if !strings.Contains(events[1].Notice.Text, "取消当前恢复流程") {
		t.Fatalf("expected detach cancellation notice, got %#v", events[1].Notice)
	}
	if snapshot := svc.SurfaceSnapshot("surface-1"); snapshot == nil || snapshot.Attachment.InstanceID != "" || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected detach to restore detached snapshot, got %#v", snapshot)
	}
	if surface := svc.root.Surfaces["surface-1"]; surface == nil || surface.RouteMode != state.RouteModeUnbound || surface.AttachedInstanceID != "" || surface.PendingHeadless != nil {
		t.Fatalf("expected detach to restore detached surface state, got %#v", surface)
	}
}

func TestApplyInstanceConnectedCancelsLegacyPendingHeadlessWithoutPreselectedThread(t *testing.T) {
	now := time.Date(2026, 4, 8, 10, 15, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionStatus, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1"})
	svc.root.Surfaces["surface-1"].PendingHeadless = &state.HeadlessLaunchRecord{
		InstanceID:  "inst-headless-1",
		RequestedAt: now,
		ExpiresAt:   now.Add(30 * time.Second),
		Status:      state.HeadlessLaunchStarting,
	}

	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-1",
		DisplayName:   "headless",
		WorkspaceRoot: "/tmp/headless",
		WorkspaceKey:  "/tmp/headless",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	events := svc.ApplyInstanceConnected("inst-headless-1")

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != "" || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected legacy pending headless to be cancelled, got %#v", snapshot)
	}
	var killed bool
	var migrated bool
	for _, event := range events {
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandKillHeadless && event.DaemonCommand.InstanceID == "inst-headless-1" {
			killed = true
		}
		if event.Notice != nil && event.Notice.Code == "legacy_headless_restore_cancelled" && strings.Contains(event.Notice.Text, "/use") {
			migrated = true
		}
	}
	if !killed || !migrated {
		t.Fatalf("expected legacy pending headless cleanup, got %#v", events)
	}
}

func TestDetachTimeoutWatchdogForcesFinalizeAfterRunningTurn(t *testing.T) {
	now := time.Date(2026, 4, 5, 11, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	detach := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionDetach,
		SurfaceSessionID: "surface-1",
	})
	if len(detach) < 2 {
		t.Fatalf("expected interrupt + detach_pending flow, got %#v", detach)
	}
	surface := svc.root.Surfaces["surface-1"]
	if !surface.Abandoning {
		t.Fatalf("expected surface to enter abandoning state")
	}

	now = now.Add(21 * time.Second)
	events := svc.Tick(now)

	surface = svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "" || surface.Abandoning {
		t.Fatalf("expected watchdog to force detach, got %#v", surface)
	}
	if claim := svc.instanceClaims["inst-1"]; claim != nil {
		t.Fatalf("expected instance claim to be released, got %#v", claim)
	}
	var sawForced bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "detach_timeout_forced" {
			sawForced = true
		}
	}
	if !sawForced {
		t.Fatalf("expected detach_timeout_forced notice, got %#v", events)
	}
}

func TestNewThreadReadyDiscardsDraftsAndPreparesCreate(t *testing.T) {
	now := time.Date(2026, 4, 6, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionImageMessage, SurfaceSessionID: "surface-1", MessageID: "msg-img", LocalPath: "/tmp/img.png", MIMEType: "image/png"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionNewThread,
		SurfaceSessionID: "surface-1",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModeNewThreadReady || surface.SelectedThreadID != "" {
		t.Fatalf("expected new_thread_ready without selected thread, got route=%q selected=%q", surface.RouteMode, surface.SelectedThreadID)
	}
	if surface.PreparedThreadCWD != "/data/dl/droid" || surface.PreparedFromThreadID != "thread-1" {
		t.Fatalf("expected prepared cwd/thread to be captured, got %#v", surface)
	}
	if claim := svc.threadClaims["thread-1"]; claim != nil {
		t.Fatalf("expected prior thread claim to be released, got %#v", claim)
	}
	if len(surface.StagedImages) != 0 {
		t.Fatalf("expected staged images to be discarded, got %#v", surface.StagedImages)
	}
	var sawSelection, sawNotice bool
	for _, event := range events {
		if event.ThreadSelection != nil && event.ThreadSelection.RouteMode == string(state.RouteModeNewThreadReady) && event.ThreadSelection.Title == preparedNewThreadSelectionTitle() {
			sawSelection = true
		}
		if event.Notice != nil && event.Notice.Code == "new_thread_ready" {
			sawNotice = true
		}
	}
	if !sawSelection || !sawNotice {
		t.Fatalf("expected new-thread selection change plus notice, got %#v", events)
	}
}

func TestNewThreadReadyFirstTextQueuesCreateAndBlocksSecondInput(t *testing.T) {
	now := time.Date(2026, 4, 6, 10, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionNewThread, SurfaceSessionID: "surface-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "开一个新会话",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.ActiveQueueItemID == "" {
		t.Fatalf("expected active queue item after first /new text, got %#v", surface)
	}
	item := surface.QueueItems[surface.ActiveQueueItemID]
	if item == nil {
		t.Fatal("expected active queue item record")
	}
	if queuedItemExecutionThreadID(item) != "" || queueItemFrozenCWD(item) != "/data/dl/droid" || item.RouteModeAtEnqueue != state.RouteModeNewThreadReady {
		t.Fatalf("expected create-thread queue item, got %#v", item)
	}
	var sawCreateThread bool
	for _, event := range events {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend && event.Command.Target.CreateThreadIfMissing {
			sawCreateThread = true
		}
	}
	if !sawCreateThread {
		t.Fatalf("expected prompt send with CreateThreadIfMissing, got %#v", events)
	}

	blocked := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-2",
		Text:             "第二条不该进来",
	})
	if len(blocked) != 1 || blocked[0].Notice == nil || blocked[0].Notice.Code != "new_thread_first_input_pending" {
		t.Fatalf("expected second input to be blocked, got %#v", blocked)
	}
}

func TestUseThreadFromNewThreadReadyDiscardsQueuedDraft(t *testing.T) {
	now := time.Date(2026, 4, 6, 11, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionNewThread, SurfaceSessionID: "surface-1"})
	surface := svc.root.Surfaces["surface-1"]
	surface.DispatchMode = state.DispatchModePausedForLocal
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface-1", MessageID: "msg-1", Text: "先排队一个新会话"})

	var queued *state.QueueItemRecord
	for _, item := range surface.QueueItems {
		if item.Status == state.QueueItemQueued {
			queued = item
			break
		}
	}
	if queued == nil {
		t.Fatalf("expected queued create-thread item, got %#v", surface.QueueItems)
	}

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-2",
	})

	if surface.RouteMode != state.RouteModePinned || surface.SelectedThreadID != "thread-2" {
		t.Fatalf("expected surface to switch to thread-2, got route=%q selected=%q", surface.RouteMode, surface.SelectedThreadID)
	}
	if queued.Status != state.QueueItemDiscarded {
		t.Fatalf("expected queued create-thread item to be discarded, got %#v", queued)
	}
}

func TestRepeatNewThreadReadyDiscardsQueuedDraft(t *testing.T) {
	now := time.Date(2026, 4, 6, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionNewThread, SurfaceSessionID: "surface-1"})
	surface := svc.root.Surfaces["surface-1"]
	surface.DispatchMode = state.DispatchModePausedForLocal
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface-1", MessageID: "msg-1", Text: "先排队"})

	var queued *state.QueueItemRecord
	for _, item := range surface.QueueItems {
		if item.Status == state.QueueItemQueued {
			queued = item
			break
		}
	}
	if queued == nil {
		t.Fatalf("expected queued create-thread item, got %#v", surface.QueueItems)
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionNewThread,
		SurfaceSessionID: "surface-1",
	})

	if surface.RouteMode != state.RouteModeNewThreadReady || surface.PreparedThreadCWD != "/data/dl/droid" {
		t.Fatalf("expected surface to stay new_thread_ready, got %#v", surface)
	}
	if queued.Status != state.QueueItemDiscarded {
		t.Fatalf("expected queued draft to be discarded, got %#v", queued)
	}
	if len(events) == 0 || events[len(events)-1].Notice == nil || events[len(events)-1].Notice.Code != "new_thread_ready_reset" {
		t.Fatalf("expected reset notice, got %#v", events)
	}
}

func TestNewThreadRejectedDuringRequestCapture(t *testing.T) {
	now := time.Date(2026, 4, 6, 12, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.root.Surfaces["surface-1"].ActiveRequestCapture = &state.RequestCaptureRecord{
		RequestID: "req-1",
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionNewThread,
		SurfaceSessionID: "surface-1",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "request_capture_waiting_text" {
		t.Fatalf("expected request capture gate to reject /new, got %#v", events)
	}
}

func TestNewThreadReadyNormalWorkspaceAttachedWithoutBoundThread(t *testing.T) {
	now := time.Date(2026, 4, 6, 12, 45, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", WorkspaceKey: "/data/dl/droid"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionNewThread,
		SurfaceSessionID: "surface-1",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModeNewThreadReady || surface.SelectedThreadID != "" {
		t.Fatalf("expected workspace-attached /new to enter new_thread_ready without selected thread, got %#v", surface)
	}
	if surface.PreparedThreadCWD != "/data/dl/droid" || surface.PreparedFromThreadID != "" {
		t.Fatalf("expected normal-mode /new to use workspace ownership, got %#v", surface)
	}
	if len(events) == 0 || events[len(events)-1].Notice == nil || events[len(events)-1].Notice.Code != "new_thread_ready" {
		t.Fatalf("expected /new readiness notice, got %#v", events)
	}
}

func TestNewThreadRejectedInVSCodeMode(t *testing.T) {
	now := time.Date(2026, 4, 9, 15, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Source:                  "vscode",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionImageMessage, SurfaceSessionID: "surface-1", MessageID: "msg-img", LocalPath: "/tmp/img.png", MIMEType: "image/png"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionNewThread,
		SurfaceSessionID: "surface-1",
	})

	surface := svc.root.Surfaces["surface-1"]
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "new_thread_disabled_vscode" {
		t.Fatalf("expected vscode /new to reject, got %#v", events)
	}
	if surface.RouteMode != state.RouteModeFollowLocal || surface.SelectedThreadID != "thread-1" {
		t.Fatalf("expected vscode /new to preserve current follow state, got %#v", surface)
	}
	if surface.PreparedThreadCWD != "" || surface.PreparedFromThreadID != "" {
		t.Fatalf("expected vscode /new not to prepare new-thread state, got %#v", surface)
	}
	if len(surface.StagedImages) != 1 {
		t.Fatalf("expected vscode /new reject not to discard staged images, got %#v", surface.StagedImages)
	}
}

func TestTextMessageNormalWorkspaceUnboundImplicitlyCreatesNewThread(t *testing.T) {
	now := time.Date(2026, 4, 6, 12, 50, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", WorkspaceKey: "/data/dl/droid"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "直接发一条消息",
	})

	sawPromptSend := false
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "thread_unbound" {
			t.Fatalf("expected implicit new-thread enqueue instead of thread_unbound, got %#v", events)
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			sawPromptSend = true
			if !event.Command.Target.CreateThreadIfMissing {
				t.Fatalf("expected prompt send to create thread, got %#v", event.Command.Target)
			}
			if event.Command.Target.CWD != "/data/dl/droid" {
				t.Fatalf("expected prompt send cwd to use attached workspace, got %#v", event.Command.Target)
			}
		}
	}
	if !sawPromptSend {
		t.Fatalf("expected text to enqueue and dispatch prompt send, got %#v", events)
	}

	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModeNewThreadReady || surface.SelectedThreadID != "" {
		t.Fatalf("expected implicit path to enter new_thread_ready before turn starts, got %#v", surface)
	}
	if strings.TrimSpace(surface.PreparedThreadCWD) != "/data/dl/droid" {
		t.Fatalf("expected prepared cwd from workspace claim, got %#v", surface)
	}
	if surface.ActiveQueueItemID == "" {
		t.Fatalf("expected implicit first input to be active, got %#v", surface)
	}
	item := surface.QueueItems[surface.ActiveQueueItemID]
	if item == nil || item.RouteModeAtEnqueue != state.RouteModeNewThreadReady || queuedItemExecutionThreadID(item) != "" || strings.TrimSpace(queueItemFrozenCWD(item)) != "/data/dl/droid" {
		t.Fatalf("expected queued first input to freeze new_thread_ready semantics, got %#v", item)
	}

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-new",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})

	surface = svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModeNewThreadReady || surface.SelectedThreadID != "" {
		t.Fatalf("expected turn.started to keep prepared state until completion, got %#v", surface)
	}
}

func TestImageThenTextNormalWorkspaceUnboundUsesImplicitNewThreadFlow(t *testing.T) {
	now := time.Date(2026, 4, 6, 12, 52, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", WorkspaceKey: "/data/dl/droid"})

	imageEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionImageMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-img",
		LocalPath:        "/tmp/diagram.png",
		MIMEType:         "image/png",
	})
	if len(imageEvents) != 1 || imageEvents[0].PendingInput == nil || imageEvents[0].PendingInput.Status != string(state.ImageStaged) {
		t.Fatalf("expected unbound image to stage input via implicit new-thread-ready route, got %#v", imageEvents)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModeNewThreadReady || strings.TrimSpace(surface.PreparedThreadCWD) != "/data/dl/droid" {
		t.Fatalf("expected image staging to prepare new-thread-ready route, got %#v", surface)
	}
	if surface.ActiveQueueItemID != "" || len(surface.QueueItems) != 0 {
		t.Fatalf("expected image-only input not to create remote turn yet, got %#v", surface)
	}

	textEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-text",
		Text:             "结合图片给建议",
	})
	sawPromptSend := false
	for _, event := range textEvents {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			sawPromptSend = true
			if !event.Command.Target.CreateThreadIfMissing || event.Command.Target.CWD != "/data/dl/droid" {
				t.Fatalf("expected staged image follow-up text to create thread in workspace, got %#v", event.Command.Target)
			}
		}
	}
	if !sawPromptSend {
		t.Fatalf("expected staged image + first text to dispatch prompt send, got %#v", textEvents)
	}

	surface = svc.root.Surfaces["surface-1"]
	if surface.ActiveQueueItemID == "" {
		t.Fatalf("expected active create-thread queue item after first text, got %#v", surface)
	}
	item := surface.QueueItems[surface.ActiveQueueItemID]
	if item == nil || len(item.Inputs) != 2 || item.Inputs[0].Type != agentproto.InputLocalImage || item.Inputs[1].Type != agentproto.InputText {
		t.Fatalf("expected first create-thread input to include staged image + text, got %#v", item)
	}
}

func TestShowThreadsDetachedShowsGlobalMergedRecentThreads(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1":      {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", LastUsedAt: now.Add(1 * time.Minute)},
			"thread-review": {ThreadID: "thread-review", Name: "审阅结果", CWD: "/data/dl/droid", LastUsedAt: now.Add(3 * time.Minute), Source: &agentproto.ThreadSourceRecord{Kind: agentproto.ThreadSourceKindReview, ParentThreadID: "thread-1"}},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-2",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        false,
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "整理样式", CWD: "/data/dl/web", LastUsedAt: now.Add(2 * time.Minute)},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected detached /use to open target picker, got %#v", events)
	}
	view := targetPickerFromEvent(t, events[0])
	if view.Source != control.TargetPickerRequestSourceUse || len(view.WorkspaceOptions) != 2 {
		t.Fatalf("unexpected target picker: %#v", view)
	}
	if view.SelectedWorkspaceKey != "/data/dl/web" {
		t.Fatalf("expected most-recent workspace to be selected first, got %#v", view)
	}
	if _, ok := targetPickerSessionOption(view, targetPickerThreadValue("thread-2")); !ok {
		t.Fatalf("expected offline workspace thread to stay visible for recovery, got %#v", view.SessionOptions)
	}
	if _, ok := targetPickerWorkspaceOption(view, "/data/dl/droid"); !ok {
		t.Fatalf("expected online workspace to remain visible, got %#v", view.WorkspaceOptions)
	}
}

func TestShowThreadsDetachedIncludesPersistedThreadsFromRecoverableWorkspaces(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-sqlite-1",
		DisplayName:   "sqlite",
		WorkspaceRoot: "/data/dl/sqlite",
		WorkspaceKey:  "/data/dl/sqlite",
		ShortName:     "sqlite",
		Online:        true,
	})
	svc.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{
		recent: []state.ThreadRecord{
			{ThreadID: "thread-persisted", Name: "数据库里的新会话", Preview: "sqlite freshness", CWD: "/data/dl/sqlite", Loaded: true, LastUsedAt: now.Add(2 * time.Minute)},
			{ThreadID: "thread-older", Name: "更旧的会话", CWD: "/data/dl/older", Loaded: true, LastUsedAt: now.Add(1 * time.Minute)},
		},
		byID: map[string]state.ThreadRecord{},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected detached /use target picker, got %#v", events)
	}
	view := targetPickerFromEvent(t, events[0])
	if len(view.WorkspaceOptions) != 2 {
		t.Fatalf("expected persisted recoverable workspaces to appear in picker, got %#v", view.WorkspaceOptions)
	}
	if view.SelectedWorkspaceKey != "/data/dl/sqlite" {
		t.Fatalf("expected newest persisted workspace first, got %#v", view)
	}
	if _, ok := targetPickerWorkspaceOption(view, "/data/dl/sqlite"); !ok {
		t.Fatalf("expected persisted workspace metadata to render in picker, got %#v", view.WorkspaceOptions)
	}
	if _, ok := targetPickerWorkspaceOption(view, "/data/dl/older"); !ok {
		t.Fatalf("expected persisted-only recoverable workspace to remain visible, got %#v", view.WorkspaceOptions)
	}
}

func TestShowThreadsDetachedShowsPersistedThreadsWhenOnlyRecoverableWorkspacesExist(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 6, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{
		recent: []state.ThreadRecord{
			{ThreadID: "thread-persisted", Name: "数据库里的新会话", Preview: "sqlite freshness", CWD: "/data/dl/sqlite", Loaded: true, LastUsedAt: now.Add(2 * time.Minute)},
		},
		byID: map[string]state.ThreadRecord{},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected persisted-only recoverable workspace to produce target picker, got %#v", events)
	}
	view := targetPickerFromEvent(t, events[0])
	if len(view.WorkspaceOptions) != 1 || view.SelectedWorkspaceKey != "/data/dl/sqlite" {
		t.Fatalf("expected persisted-only recoverable workspace to remain selectable, got %#v", view)
	}
	if _, ok := targetPickerSessionOption(view, targetPickerThreadValue("thread-persisted")); !ok {
		t.Fatalf("expected persisted-only recoverable thread to remain selectable, got %#v", view.SessionOptions)
	}
}

func TestShowThreadsDetachedFallsBackWhenPersistedReaderFails(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 7, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{
		recentErr: errors.New("sqlite unavailable"),
		byID:      map[string]state.ThreadRecord{},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true, LastUsedAt: now.Add(1 * time.Minute)},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected fallback target picker, got %#v", events)
	}
	view := targetPickerFromEvent(t, events[0])
	if len(view.WorkspaceOptions) != 1 || view.SelectedWorkspaceKey != "/data/dl/droid" {
		t.Fatalf("expected catalog-only fallback workspace, got %#v", view)
	}
	if _, ok := targetPickerSessionOption(view, targetPickerThreadValue("thread-1")); !ok {
		t.Fatalf("expected catalog-only fallback thread, got %#v", view.SessionOptions)
	}
}

func TestShowThreadsDetachedPrefersPersistedFreshMetadataForVisibleThread(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 8, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{
		recent: []state.ThreadRecord{
			{ThreadID: "thread-1", Name: "数据库里的新标题", Preview: "数据库里的摘要", CWD: "/data/dl/droid", Loaded: true, LastUsedAt: now.Add(3 * time.Minute)},
		},
		byID: map[string]state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "数据库里的新标题", Preview: "数据库里的摘要", CWD: "/data/dl/droid", Loaded: true, LastUsedAt: now.Add(3 * time.Minute)},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "旧标题", CWD: "/data/dl/droid", Loaded: true, LastUsedAt: now.Add(1 * time.Minute)},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected target picker, got %#v", events)
	}
	view := targetPickerFromEvent(t, events[0])
	option, ok := targetPickerSessionOption(view, targetPickerThreadValue("thread-1"))
	if !ok || option.Label != "droid · 数据库里的新标题" || option.MetaText != "" {
		t.Fatalf("expected persisted freshness to improve visible thread metadata without changing attach mode, got %#v", option)
	}
}

func TestPresentGlobalThreadSelectionMarksBusyThreadDisabled(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-free": {ThreadID: "thread-free", Name: "空闲会话", CWD: "/data/dl/droid", LastUsedAt: now.Add(1 * time.Minute)},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-2",
		DisplayName:             "web",
		WorkspaceRoot:           "/data/dl/web",
		WorkspaceKey:            "/data/dl/web",
		ShortName:               "web",
		Online:                  true,
		ObservedFocusedThreadID: "thread-busy",
		Threads: map[string]*state.ThreadRecord{
			"thread-busy": {ThreadID: "thread-busy", Name: "忙碌会话", CWD: "/data/dl/web", LastUsedAt: now.Add(2 * time.Minute)},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-busy", ChatID: "chat-busy", ActorUserID: "user-busy", InstanceID: "inst-2"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected target picker, got %#v", events)
	}
	view := targetPickerFromEvent(t, events[0])
	if _, ok := targetPickerWorkspaceOption(view, "/data/dl/web"); ok {
		t.Fatalf("expected busy workspace to be omitted from target picker, got %#v", view.WorkspaceOptions)
	}
	if _, ok := targetPickerWorkspaceOption(view, "/data/dl/droid"); !ok {
		t.Fatalf("expected free workspace to remain visible, got %#v", view.WorkspaceOptions)
	}
}

func TestShowThreadsAttachedNormalFiltersToCurrentWorkspace(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 15, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-droid-1",
		DisplayName:   "droid-a",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid-a",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "当前实例会话", CWD: "/data/dl/droid", LastUsedAt: now.Add(2 * time.Minute)},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-droid-2",
		DisplayName:   "droid-b",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid-b",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "同工作区另一实例", CWD: "/data/dl/droid", LastUsedAt: now.Add(1 * time.Minute)},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-web-1",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-3": {ThreadID: "thread-3", Name: "其他工作区会话", CWD: "/data/dl/web", LastUsedAt: now.Add(3 * time.Minute)},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", WorkspaceKey: "/data/dl/droid"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected target picker, got %#v", events)
	}
	view := targetPickerFromEvent(t, events[0])
	if view.SelectedWorkspaceKey != "/data/dl/droid" {
		t.Fatalf("expected current workspace to remain selected, got %#v", view)
	}
	if len(view.SessionOptions) != 2 {
		t.Fatalf("expected current workspace threads only, got %#v", view.SessionOptions)
	}
	if _, ok := targetPickerSessionOption(view, targetPickerThreadValue("thread-3")); ok {
		t.Fatalf("expected other-workspace thread to be filtered out, got %#v", view.SessionOptions)
	}
}

func TestShowAllThreadsAttachedNormalShowsCrossWorkspaceSessions(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 16, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-droid-1",
		DisplayName:   "droid-a",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid-a",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "当前工作区会话", CWD: "/data/dl/droid", LastUsedAt: now.Add(2 * time.Minute)},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-web-1",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-3": {ThreadID: "thread-3", Name: "其他工作区会话", CWD: "/data/dl/web", LastUsedAt: now.Add(3 * time.Minute)},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", WorkspaceKey: "/data/dl/droid"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowAllThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected target picker, got %#v", events)
	}
	view := targetPickerFromEvent(t, events[0])
	if view.Source != control.TargetPickerRequestSourceUseAll || view.SelectedWorkspaceKey != "/data/dl/droid" {
		t.Fatalf("expected attached /useall to open current workspace picker, got %#v", view)
	}
	if _, ok := targetPickerWorkspaceOption(view, "/data/dl/web"); !ok {
		t.Fatalf("expected other workspace to appear in /useall picker, got %#v", view.WorkspaceOptions)
	}
}

func TestUseThreadAttachedNormalAllowsCrossWorkspaceSelectionWhenRequested(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 16, 30, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-droid-1",
		DisplayName:   "droid-a",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid-a",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "当前工作区会话", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-web-1",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-3": {ThreadID: "thread-3", Name: "其他工作区会话", CWD: "/data/dl/web", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", WorkspaceKey: "/data/dl/droid"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:                control.ActionUseThread,
		SurfaceSessionID:    "surface-1",
		ThreadID:            "thread-3",
		AllowCrossWorkspace: true,
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "inst-web-1" || surface.ClaimedWorkspaceKey != "/data/dl/web" || surface.SelectedThreadID != "thread-3" {
		t.Fatalf("expected cross-workspace /useall selection to rebind workspace, got %#v", surface)
	}
	var sawAttached bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "attached" && strings.Contains(event.Notice.Text, "其他工作区会话") {
			sawAttached = true
		}
	}
	if !sawAttached {
		t.Fatalf("expected reattach notice for cross-workspace thread selection, got %#v", events)
	}
}

func TestShowThreadsAttachedVSCodeFiltersToCurrentInstance(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 17, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	instCurrent := &state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "当前实例会话", CWD: "/data/dl/droid", LastUsedAt: now.Add(-3 * time.Minute)},
			"thread-2": {ThreadID: "thread-2", Name: "当前实例旧会话", CWD: "/data/dl/droid", LastUsedAt: now.Add(-2 * time.Minute)},
		},
	}
	svc.UpsertInstance(instCurrent)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-2",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-3": {ThreadID: "thread-3", Name: "其他实例会话", CWD: "/data/dl/web", LastUsedAt: now.Add(-4 * time.Minute)},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	instCurrent.ObservedFocusedThreadID = "thread-2"

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected vscode prompt scoped to current instance, got %#v", events)
	}
	view := selectionViewFromEvent(t, events[0])
	ctx := selectionContextFromEvent(t, events[0])
	if view.Thread == nil || ctx.Layout != "vscode_instance_threads" || ctx.ContextTitle != "当前实例" || !strings.Contains(ctx.ContextText, "droid · 当前跟随中") {
		t.Fatalf("expected vscode /use prompt to expose current instance summary, got view=%#v ctx=%#v", view, ctx)
	}
	if view.Thread.CurrentInstance == nil || view.Thread.CurrentInstance.Label != "droid" || view.Thread.CurrentInstance.Status != "当前跟随中" {
		t.Fatalf("expected current instance summary in structured view, got %#v", view.Thread.CurrentInstance)
	}
	if len(view.Thread.Entries) != 2 {
		t.Fatalf("expected only current instance threads, got %#v", view.Thread.Entries)
	}
	for _, entry := range view.Thread.Entries {
		if entry.ThreadID == "thread-3" {
			t.Fatalf("expected other instance thread to be filtered out, got %#v", view.Thread.Entries)
		}
	}
	if view.Thread.Entries[0].ThreadID != "thread-2" || view.Thread.Entries[0].Summary != "droid · 当前实例旧会话" || !view.Thread.Entries[0].VSCodeFocused || view.Thread.Entries[0].AgeText != "2分前" {
		t.Fatalf("expected non-current focus thread first with focus hint, got %#v", view.Thread.Entries[0])
	}
	if view.Thread.Entries[1].ThreadID != "thread-1" || view.Thread.Entries[1].Summary != "droid · 当前实例会话" || !view.Thread.Entries[1].Current || view.Thread.Entries[1].AgeText != "3分前" {
		t.Fatalf("expected current vscode thread to keep instance-scoped summary/meta, got %#v", view.Thread.Entries[1])
	}
}

func TestShowAllThreadsAttachedVSCodeShowsCurrentInstanceAllSessions(t *testing.T) {
	now := time.Date(2026, 4, 10, 14, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	instCurrent := &state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Source:                  "vscode",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1":      {ThreadID: "thread-1", Name: "当前实例会话", CWD: "/data/dl/droid", LastUsedAt: now.Add(-3 * time.Minute)},
			"thread-2":      {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid", LastUsedAt: now.Add(-1 * time.Minute)},
			"thread-review": {ThreadID: "thread-review", Name: "审阅结果", CWD: "/data/dl/droid", LastUsedAt: now.Add(-30 * time.Second), Source: &agentproto.ThreadSourceRecord{Kind: agentproto.ThreadSourceKindReview, ParentThreadID: "thread-2"}},
		},
	}
	svc.UpsertInstance(instCurrent)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-2",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Source:        "vscode",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-3": {ThreadID: "thread-3", Name: "其他实例会话", CWD: "/data/dl/web", LastUsedAt: now.Add(-30 * time.Second)},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	instCurrent.ObservedFocusedThreadID = "thread-2"

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowAllThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected selection prompt, got %#v", events)
	}
	view := selectionViewFromEvent(t, events[0])
	ctx := selectionContextFromEvent(t, events[0])
	if view.Thread == nil || view.Thread.Mode != control.FeishuThreadSelectionVSCodeAll || ctx.Title != "当前实例全部会话" || ctx.Layout != "vscode_instance_threads" {
		t.Fatalf("expected instance-scoped vscode /useall selection view, got view=%#v ctx=%#v", view, ctx)
	}
	if ctx.ContextTitle != "当前实例" || ctx.ContextText != "droid · 当前跟随中" {
		t.Fatalf("expected current instance summary, got ctx=%#v", ctx)
	}
	if view.Thread.CurrentInstance == nil || view.Thread.CurrentInstance.Label != "droid" || view.Thread.CurrentInstance.Status != "当前跟随中" {
		t.Fatalf("expected current instance summary in structured view, got %#v", view.Thread.CurrentInstance)
	}
	if len(view.Thread.Entries) != 2 || view.Thread.Entries[0].ThreadID != "thread-2" || view.Thread.Entries[1].ThreadID != "thread-1" {
		t.Fatalf("expected only current instance sessions in recency order, got %#v", view.Thread.Entries)
	}
	for _, entry := range view.Thread.Entries {
		if entry.ThreadID == "thread-review" {
			t.Fatalf("expected review thread to stay out of normal vscode /useall, got %#v", view.Thread.Entries)
		}
	}
	if view.Thread.Entries[0].Summary != "droid · 整理日志" || !view.Thread.Entries[0].VSCodeFocused || view.Thread.Entries[0].AgeText != "1分前" {
		t.Fatalf("expected focused non-current thread metadata, got %#v", view.Thread.Entries[0])
	}
	if view.Thread.Entries[1].Summary != "droid · 当前实例会话" || !view.Thread.Entries[1].Current || view.Thread.Entries[1].AgeText != "3分前" {
		t.Fatalf("expected current thread metadata, got %#v", view.Thread.Entries[1])
	}
	if view.Thread.Page != 1 || view.Thread.TotalPages != 1 {
		t.Fatalf("expected paged vscode all-thread metadata, got %#v", view.Thread)
	}
}

func TestVSCodeMigrateCommandActionDispatchesDaemonCommand(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 17, 30, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionVSCodeMigrateCommand,
		Text:             "/vscode-migrate",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 || events[0].DaemonCommand == nil {
		t.Fatalf("expected one daemon command event, got %#v", events)
	}
	if events[0].DaemonCommand.Kind != control.DaemonCommandVSCodeMigrateCommand {
		t.Fatalf("expected vscode migrate command daemon command, got %#v", events[0].DaemonCommand)
	}
}

func TestVSCodeMigrateOwnerFlowActionDispatchesDaemonCommand(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 17, 30, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionVSCodeMigrate,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		OwnerFlow:        testOwnerFlow("flow-1", "run"),
	})

	if len(events) != 1 || events[0].DaemonCommand == nil {
		t.Fatalf("expected one daemon command event, got %#v", events)
	}
	if events[0].DaemonCommand.Kind != control.DaemonCommandVSCodeMigrate {
		t.Fatalf("expected vscode migrate daemon command, got %#v", events[0].DaemonCommand)
	}
}

func TestShowThreadsDetachedVSCodeRequiresAttach(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 18, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "not_attached_vscode" {
		t.Fatalf("expected detached vscode /use to require /list first, got %#v", events)
	}
}

func TestUseThreadDetachedAttachesFreeVisibleInstanceAndReplays(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})

	recordLocalFinalText(t, svc, "inst-1", "thread-1", "turn-1", "item-1", "全局 /use 的 replay")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != "inst-1" || snapshot.Attachment.SelectedThreadID != "thread-1" {
		t.Fatalf("expected detached /use to attach visible instance and thread, got %#v", snapshot)
	}
	var sawAttached, sawReplay bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "attached" && strings.Contains(event.Notice.Text, "已接管工作区 /data/dl/droid") && !strings.Contains(event.Notice.Text, "已接管 droid") {
			sawAttached = true
		}
		if event.Block != nil && event.Block.Text == "全局 /use 的 replay" && event.Block.Final {
			sawReplay = true
		}
	}
	if !sawAttached {
		t.Fatalf("expected global /use attach notice to stay workspace-first, got %#v", events)
	}
	if !sawReplay {
		t.Fatalf("expected global /use attach to replay stored final, got %#v", events)
	}
}

func TestUseThreadAttachedNormalRejectsCrossWorkspaceSelection(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 25, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-2",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "整理样式", CWD: "/data/dl/web", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", WorkspaceKey: "/data/dl/droid"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-2",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "thread_outside_workspace" {
		t.Fatalf("expected normal attached /use to reject other workspace, got %#v", events)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "inst-1" || surface.ClaimedWorkspaceKey != "/data/dl/droid" || surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected surface to stay on current workspace without rebinding, got %#v", surface)
	}
}

func TestUseThreadAttachedNormalIgnoresPollutedCurrentInstanceThreadVisibility(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 27, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-picdetect",
		DisplayName:   "picdetect",
		WorkspaceRoot: "/data/dl/picdetect",
		WorkspaceKey:  "/data/dl/picdetect",
		ShortName:     "picdetect",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-pic": {ThreadID: "thread-pic", Name: "图片检测", CWD: "/data/dl/picdetect", Loaded: true},
			"thread-fs":  {ThreadID: "thread-fs", Name: "整理 atlas", CWD: "/data/dl/atlas", Loaded: true},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-atlas",
		DisplayName:   "atlas",
		WorkspaceRoot: "/data/dl/atlas",
		WorkspaceKey:  "/data/dl/atlas",
		ShortName:     "atlas",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-fs": {ThreadID: "thread-fs", Name: "整理 atlas", CWD: "/data/dl/atlas", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/picdetect",
	})

	svc.ApplySurfaceAction(control.Action{
		Kind:                control.ActionUseThread,
		SurfaceSessionID:    "surface-1",
		ChatID:              "chat-1",
		ActorUserID:         "user-1",
		ThreadID:            "thread-fs",
		AllowCrossWorkspace: true,
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "inst-atlas" || surface.ClaimedWorkspaceKey != "/data/dl/atlas" || surface.SelectedThreadID != "thread-fs" {
		t.Fatalf("expected cross-workspace /use to reattach to the correct workspace instance, got %#v", surface)
	}
	if snapshot := svc.SurfaceSnapshot("surface-1"); snapshot == nil || snapshot.WorkspaceKey != "/data/dl/atlas" || snapshot.Attachment.InstanceID != "inst-atlas" {
		t.Fatalf("expected snapshot to stay aligned with selected thread workspace, got %#v", snapshot)
	}
}

func TestUseThreadAttachedHeadlessPersistedCrossWorkspaceStartsNewHeadless(t *testing.T) {
	now := time.Date(2026, 5, 3, 15, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendClaude, "profile-a", "", "")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:      "inst-claude-a",
		DisplayName:     "repo-a",
		WorkspaceRoot:   "/data/dl/repo-a",
		WorkspaceKey:    "/data/dl/repo-a",
		ShortName:       "repo-a",
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "profile-a",
		Source:          "headless",
		Managed:         true,
		Online:          true,
		Threads: map[string]*state.ThreadRecord{
			"thread-a": {ThreadID: "thread-a", Name: "当前会话", CWD: "/data/dl/repo-a", Loaded: true},
		},
	})
	svc.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{
		byIDByBackend: map[agentproto.Backend]map[string]state.ThreadRecord{
			agentproto.BackendClaude: {
				"thread-b": {
					ThreadID:   "thread-b",
					Name:       "其他工作区会话",
					Preview:    "来自 persisted catalog",
					CWD:        "/data/dl/repo-b",
					Loaded:     true,
					LastUsedAt: now.Add(-1 * time.Minute),
				},
			},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/repo-a",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-a",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:                control.ActionUseThread,
		SurfaceSessionID:    "surface-1",
		ChatID:              "chat-1",
		ActorUserID:         "user-1",
		ThreadID:            "thread-b",
		AllowCrossWorkspace: true,
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "" || surface.PendingHeadless == nil {
		t.Fatalf("expected persisted cross-workspace switch to detach current instance and start pending headless, got %#v", surface)
	}
	if surface.PendingHeadless.ThreadID != "thread-b" || surface.PendingHeadless.ThreadCWD != "/data/dl/repo-b" {
		t.Fatalf("expected pending headless to target repo-b thread, got %#v", surface.PendingHeadless)
	}
	if surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound || surface.ClaimedWorkspaceKey != "/data/dl/repo-b" {
		t.Fatalf("expected surface to wait unbound on repo-b instead of reusing repo-a instance, got %#v", surface)
	}
	var sawStartHeadless bool
	for _, event := range events {
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandStartHeadless {
			sawStartHeadless = true
			break
		}
	}
	if !sawStartHeadless {
		t.Fatalf("expected persisted cross-workspace switch to start headless restore, got %#v", events)
	}
}

func TestUseThreadDetachedPrefersMatchingVisibleWorkspaceInstance(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 28, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-picdetect",
		DisplayName:   "picdetect",
		WorkspaceRoot: "/data/dl/picdetect",
		WorkspaceKey:  "/data/dl/picdetect",
		ShortName:     "picdetect",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-fs": {ThreadID: "thread-fs", Name: "整理 atlas", CWD: "/data/dl/atlas", Loaded: true},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-atlas",
		DisplayName:   "atlas",
		WorkspaceRoot: "/data/dl/atlas",
		WorkspaceKey:  "/data/dl/atlas",
		ShortName:     "atlas",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-fs": {ThreadID: "thread-fs", Name: "整理 atlas", CWD: "/data/dl/atlas", Loaded: true},
		},
	})

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-fs",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "inst-atlas" || surface.ClaimedWorkspaceKey != "/data/dl/atlas" || surface.SelectedThreadID != "thread-fs" {
		t.Fatalf("expected detached /use to attach the matching workspace instance, got %#v", surface)
	}
}

func TestUseThreadDetachedRejectsInVSCodeMode(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-1",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "not_attached_vscode" {
		t.Fatalf("expected detached vscode /use to reject until /list attach, got %#v", events)
	}
}

func TestUseThreadAttachedVSCodeRejectsCrossInstanceSelection(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
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
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-2",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "整理样式", CWD: "/data/dl/web", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionImageMessage, SurfaceSessionID: "surface-1", MessageID: "msg-img", LocalPath: "/tmp/img.png", MIMEType: "image/png"})
	svc.root.Surfaces["surface-1"].PromptOverride = state.ModelConfigRecord{Model: "gpt-5.4"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-2",
	})

	surface := svc.root.Surfaces["surface-1"]
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "thread_outside_instance" {
		t.Fatalf("expected attached vscode /use to reject cross-instance thread, got %#v", events)
	}
	if surface.AttachedInstanceID != "inst-1" || surface.SelectedThreadID != "thread-1" || surface.RouteMode != state.RouteModeFollowLocal {
		t.Fatalf("expected attached vscode surface to stay on current instance, got %#v", surface)
	}
	if surface.PromptOverride != (state.ModelConfigRecord{Model: "gpt-5.4"}) {
		t.Fatalf("expected invalid cross-instance /use not to clear prompt override, got %#v", surface.PromptOverride)
	}
	if len(surface.StagedImages) != 1 {
		t.Fatalf("expected invalid cross-instance /use not to discard staged images, got %#v", surface.StagedImages)
	}
	if svc.instanceClaimSurface("inst-1") == nil || svc.instanceClaimSurface("inst-2") != nil {
		t.Fatalf("expected instance claim to remain on current instance")
	}
}

func TestUseThreadCrossInstanceBlockedByPendingRequestInVSCodeMode(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 35, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
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
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-2",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "整理样式", CWD: "/data/dl/web", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.root.Surfaces["surface-1"].PendingRequests["req-1"] = &state.RequestPromptRecord{RequestID: "req-1"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-2",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "thread_outside_instance" {
		t.Fatalf("expected vscode cross-instance /use to reject before retargeting, got %#v", events)
	}
	if surface := svc.root.Surfaces["surface-1"]; surface.AttachedInstanceID != "inst-1" || surface.SelectedThreadID != "thread-1" || surface.RouteMode != state.RouteModeFollowLocal {
		t.Fatalf("expected surface to remain on original attachment, got %#v", surface)
	}
}

func TestUseThreadAttachedVSCodeKeepsFollowLocalUntilObservedFocusChanges(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 36, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
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
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid", Loaded: true},
			"thread-3": {ThreadID: "thread-3", Name: "新的焦点", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-2",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.SelectedThreadID != "thread-2" || surface.RouteMode != state.RouteModeFollowLocal {
		t.Fatalf("expected vscode force-pick to keep follow-local route, got %#v", surface)
	}
	var sawFollowLocalSelection bool
	for _, event := range events {
		if event.ThreadSelection != nil && event.ThreadSelection.ThreadID == "thread-2" && event.ThreadSelection.RouteMode == string(state.RouteModeFollowLocal) {
			sawFollowLocalSelection = true
		}
	}
	if !sawFollowLocalSelection {
		t.Fatalf("expected vscode force-pick to announce follow-local selection, got %#v", events)
	}

	events = svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventLocalInteractionObserved,
		ThreadID: "thread-3",
		CWD:      "/data/dl/droid",
		Action:   "turn_start",
	})

	if surface.SelectedThreadID != "thread-3" || surface.RouteMode != state.RouteModeFollowLocal {
		t.Fatalf("expected later observed focus to override one-shot pick, got %#v", surface)
	}
	var sawRetarget bool
	for _, event := range events {
		if event.ThreadSelection != nil && event.ThreadSelection.ThreadID == "thread-3" && event.ThreadSelection.RouteMode == string(state.RouteModeFollowLocal) {
			sawRetarget = true
		}
	}
	if !sawRetarget {
		t.Fatalf("expected follow-local retarget after later observed focus, got %#v", events)
	}
}

func TestUseThreadSameInstanceBlockedByPendingRequest(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 37, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
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
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.root.Surfaces["surface-1"].PendingRequests["req-1"] = &state.RequestPromptRecord{RequestID: "req-1"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-2",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "request_pending" {
		t.Fatalf("expected pending request gate to block same-instance /use, got %#v", events)
	}
	if surface := svc.root.Surfaces["surface-1"]; surface.AttachedInstanceID != "inst-1" || surface.SelectedThreadID != "thread-1" || surface.RouteMode != state.RouteModePinned {
		t.Fatalf("expected same-instance /use block to keep prior route, got %#v", surface)
	}
}

func TestUseThreadSameInstanceBlockedByRequestCapture(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 38, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
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
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.root.Surfaces["surface-1"].ActiveRequestCapture = &state.RequestCaptureRecord{RequestID: "req-1"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-2",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "request_capture_waiting_text" {
		t.Fatalf("expected capture gate to block same-instance /use, got %#v", events)
	}
	if surface := svc.root.Surfaces["surface-1"]; surface.AttachedInstanceID != "inst-1" || surface.SelectedThreadID != "thread-1" || surface.RouteMode != state.RouteModePinned {
		t.Fatalf("expected same-instance /use block to keep prior route, got %#v", surface)
	}
}

func TestUseThreadDetachedStartsPreselectedHeadlessWhenOnlyOfflineSnapshotAvailable(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 40, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-offline",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        false,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", Preview: "修登录", CWD: "/data/dl/droid", Loaded: true},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.PendingHeadless.ThreadID != "thread-1" || snapshot.PendingHeadless.ThreadCWD != "/data/dl/droid" {
		t.Fatalf("expected detached /use to start preselected headless flow, got %#v", snapshot)
	}
	if len(events) != 2 || events[1].DaemonCommand == nil || events[1].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected headless start flow, got %#v", events)
	}
	if events[1].DaemonCommand.ThreadID != "thread-1" || events[1].DaemonCommand.ThreadCWD != "/data/dl/droid" {
		t.Fatalf("expected daemon command to carry preselected thread info, got %#v", events[1].DaemonCommand)
	}
}
