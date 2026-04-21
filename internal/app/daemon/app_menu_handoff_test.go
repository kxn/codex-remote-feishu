package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestHandleGatewayActionReplacesMenuCardForListHandoffInNormalMode(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "proj1",
		WorkspaceRoot: "/data/dl/proj1",
		WorkspaceKey:  "/data/dl/proj1",
		ShortName:     "proj1",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "会话1", CWD: "/data/dl/proj1", LastUsedAt: time.Date(2026, 4, 10, 10, 2, 0, 0, time.UTC)},
		},
	})

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionListInstances,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
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
	if result.ReplaceCurrentCard.CardTitle != "选择工作区与会话" {
		t.Fatalf("unexpected replacement card title: %#v", result.ReplaceCurrentCard)
	}
}

func TestHandleGatewayActionReplacesMenuCardForListHandoffInVSCodeMode(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode vscode",
	})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-vscode-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Source:        "vscode",
		Online:        true,
	})

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionListInstances,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
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
	if result.ReplaceCurrentCard.CardTitle != "在线 VS Code 实例" {
		t.Fatalf("unexpected replacement card title: %#v", result.ReplaceCurrentCard)
	}
}

func TestHandleGatewayActionReplacesMenuCardForVSCodeListEmptyState(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode vscode",
	})

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionListInstances,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-menu-list-empty-1",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected list empty-state to replace current card, got %#v", result)
	}
	if !strings.Contains(operationCardText(*result.ReplaceCurrentCard), "当前没有在线 VS Code 实例") {
		t.Fatalf("expected empty-state text in replacement card, got %#v", result.ReplaceCurrentCard.CardElements)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no appended gateway operations, got %#v", gateway.operations)
	}
}

func TestHandleGatewayActionReplacesMenuCardForVSCodeDetachedThreadCommands(t *testing.T) {
	tests := []struct {
		name  string
		kind  control.ActionKind
		text  string
		msgID string
	}{
		{name: "use", kind: control.ActionShowThreads, text: "/use", msgID: "om-menu-use-detached-1"},
		{name: "useall", kind: control.ActionShowAllThreads, text: "/useall", msgID: "om-menu-useall-detached-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := &recordingGateway{}
			app := New(":0", ":0", gateway, agentproto.ServerIdentity{
				PID:       42,
				StartedAt: time.Date(2026, 4, 19, 10, 1, 0, 0, time.UTC),
			})
			app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
			app.service.ApplySurfaceAction(control.Action{
				Kind:             control.ActionModeCommand,
				SurfaceSessionID: "surface-1",
				ChatID:           "chat-1",
				ActorUserID:      "user-1",
				Text:             "/mode vscode",
			})

			result := app.HandleGatewayAction(context.Background(), control.Action{
				Kind:             tt.kind,
				GatewayID:        "app-1",
				SurfaceSessionID: "surface-1",
				ChatID:           "chat-1",
				ActorUserID:      "user-1",
				MessageID:        tt.msgID,
				Text:             tt.text,
				Inbound: &control.ActionInboundMeta{
					CardDaemonLifecycleID: app.daemonLifecycleID,
				},
			})

			if result == nil || result.ReplaceCurrentCard == nil {
				t.Fatalf("expected detached %s to replace current card, got %#v", tt.text, result)
			}
			if result.ReplaceCurrentCard.CardTitle == "命令已提交" {
				t.Fatalf("did not expect %s detached state to fall back to submission anchor, got %#v", tt.text, result.ReplaceCurrentCard)
			}
			if !strings.Contains(operationCardText(*result.ReplaceCurrentCard), "请先 /list 选择一个 VS Code 实例") {
				t.Fatalf("expected detached %s guidance in replacement card, got %#v", tt.text, result.ReplaceCurrentCard.CardElements)
			}
			if len(gateway.operations) != 0 {
				t.Fatalf("expected no appended gateway operations, got %#v", gateway.operations)
			}
		})
	}
}

func TestHandleGatewayActionReplacesVSCodeInstanceSelectionCardForAttachResult(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 19, 10, 2, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode vscode",
	})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-vscode-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Source:        "vscode",
		Online:        true,
	})

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-vscode-list-1",
		InstanceID:       "inst-vscode-1",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected attach result to replace current selection card, got %#v", result)
	}
	if !strings.Contains(operationCardText(*result.ReplaceCurrentCard), "已接管 droid") {
		t.Fatalf("expected attach success text in replacement card, got %#v", result.ReplaceCurrentCard.CardElements)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no appended gateway operations, got %#v", gateway.operations)
	}
}

func TestHandleGatewayActionReplacesVSCodeThreadSelectionCardForUseResult(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 19, 10, 3, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode vscode",
	})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-vscode-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Source:        "vscode",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "会话1", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-vscode-1",
	})

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionUseThread,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-vscode-use-1",
		ThreadID:         "thread-1",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected /use result to replace current selection card, got %#v", result)
	}
	cardText := operationCardText(*result.ReplaceCurrentCard)
	if !strings.Contains(cardText, "当前输入目标") || !strings.Contains(cardText, "droid · 会话1") {
		t.Fatalf("expected notice-family thread-selection result in replacement card, got %q / %#v", cardText, result.ReplaceCurrentCard.CardElements)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no appended gateway operations, got %#v", gateway.operations)
	}
}

func TestHandleGatewayActionReplacesMenuCardForSendFileHandoff(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
	})
	workspaceRoot := t.TempDir()
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "headless",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionSendFile,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
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
	if result.ReplaceCurrentCard.CardTitle != "选择要发送的文件" {
		t.Fatalf("unexpected replacement card title: %#v", result.ReplaceCurrentCard)
	}
}

func TestHandleGatewayActionReplacesMenuCardForHelpHandoff(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 19, 9, 10, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionShowCommandHelp,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-menu-help-1",
		Text:             "/help",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected inline replacement result, got %#v", result)
	}
	if result.ReplaceCurrentCard.CardTitle != "命令帮助" {
		t.Fatalf("unexpected help replacement title: %#v", result.ReplaceCurrentCard)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no appended gateway operations, got %#v", gateway.operations)
	}
}

func TestHandleGatewayActionUpdatesMenuCardForSteerAllNoopHandoff(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 19, 9, 11, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionSteerAll,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-menu-steer-1",
		Text:             "/steerall",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result != nil {
		t.Fatalf("expected steerall noop handoff to patch current card asynchronously, got %#v", result)
	}
	if len(gateway.operations) != 1 {
		t.Fatalf("expected one gateway patch op, got %#v", gateway.operations)
	}
	if gateway.operations[0].Kind != feishu.OperationUpdateCard || gateway.operations[0].CardTitle != "没有可并入的排队输入" {
		t.Fatalf("unexpected steerall noop patch: %#v", gateway.operations[0])
	}
}

func TestHandleGatewayActionSealsMenuCardForStopHandoff(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 19, 9, 12, 0, 0, time.UTC),
	})
	var sent []agentproto.Command
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		sent = append(sent, command)
		return nil
	}
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "proj1",
		WorkspaceRoot: "/data/dl/proj1",
		WorkspaceKey:  "/data/dl/proj1",
		ShortName:     "proj1",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "会话1", CWD: "/data/dl/proj1", Loaded: true},
		},
		ActiveThreadID: "thread-1",
		ActiveTurnID:   "turn-1",
	})
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-1",
	})

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionStop,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-menu-stop-1",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected stop handoff to seal current card, got %#v", result)
	}
	if !strings.Contains(operationCardText(*result.ReplaceCurrentCard), "已向当前运行中的 turn 发送停止请求。") {
		t.Fatalf("expected stop replacement card text, got %#v", result.ReplaceCurrentCard.CardElements)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no appended gateway operations, got %#v", gateway.operations)
	}
	if len(sent) != 1 || sent[0].Kind != agentproto.CommandTurnInterrupt {
		t.Fatalf("expected one interrupt command, got %#v", sent)
	}
}

func TestHandleGatewayActionSealsMenuCardForNewThreadHandoff(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 19, 9, 14, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "proj1",
		WorkspaceRoot: "/data/dl/proj1",
		WorkspaceKey:  "/data/dl/proj1",
		ShortName:     "proj1",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "会话1", CWD: "/data/dl/proj1", Loaded: true},
		},
	})
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-1",
	})

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionNewThread,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-menu-new-1",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected new-thread handoff to seal current card, got %#v", result)
	}
	if !strings.Contains(operationCardText(*result.ReplaceCurrentCard), "已准备新建会话") {
		t.Fatalf("expected new-thread replacement card text, got %#v", result.ReplaceCurrentCard.CardElements)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no appended gateway operations, got %#v", gateway.operations)
	}
	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.RouteMode != string(state.RouteModeNewThreadReady) {
		t.Fatalf("expected surface to enter new-thread-ready route, got %#v", snapshot)
	}
}

func TestHandleGatewayActionSealsMenuCardForFollowHandoff(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 19, 9, 16, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode vscode",
	})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "vscode-1",
		WorkspaceRoot:           "/data/dl/proj1",
		WorkspaceKey:            "/data/dl/proj1",
		ShortName:               "vscode-1",
		Source:                  "vscode",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "会话1", CWD: "/data/dl/proj1", Loaded: true},
		},
	})
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionFollowLocal,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-menu-follow-1",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected follow handoff to seal current card, got %#v", result)
	}
	if result.ReplaceCurrentCard.CardTitle == "命令已提交" {
		t.Fatalf("did not expect follow handoff to fall back to submitted-anchor card, got %#v", result.ReplaceCurrentCard)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no appended gateway operations, got %#v", gateway.operations)
	}
	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.RouteMode != string(state.RouteModeFollowLocal) || snapshot.Attachment.SelectedThreadID != "thread-1" {
		t.Fatalf("expected surface to enter follow-local route, got %#v", snapshot)
	}
}

func TestHandleGatewayActionSealsMenuCardForDetachHandoff(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 19, 9, 18, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "proj1",
		WorkspaceRoot: "/data/dl/proj1",
		WorkspaceKey:  "/data/dl/proj1",
		ShortName:     "proj1",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "会话1", CWD: "/data/dl/proj1", Loaded: true},
		},
	})
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionDetach,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-menu-detach-1",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected detach handoff to seal current card, got %#v", result)
	}
	if !strings.Contains(operationCardText(*result.ReplaceCurrentCard), "已断开当前工作区接管") {
		t.Fatalf("expected detach replacement card text, got %#v", result.ReplaceCurrentCard.CardElements)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no appended gateway operations, got %#v", gateway.operations)
	}
	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != "" {
		t.Fatalf("expected detach to clear current attachment, got %#v", snapshot)
	}
}

func TestHandleGatewayActionUpdatesMenuCardForCompactOwnerFlow(t *testing.T) {
	gateway := newLifecycleGateway()
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 19, 9, 0, 0, 0, time.UTC),
	})
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		if command.Kind != agentproto.CommandThreadCompactStart {
			t.Fatalf("unexpected command dispatched during compact menu handoff: %#v", command)
		}
		return nil
	}
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "proj1",
		WorkspaceRoot: "/data/dl/proj1",
		WorkspaceKey:  "/data/dl/proj1",
		ShortName:     "proj1",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "会话1", CWD: "/data/dl/proj1", Loaded: true},
		},
	})
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-1",
	})

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionCompact,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-menu-compact-1",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result != nil {
		t.Fatalf("expected compact menu handoff to stream through gateway updates, got %#v", result)
	}
	ops := gateway.snapshotOperations()
	if len(ops) != 1 {
		t.Fatalf("expected one in-place compact card update, got %#v", ops)
	}
	if ops[0].Kind != feishu.OperationUpdateCard || ops[0].MessageID != "om-menu-compact-1" || ops[0].CardTitle != "正在压缩上下文" {
		t.Fatalf("unexpected compact owner-card update: %#v", ops[0])
	}
}

func TestHandleGatewayActionReplacesMenuCardWhenSendFileUnavailable(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 19, 9, 5, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode vscode",
	})

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionSendFile,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-menu-sendfile-1",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected inline replacement result, got %#v", result)
	}
	if result.ReplaceCurrentCard.CardTitle != "当前不能发送文件" {
		t.Fatalf("unexpected unavailable replacement title: %#v", result.ReplaceCurrentCard)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no appended gateway operations, got %#v", gateway.operations)
	}
}
