package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestCompactOwnerCardUpdatesReuseInitialMessageID(t *testing.T) {
	gateway := newLifecycleGateway()
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC),
	})
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		if command.Kind != agentproto.CommandThreadCompactStart {
			t.Fatalf("unexpected command dispatched during compact owner-card test: %#v", command)
		}
		return nil
	}

	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionCompact,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-compact-1",
		Text:             "/compact",
	})

	var initialMessageID string
	for _, op := range gateway.snapshotOperations() {
		if op.CardTitle == "正在压缩上下文" {
			initialMessageID = op.MessageID
			break
		}
	}
	if initialMessageID == "" {
		t.Fatalf("expected initial compact owner card send, got %#v", gateway.snapshotOperations())
	}

	started := app.service.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-compact-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})
	app.handleUIEvents(context.Background(), started)

	completed := app.service.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-compact-1",
		ItemID:   "compact-1",
		ItemKind: "context_compaction",
	})
	app.handleUIEvents(context.Background(), completed)

	var sawRunningUpdate bool
	var sawCompletedUpdate bool
	for _, op := range gateway.snapshotOperations() {
		if op.MessageID != initialMessageID {
			continue
		}
		if op.CardTitle == "正在压缩上下文" && op.Kind == "update_card" {
			sawRunningUpdate = true
		}
		if op.CardTitle == "上下文已压缩" && op.Kind == "update_card" {
			sawCompletedUpdate = true
		}
	}
	if !sawRunningUpdate || !sawCompletedUpdate {
		t.Fatalf("expected compact lifecycle to keep patching the initial owner card, got %#v", gateway.snapshotOperations())
	}
}
