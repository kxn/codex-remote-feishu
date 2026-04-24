package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestTextMessageEmitsAcceptedNoticeWhenDispatching(t *testing.T) {
	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "处理一下",
	})

	var notice *eventcontract.Event
	for i := range events {
		if events[i].Notice != nil && events[i].Notice.Code == remoteTurnAcceptedNoticeCode {
			notice = &events[i]
			break
		}
	}
	if notice == nil {
		t.Fatalf("expected accepted notice event, got %#v", events)
	}
	if notice.SourceMessageID != "msg-1" {
		t.Fatalf("expected accepted notice to reply to source message, got %#v", notice)
	}
	if got := notice.Meta.Semantics.VisibilityClass; got != eventcontract.VisibilityClassProgressText {
		t.Fatalf("expected progress-text visibility, got %#v", notice.Meta.Semantics)
	}
	if len(notice.Notice.Sections) != 1 || notice.Notice.Sections[0].Lines[0] != "正在发送到本地 Codex 并开始处理。" {
		t.Fatalf("unexpected accepted notice sections: %#v", notice.Notice.Sections)
	}
}

func TestTextMessageEmitsQueuedAcceptedNoticeWhenPausedForLocal(t *testing.T) {
	now := time.Date(2026, 4, 24, 12, 5, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventLocalInteractionObserved,
		ThreadID: "thread-1",
		Action:   "turn_start",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "处理一下",
	})

	var notice *eventcontract.Event
	for i := range events {
		if events[i].Notice != nil && events[i].Notice.Code == remoteTurnAcceptedNoticeCode {
			notice = &events[i]
			break
		}
	}
	if notice == nil {
		t.Fatalf("expected queued accepted notice event, got %#v", events)
	}
	if len(notice.Notice.Sections) != 1 || notice.Notice.Sections[0].Lines[0] != "当前本地 VS Code 占用，消息已排队。" {
		t.Fatalf("unexpected queued accepted notice sections: %#v", notice.Notice.Sections)
	}
}

func TestTextMessageDoesNotEmitAcceptedNoticeInQuietMode(t *testing.T) {
	now := time.Date(2026, 4, 24, 12, 10, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.root.Surfaces["surface-1"].Verbosity = state.SurfaceVerbosityQuiet

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "处理一下",
	})

	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == remoteTurnAcceptedNoticeCode {
			t.Fatalf("expected quiet mode to suppress accepted notice, got %#v", events)
		}
	}
}

func TestThreadDiscoveryDoesNotBackfillLastAssistantFromPreview(t *testing.T) {
	now := time.Date(2026, 4, 24, 12, 15, 0, 0, time.UTC)
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
		Kind:     agentproto.EventThreadDiscovered,
		ThreadID: "thread-1",
		Name:     "修复登录流程",
		Preview:  "用户自己的首条消息预览",
		CWD:      "/data/dl/droid",
	})

	thread := svc.root.Instances["inst-1"].Threads["thread-1"]
	if thread == nil {
		t.Fatalf("expected discovered thread to exist")
	}
	if thread.LastAssistantMessage != "" {
		t.Fatalf("expected preview not to backfill last assistant message, got %#v", thread)
	}
}

func TestThreadsSnapshotDoesNotBackfillLastAssistantFromPreview(t *testing.T) {
	now := time.Date(2026, 4, 24, 12, 20, 0, 0, time.UTC)
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
		Kind: agentproto.EventThreadsSnapshot,
		Threads: []agentproto.ThreadSnapshotRecord{{
			ThreadID: "thread-1",
			Name:     "修复登录流程",
			Preview:  "用户自己的首条消息预览",
			CWD:      "/data/dl/droid",
			Loaded:   true,
		}},
	})

	thread := svc.root.Instances["inst-1"].Threads["thread-1"]
	if thread == nil {
		t.Fatalf("expected snapshot thread to exist")
	}
	if thread.LastAssistantMessage != "" {
		t.Fatalf("expected preview not to backfill last assistant message, got %#v", thread)
	}
}
