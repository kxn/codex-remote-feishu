package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestTextMessageDetachedNormalUsesWorkspaceWording(t *testing.T) {
	now := time.Date(2026, 4, 9, 19, 40, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "not_attached" {
		t.Fatalf("expected detached normal text to reject with not_attached, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, "您没有接管任何工作区") || strings.Contains(events[0].Notice.Text, "实例") {
		t.Fatalf("expected workspace-first detached wording, got %#v", events[0].Notice)
	}
}

func TestApplyInstanceConnectedDoesNotResumeDetachedSurfaceQueue(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
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

	svc.ApplyInstanceDisconnected("inst-1")
	queued := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	if len(queued) != 1 || queued[0].Notice == nil || queued[0].Notice.Code != "not_attached" {
		t.Fatalf("expected detached surface to reject new input, got %#v", queued)
	}

	events := svc.ApplyInstanceConnected("inst-1")
	if len(events) != 0 {
		t.Fatalf("expected reconnect not to resume a detached surface, got %#v", events)
	}
}

func TestApplyAgentSystemErrorTargetsAttachedSurface(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	events := svc.ApplyAgentEvent("inst-1", agentproto.NewSystemErrorEvent(agentproto.ErrorInfo{
		Code:      "stdout_parse_failed",
		Layer:     "wrapper",
		Stage:     "observe_codex_stdout",
		Operation: "codex.stdout",
		Message:   "wrapper 无法解析 Codex 子进程输出的 JSON-RPC 帧。",
		Details:   "invalid character 'x' looking for beginning of value",
	}))
	if len(events) != 1 || events[0].Notice == nil {
		t.Fatalf("expected one problem notice, got %#v", events)
	}
	if events[0].SurfaceSessionID != "surface-1" {
		t.Fatalf("expected notice on attached surface, got %#v", events[0])
	}
	if events[0].Notice.Code != debugErrorNoticeCode {
		t.Fatalf("unexpected notice code: %#v", events[0].Notice)
	}
	if !strings.Contains(events[0].Notice.Title, "wrapper.observe_codex_stdout") || !strings.Contains(events[0].Notice.Text, "invalid character") {
		t.Fatalf("expected structured problem text, got %#v", events[0].Notice)
	}
}
