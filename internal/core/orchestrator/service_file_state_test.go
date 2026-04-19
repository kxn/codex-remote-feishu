package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestTextMessageConsumesStagedFilesAndAddsPromptReference(t *testing.T) {
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "repo",
		WorkspaceRoot:           "/data/dl/repo",
		WorkspaceKey:            "/data/dl/repo",
		ShortName:               "repo",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "主会话", CWD: "/data/dl/repo"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionFileMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-file",
		LocalPath:        "/data/dl/repo/.codex-remote/inbox/feishu-files/msg-file/notes.txt",
		FileName:         "notes.txt",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-text",
		Text:             "请结合文件继续处理",
	})

	if len(events) < 3 {
		t.Fatalf("expected queued + dispatch + command events, got %#v", events)
	}
	surface := svc.root.Surfaces["surface-1"]
	if len(surface.QueueItems) != 1 {
		t.Fatalf("expected one queue item, got %#v", surface.QueueItems)
	}
	var item *state.QueueItemRecord
	for _, current := range surface.QueueItems {
		item = current
	}
	if item == nil {
		t.Fatal("expected one queue item")
	}
	if len(item.Inputs) != 2 {
		t.Fatalf("expected file reference prompt + user text, got %#v", item.Inputs)
	}
	if item.Inputs[0].Type != agentproto.InputText || !strings.Contains(item.Inputs[0].Text, "notes.txt") || !strings.Contains(item.Inputs[0].Text, "/data/dl/repo/.codex-remote/inbox/feishu-files/msg-file/notes.txt") {
		t.Fatalf("unexpected file reference prompt: %#v", item.Inputs[0])
	}
	if item.Inputs[1].Type != agentproto.InputText || item.Inputs[1].Text != "请结合文件继续处理" {
		t.Fatalf("unexpected user input: %#v", item.Inputs[1])
	}
}

func TestMessageRecalledCancelsStagedFile(t *testing.T) {
	now := time.Date(2026, 4, 20, 12, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "repo",
		WorkspaceRoot: "/data/dl/repo",
		WorkspaceKey:  "/data/dl/repo",
		ShortName:     "repo",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionFileMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-file",
		LocalPath:        "/data/dl/repo/.codex-remote/inbox/feishu-files/msg-file/spec.pdf",
		FileName:         "spec.pdf",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionMessageRecalled,
		SurfaceSessionID: "surface-1",
		TargetMessageID:  "msg-file",
	})

	if len(events) != 1 || events[0].PendingInput == nil {
		t.Fatalf("expected pending input cancellation event, got %#v", events)
	}
	if events[0].PendingInput.Status != string(state.FileCancelled) || !events[0].PendingInput.QueueOff || !events[0].PendingInput.ThumbsDown {
		t.Fatalf("unexpected file recall event: %#v", events[0].PendingInput)
	}
}

func TestUseThreadDiscardsStagedFilesOnRouteChange(t *testing.T) {
	now := time.Date(2026, 4, 20, 12, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "repo",
		WorkspaceRoot:           "/data/dl/repo",
		WorkspaceKey:            "/data/dl/repo",
		ShortName:               "repo",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "主会话", CWD: "/data/dl/repo"},
			"thread-2": {ThreadID: "thread-2", Name: "另一个会话", CWD: "/data/dl/repo"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionFileMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-file",
		LocalPath:        "/data/dl/repo/.codex-remote/inbox/feishu-files/msg-file/spec.pdf",
		FileName:         "spec.pdf",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-2",
	})

	if len(svc.root.Surfaces["surface-1"].StagedFiles) != 0 {
		t.Fatalf("expected staged file to be dropped on /use route change, got %#v", svc.root.Surfaces["surface-1"].StagedFiles)
	}
	var sawDiscard, sawNotice bool
	for _, event := range events {
		if event.PendingInput != nil && event.PendingInput.Status == string(state.FileDiscarded) && event.PendingInput.QueueOff && event.PendingInput.ThumbsDown {
			sawDiscard = true
		}
		if event.Notice != nil && event.Notice.Code == "staged_inputs_discarded_on_route_change" {
			sawNotice = true
		}
	}
	if !sawDiscard || !sawNotice {
		t.Fatalf("expected discard event and notice, got %#v", events)
	}
}
