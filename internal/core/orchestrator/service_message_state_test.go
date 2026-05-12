package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestTextMessageFreezesThreadAndConsumesStagedImages(t *testing.T) {
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionImageMessage, SurfaceSessionID: "surface-1", MessageID: "msg-img", LocalPath: "/tmp/img.png", MIMEType: "image/png"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-text",
		Text:             "请分析这张图",
	})

	if len(events) < 3 {
		t.Fatalf("expected queued + dispatch + command events, got %d", len(events))
	}
	surface := svc.root.Surfaces["surface-1"]
	if len(surface.QueueItems) != 1 {
		t.Fatalf("expected one queue item, got %d", len(surface.QueueItems))
	}
	var item *state.QueueItemRecord
	for _, current := range surface.QueueItems {
		item = current
	}
	if queuedItemExecutionThreadID(item) != "thread-1" || queueItemFrozenCWD(item) != "/data/dl/droid" {
		t.Fatalf("unexpected frozen route: %#v", item)
	}
	if len(item.Inputs) != 2 || item.Inputs[0].Type != agentproto.InputLocalImage || item.Inputs[1].Type != agentproto.InputText {
		t.Fatalf("unexpected inputs: %#v", item.Inputs)
	}
}

func TestTextMessageUsesProvidedInputsAlongsideStagedImages(t *testing.T) {
	now := time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionImageMessage, SurfaceSessionID: "surface-1", MessageID: "msg-staged", LocalPath: "/tmp/staged.png", MIMEType: "image/png"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-post",
		Text:             "这是图文混合消息",
		Inputs: []agentproto.Input{
			{Type: agentproto.InputLocalImage, Path: "/tmp/inline.png", MIMEType: "image/png"},
			{Type: agentproto.InputText, Text: "这是图文混合消息"},
		},
	})

	if len(events) < 3 {
		t.Fatalf("expected queued + dispatch + command events, got %#v", events)
	}
	surface := svc.root.Surfaces["surface-1"]
	var item *state.QueueItemRecord
	for _, current := range surface.QueueItems {
		item = current
	}
	if item == nil {
		t.Fatal("expected one queue item")
	}
	if len(item.Inputs) != 3 {
		t.Fatalf("expected staged image + inline image + text, got %#v", item.Inputs)
	}
	if item.Inputs[0].Type != agentproto.InputLocalImage || item.Inputs[0].Path != "/tmp/staged.png" {
		t.Fatalf("unexpected first input: %#v", item.Inputs[0])
	}
	if item.Inputs[1].Type != agentproto.InputLocalImage || item.Inputs[1].Path != "/tmp/inline.png" {
		t.Fatalf("unexpected second input: %#v", item.Inputs[1])
	}
	if item.Inputs[2].Type != agentproto.InputText || item.Inputs[2].Text != "这是图文混合消息" {
		t.Fatalf("unexpected third input: %#v", item.Inputs[2])
	}
}

func TestStatusReflectsObservedDefaultConfigAndSurfaceOverride(t *testing.T) {
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

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:            agentproto.EventConfigObserved,
		CWD:             "/data/dl/droid",
		ConfigScope:     "cwd_default",
		Model:           "gpt-5.3-codex",
		ReasoningEffort: "medium",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModelCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/model gpt-5.4 high",
	})

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected surface snapshot")
	}
	if snapshot.NextPrompt.CreateThread || snapshot.NextPrompt.CWD != "/data/dl/droid" {
		t.Fatalf("expected unbound surface to stay blocked in workspace root, got %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.BaseModel != "gpt-5.3-codex" || snapshot.NextPrompt.BaseReasoningEffort != "medium" {
		t.Fatalf("expected base config from workspace default, got %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.BaseModelSource != "workspace_default" || snapshot.NextPrompt.BaseReasoningEffortSource != "workspace_default" {
		t.Fatalf("expected workspace default sources, got %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.EffectiveModel != "gpt-5.4" || snapshot.NextPrompt.EffectiveReasoningEffort != "high" {
		t.Fatalf("expected effective config to use surface override, got %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.EffectiveModelSource != "surface_override" || snapshot.NextPrompt.EffectiveReasoningEffortSource != "surface_override" {
		t.Fatalf("expected override sources in snapshot, got %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.EffectiveAccessMode != agentproto.AccessModeFullAccess || snapshot.NextPrompt.EffectiveAccessModeSource != "surface_default" {
		t.Fatalf("expected default full access in snapshot, got %#v", snapshot.NextPrompt)
	}
}
