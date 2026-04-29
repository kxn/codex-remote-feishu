package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestTryAutoResumeNormalSurfaceWaitsBeforeFreshWorkspaceFallbackUntilMissingTargetsAllowed(t *testing.T) {
	now := time.Date(2026, 4, 29, 4, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "app-1", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendClaude, "devseek", "", "")

	events, result := svc.TryAutoResumeNormalSurface("surface-1", SurfaceResumeAttempt{
		WorkspaceKey:     "/data/dl/repo",
		Backend:          agentproto.BackendClaude,
		PrepareNewThread: true,
	}, false)

	if len(events) != 0 {
		t.Fatalf("expected no resume events before missing targets are allowed, got %#v", events)
	}
	if result.Status != SurfaceResumeStatusWaiting {
		t.Fatalf("expected waiting before missing targets are allowed, got %#v", result)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.PendingHeadless != nil || strings.TrimSpace(surface.AttachedInstanceID) != "" {
		t.Fatalf("expected surface to stay unattached without pending launch, got %#v", surface)
	}
}

func TestTryAutoResumeNormalSurfaceStartsFreshWorkspaceWhenTargetBackendMissing(t *testing.T) {
	now := time.Date(2026, 4, 29, 4, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "app-1", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendClaude, "devseek", "", "")

	events, result := svc.TryAutoResumeNormalSurface("surface-1", SurfaceResumeAttempt{
		WorkspaceKey:     "/data/dl/repo",
		Backend:          agentproto.BackendClaude,
		PrepareNewThread: true,
	}, true)

	if result.Status != SurfaceResumeStatusStarting {
		t.Fatalf("expected fresh workspace start when target backend workspace is missing, got %#v", result)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.PendingHeadless == nil {
		t.Fatalf("expected pending headless launch after workspace-level resume fallback, got %#v", surface)
	}
	if !surface.PendingHeadless.PrepareNewThread || !strings.EqualFold(surface.PendingHeadless.ThreadCWD, "/data/dl/repo") {
		t.Fatalf("expected pending launch to preserve new-thread-ready workspace intent, got %#v", surface.PendingHeadless)
	}
	if surface.PendingHeadless.ClaudeProfileID != "devseek" {
		t.Fatalf("expected pending launch to keep current claude profile, got %#v", surface.PendingHeadless)
	}
	if !strings.EqualFold(surface.ClaimedWorkspaceKey, "/data/dl/repo") {
		t.Fatalf("expected workspace claim to persist across resume fallback, got %#v", surface)
	}
	if len(events) != 2 {
		t.Fatalf("expected workspace starting notice + start headless command, got %#v", events)
	}
	if events[0].Notice == nil || events[0].Notice.Code != "workspace_create_starting" {
		t.Fatalf("expected workspace_create_starting notice first, got %#v", events)
	}
	if events[1].DaemonCommand == nil || events[1].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected start headless daemon command second, got %#v", events)
	}
	if events[1].DaemonCommand.ClaudeProfileID != "devseek" {
		t.Fatalf("expected start headless command to carry current claude profile, got %#v", events[1].DaemonCommand)
	}
}

func TestTryAutoResumeNormalSurfaceRestoresPreparedNewThreadRouteOnVisibleWorkspace(t *testing.T) {
	now := time.Date(2026, 4, 29, 4, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "app-1", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendClaude, "devseek", "", "")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-claude",
		DisplayName:   "repo-claude",
		WorkspaceRoot: "/data/dl/repo",
		WorkspaceKey:  "/data/dl/repo",
		ShortName:     "repo-claude",
		Backend:       agentproto.BackendClaude,
		Online:        true,
	})

	events, result := svc.TryAutoResumeNormalSurface("surface-1", SurfaceResumeAttempt{
		WorkspaceKey:     "/data/dl/repo",
		Backend:          agentproto.BackendClaude,
		PrepareNewThread: true,
	}, true)

	if result.Status != SurfaceResumeStatusWorkspaceAttached {
		t.Fatalf("expected visible workspace resume to attach directly, got %#v", result)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "inst-claude" || surface.PendingHeadless != nil {
		t.Fatalf("expected visible workspace resume to attach existing backend workspace, got %#v", surface)
	}
	if surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeNewThreadReady {
		t.Fatalf("expected prepared workspace resume to land in new_thread_ready, got %#v", surface)
	}
	if !strings.EqualFold(surface.PreparedThreadCWD, "/data/dl/repo") || !strings.EqualFold(surface.ClaimedWorkspaceKey, "/data/dl/repo") {
		t.Fatalf("expected prepared workspace route to stay on repo, got %#v", surface)
	}
	var sawPreparedSelection, sawReadyNotice bool
	for _, event := range events {
		if event.ThreadSelection != nil && event.ThreadSelection.RouteMode == string(state.RouteModeNewThreadReady) {
			sawPreparedSelection = true
		}
		if event.Notice != nil && event.Notice.Code == "new_thread_ready" {
			sawReadyNotice = true
		}
	}
	if !sawPreparedSelection || !sawReadyNotice {
		t.Fatalf("expected prepared workspace resume to emit new-thread-ready events, got %#v", events)
	}
}

func TestTryAutoResumeNormalSurfacePlainWorkspaceFallbackKeepsUnboundIntent(t *testing.T) {
	now := time.Date(2026, 4, 29, 4, 15, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "app-1", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendClaude, "devseek", "", "")

	events, result := svc.TryAutoResumeNormalSurface("surface-1", SurfaceResumeAttempt{
		WorkspaceKey: "/data/dl/repo",
		Backend:      agentproto.BackendClaude,
	}, true)

	if result.Status != SurfaceResumeStatusStarting {
		t.Fatalf("expected workspace fallback to start fresh workspace, got %#v", result)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.PendingHeadless == nil || !strings.EqualFold(surface.PendingHeadless.ThreadCWD, "/data/dl/repo") {
		t.Fatalf("expected pending workspace launch for plain workspace resume, got %#v", surface)
	}
	if surface.PendingHeadless.PrepareNewThread {
		t.Fatalf("expected plain workspace resume to keep unbound workspace intent, got %#v", surface.PendingHeadless)
	}
	if len(events) != 2 || events[1].DaemonCommand == nil || events[1].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected workspace start notice + start headless command, got %#v", events)
	}
}

func TestTryAutoResumeNormalSurfaceLostPinnedThreadStillPreparesNewThreadWhenWorkspaceMissing(t *testing.T) {
	now := time.Date(2026, 4, 29, 4, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "app-1", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendClaude, "devseek", "", "")

	_, result := svc.TryAutoResumeNormalSurface("surface-1", SurfaceResumeAttempt{
		ThreadID:     "thread-missing",
		WorkspaceKey: "/data/dl/repo",
		Backend:      agentproto.BackendClaude,
	}, true)

	if result.Status != SurfaceResumeStatusStarting {
		t.Fatalf("expected missing pinned thread fallback to start fresh workspace, got %#v", result)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.PendingHeadless == nil || !surface.PendingHeadless.PrepareNewThread {
		t.Fatalf("expected missing pinned thread fallback to preserve new-thread-ready intent, got %#v", surface)
	}
}
