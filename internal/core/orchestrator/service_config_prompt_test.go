package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestStatusUsesObservedWorkspaceDefaultAccessMode(t *testing.T) {
	now := time.Date(2026, 4, 9, 13, 30, 0, 0, time.UTC)
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

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:        agentproto.EventConfigObserved,
		ThreadID:    "thread-1",
		CWD:         "/data/dl/droid",
		ConfigScope: "cwd_default",
		AccessMode:  agentproto.AccessModeConfirm,
	})

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected surface snapshot")
	}
	if snapshot.NextPrompt.EffectiveAccessMode != agentproto.AccessModeConfirm || snapshot.NextPrompt.EffectiveAccessModeSource != "workspace_default" {
		t.Fatalf("expected workspace default confirm access in snapshot, got %#v", snapshot.NextPrompt)
	}

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})

	surface := svc.root.Surfaces["surface-1"]
	var item *state.QueueItemRecord
	for _, current := range surface.QueueItems {
		item = current
	}
	if item == nil {
		t.Fatal("expected queue item")
	}
	if item.FrozenOverride.AccessMode != agentproto.AccessModeConfirm {
		t.Fatalf("expected queue item to freeze workspace default access, got %#v", item.FrozenOverride)
	}
}

func TestObserveConfigVSCodeDoesNotPersistWorkspaceDefaults(t *testing.T) {
	now := time.Date(2026, 4, 9, 14, 30, 0, 0, time.UTC)
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

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:            agentproto.EventConfigObserved,
		ThreadID:        "thread-1",
		CWD:             "/data/dl/droid",
		ConfigScope:     "cwd_default",
		Model:           "gpt-5.3-codex",
		ReasoningEffort: "medium",
		AccessMode:      agentproto.AccessModeConfirm,
	})

	if defaults := svc.root.WorkspaceDefaults["/data/dl/droid"]; defaults != (state.ModelConfigRecord{}) {
		t.Fatalf("expected vscode config observation not to persist workspace defaults, got %#v", defaults)
	}

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected surface snapshot")
	}
	if snapshot.NextPrompt.BaseModel != "gpt-5.3-codex" || snapshot.NextPrompt.BaseModelSource != "cwd_default" {
		t.Fatalf("expected vscode snapshot to resolve model from instance cwd defaults, got %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.BaseReasoningEffort != "medium" || snapshot.NextPrompt.BaseReasoningEffortSource != "cwd_default" {
		t.Fatalf("expected vscode snapshot to resolve effort from instance cwd defaults, got %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.EffectiveAccessMode != agentproto.AccessModeConfirm || snapshot.NextPrompt.EffectiveAccessModeSource != "cwd_default" {
		t.Fatalf("expected vscode snapshot to resolve access from instance cwd defaults, got %#v", snapshot.NextPrompt)
	}
}

func TestSurfaceSnapshotExposesAttachmentObjectTypeByMode(t *testing.T) {
	now := time.Date(2026, 4, 9, 14, 45, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-normal",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-vscode",
		DisplayName:             "web",
		WorkspaceRoot:           "/data/dl/web",
		WorkspaceKey:            "/data/dl/web",
		ShortName:               "web",
		Source:                  "vscode",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "整理样式", CWD: "/data/dl/web", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "surface-normal", ChatID: "chat-normal", ActorUserID: "user-normal", WorkspaceKey: "/data/dl/droid"})
	materializeVSCodeSurfaceForTest(svc, "surface-vscode")
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-vscode", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-vscode"})

	normalSnapshot := svc.SurfaceSnapshot("surface-normal")
	if normalSnapshot == nil || normalSnapshot.Attachment.ObjectType != "workspace" {
		t.Fatalf("expected normal snapshot attachment type workspace, got %#v", normalSnapshot)
	}

	vscodeSnapshot := svc.SurfaceSnapshot("surface-vscode")
	if vscodeSnapshot == nil || vscodeSnapshot.Attachment.ObjectType != "vscode_instance" {
		t.Fatalf("expected vscode snapshot attachment type vscode_instance, got %#v", vscodeSnapshot)
	}
}

func TestStatusUsesSurfaceDefaultsWhenObservedConfigUnknown(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-2",
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected surface snapshot")
	}
	if snapshot.NextPrompt.EffectiveModel != "gpt-5.4" || snapshot.NextPrompt.EffectiveModelSource != "surface_default" {
		t.Fatalf("expected default model fallback, got %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.EffectiveReasoningEffort != "xhigh" || snapshot.NextPrompt.EffectiveReasoningEffortSource != "surface_default" {
		t.Fatalf("expected xhigh reasoning fallback, got %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.EffectiveAccessMode != agentproto.AccessModeFullAccess || snapshot.NextPrompt.EffectiveAccessModeSource != "surface_default" {
		t.Fatalf("expected default full access, got %#v", snapshot.NextPrompt)
	}
}

func TestTextMessageFreezesObservedPromptConfigWhenNoSurfaceOverride(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		CWDDefaults: map[string]state.ModelConfigRecord{
			"/data/dl/droid": {Model: "gpt-5.4", ReasoningEffort: "high"},
		},
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

	surface := svc.root.Surfaces["surface-1"]
	var item *state.QueueItemRecord
	for _, current := range surface.QueueItems {
		item = current
	}
	if item == nil {
		t.Fatal("expected queue item")
	}
	if item.FrozenOverride.Model != "gpt-5.4" || item.FrozenOverride.ReasoningEffort != "high" {
		t.Fatalf("expected queued item to freeze observed config, got %#v", item.FrozenOverride)
	}
	if item.FrozenOverride.AccessMode != agentproto.AccessModeFullAccess {
		t.Fatalf("expected queued item to freeze full access, got %#v", item.FrozenOverride)
	}
}

func TestUpsertInstanceBackfillsLegacyCWDDefaultsIntoWorkspaceDefaults(t *testing.T) {
	now := time.Date(2026, 4, 9, 14, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := testutil.WorkspacePath("data", "dl", "droid")
	subdirKey := testutil.WorkspacePath("data", "dl", "droid", "subdir")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           workspaceKey,
		WorkspaceKey:            workspaceKey,
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		CWDDefaults: map[string]state.ModelConfigRecord{
			workspaceKey + "/.": {Model: "gpt-5.4", ReasoningEffort: "high"},
			subdirKey:           {AccessMode: agentproto.AccessModeConfirm},
		},
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: workspaceKey},
		},
	})

	defaults := svc.root.WorkspaceDefaults[workspaceKey]
	if defaults.Model != "gpt-5.4" || defaults.ReasoningEffort != "high" || defaults.AccessMode != agentproto.AccessModeConfirm {
		t.Fatalf("expected legacy defaults backfilled into workspace defaults, got %#v", defaults)
	}

	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected surface snapshot")
	}
	if snapshot.NextPrompt.BaseModelSource != "workspace_default" || snapshot.NextPrompt.BaseReasoningEffortSource != "workspace_default" {
		t.Fatalf("expected legacy defaults to resolve through workspace defaults, got %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.EffectiveAccessMode != agentproto.AccessModeConfirm || snapshot.NextPrompt.EffectiveAccessModeSource != "workspace_default" {
		t.Fatalf("expected migrated workspace access default in snapshot, got %#v", snapshot.NextPrompt)
	}
}

func TestTextMessageFreezesFallbackReasoningWhenConfigUnknown(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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

	surface := svc.root.Surfaces["surface-1"]
	var item *state.QueueItemRecord
	for _, current := range surface.QueueItems {
		item = current
	}
	if item == nil {
		t.Fatal("expected queue item")
	}
	if item.FrozenOverride.Model != "gpt-5.4" {
		t.Fatalf("expected queued item to freeze default model, got %#v", item.FrozenOverride)
	}
	if item.FrozenOverride.ReasoningEffort != "xhigh" || item.FrozenOverride.AccessMode != agentproto.AccessModeFullAccess {
		t.Fatalf("expected queued item to freeze fallback config, got %#v", item.FrozenOverride)
	}
}

func TestAccessCommandUpdatesSnapshotAndQueueFreeze(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
		Kind:             control.ActionAccessCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/access confirm",
	})

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected surface snapshot")
	}
	if snapshot.NextPrompt.OverrideAccessMode != agentproto.AccessModeConfirm {
		t.Fatalf("expected access override in snapshot, got %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.EffectiveAccessMode != agentproto.AccessModeConfirm || snapshot.NextPrompt.EffectiveAccessModeSource != "surface_override" {
		t.Fatalf("expected confirm access in snapshot, got %#v", snapshot.NextPrompt)
	}

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})

	surface := svc.root.Surfaces["surface-1"]
	var item *state.QueueItemRecord
	for _, current := range surface.QueueItems {
		item = current
	}
	if item == nil {
		t.Fatal("expected queue item")
	}
	if item.FrozenOverride.AccessMode != agentproto.AccessModeConfirm {
		t.Fatalf("expected queued item to freeze access override, got %#v", item.FrozenOverride)
	}
}

func TestAutoContinueCommandUpdatesSnapshotWithoutAttach(t *testing.T) {
	now := time.Date(2026, 4, 9, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	enabled := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAutoContinueCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/autowhip on",
	})
	if len(enabled) != 1 || enabled[0].Notice == nil || enabled[0].Notice.Code != "auto_continue_enabled" {
		t.Fatalf("expected enable notice, got %#v", enabled)
	}

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected snapshot after enable")
	}
	if !snapshot.AutoContinue.Enabled {
		t.Fatalf("expected auto-continue enabled in snapshot, got %#v", snapshot.AutoContinue)
	}

	disabled := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAutoContinueCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/autowhip off",
	})
	if len(disabled) != 1 || disabled[0].Notice == nil || disabled[0].Notice.Code != "auto_continue_disabled" {
		t.Fatalf("expected disable notice, got %#v", disabled)
	}
	if snapshot := svc.SurfaceSnapshot("surface-1"); snapshot == nil || snapshot.AutoContinue.Enabled {
		t.Fatalf("expected auto-continue disabled in snapshot, got %#v", snapshot)
	}
}

func TestSurfaceSnapshotIncludesAutoContinueSummary(t *testing.T) {
	now := time.Date(2026, 4, 9, 11, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.root.Surfaces["surface-1"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID: "surface-1",
		DispatchMode:     state.DispatchModeNormal,
		QueueItems:       map[string]*state.QueueItemRecord{},
		StagedImages:     map[string]*state.StagedImageRecord{},
		PendingRequests:  map[string]*state.RequestPromptRecord{},
		AutoContinue: state.AutoContinueRuntimeRecord{
			Enabled:             true,
			PendingReason:       state.AutoContinueReasonRetryableFailure,
			PendingDueAt:        now.Add(30 * time.Second),
			ConsecutiveCount:    2,
			LastTriggeredTurnID: "turn-1",
		},
	}

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if !snapshot.AutoContinue.Enabled ||
		snapshot.AutoContinue.PendingReason != string(state.AutoContinueReasonRetryableFailure) ||
		!snapshot.AutoContinue.PendingDueAt.Equal(now.Add(30*time.Second)) ||
		snapshot.AutoContinue.ConsecutiveCount != 2 ||
		snapshot.AutoContinue.LastTriggeredTurnID != "turn-1" {
		t.Fatalf("unexpected auto-continue snapshot: %#v", snapshot.AutoContinue)
	}
}

func TestAutoContinueSchedulesIncompleteStopAfterRemoteTurn(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "继续处理", "turn-1")
	completeRemoteTurnWithFinalText(t, svc, "turn-1", "completed", "", "我先去检查一下还有没有遗漏。", nil)

	if surface.AutoContinue.PendingReason != state.AutoContinueReasonIncompleteStop {
		t.Fatalf("expected incomplete-stop schedule, got %#v", surface.AutoContinue)
	}
	if !surface.AutoContinue.PendingDueAt.Equal(now.Add(3 * time.Second)) {
		t.Fatalf("expected first incomplete-stop backoff at +3s, got %#v", surface.AutoContinue.PendingDueAt)
	}
	if surface.AutoContinue.IncompleteStopCount != 1 || surface.AutoContinue.ConsecutiveCount != 1 {
		t.Fatalf("expected first incomplete-stop counters, got %#v", surface.AutoContinue)
	}
	if surface.AutoContinue.PendingReplyToMessageID != "msg-1" {
		t.Fatalf("expected pending reply anchor to stick to original message, got %#v", surface.AutoContinue)
	}
}

func TestAutoContinueStopsWhenFinalTextContainsStopPhrase(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 2, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "继续处理", "turn-1")
	events := completeRemoteTurnWithFinalText(t, svc, "turn-1", "completed", "", "这边都检查过了，老板不要再打我了，真的没有事情干了", nil)

	if surface.AutoContinue.PendingReason != "" || surface.AutoContinue.ConsecutiveCount != 0 {
		t.Fatalf("expected stop phrase to keep autowhip idle, got %#v", surface.AutoContinue)
	}
	var sawCompletedNotice bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "auto_continue_completed" {
			sawCompletedNotice = event.Notice.Title == "AutoWhip" && event.Notice.Text == "Codex 已经把活干完了，老板放过他吧"
		}
	}
	if !sawCompletedNotice {
		t.Fatalf("expected completion notice when autowhip decides there is no more work, got %#v", events)
	}
}

func TestAutoContinueSchedulesRetryableFailureBackoff(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "继续处理", "turn-1")
	events := completeRemoteTurnWithFinalText(t, svc, "turn-1", "interrupted", "upstream stream closed", "", &agentproto.ErrorInfo{
		Code:      "responseStreamDisconnected",
		Layer:     "codex",
		Stage:     "runtime_error",
		Message:   "upstream stream closed",
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Retryable: true,
	})

	if surface.AutoContinue.PendingReason != state.AutoContinueReasonRetryableFailure {
		t.Fatalf("expected retryable-failure schedule, got %#v", surface.AutoContinue)
	}
	if !surface.AutoContinue.PendingDueAt.Equal(now.Add(10 * time.Second)) {
		t.Fatalf("expected first retryable-failure backoff at +10s, got %#v", surface.AutoContinue.PendingDueAt)
	}
	if surface.AutoContinue.RetryableFailureCount != 1 || surface.AutoContinue.ConsecutiveCount != 1 {
		t.Fatalf("expected first retryable-failure counters, got %#v", surface.AutoContinue)
	}
	var sawTurnFailedNotice bool
	var sawRetryScheduledNotice bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "turn_failed" {
			sawTurnFailedNotice = true
		}
		if event.Notice != nil && event.Notice.Code == "auto_continue_retry_scheduled" {
			sawRetryScheduledNotice = event.Notice.Title == "AutoWhip" && event.Notice.Text == "上游不稳定，第 1/4 次，10秒后重试"
		}
	}
	if !sawTurnFailedNotice {
		t.Fatalf("expected turn failure notice, got %#v", events)
	}
	if !sawRetryScheduledNotice {
		t.Fatalf("expected retry schedule notice, got %#v", events)
	}
}

func TestAutoContinueDispatchSkipsPendingProjectionButKeepsReplyAnchor(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "继续处理", "turn-1")
	completeRemoteTurnWithFinalText(t, svc, "turn-1", "completed", "", "我再检查一轮，把剩下的事情都扫掉。", nil)

	now = now.Add(3 * time.Second)
	tickEvents := svc.Tick(now)
	if surface.ActiveQueueItemID == "" {
		t.Fatal("expected auto-continue item to dispatch after backoff")
	}
	autoItem := surface.QueueItems[surface.ActiveQueueItemID]
	if autoItem == nil {
		t.Fatal("expected auto-continue queue item")
	}
	if autoItem.SourceKind != state.QueueItemSourceAutoContinue || autoItem.SourceMessageID != "" || autoItem.ReplyToMessageID != "msg-1" {
		t.Fatalf("unexpected auto-continue queue item: %#v", autoItem)
	}
	var prompt *agentproto.Command
	var sawWhipStartedNotice bool
	for _, event := range tickEvents {
		if event.PendingInput != nil {
			t.Fatalf("expected auto-continue enqueue/dispatch to skip pending projection, got %#v", tickEvents)
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			prompt = event.Command
		}
		if event.Notice != nil && event.Notice.Code == "auto_continue_whip_started" {
			sawWhipStartedNotice = event.Notice.Title == "AutoWhip" && event.Notice.Text == "Codex疑似偷懒,已抽打 1次"
		}
	}
	if prompt == nil || len(prompt.Prompt.Inputs) != 1 || prompt.Prompt.Inputs[0].Text != autoContinuePromptText {
		t.Fatalf("expected auto-continue prompt send, got %#v", tickEvents)
	}
	if !sawWhipStartedNotice {
		t.Fatalf("expected whip start notice, got %#v", tickEvents)
	}

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-2",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	for _, event := range started {
		if event.PendingInput != nil {
			t.Fatalf("expected auto-continue running state to skip pending projection, got %#v", started)
		}
	}

	finished := completeRemoteTurnWithFinalText(t, svc, "turn-2", "completed", "", "老板不要再打我了，真的没有事情干了", nil)
	var sawReplyBlock bool
	var sawCompletedNotice bool
	for _, event := range finished {
		if event.PendingInput != nil {
			t.Fatalf("expected auto-continue completion to skip pending projection, got %#v", finished)
		}
		if event.Block != nil && event.SourceMessageID == "msg-1" {
			sawReplyBlock = true
		}
		if event.Notice != nil && event.Notice.Code == "auto_continue_completed" {
			sawCompletedNotice = event.Notice.Title == "AutoWhip" && event.Notice.Text == "Codex 已经把活干完了，老板放过他吧"
		}
	}
	if !sawReplyBlock {
		t.Fatalf("expected final auto-continue reply to stay anchored under original message, got %#v", finished)
	}
	if !sawCompletedNotice {
		t.Fatalf("expected completion notice after auto-continue decides there is no more work, got %#v", finished)
	}
	if !surface.AutoContinue.Enabled || surface.AutoContinue.PendingReason != "" || surface.AutoContinue.ConsecutiveCount != 0 {
		t.Fatalf("expected successful auto-continue completion to reset runtime state, got %#v", surface.AutoContinue)
	}
}

func TestStopSuppressesNextAutoContinueSchedule(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 15, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "继续处理", "turn-1")
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionStop,
		SurfaceSessionID: "surface-1",
	})
	if !surface.AutoContinue.SuppressOnce {
		t.Fatalf("expected /stop to suppress the next auto-continue scheduling attempt, got %#v", surface.AutoContinue)
	}

	completeRemoteTurnWithFinalText(t, svc, "turn-1", "interrupted", "", "我再去核一遍，看看还有没有别的活。", nil)
	if surface.AutoContinue.PendingReason != "" || !surface.AutoContinue.Enabled || surface.AutoContinue.SuppressOnce {
		t.Fatalf("expected suppressed turn completion to leave auto-continue enabled but idle, got %#v", surface.AutoContinue)
	}
}

func TestSurfaceSnapshotIncludesGateAndDispatchSummary(t *testing.T) {
	now := time.Date(2026, 4, 7, 19, 20, 0, 0, time.UTC)
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
	surface := svc.root.Surfaces["surface-1"]
	surface.PendingRequests["req-1"] = &state.RequestPromptRecord{RequestID: "req-1"}
	surface.ActiveQueueItemID = "queue-1"
	surface.QueuedQueueItemIDs = []string{"queue-2"}
	surface.QueueItems["queue-1"] = &state.QueueItemRecord{ID: "queue-1", Status: state.QueueItemRunning}
	surface.QueueItems["queue-2"] = &state.QueueItemRecord{ID: "queue-2", Status: state.QueueItemQueued}

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if snapshot.Gate.Kind != "pending_request" || snapshot.Gate.PendingRequestCount != 1 {
		t.Fatalf("expected pending request summary, got %#v", snapshot.Gate)
	}
	if !snapshot.Dispatch.InstanceOnline || snapshot.Dispatch.DispatchMode != string(state.DispatchModeNormal) || snapshot.Dispatch.ActiveItemStatus != string(state.QueueItemRunning) || snapshot.Dispatch.QueuedCount != 1 {
		t.Fatalf("expected dispatch summary to include active+queued work, got %#v", snapshot.Dispatch)
	}
}

func TestQueuedMessageFreezesSurfaceOverrideAtEnqueue(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {
				ThreadID:                "thread-1",
				Name:                    "修复登录流程",
				CWD:                     "/data/dl/droid",
				ExplicitModel:           "gpt-5.3-codex",
				ExplicitReasoningEffort: "medium",
			},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventLocalInteractionObserved,
		ThreadID: "thread-1",
		Action:   "turn_start",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModelCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/model gpt-5.4 high",
	})

	queued := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	if len(queued) != 1 || queued[0].PendingInput == nil || queued[0].PendingInput.Status != string(state.QueueItemQueued) {
		t.Fatalf("expected queued-only event while paused, got %#v", queued)
	}

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModelCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/model gpt-5.2-codex low",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})

	now = now.Add(900 * time.Millisecond)
	resumed := svc.Tick(now)
	var prompt *agentproto.Command
	for _, event := range resumed {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			prompt = event.Command
			break
		}
	}
	if prompt == nil {
		t.Fatalf("expected resumed queue item to dispatch prompt command, got %#v", resumed)
	}
	if prompt.Overrides.Model != "gpt-5.4" || prompt.Overrides.ReasoningEffort != "high" {
		t.Fatalf("expected queued message to keep original frozen override, got %#v", prompt.Overrides)
	}
}

func TestLocalInteractionPausesRemoteQueueAndHandoffResumes(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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

	localEvents := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventLocalInteractionObserved,
		ThreadID: "thread-1",
		Action:   "turn_start",
	})
	if len(localEvents) != 1 || localEvents[0].Notice == nil || localEvents[0].Notice.Code != "local_activity_detected" {
		t.Fatalf("unexpected local pause events: %#v", localEvents)
	}

	queued := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	if len(queued) != 1 || queued[0].PendingInput == nil || queued[0].PendingInput.Status != "queued" {
		t.Fatalf("expected queued-only event while paused, got %#v", queued)
	}

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	surface := svc.root.Surfaces["surface-1"]
	if surface.DispatchMode != state.DispatchModeHandoffWait {
		t.Fatalf("expected handoff wait, got %q", surface.DispatchMode)
	}

	now = now.Add(900 * time.Millisecond)
	tick := svc.Tick(now)
	if len(tick) < 3 {
		t.Fatalf("expected resume notice + dispatch events, got %#v", tick)
	}
	if tick[0].Notice == nil || tick[0].Notice.Code != "remote_queue_resumed" {
		t.Fatalf("expected resume notice, got %#v", tick[0])
	}
}

func TestLocalPauseNoticeIsNotRepeatedWhenTurnStartedArrives(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventLocalInteractionObserved,
		ThreadID: "thread-1",
		Action:   "turn_start",
	})
	if len(first) != 1 || first[0].Notice == nil || first[0].Notice.Code != "local_activity_detected" {
		t.Fatalf("expected first local pause notice, got %#v", first)
	}

	second := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "running",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	if len(second) != 0 {
		t.Fatalf("expected no duplicate local pause notice, got %#v", second)
	}
}

func TestLocalPauseWatchdogResumesQueuedWorkWithoutLocalCompletion(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventLocalInteractionObserved,
		ThreadID: "thread-1",
		Action:   "turn_start",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})

	now = now.Add(16 * time.Second)
	events := svc.Tick(now)

	surface := svc.root.Surfaces["surface-1"]
	if surface.DispatchMode != state.DispatchModeNormal {
		t.Fatalf("expected watchdog to restore normal dispatch mode, got %q", surface.DispatchMode)
	}
	var sawResumeNotice, sawDispatch bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "local_activity_watchdog_resumed" {
			sawResumeNotice = true
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			sawDispatch = true
		}
	}
	if !sawResumeNotice || !sawDispatch {
		t.Fatalf("expected watchdog resume notice + dispatch, got %#v", events)
	}
}

func TestInternalHelperLocalInteractionDoesNotPauseSurface(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:         agentproto.EventLocalInteractionObserved,
		ThreadID:     "thread-helper",
		CWD:          "/data/dl/droid",
		Action:       "turn_start",
		TrafficClass: agentproto.TrafficClassInternalHelper,
		Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorInternalHelper},
	})
	if len(events) != 0 {
		t.Fatalf("expected helper interaction to stay out of product UI, got %#v", events)
	}
	if svc.root.Surfaces["surface-1"].DispatchMode != state.DispatchModeNormal {
		t.Fatalf("expected helper interaction not to pause surface, got %q", svc.root.Surfaces["surface-1"].DispatchMode)
	}
	if svc.root.Instances["inst-1"].ObservedFocusedThreadID != "" {
		t.Fatalf("expected helper interaction not to mutate observed focus, got %q", svc.root.Instances["inst-1"].ObservedFocusedThreadID)
	}
}

func TestInternalHelperThreadIsNotAddedToVisibleThreadState(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:         agentproto.EventThreadDiscovered,
		ThreadID:     "thread-helper",
		CWD:          "/data/dl/droid",
		Name:         "helper",
		TrafficClass: agentproto.TrafficClassInternalHelper,
		Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorInternalHelper},
	})
	if len(events) != 0 {
		t.Fatalf("expected helper thread discovery not to emit UI events, got %#v", events)
	}
	if _, exists := svc.root.Instances["inst-1"].Threads["thread-helper"]; exists {
		t.Fatalf("expected helper thread not to enter visible thread state, got %#v", svc.root.Instances["inst-1"].Threads["thread-helper"])
	}
}

func TestInternalHelperTurnLifecycleDoesNotAffectRemoteQueue(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	queued := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	foundDispatch := false
	for _, event := range queued {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			foundDispatch = true
			break
		}
	}
	if !foundDispatch {
		t.Fatalf("expected remote queue item to dispatch immediately, got %#v", queued)
	}
	surface := svc.root.Surfaces["surface-1"]
	activeQueueItemID := surface.ActiveQueueItemID

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:         agentproto.EventTurnStarted,
		ThreadID:     "thread-helper",
		TurnID:       "turn-helper",
		TrafficClass: agentproto.TrafficClassInternalHelper,
		Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorInternalHelper},
	})
	if len(started) != 0 {
		t.Fatalf("expected helper turn start to stay out of UI, got %#v", started)
	}
	item := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:         agentproto.EventItemCompleted,
		ThreadID:     "thread-helper",
		TurnID:       "turn-helper",
		ItemID:       "item-helper",
		ItemKind:     "agent_message",
		TrafficClass: agentproto.TrafficClassInternalHelper,
		Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorInternalHelper},
		Metadata:     map[string]any{"text": "{\"title\":\"helper\"}"},
	})
	if len(item) != 0 {
		t.Fatalf("expected helper item completion not to render, got %#v", item)
	}
	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:         agentproto.EventTurnCompleted,
		ThreadID:     "thread-helper",
		TurnID:       "turn-helper",
		Status:       "completed",
		TrafficClass: agentproto.TrafficClassInternalHelper,
		Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorInternalHelper},
	})
	if len(completed) != 0 {
		t.Fatalf("expected helper turn completion to stay out of UI, got %#v", completed)
	}
	if surface.ActiveQueueItemID != activeQueueItemID {
		t.Fatalf("expected helper lifecycle not to disturb remote active queue item, before=%q after=%q", activeQueueItemID, surface.ActiveQueueItemID)
	}
	if svc.root.Instances["inst-1"].ActiveTurnID != "" {
		t.Fatalf("expected helper lifecycle not to mutate instance active turn, got %q", svc.root.Instances["inst-1"].ActiveTurnID)
	}
}

func TestMessageRecallCancelsQueuedItemAcrossTextAndImageSources(t *testing.T) {
	now := time.Date(2026, 4, 6, 9, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-2",
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	surface := svc.root.Surfaces["surface-1"]
	surface.QueuedQueueItemIDs = []string{"queue-1"}
	surface.QueueItems["queue-1"] = &state.QueueItemRecord{
		ID:               "queue-1",
		SourceMessageID:  "msg-text",
		SourceMessageIDs: []string{"msg-text", "msg-img"},
		Status:           state.QueueItemQueued,
	}
	surface.StagedImages["img-1"] = &state.StagedImageRecord{
		ImageID:         "img-1",
		SourceMessageID: "msg-img",
		State:           state.ImageBound,
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionMessageRecalled,
		SurfaceSessionID: "surface-1",
		TargetMessageID:  "msg-img",
	})
	if len(events) != 2 {
		t.Fatalf("expected discard reactions for text and image sources, got %#v", events)
	}
	for _, event := range events {
		if event.PendingInput == nil || !event.PendingInput.QueueOff || !event.PendingInput.ThumbsDown || event.PendingInput.Status != string(state.QueueItemDiscarded) {
			t.Fatalf("unexpected queued item discard projection: %#v", events)
		}
	}
	if surface.QueueItems["queue-1"].Status != state.QueueItemDiscarded || len(surface.QueuedQueueItemIDs) != 0 {
		t.Fatalf("expected queue item to be removed from queue, got item=%#v queue=%#v", surface.QueueItems["queue-1"], surface.QueuedQueueItemIDs)
	}
	if surface.StagedImages["img-1"].State != state.ImageDiscarded {
		t.Fatalf("expected bound image to be marked discarded, got %#v", surface.StagedImages["img-1"])
	}
}

func TestMessageRecallCancelsStagedImage(t *testing.T) {
	now := time.Date(2026, 4, 6, 9, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.root.Surfaces["surface-1"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID: "surface-1",
		QueueItems:       map[string]*state.QueueItemRecord{},
		StagedImages: map[string]*state.StagedImageRecord{
			"img-1": {
				ImageID:         "img-1",
				SourceMessageID: "msg-img",
				State:           state.ImageStaged,
			},
		},
		PendingRequests: map[string]*state.RequestPromptRecord{},
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionMessageRecalled,
		SurfaceSessionID: "surface-1",
		TargetMessageID:  "msg-img",
	})
	if len(events) != 1 || events[0].PendingInput == nil {
		t.Fatalf("expected staged image cancellation event, got %#v", events)
	}
	if events[0].PendingInput.Status != string(state.ImageCancelled) || !events[0].PendingInput.QueueOff || !events[0].PendingInput.ThumbsDown {
		t.Fatalf("unexpected staged image cancellation projection: %#v", events[0].PendingInput)
	}
}

func TestReactionCreatedSteersQueuedItemAndAcknowledgesWholeInputSet(t *testing.T) {
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-active",
		Text:             "先开始",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionImageMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-img",
		LocalPath:        "/tmp/queued.png",
		MIMEType:         "image/png",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-queued",
		Text:             "补充信息",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReactionCreated,
		SurfaceSessionID: "surface-1",
		TargetMessageID:  "msg-queued",
		ReactionType:     "ThumbsUp",
	})
	if len(events) != 1 || events[0].Command == nil || events[0].Command.Kind != agentproto.CommandTurnSteer {
		t.Fatalf("expected single steer command, got %#v", events)
	}
	if events[0].Command.Target.ThreadID != "thread-1" || events[0].Command.Target.TurnID != "turn-1" {
		t.Fatalf("unexpected steer target: %#v", events[0].Command.Target)
	}
	if len(events[0].Command.Prompt.Inputs) != 2 || events[0].Command.Prompt.Inputs[0].Type != agentproto.InputLocalImage || events[0].Command.Prompt.Inputs[1].Text != "补充信息" {
		t.Fatalf("expected bound image + text inputs in steer command, got %#v", events[0].Command.Prompt.Inputs)
	}

	surface := svc.root.Surfaces["surface-1"]
	if len(surface.QueuedQueueItemIDs) != 0 {
		t.Fatalf("expected queued item to leave normal queue while steering, got %#v", surface.QueuedQueueItemIDs)
	}
	item := surface.QueueItems["queue-2"]
	if item == nil || item.Status != state.QueueItemSteering {
		t.Fatalf("expected second queue item to enter steering pending, got %#v", item)
	}
	if binding := svc.pendingSteers["queue-2"]; binding == nil || binding.ThreadID != "thread-1" || binding.TurnID != "turn-1" {
		t.Fatalf("expected pending steer binding for queue-2, got %#v", svc.pendingSteers)
	}

	svc.BindPendingRemoteCommand("surface-1", "cmd-steer-1")
	accepted := svc.HandleCommandAccepted("inst-1", agentproto.CommandAck{CommandID: "cmd-steer-1", Accepted: true})
	if svc.pendingSteers["queue-2"] != nil {
		t.Fatalf("expected pending steer binding to clear after accepted ack")
	}
	if item.Status != state.QueueItemSteered {
		t.Fatalf("expected queue item to be marked steered, got %#v", item)
	}
	if len(accepted) != 3 {
		t.Fatalf("expected supplement text plus queue-off + thumbs-up for text and image sources, got %#v", accepted)
	}
	if accepted[0].TimelineText == nil || accepted[0].TimelineText.Type != control.TimelineTextSteerUserSupplement || accepted[0].TimelineText.Text != "用户补充：补充信息（追加 1 张图片）" {
		t.Fatalf("unexpected steer supplement event: %#v", accepted[0])
	}
	if accepted[0].TimelineText.ReplyToMessageID != "msg-active" {
		t.Fatalf("expected supplement to reply to turn anchor, got %#v", accepted[0].TimelineText)
	}
	for _, event := range accepted[1:] {
		if event.PendingInput == nil || !event.PendingInput.QueueOff || !event.PendingInput.ThumbsUp || event.PendingInput.Status != string(state.QueueItemSteered) {
			t.Fatalf("unexpected steer acknowledgement projection: %#v", accepted)
		}
	}
}

func TestReactionCreatedIgnoresImageSourceAndThreadMismatch(t *testing.T) {
	now := time.Date(2026, 4, 6, 10, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		ActiveThreadID:          "thread-2",
		ActiveTurnID:            "turn-2",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})

	surface := svc.root.Surfaces["surface-1"]
	surface.QueuedQueueItemIDs = []string{"queue-1"}
	surface.QueueItems["queue-1"] = &state.QueueItemRecord{
		ID:               "queue-1",
		SourceMessageID:  "msg-text",
		SourceMessageIDs: []string{"msg-text", "msg-img"},
		Inputs: []agentproto.Input{
			{Type: agentproto.InputLocalImage, Path: "/tmp/queued.png", MIMEType: "image/png"},
			{Type: agentproto.InputText, Text: "补充信息"},
		},
		FrozenThreadID: "thread-1",
		Status:         state.QueueItemQueued,
	}

	if events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReactionCreated,
		SurfaceSessionID: "surface-1",
		TargetMessageID:  "msg-img",
		ReactionType:     "ThumbsUp",
	}); len(events) != 0 {
		t.Fatalf("expected image reaction to be ignored, got %#v", events)
	}
	if events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReactionCreated,
		SurfaceSessionID: "surface-1",
		TargetMessageID:  "msg-text",
		ReactionType:     "ThumbsUp",
	}); len(events) != 0 {
		t.Fatalf("expected mismatched active thread to be ignored, got %#v", events)
	}
	if surface.QueueItems["queue-1"].Status != state.QueueItemQueued || len(surface.QueuedQueueItemIDs) != 1 {
		t.Fatalf("expected queued item to remain untouched, got item=%#v queue=%#v", surface.QueueItems["queue-1"], surface.QueuedQueueItemIDs)
	}
}

func TestReactionCreatedSteerRejectedRestoresOriginalQueueOrder(t *testing.T) {
	now := time.Date(2026, 4, 6, 10, 10, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-active",
		Text:             "先开始",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-queued-1",
		Text:             "第二条",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-queued-2",
		Text:             "第三条",
	})

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReactionCreated,
		SurfaceSessionID: "surface-1",
		TargetMessageID:  "msg-queued-1",
		ReactionType:     "ThumbsUp",
	})
	svc.BindPendingRemoteCommand("surface-1", "cmd-steer-2")
	events := svc.HandleCommandRejected("inst-1", agentproto.CommandAck{
		CommandID: "cmd-steer-2",
		Accepted:  false,
		Error:     "steer rejected",
		Problem: &agentproto.ErrorInfo{
			Code:      "translate_command_failed",
			Layer:     "wrapper",
			Stage:     "translate_command",
			Message:   "wrapper 无法把 steer 命令转换成 Codex 请求。",
			Details:   "steer rejected",
			CommandID: "cmd-steer-2",
		},
	})

	surface := svc.root.Surfaces["surface-1"]
	if got := surface.QueuedQueueItemIDs; len(got) != 2 || got[0] != "queue-2" || got[1] != "queue-3" {
		t.Fatalf("expected queue order to be restored, got %#v", got)
	}
	if item := surface.QueueItems["queue-2"]; item == nil || item.Status != state.QueueItemQueued {
		t.Fatalf("expected rejected steer item to return to queued, got %#v", item)
	}
	if svc.pendingSteers["queue-2"] != nil {
		t.Fatalf("expected pending steer binding to clear after rejection")
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "steer_failed" || !strings.Contains(events[0].Notice.Text, "恢复原排队位置") {
		t.Fatalf("expected steer_failed notice with restore hint, got %#v", events)
	}
}

func TestUseThreadDiscardsStagedImagesOnRouteChange(t *testing.T) {
	now := time.Date(2026, 4, 6, 9, 45, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionImageMessage, SurfaceSessionID: "surface-1", MessageID: "msg-img", LocalPath: "/tmp/img.png", MIMEType: "image/png"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-2",
	})

	if _, ok := svc.root.Surfaces["surface-1"].StagedImages["img-1"]; ok {
		t.Fatalf("expected staged image to be dropped on /use route change")
	}
	var sawDiscard, sawNotice, sawSelection bool
	for _, event := range events {
		if event.PendingInput != nil && event.PendingInput.Status == string(state.ImageDiscarded) && event.PendingInput.QueueOff && event.PendingInput.ThumbsDown {
			sawDiscard = true
		}
		if event.Notice != nil && event.Notice.Code == "staged_images_discarded_on_route_change" {
			sawNotice = true
		}
		if event.ThreadSelection != nil && event.ThreadSelection.ThreadID == "thread-2" && event.ThreadSelection.RouteMode == string(state.RouteModePinned) {
			sawSelection = true
		}
	}
	if !sawDiscard || !sawNotice || !sawSelection {
		t.Fatalf("expected discard notice + new selection, got %#v", events)
	}
}

func TestFollowLocalDiscardsStagedImagesOnRouteModeChange(t *testing.T) {
	now := time.Date(2026, 4, 6, 9, 50, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionImageMessage, SurfaceSessionID: "surface-1", MessageID: "msg-img", LocalPath: "/tmp/img.png", MIMEType: "image/png"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionFollowLocal,
		SurfaceSessionID: "surface-1",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModeFollowLocal || surface.SelectedThreadID != "thread-1" {
		t.Fatalf("expected follow_local to keep thread and switch mode, got selected=%q route=%q", surface.SelectedThreadID, surface.RouteMode)
	}
	if _, ok := surface.StagedImages["img-1"]; ok {
		t.Fatalf("expected staged image to be dropped on /follow route change")
	}
	var sawDiscard, sawNotice bool
	for _, event := range events {
		if event.PendingInput != nil && event.PendingInput.Status == string(state.ImageDiscarded) && event.PendingInput.QueueOff && event.PendingInput.ThumbsDown {
			sawDiscard = true
		}
		if event.Notice != nil && event.Notice.Code == "staged_images_discarded_on_route_change" {
			sawNotice = true
		}
	}
	if !sawDiscard || !sawNotice {
		t.Fatalf("expected discard notice while switching into follow_local, got %#v", events)
	}
}

func TestFollowLocalBlockedByPendingRequest(t *testing.T) {
	now := time.Date(2026, 4, 7, 19, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.root.Surfaces["surface-1"].RouteMode = state.RouteModePinned
	svc.root.Surfaces["surface-1"].LastSelection = &state.SelectionAnnouncementRecord{
		ThreadID:  "thread-1",
		RouteMode: string(state.RouteModePinned),
	}
	svc.root.Surfaces["surface-1"].PendingRequests["req-1"] = &state.RequestPromptRecord{RequestID: "req-1"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionFollowLocal,
		SurfaceSessionID: "surface-1",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "request_pending" {
		t.Fatalf("expected request gate to block /follow, got %#v", events)
	}
	if surface := svc.root.Surfaces["surface-1"]; surface.RouteMode != state.RouteModePinned || surface.SelectedThreadID != "thread-1" {
		t.Fatalf("expected /follow block to keep prior route, got %#v", surface)
	}
}

func TestFollowLocalBlockedByRequestCapture(t *testing.T) {
	now := time.Date(2026, 4, 7, 19, 5, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.root.Surfaces["surface-1"].RouteMode = state.RouteModePinned
	svc.root.Surfaces["surface-1"].LastSelection = &state.SelectionAnnouncementRecord{
		ThreadID:  "thread-1",
		RouteMode: string(state.RouteModePinned),
	}
	svc.root.Surfaces["surface-1"].ActiveRequestCapture = &state.RequestCaptureRecord{RequestID: "req-1"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionFollowLocal,
		SurfaceSessionID: "surface-1",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "request_capture_waiting_text" {
		t.Fatalf("expected capture gate to block /follow, got %#v", events)
	}
	if surface := svc.root.Surfaces["surface-1"]; surface.RouteMode != state.RouteModePinned || surface.SelectedThreadID != "thread-1" {
		t.Fatalf("expected /follow block to keep prior route, got %#v", surface)
	}
}

func TestFollowLocalManualPickBlockedByPendingRequest(t *testing.T) {
	now := time.Date(2026, 4, 7, 19, 7, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-2"})
	svc.root.Surfaces["surface-1"].PendingRequests["req-1"] = &state.RequestPromptRecord{RequestID: "req-1"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionFollowLocal,
		SurfaceSessionID: "surface-1",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "request_pending" {
		t.Fatalf("expected pending request gate to freeze manual follow-local rebind, got %#v", events)
	}
	if surface := svc.root.Surfaces["surface-1"]; surface.RouteMode != state.RouteModeFollowLocal || surface.SelectedThreadID != "thread-2" {
		t.Fatalf("expected follow-local manual pick to stay put while request gate is active, got %#v", surface)
	}
}

func TestFollowLocalManualPickBlockedByRequestCapture(t *testing.T) {
	now := time.Date(2026, 4, 7, 19, 8, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-2"})
	svc.root.Surfaces["surface-1"].ActiveRequestCapture = &state.RequestCaptureRecord{RequestID: "req-1"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionFollowLocal,
		SurfaceSessionID: "surface-1",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "request_capture_waiting_text" {
		t.Fatalf("expected request capture gate to freeze manual follow-local rebind, got %#v", events)
	}
	if surface := svc.root.Surfaces["surface-1"]; surface.RouteMode != state.RouteModeFollowLocal || surface.SelectedThreadID != "thread-2" {
		t.Fatalf("expected follow-local manual pick to stay put while capture gate is active, got %#v", surface)
	}
}

func TestFollowLocalNormalModeShowsMigrationNotice(t *testing.T) {
	now := time.Date(2026, 4, 7, 19, 7, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", WorkspaceKey: "/data/dl/droid"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionFollowLocal,
		SurfaceSessionID: "surface-1",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "follow_deprecated_normal" || !strings.Contains(events[0].Notice.Text, "/mode vscode") {
		t.Fatalf("expected normal-mode /follow migration notice, got %#v", events)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModeUnbound || surface.SelectedThreadID != "" {
		t.Fatalf("expected normal-mode /follow rejection to keep workspace-unbound state, got %#v", surface)
	}
}

func TestLegacyNormalFollowRouteNormalizesToPinned(t *testing.T) {
	now := time.Date(2026, 4, 7, 19, 8, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.root.Surfaces["surface-1"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID:    "surface-1",
		ProductMode:         state.ProductModeNormal,
		ClaimedWorkspaceKey: "/data/dl/droid",
		AttachedInstanceID:  "inst-1",
		SelectedThreadID:    "thread-1",
		RouteMode:           state.RouteModeFollowLocal,
		QueueItems:          map[string]*state.QueueItemRecord{},
		StagedImages:        map[string]*state.StagedImageRecord{},
		PendingRequests:     map[string]*state.RequestPromptRecord{},
	}
	svc.threadClaims["thread-1"] = &threadClaimRecord{
		ThreadID:         "thread-1",
		InstanceID:       "inst-1",
		SurfaceSessionID: "surface-1",
	}

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if snapshot.Attachment.RouteMode != string(state.RouteModePinned) || snapshot.Attachment.SelectedThreadID != "thread-1" {
		t.Fatalf("expected legacy normal follow route to normalize to pinned, got %#v", snapshot)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModePinned || surface.SelectedThreadID != "thread-1" {
		t.Fatalf("expected surface route to normalize to pinned, got %#v", surface)
	}
}

func TestLegacyVSCodePreparedNewThreadRouteNormalizesToFollowLocal(t *testing.T) {
	now := time.Date(2026, 4, 9, 15, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
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
	svc.root.Surfaces["surface-1"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID:     "surface-1",
		ProductMode:          state.ProductModeVSCode,
		AttachedInstanceID:   "inst-1",
		RouteMode:            state.RouteModeNewThreadReady,
		PreparedThreadCWD:    "/data/dl/droid",
		PreparedFromThreadID: "thread-legacy",
		QueueItems:           map[string]*state.QueueItemRecord{},
		StagedImages:         map[string]*state.StagedImageRecord{},
		PendingRequests:      map[string]*state.RequestPromptRecord{},
	}

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if snapshot.Attachment.RouteMode != string(state.RouteModeFollowLocal) || snapshot.Attachment.SelectedThreadID != "thread-1" {
		t.Fatalf("expected legacy vscode new-thread route to normalize to follow_local, got %#v", snapshot)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModeFollowLocal || surface.SelectedThreadID != "thread-1" {
		t.Fatalf("expected surface route to normalize to follow_local, got %#v", surface)
	}
	if surface.PreparedThreadCWD != "" || surface.PreparedFromThreadID != "" {
		t.Fatalf("expected legacy prepared-new-thread state to clear, got %#v", surface)
	}
}

func TestMessageRecallRunningItemReturnsNotice(t *testing.T) {
	now := time.Date(2026, 4, 6, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.root.Surfaces["surface-1"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID:  "surface-1",
		ActiveQueueItemID: "queue-1",
		QueueItems: map[string]*state.QueueItemRecord{
			"queue-1": {
				ID:               "queue-1",
				SourceMessageID:  "msg-text",
				SourceMessageIDs: []string{"msg-text", "msg-img"},
				Status:           state.QueueItemRunning,
			},
		},
		StagedImages:    map[string]*state.StagedImageRecord{},
		PendingRequests: map[string]*state.RequestPromptRecord{},
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionMessageRecalled,
		SurfaceSessionID: "surface-1",
		TargetMessageID:  "msg-img",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "message_recall_too_late" {
		t.Fatalf("expected message_recall_too_late notice, got %#v", events)
	}
}

func TestMessageRecallCompletedItemIsIgnored(t *testing.T) {
	now := time.Date(2026, 4, 6, 10, 15, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.root.Surfaces["surface-1"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID: "surface-1",
		QueueItems: map[string]*state.QueueItemRecord{
			"queue-1": {
				ID:               "queue-1",
				SourceMessageID:  "msg-text",
				SourceMessageIDs: []string{"msg-text", "msg-img"},
				Status:           state.QueueItemCompleted,
			},
		},
		StagedImages:    map[string]*state.StagedImageRecord{},
		PendingRequests: map[string]*state.RequestPromptRecord{},
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionMessageRecalled,
		SurfaceSessionID: "surface-1",
		TargetMessageID:  "msg-text",
	})
	if len(events) != 0 {
		t.Fatalf("expected completed item recall to be ignored, got %#v", events)
	}
}

func TestAssistantTextIsBufferedFromDeltaUntilItemCompleted(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-2",
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-2",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "agent_message",
	})
	if len(started) != 0 {
		t.Fatalf("expected no UI events on item start, got %#v", started)
	}

	delta := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-2",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "agent_message",
		Delta:    "您好",
	})
	if len(delta) != 0 {
		t.Fatalf("expected no UI events on item delta, got %#v", delta)
	}

	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-2",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "agent_message",
	})
	if len(completed) != 0 {
		t.Fatalf("expected no UI events until turn completion, got %#v", completed)
	}

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-2",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})
	if len(finished) != 1 || finished[0].Block == nil {
		t.Fatalf("expected final block without extra thread switch, got %#v", finished)
	}
	if finished[0].Block == nil || finished[0].Block.Text != "您好" || !finished[0].Block.Final {
		t.Fatalf("expected final rendered assistant block on turn completion, got %#v", finished)
	}
}

func TestAssistantProcessTextFlushesWhenTurnContinuesWithAnotherItem(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-2",
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-2",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "agent_message",
		Delta:    "sort 在只读沙箱里没法创建临时文件。我改用不需要写临时文件的目录列表方式。",
	})
	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-2",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "agent_message",
	})
	if len(completed) != 0 {
		t.Fatalf("expected pending process text after first agent message, got %#v", completed)
	}

	flushed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-2",
		TurnID:   "turn-1",
		ItemID:   "item-2",
		ItemKind: "command_execution",
	})
	if len(flushed) != 1 || flushed[0].Block == nil {
		t.Fatalf("expected flushed process text without selection change, got %#v", flushed)
	}
	if flushed[0].Block == nil || flushed[0].Block.Final || flushed[0].Block.Text != "sort 在只读沙箱里没法创建临时文件。我改用不需要写临时文件的目录列表方式。" {
		t.Fatalf("expected process text to flush when turn continues, got %#v", flushed)
	}
}

func TestThreadSelectionNoticeIsNotRepeatedWhenOnlyPreviewChanges(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", Preview: "旧预览", CWD: "/data/dl/droid"},
		},
	})

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	surface := svc.root.Surfaces["surface-1"]
	events := svc.threadSelectionEvents(surface, "thread-1", string(state.RouteModePinned), "droid · 修复登录流程", "新预览")
	if len(events) != 0 {
		t.Fatalf("expected no repeated selection notice for same thread, got %#v", events)
	}
}

func TestThreadFocusDoesNotNarrowWorkspaceRootToChildDirectory(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadFocused,
		ThreadID: "thread-1",
		CWD:      "/data/dl/droid/subdir",
	})

	inst := svc.root.Instances["inst-1"]
	if inst.WorkspaceRoot != "/data/dl/droid" {
		t.Fatalf("expected workspace root to remain original root, got %q", inst.WorkspaceRoot)
	}
}

func TestLocalInteractionUpdatesObservedFocusButDoesNotSwitchSurface(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventLocalInteractionObserved,
		ThreadID: "thread-2",
		CWD:      "/data/dl/droid",
		Action:   "turn_start",
	})

	inst := svc.root.Instances["inst-1"]
	if inst.ObservedFocusedThreadID != "thread-2" {
		t.Fatalf("expected observed focused thread to be updated, got %q", inst.ObservedFocusedThreadID)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected surface selection to stay unchanged before actual turn starts, got selected=%q route=%q", surface.SelectedThreadID, surface.RouteMode)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "local_activity_detected" {
		t.Fatalf("expected only local pause notice, got %#v", events)
	}
}
