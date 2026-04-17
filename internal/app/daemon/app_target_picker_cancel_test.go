package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestHandleGatewayActionReplacesTargetPickerWithCancelNotice(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "proj",
		WorkspaceRoot: "/data/dl/proj",
		WorkspaceKey:  "/data/dl/proj",
		ShortName:     "proj",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID:   "thread-1",
				Name:       "会话-1",
				CWD:        "/data/dl/proj",
				LastUsedAt: time.Date(2026, 4, 10, 10, 1, 0, 0, time.UTC),
			},
		},
	})

	_ = app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	runtime := app.service.SurfaceUIRuntime("surface-1")
	if runtime.ActiveTargetPickerID == "" {
		t.Fatal("expected target picker to become active")
	}

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionTargetPickerCancel,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         runtime.ActiveTargetPickerID,
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected inline replacement result, got %#v", result)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no appended gateway operations, got %#v", gateway.operations)
	}
	if result.ReplaceCurrentCard.CardTitle != "系统提示" {
		t.Fatalf("unexpected replacement card title: %#v", result.ReplaceCurrentCard)
	}
	if !strings.Contains(result.ReplaceCurrentCard.CardBody, "已取消选择工作区/会话") {
		t.Fatalf("expected cancel notice body, got %#v", result.ReplaceCurrentCard)
	}
	if runtime := app.service.SurfaceUIRuntime("surface-1"); runtime.ActiveTargetPickerID != "" {
		t.Fatalf("expected cancel to clear active target picker, got %#v", runtime)
	}
}
