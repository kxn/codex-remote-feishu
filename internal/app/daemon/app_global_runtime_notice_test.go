package daemon

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestGlobalRuntimeNoticeSuppressionForTransportDegraded(t *testing.T) {
	app := New(":0", ":0", nil, serverIdentityForTest())
	event := control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "msg-1",
		Notice: &control.Notice{
			Code:             "attached_instance_transport_degraded",
			Text:             "实例离线。",
			DeliveryClass:    control.NoticeDeliveryClassGlobalRuntime,
			DeliveryFamily:   control.NoticeDeliveryFamilyTransportDegraded,
			DeliveryDedupKey: "attached_instance_transport_degraded",
		},
	}

	normalized, ok := normalizeGlobalRuntimeNoticeEvent(event)
	if !ok {
		t.Fatalf("expected global runtime notice")
	}
	if normalized.SourceMessageID != "" {
		t.Fatalf("expected runtime notice normalization to clear reply anchor, got %#v", normalized)
	}

	now := time.Now()
	if app.shouldSuppressGlobalRuntimeNoticeLocked(normalized, now) {
		t.Fatalf("expected first notice not to be suppressed")
	}
	app.recordGlobalRuntimeNoticeLocked(normalized, now)
	if !app.shouldSuppressGlobalRuntimeNoticeLocked(normalized, now.Add(5*time.Second)) {
		t.Fatalf("expected second notice inside throttle window to be suppressed")
	}
	if app.shouldSuppressGlobalRuntimeNoticeLocked(normalized, now.Add(20*time.Second)) {
		t.Fatalf("expected notice after throttle window to pass")
	}
}

func TestQueueGlobalRuntimeNoticeDedupesPendingEvents(t *testing.T) {
	app := New(":0", ":0", nil, serverIdentityForTest())
	event := control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: "surface-1",
		Notice: &control.Notice{
			Code:             "gateway_apply_failed",
			Text:             "服务无法把消息发送到飞书。",
			DeliveryClass:    control.NoticeDeliveryClassGlobalRuntime,
			DeliveryFamily:   control.NoticeDeliveryFamilyGatewayApplyFailure,
			DeliveryDedupKey: "gateway_apply_failed",
		},
	}

	app.queueGlobalRuntimeNoticeLocked(event)
	app.queueGlobalRuntimeNoticeLocked(event)

	if got := len(app.pendingGlobalRuntimeNotices["surface-1"]); got != 1 {
		t.Fatalf("expected one queued runtime notice, got %d", got)
	}
}
