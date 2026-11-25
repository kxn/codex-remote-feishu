package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateHeadlessLateReceiptCannotAlterNewPendingCarrier(t *testing.T) {
	now := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	s := newServiceForTest(&now)
	s.ApplySurfaceAction(control.Action{Kind: control.ActionStatus, SurfaceSessionID: "surface", ChatID: "chat", ActorUserID: "user"})
	surface := s.root.Surfaces["surface"]
	surface.PendingHeadless = &state.HeadlessLaunchRecord{InstanceID: "new", RequestedAt: now, ExpiresAt: now.Add(time.Minute), Status: state.HeadlessLaunchStarting}
	if events := s.HandleHeadlessLaunchStarted("surface", "old", 123); len(events) != 0 {
		t.Fatalf("late receipt generated effects: %+v", events)
	}
	if surface.PendingHeadless == nil || surface.PendingHeadless.InstanceID != "new" || surface.PendingHeadless.PID != 0 {
		t.Fatalf("late receipt changed new launch: %+v", surface.PendingHeadless)
	}
	if events := s.HandleHeadlessLaunchFailed("surface", "old", nil); len(events) != 0 {
		t.Fatalf("old failure changed new launch: %+v", events)
	}
	if surface.PendingHeadless == nil || surface.PendingHeadless.InstanceID != "new" {
		t.Fatal("old failure cleared new launch")
	}
}
