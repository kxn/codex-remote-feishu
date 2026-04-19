package orchestrator

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestUseThreadDetachedFallsBackToPersistedThreadLookupByID(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 41, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{
		byID: map[string]state.ThreadRecord{
			"thread-1": {
				ThreadID:   "thread-1",
				Name:       "sqlite only thread",
				Preview:    "来自 sqlite 的会话",
				CWD:        "/data/dl/droid",
				Loaded:     true,
				LastUsedAt: now.Add(2 * time.Minute),
			},
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
		t.Fatalf("expected detached /use to resolve thread through persisted lookup, got %#v", snapshot)
	}
	if len(events) != 2 || events[1].DaemonCommand == nil || events[1].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected persisted by-id lookup to reuse existing headless-start path, got %#v", events)
	}
}

func TestApplyInstanceConnectedAttachesPreselectedHeadlessThread(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 45, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-1"})
	pending := svc.SurfaceSnapshot("surface-1").PendingHeadless
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    pending.InstanceID,
		DisplayName:   "headless",
		WorkspaceRoot: "/tmp",
		WorkspaceKey:  "/tmp",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	events := svc.ApplyInstanceConnected(pending.InstanceID)

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != pending.InstanceID || snapshot.Attachment.SelectedThreadID != "thread-1" || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected pending headless to auto-attach target thread, got %#v", snapshot)
	}
	if inst := svc.root.Instances[pending.InstanceID]; inst.WorkspaceRoot != "/data/dl/droid" {
		t.Fatalf("expected managed headless metadata retargeted to thread cwd, got %#v", inst)
	}
	var sawSelection bool
	for _, event := range events {
		if event.ThreadSelection != nil && event.ThreadSelection.ThreadID == "thread-1" {
			sawSelection = true
		}
	}
	if !sawSelection {
		t.Fatalf("expected thread selection event when preselected headless connects, got %#v", events)
	}
}

func TestApplyInstanceConnectedAttachesPreselectedHeadlessThreadReplaysStoredNotice(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 47, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-offline",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        false,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID: "thread-1",
				Name:     "修复登录流程",
				Preview:  "修登录",
				CWD:      "/data/dl/droid",
				Loaded:   true,
				UndeliveredReplay: &state.ThreadReplayRecord{
					Kind:           state.ThreadReplayNotice,
					NoticeCode:     "problem_saved",
					NoticeTitle:    "问题提示",
					NoticeText:     "等待 headless 接手的 notice",
					NoticeThemeKey: "warning",
				},
			},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-1"})
	pending := svc.SurfaceSnapshot("surface-1").PendingHeadless
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    pending.InstanceID,
		DisplayName:   "headless",
		WorkspaceRoot: "/tmp",
		WorkspaceKey:  "/tmp",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	events := svc.ApplyInstanceConnected(pending.InstanceID)

	var sawNotice bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "problem_saved" && event.Notice.Text == "等待 headless 接手的 notice" {
			sawNotice = true
		}
	}
	if !sawNotice {
		t.Fatalf("expected preselected headless attach to replay stored notice, got %#v", events)
	}
	if replay := svc.root.Instances["inst-offline"].Threads["thread-1"].UndeliveredReplay; replay != nil {
		t.Fatalf("expected source replay to be drained after attach, got %#v", replay)
	}
	if replay := svc.root.Instances[pending.InstanceID].Threads["thread-1"].UndeliveredReplay; replay != nil {
		t.Fatalf("expected adopted replay to be one-shot, got %#v", replay)
	}
}

func TestTryAutoRestoreHeadlessPrefersHeadlessOverVisibleVSCode(t *testing.T) {
	now := time.Date(2026, 4, 8, 3, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-vscode-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Source:        "vscode",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-1",
		DisplayName:   "headless",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "headless",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	events, result := svc.TryAutoRestoreHeadless("surface-1", HeadlessRestoreAttempt{
		ThreadID:    "thread-1",
		ThreadTitle: "修复登录流程",
		ThreadCWD:   "/data/dl/droid",
	}, true)

	if result.Status != HeadlessRestoreStatusAttached {
		t.Fatalf("expected headless restore to attach, got %#v", result)
	}
	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != "inst-headless-1" || snapshot.Attachment.SelectedThreadID != "thread-1" {
		t.Fatalf("expected auto restore to bind idle headless instead of vscode, got %#v", snapshot)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "headless_restore_attached" {
		t.Fatalf("expected single recovery success notice, got %#v", events)
	}
}

func TestTryAutoRestoreHeadlessSkipsVSCodeSurface(t *testing.T) {
	now := time.Date(2026, 4, 8, 3, 2, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-vscode-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Source:        "vscode",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-1",
		DisplayName:   "headless",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "headless",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	events, result := svc.TryAutoRestoreHeadless("surface-1", HeadlessRestoreAttempt{
		ThreadID:    "thread-1",
		ThreadTitle: "修复登录流程",
		ThreadCWD:   "/data/dl/droid",
	}, true)

	if result.Status != HeadlessRestoreStatusSkipped {
		t.Fatalf("expected vscode surface to skip headless auto-restore, got %#v", result)
	}
	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.ProductMode != "vscode" || snapshot.Attachment.InstanceID != "" || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected vscode surface to remain detached after skipped restore, got %#v", snapshot)
	}
	if len(events) != 0 {
		t.Fatalf("expected no events when vscode surface rejects headless auto-restore, got %#v", events)
	}
}

func TestApplyInstanceConnectedAutoRestoreHeadlessSuppressesReplayAndSelectionNoise(t *testing.T) {
	now := time.Date(2026, 4, 8, 3, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-offline",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Source:        "headless",
		Managed:       true,
		Online:        false,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID: "thread-1",
				Name:     "修复登录流程",
				CWD:      "/data/dl/droid",
				Loaded:   true,
				UndeliveredReplay: &state.ThreadReplayRecord{
					Kind:           state.ThreadReplayNotice,
					NoticeCode:     "problem_saved",
					NoticeTitle:    "问题提示",
					NoticeText:     "等待 headless 接手的 notice",
					NoticeThemeKey: "warning",
				},
			},
		},
	})

	events, result := svc.TryAutoRestoreHeadless("surface-1", HeadlessRestoreAttempt{
		ThreadID:    "thread-1",
		ThreadTitle: "修复登录流程",
		ThreadCWD:   "/data/dl/droid",
	}, true)
	if result.Status != HeadlessRestoreStatusStarting {
		t.Fatalf("expected auto restore to start pending headless flow, got %#v", result)
	}
	if len(events) != 1 || events[0].DaemonCommand == nil || !events[0].DaemonCommand.AutoRestore {
		t.Fatalf("expected silent auto-restore headless start, got %#v", events)
	}
	pending := svc.SurfaceSnapshot("surface-1").PendingHeadless
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    pending.InstanceID,
		DisplayName:   "headless",
		WorkspaceRoot: "/tmp",
		WorkspaceKey:  "/tmp",
		ShortName:     "headless",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	connectEvents := svc.ApplyInstanceConnected(pending.InstanceID)

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != pending.InstanceID || snapshot.Attachment.SelectedThreadID != "thread-1" || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected auto-restore headless connect to attach restored thread, got %#v", snapshot)
	}
	if len(connectEvents) != 1 || connectEvents[0].Notice == nil || connectEvents[0].Notice.Code != "headless_restore_attached" {
		t.Fatalf("expected only recovery success notice, got %#v", connectEvents)
	}
	for _, event := range connectEvents {
		if event.ThreadSelection != nil {
			t.Fatalf("expected no extra selection event during auto-restore attach, got %#v", connectEvents)
		}
		if event.Notice != nil && event.Notice.Code == "problem_saved" {
			t.Fatalf("expected no stale replay notice during auto-restore attach, got %#v", connectEvents)
		}
	}
	if replay := svc.root.Instances["inst-offline"].Threads["thread-1"].UndeliveredReplay; replay != nil {
		t.Fatalf("expected source replay cleared during auto-restore attach, got %#v", replay)
	}
	if replay := svc.root.Instances[pending.InstanceID].Threads["thread-1"].UndeliveredReplay; replay != nil {
		t.Fatalf("expected target replay cleared during auto-restore attach, got %#v", replay)
	}
}

func TestUseThreadDetachedReusesManagedHeadlessForKnownThread(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 50, 0, 0, time.UTC)
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
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-1",
		DisplayName:   "headless",
		WorkspaceRoot: "/tmp/headless",
		WorkspaceKey:  "/tmp/headless",
		ShortName:     "headless",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != "inst-headless-1" || snapshot.Attachment.SelectedThreadID != "thread-1" || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected detached /use to reuse idle managed headless, got %#v", snapshot)
	}
	if inst := svc.root.Instances["inst-headless-1"]; inst.WorkspaceRoot != "/data/dl/droid" {
		t.Fatalf("expected reused headless metadata to retarget to thread cwd, got %#v", inst)
	}
	var sawSelection bool
	for _, event := range events {
		if event.ThreadSelection != nil && event.ThreadSelection.ThreadID == "thread-1" {
			sawSelection = true
		}
	}
	if !sawSelection {
		t.Fatalf("expected thread selection when reusing headless, got %#v", events)
	}
}

func TestUseThreadDetachedReusesManagedHeadlessForKnownThreadReplaysStoredFinal(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 52, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-offline",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        false,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID: "thread-1",
				Name:     "修复登录流程",
				Preview:  "修登录",
				CWD:      "/data/dl/droid",
				Loaded:   true,
				UndeliveredReplay: &state.ThreadReplayRecord{
					Kind:   state.ThreadReplayAssistantFinal,
					TurnID: "turn-1",
					ItemID: "item-1",
					Text:   "等待复用 headless 的 final",
				},
			},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-1",
		DisplayName:   "headless",
		WorkspaceRoot: "/tmp/headless",
		WorkspaceKey:  "/tmp/headless",
		ShortName:     "headless",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})

	var sawReplay bool
	for _, event := range events {
		if event.Block != nil && event.Block.Text == "等待复用 headless 的 final" && event.Block.Final {
			sawReplay = true
		}
	}
	if !sawReplay {
		t.Fatalf("expected reused managed headless attach to replay stored final, got %#v", events)
	}
	if replay := svc.root.Instances["inst-offline"].Threads["thread-1"].UndeliveredReplay; replay != nil {
		t.Fatalf("expected source replay to be drained after reuse attach, got %#v", replay)
	}
	if replay := svc.root.Instances["inst-headless-1"].Threads["thread-1"].UndeliveredReplay; replay != nil {
		t.Fatalf("expected adopted replay to be one-shot, got %#v", replay)
	}
}

func TestRemoteTurnImageGenerationProducesImmediateImageEventAndFinalText(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修图", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "画一只猫",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	imageEvents := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "img-1",
		ItemKind: "image_generation",
		Metadata: map[string]any{
			"savedPath":     "/tmp/generated.png",
			"revisedPrompt": "一只猫，水彩风格",
		},
	})
	var imageEvent *control.UIEvent
	for i := range imageEvents {
		if imageEvents[i].ImageOutput != nil {
			imageEvent = &imageEvents[i]
			break
		}
	}
	if imageEvent == nil {
		t.Fatalf("expected image output event, got %#v", imageEvents)
	}
	if imageEvent.SurfaceSessionID != "surface-1" || imageEvent.ImageOutput.SavedPath != "/tmp/generated.png" {
		t.Fatalf("unexpected image output event: %#v", imageEvent)
	}
	if imageEvent.SourceMessageID != "msg-1" {
		t.Fatalf("expected image output to reply to original source message, got %#v", imageEvent)
	}

	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "msg-2",
		ItemKind: "agent_message",
		Metadata: map[string]any{"text": "已生成图片。"},
	}); len(events) != 0 {
		t.Fatalf("expected final assistant text to remain buffered until turn completion, got %#v", events)
	}

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	var sawFinal bool
	for _, event := range finished {
		if event.Block != nil && event.Block.Final && event.Block.Text == "已生成图片。" {
			sawFinal = true
		}
	}
	if !sawFinal {
		t.Fatalf("expected final assistant text after image output, got %#v", finished)
	}
}

func TestRemoteTurnDynamicToolCallProducesSummaryWithoutImageEvent(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 2, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "工具测试", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "执行工具",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "tool-1",
		ItemKind: "dynamic_tool_call",
		Metadata: map[string]any{
			"tool": "demo_tool",
			"text": "dynamic-ok",
			"contentItems": []map[string]any{
				{"type": "text", "text": "dynamic-ok"},
				{"type": "image", "imageBase64": "data:image/png;base64,AAA"},
			},
		},
	})

	var sawText bool
	for _, event := range events {
		if event.Block != nil && strings.Contains(event.Block.Text, "工具 `demo_tool` 返回") {
			sawText = true
		}
		if event.ImageOutput != nil {
			t.Fatalf("expected dynamic tool image result to stay model-consumable unless a delivery tool is called, got %#v", events)
		}
	}
	if !sawText {
		t.Fatalf("expected dynamic tool text summary without auto image output, got %#v", events)
	}
}

func TestRemoteTurnDynamicToolCallImageOnlyProducesFallbackSummary(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 3, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "工具测试", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "执行工具",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "tool-1",
		ItemKind: "dynamic_tool_call",
		Metadata: map[string]any{
			"tool": "demo_tool",
			"contentItems": []map[string]any{
				{"type": "image", "imageBase64": "data:image/png;base64,AAA"},
			},
		},
	})

	var sawFallbackSummary bool
	for _, event := range events {
		if event.Block != nil && strings.Contains(event.Block.Text, "返回了 1 张图片") {
			sawFallbackSummary = true
		}
	}
	if !sawFallbackSummary {
		t.Fatalf("expected fallback text summary for image-only dynamic tool output, got %#v", events)
	}
}

func TestRemoteTurnDynamicToolCallRemoteImageLinkFallsBackToTextLink(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 4, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "工具测试", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "执行工具",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "tool-1",
		ItemKind: "dynamic_tool_call",
		Metadata: map[string]any{
			"tool": "demo_tool",
			"contentItems": []map[string]any{
				{"type": "image", "url": "https://example.com/demo.png"},
			},
		},
	})

	var sawLinkSummary bool
	for _, event := range events {
		if event.Block != nil &&
			strings.Contains(event.Block.Text, "图片链接") &&
			strings.Contains(event.Block.Text, "https://example.com/demo.png") {
			sawLinkSummary = true
		}
		if event.ImageOutput != nil {
			t.Fatalf("unexpected image upload event for remote-only url image: %#v", events)
		}
	}
	if !sawLinkSummary {
		t.Fatalf("expected text fallback for remote image link, got %#v", events)
	}
}

func TestRemoteTurnDynamicToolCallEmptyOutputStaysSilent(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 4, 30, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "工具测试", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "执行工具",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "tool-1",
		ItemKind: "dynamic_tool_call",
		Metadata: map[string]any{
			"tool": "read",
		},
	})
	for _, event := range events {
		if event.Notice != nil {
			t.Fatalf("expected empty dynamic tool output to stay silent, got %#v", events)
		}
	}
}

func TestRemoteTurnImageGenerationOnlyTurnDoesNotForceSyntheticFinalText(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 5, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修图", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "只要图片",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "img-1",
		ItemKind: "image_generation",
		Metadata: map[string]any{"savedPath": "/tmp/only-image.png"},
	}); len(events) == 0 {
		t.Fatalf("expected image output event, got %#v", events)
	}

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	for _, event := range finished {
		if event.Block != nil {
			t.Fatalf("expected image-only turn completion to avoid synthetic final text, got %#v", finished)
		}
	}
}

func TestRemoteTurnImageGenerationMissingPayloadEmitsNotice(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 10, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修图", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "坏图",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "img-1",
		ItemKind: "image_generation",
		Metadata: map[string]any{"revisedPrompt": "坏图"},
	})
	var sawNotice bool
	for _, event := range events {
		if event.Notice != nil && strings.Contains(event.Notice.Text, "图片生成结果缺少可发送内容") {
			sawNotice = true
		}
	}
	if !sawNotice {
		t.Fatalf("expected visible notice for missing image payload, got %#v", events)
	}
}

func TestRemoteTurnMultipleImageGenerationResultsEachEmitImageEvent(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 15, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修图", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "两张图",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	for index, path := range []string{"/tmp/one.png", "/tmp/two.png"} {
		events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
			Kind:     agentproto.EventItemCompleted,
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   fmt.Sprintf("img-%d", index+1),
			ItemKind: "image_generation",
			Metadata: map[string]any{"savedPath": path},
		})
		var imageCount int
		for _, event := range events {
			if event.ImageOutput != nil && event.ImageOutput.SavedPath == path {
				imageCount++
			}
		}
		if imageCount != 1 {
			t.Fatalf("expected one image event for %s, got %#v", path, events)
		}
	}
}
