package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestNonStatusDoesNotDiscardRequestReplayOrIncrementAttempt(t *testing.T) {
	now := time.Now()
	s := newServiceForTest(&now)
	s.MaterializeSurface("s", "g", "chat", "user")
	v := s.Surface("s")
	v.PendingRequests = map[string]*state.RequestPromptRecord{"r": {RequestID: "r", RequestType: "approval", Title: "待确认", VisibilityState: "delivery_degraded", LifecycleState: "awaiting_visibility", NeedsRedelivery: true}}
	v.PendingRequestOrder = []string{"r"}
	s.ApplySurfaceAction(control.Action{Kind: control.ActionShowCommandMenu, GatewayID: "g", SurfaceSessionID: "s", ChatID: "chat", ActorUserID: "user", Text: "/menu"})
	if v.PendingRequests["r"].DeliveryAttemptCount != 0 {
		t.Fatal("non-status action changed request delivery attempt without emitting it")
	}
	if events := s.ReplayActivePendingRequestVisibility("s"); len(events) != 1 || events[0].RequestView == nil {
		t.Fatalf("legacy explicit replay lost: %+v", events)
	}
	if v.PendingRequests["r"].DeliveryAttemptCount != 1 {
		t.Fatal("explicit replay did not count exactly once")
	}
}
