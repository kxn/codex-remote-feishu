package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestHandleGatewayActionPathPickerCancelTargetPickerPatchesOwnerCard(t *testing.T) {
	gateway := &recordingGateway{}
	app := newTargetPickerPathReturnTestApp(t, gateway)
	messageID := "om-target-picker-1"
	_, pathPickerID := openTargetPickerLocalDirectoryPathPickerForTest(t, app, gateway, messageID)

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionPathPickerCancel,
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         pathPickerID,
		MessageID:        messageID,
		Inbound:          &control.ActionInboundMeta{CardDaemonLifecycleID: app.daemonLifecycleID},
	})
	if result != nil {
		t.Fatalf("expected path picker cancel to go through gateway update, got %#v", result)
	}
	if len(gateway.operations) != 1 {
		t.Fatalf("expected one gateway operation, got %#v", gateway.operations)
	}
	op := gateway.operations[0]
	if op.Kind != feishu.OperationUpdateCard || op.MessageID != messageID {
		t.Fatalf("expected owner-card update for cancel, got %#v", op)
	}
	if op.CardTitle != "切换工作会话" {
		t.Fatalf("expected cancel to return to target picker card, got %#v", op)
	}
	runtime := app.service.SurfaceUIRuntime("surface-1")
	if runtime.ActivePathPickerID != "" || runtime.ActiveTargetPickerID == "" {
		t.Fatalf("expected path picker cleared and target picker still active, got %#v", runtime)
	}
}

func TestHandleGatewayActionPathPickerConfirmTargetPickerPatchesOwnerCard(t *testing.T) {
	gateway := &recordingGateway{}
	app := newTargetPickerPathReturnTestApp(t, gateway)
	messageID := "om-target-picker-2"
	_, pathPickerID := openTargetPickerLocalDirectoryPathPickerForTest(t, app, gateway, messageID)

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionPathPickerConfirm,
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         pathPickerID,
		MessageID:        messageID,
		Inbound:          &control.ActionInboundMeta{CardDaemonLifecycleID: app.daemonLifecycleID},
	})
	if result != nil {
		t.Fatalf("expected path picker confirm to go through gateway update, got %#v", result)
	}
	if len(gateway.operations) != 1 {
		t.Fatalf("expected one gateway operation, got %#v", gateway.operations)
	}
	op := gateway.operations[0]
	if op.Kind != feishu.OperationUpdateCard || op.MessageID != messageID {
		t.Fatalf("expected owner-card update for confirm, got %#v", op)
	}
	if op.CardTitle != "切换工作会话" {
		t.Fatalf("expected confirm to return to target picker card, got %#v", op)
	}
	runtime := app.service.SurfaceUIRuntime("surface-1")
	if runtime.ActivePathPickerID != "" || runtime.ActiveTargetPickerID == "" {
		t.Fatalf("expected path picker cleared and target picker still active, got %#v", runtime)
	}
}

func newTargetPickerPathReturnTestApp(t *testing.T, gateway *recordingGateway) *App {
	t.Helper()
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	workspaceRoot := t.TempDir()
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "proj",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		ShortName:     "proj",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	return app
}

func openTargetPickerLocalDirectoryPathPickerForTest(t *testing.T, app *App, gateway *recordingGateway, messageID string) (string, string) {
	t.Helper()
	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        messageID,
		Inbound:          &control.ActionInboundMeta{CardDaemonLifecycleID: app.daemonLifecycleID},
	})
	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected list to open target picker inline, got %#v", result)
	}
	runtime := app.service.SurfaceUIRuntime("surface-1")
	targetPickerID := runtime.ActiveTargetPickerID
	if targetPickerID == "" {
		t.Fatalf("expected active target picker after /list")
	}

	result = app.HandleGatewayAction(context.Background(), control.Action{
		Kind:              control.ActionTargetPickerSelectMode,
		SurfaceSessionID:  "surface-1",
		GatewayID:         "app-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          targetPickerID,
		TargetPickerValue: string(control.FeishuTargetPickerModeAddWorkspace),
		MessageID:         messageID,
		Inbound:           &control.ActionInboundMeta{CardDaemonLifecycleID: app.daemonLifecycleID},
	})
	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected mode select to replace card inline, got %#v", result)
	}

	result = app.HandleGatewayAction(context.Background(), control.Action{
		Kind:              control.ActionTargetPickerSelectSource,
		SurfaceSessionID:  "surface-1",
		GatewayID:         "app-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          targetPickerID,
		TargetPickerValue: string(control.FeishuTargetPickerSourceLocalDirectory),
		MessageID:         messageID,
		Inbound:           &control.ActionInboundMeta{CardDaemonLifecycleID: app.daemonLifecycleID},
	})
	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected source select to replace card inline, got %#v", result)
	}

	result = app.HandleGatewayAction(context.Background(), control.Action{
		Kind:              control.ActionTargetPickerOpenPathPicker,
		SurfaceSessionID:  "surface-1",
		GatewayID:         "app-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          targetPickerID,
		TargetPickerValue: control.FeishuTargetPickerPathFieldLocalDirectory,
		MessageID:         messageID,
		Inbound:           &control.ActionInboundMeta{CardDaemonLifecycleID: app.daemonLifecycleID},
	})
	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected open-path action to replace card inline, got %#v", result)
	}
	runtime = app.service.SurfaceUIRuntime("surface-1")
	if runtime.ActivePathPickerID == "" {
		t.Fatalf("expected active path picker after opening subpage")
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no async operations before confirm/cancel, got %#v", gateway.operations)
	}
	return targetPickerID, runtime.ActivePathPickerID
}
