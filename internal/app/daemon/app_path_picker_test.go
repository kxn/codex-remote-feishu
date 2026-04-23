package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestHandleGatewayActionBlocksMenuWhilePathPickerActive(t *testing.T) {
	gateway := &recordingGateway{}
	startedAt := time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC)
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: startedAt,
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	root := t.TempDir()
	events := app.service.OpenPathPicker(control.Action{
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}, control.PathPickerRequest{
		Mode:     control.PathPickerModeDirectory,
		RootPath: root,
	})
	if len(events) != 1 || events[0].PathPickerView == nil {
		t.Fatalf("expected active picker open event, got %#v", events)
	}
	before := len(gateway.operations)
	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionShowCommandMenu,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/menu",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})
	if result != nil {
		t.Fatalf("expected active path picker gate to prevent inline menu replacement, got %#v", result)
	}
	delta := gateway.operations[before:]
	if len(delta) != 1 {
		t.Fatalf("expected one blocking notice operation, got %#v", delta)
	}
	if !strings.Contains(delta[0].CardBody, "路径选择") {
		t.Fatalf("expected blocking notice about path picker, got %#v", delta)
	}
	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Gate.Kind != "path_picker" {
		t.Fatalf("expected path picker gate to remain active after blocked menu action, got %#v", snapshot)
	}
}
