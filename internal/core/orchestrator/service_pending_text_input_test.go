package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestStoreAndTakePendingTextInput(t *testing.T) {
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessCodexSurfaceBackendContract("default"), state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	surface := svc.root.Surfaces["surface-1"]

	svc.storePendingTextInput(surface, "hello world", nil, "msg-1", "user-1", "msg-1", nil)
	if !svc.hasPendingTextInput(surface) {
		t.Fatal("expected hasPendingTextInput to be true after store")
	}

	pending := svc.takePendingTextInput(surface)
	if pending == nil {
		t.Fatal("expected non-nil pending input")
	}
	if pending.Text != "hello world" {
		t.Fatalf("text = %q, want %q", pending.Text, "hello world")
	}
	if pending.SourceMessageID != "msg-1" {
		t.Fatalf("sourceMessageID = %q, want %q", pending.SourceMessageID, "msg-1")
	}
	// take clears the pending
	if svc.hasPendingTextInput(surface) {
		t.Fatal("expected hasPendingTextInput to be false after take")
	}
}

func TestTakePendingTextInputReturnsNilWhenExpired(t *testing.T) {
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessCodexSurfaceBackendContract("default"), state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	surface := svc.root.Surfaces["surface-1"]

	svc.storePendingTextInput(surface, "old message", nil, "msg-1", "user-1", "msg-1", nil)

	// Advance time past TTL
	now = now.Add(pendingTextInputTTL + time.Second)

	if svc.hasPendingTextInput(surface) {
		t.Fatal("expected hasPendingTextInput to be false after expiry")
	}
	pending := svc.takePendingTextInput(surface)
	if pending != nil {
		t.Fatalf("expected nil pending after expiry, got %#v", pending)
	}
}

func TestClearPendingTextInput(t *testing.T) {
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessCodexSurfaceBackendContract("default"), state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	surface := svc.root.Surfaces["surface-1"]

	svc.storePendingTextInput(surface, "hello", []agentproto.Input{{Type: agentproto.InputText, Text: "hi"}}, "msg-1", "user-1", "msg-1", []string{"img-1"})
	svc.clearPendingTextInput(surface)
	if svc.hasPendingTextInput(surface) {
		t.Fatal("expected hasPendingTextInput to be false after clear")
	}
}

func TestStorePendingTextInputOverwritesPrevious(t *testing.T) {
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessCodexSurfaceBackendContract("default"), state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	surface := svc.root.Surfaces["surface-1"]

	svc.storePendingTextInput(surface, "first", nil, "msg-1", "user-1", "msg-1", nil)
	svc.storePendingTextInput(surface, "second", nil, "msg-2", "user-1", "msg-2", nil)

	pending := svc.takePendingTextInput(surface)
	if pending == nil || pending.Text != "second" {
		t.Fatalf("expected second message, got %#v", pending)
	}
}

func TestPendingTextInputClearOnNilSurface(t *testing.T) {
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	// Should not panic
	svc.storePendingTextInput(nil, "text", nil, "msg", "user", "msg", nil)
	svc.clearPendingTextInput(nil)
	if svc.hasPendingTextInput(nil) {
		t.Fatal("expected false for nil surface")
	}
	if svc.takePendingTextInput(nil) != nil {
		t.Fatal("expected nil for nil surface")
	}
}
