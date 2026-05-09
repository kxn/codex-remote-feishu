package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestRecordUIEventDeliveryMarksRequestVisible(t *testing.T) {
	gateway := &messageIDAssigningGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "主线程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	app.service.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", GatewayID: "app-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	surface := app.service.Surface("surface-1")
	surface.PendingRequests["req-1"] = &state.RequestPromptRecord{
		RequestID:             "req-1",
		RequestType:           "approval",
		InstanceID:            "inst-1",
		ThreadID:              "thread-1",
		TurnID:                "turn-1",
		OwnerSurfaceSessionID: "surface-1",
		OwnerGatewayID:        "app-1",
		OwnerChatID:           "chat-1",
		VisibilityState:       "pending_visibility",
		CardRevision:          1,
		Title:                 "需要确认",
	}
	surface.PendingRequestOrder = []string{"req-1"}

	err := app.deliverUIEvent(eventcontract.Event{
		Kind:             eventcontract.KindRequest,
		SurfaceSessionID: "surface-1",
		RequestView: &control.FeishuRequestView{
			RequestID:   "req-1",
			RequestType: "approval",
			Title:       "需要确认",
		},
	})
	if err != nil {
		t.Fatalf("deliverUIEvent returned error: %v", err)
	}
	record := app.service.PendingRequest("surface-1", "req-1")
	if record == nil || record.VisibleMessageID == "" || record.VisibilityState != "visible" {
		t.Fatalf("expected visible request state, got %#v", record)
	}
}

func TestQueueGatewayFailureNoticeMarksRequestDegraded(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "主线程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	app.service.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", GatewayID: "app-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	surface := app.service.Surface("surface-1")
	surface.PendingRequests["req-1"] = &state.RequestPromptRecord{
		RequestID:             "req-1",
		RequestType:           "approval",
		InstanceID:            "inst-1",
		ThreadID:              "thread-1",
		TurnID:                "turn-1",
		OwnerSurfaceSessionID: "surface-1",
		OwnerGatewayID:        "app-1",
		OwnerChatID:           "chat-1",
		VisibilityState:       "pending_visibility",
		CardRevision:          1,
		Title:                 "需要确认",
	}
	surface.PendingRequestOrder = []string{"req-1"}

	app.queueGatewayFailureNotice(eventcontract.Event{
		Kind:             eventcontract.KindRequest,
		SurfaceSessionID: "surface-1",
		RequestView: &control.FeishuRequestView{
			RequestID:   "req-1",
			RequestType: "approval",
			Title:       "需要确认",
		},
	}, errors.New("gateway down"))

	record := app.service.PendingRequest("surface-1", "req-1")
	if record == nil || record.VisibilityState != "delivery_degraded" || !record.NeedsRedelivery {
		t.Fatalf("expected degraded request after gateway failure, got %#v", record)
	}
}

func TestHandleActionReplaysDegradedPendingRequestWithoutChangingActionResult(t *testing.T) {
	gateway := &messageIDAssigningGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "主线程", CWD: "/data/dl/droid", Loaded: true},
			"thread-2": {ThreadID: "thread-2", Name: "第二线程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	app.service.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", GatewayID: "app-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	surface := app.service.Surface("surface-1")
	surface.PendingRequests["req-1"] = &state.RequestPromptRecord{
		RequestID:             "req-1",
		RequestType:           "approval",
		InstanceID:            "inst-1",
		ThreadID:              "thread-1",
		TurnID:                "turn-1",
		OwnerSurfaceSessionID: "surface-1",
		OwnerGatewayID:        "app-1",
		OwnerChatID:           "chat-1",
		VisibilityState:       "delivery_degraded",
		NeedsRedelivery:       true,
		CardRevision:          1,
		Title:                 "需要确认",
	}
	surface.PendingRequestOrder = []string{"req-1"}

	result := app.handleAction(context.Background(), control.Action{
		Kind:             control.ActionFollowLocal,
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-1",
	})
	if result != nil {
		t.Fatalf("expected no synchronous replacement from /follow notice path, got %#v", result)
	}
	record := app.service.PendingRequest("surface-1", "req-1")
	if record == nil || record.VisibilityState != "visible" || record.VisibleMessageID == "" {
		t.Fatalf("expected action side-effect to restore request visibility, got %#v", record)
	}
	ops := gateway.snapshotOperations()
	if len(ops) < 2 {
		t.Fatalf("expected notice plus replayed request card, got %#v", ops)
	}
}
