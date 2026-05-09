package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestRequestDeliverySuccessMarksActiveRequestVisible(t *testing.T) {
	now := time.Date(2026, 5, 8, 15, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "主线程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", GatewayID: "app-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})
	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-1",
		Metadata: map[string]any{
			"requestType": "approval",
			"title":       "需要确认",
		},
	})
	if len(events) != 1 || events[0].RequestView == nil {
		t.Fatalf("expected request prompt event, got %#v", events)
	}
	record := svc.root.Surfaces["surface-1"].PendingRequests["req-1"]
	if record == nil || record.VisibilityState != requestVisibilityPendingVisibility {
		t.Fatalf("expected pending visibility request, got %#v", record)
	}

	deliveredAt := now.Add(2 * time.Second)
	svc.RecordRequestPromptDelivery(RequestDeliveryReport{
		SurfaceSessionID: "surface-1",
		RequestID:        "req-1",
		MessageID:        "om-request-1",
		DeliveredAt:      deliveredAt,
	})

	record = svc.root.Surfaces["surface-1"].PendingRequests["req-1"]
	if record == nil || record.VisibilityState != requestVisibilityVisible || record.VisibleMessageID != "om-request-1" {
		t.Fatalf("expected visible request with message id, got %#v", record)
	}
	if !record.VisibleAt.Equal(deliveredAt) || record.NeedsRedelivery {
		t.Fatalf("expected visible timestamp and no redelivery, got %#v", record)
	}
}

func TestRequestDeliveryFailureStatusTriggersStatusRedelivery(t *testing.T) {
	now := time.Date(2026, 5, 8, 15, 5, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "主线程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", GatewayID: "app-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-1",
		Metadata: map[string]any{
			"requestType": "approval",
			"title":       "需要确认",
		},
	})
	svc.RecordRequestPromptDeliveryFailure("surface-1", "req-1", errTest("send failed"))

	record := svc.root.Surfaces["surface-1"].PendingRequests["req-1"]
	if record == nil || record.VisibilityState != requestVisibilityDeliveryDegraded || !record.NeedsRedelivery {
		t.Fatalf("expected degraded request, got %#v", record)
	}

	statusEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionStatus,
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(statusEvents) < 2 || statusEvents[0].Snapshot == nil || statusEvents[1].RequestView == nil {
		t.Fatalf("expected snapshot plus request redelivery, got %#v", statusEvents)
	}
	if statusEvents[1].RequestView.MessageID != "" {
		t.Fatalf("expected degraded redelivery to resend instead of patch old anchor, got %#v", statusEvents[1].RequestView)
	}
	if statusEvents[1].RequestView.StatusText == "" {
		t.Fatalf("expected degraded redelivery status text, got %#v", statusEvents[1].RequestView)
	}
}

func TestResolvePromotesNextQueuedRequestThroughVisibilityEntry(t *testing.T) {
	now := time.Date(2026, 5, 8, 15, 10, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "主线程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", GatewayID: "app-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	surface := svc.root.Surfaces["surface-1"]
	surface.PendingRequests["req-1"] = &state.RequestPromptRecord{
		RequestID:             "req-1",
		RequestType:           "approval",
		InstanceID:            "inst-1",
		ThreadID:              "thread-1",
		TurnID:                "turn-1",
		OwnerSurfaceSessionID: "surface-1",
		OwnerGatewayID:        "app-1",
		OwnerChatID:           "chat-1",
		VisibilityState:       requestVisibilityVisible,
		VisibleMessageID:      "om-1",
		CardRevision:          1,
		CreatedAt:             now,
	}
	surface.PendingRequests["req-2"] = &state.RequestPromptRecord{
		RequestID:             "req-2",
		RequestType:           "approval",
		InstanceID:            "inst-1",
		ThreadID:              "thread-1",
		TurnID:                "turn-2",
		OwnerSurfaceSessionID: "surface-1",
		OwnerGatewayID:        "app-1",
		OwnerChatID:           "chat-1",
		VisibilityState:       requestVisibilityPendingVisibility,
		CardRevision:          1,
		CreatedAt:             now.Add(time.Second),
		Title:                 "第二条请求",
	}
	surface.PendingRequestOrder = []string{"req-1", "req-2"}

	events := svc.resolvePendingRequestOnSurface(surface, surface.PendingRequests["req-1"], agentproto.Event{
		Kind:      agentproto.EventRequestResolved,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-1",
	})
	if len(events) != 1 || events[0].RequestView == nil {
		t.Fatalf("expected next request activation event, got %#v", events)
	}
	if got := events[0].RequestView.RequestID; got != "req-2" {
		t.Fatalf("expected req-2 activation, got %#v", events[0].RequestView)
	}
	if events[0].RequestView.MessageID != "" {
		t.Fatalf("expected promoted queued request to start without anchor, got %#v", events[0].RequestView)
	}
}

func TestPendingRequestNoticeTextDistinguishesVisibilityState(t *testing.T) {
	pendingText := pendingRequestNoticeText(&state.RequestPromptRecord{
		RequestType:     "approval",
		SemanticKind:    control.RequestSemanticApproval,
		VisibilityState: requestVisibilityPendingVisibility,
	})
	if !strings.Contains(pendingText, "正在尝试把确认卡片显示到前台") {
		t.Fatalf("expected pending visibility blocker text, got %q", pendingText)
	}

	degradedText := pendingRequestNoticeText(&state.RequestPromptRecord{
		RequestType:     "approval",
		SemanticKind:    control.RequestSemanticApproval,
		VisibilityState: requestVisibilityDeliveryDegraded,
	})
	if !strings.Contains(degradedText, "尚未成功送达前台") {
		t.Fatalf("expected degraded visibility blocker text, got %q", degradedText)
	}
}

type requestDeliveryErr string

func (e requestDeliveryErr) Error() string { return string(e) }

func errTest(text string) error { return requestDeliveryErr(text) }
