package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestTryAutoResumeHeadlessSurfaceWaitsBeforeFreshWorkspaceFallbackUntilMissingTargetsAllowed(t *testing.T) {
	now := time.Date(2026, 4, 29, 4, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "app-1", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendClaude, "devseek", "", "")

	events, result := svc.TryAutoResumeHeadlessSurface("surface-1", SurfaceResumeAttempt{
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

func TestTryAutoResumeHeadlessSurfaceStartsFreshWorkspaceWhenTargetBackendMissing(t *testing.T) {
	now := time.Date(2026, 4, 29, 4, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "app-1", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendClaude, "devseek", "", "")

	events, result := svc.TryAutoResumeHeadlessSurface("surface-1", SurfaceResumeAttempt{
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

func TestTryAutoResumeHeadlessSurfaceRestoresPreparedNewThreadRouteOnVisibleWorkspace(t *testing.T) {
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

	events, result := svc.TryAutoResumeHeadlessSurface("surface-1", SurfaceResumeAttempt{
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

func TestTryAutoResumeHeadlessSurfacePlainWorkspaceFallbackKeepsUnboundIntent(t *testing.T) {
	now := time.Date(2026, 4, 29, 4, 15, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "app-1", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendClaude, "devseek", "", "")

	events, result := svc.TryAutoResumeHeadlessSurface("surface-1", SurfaceResumeAttempt{
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

func TestTryAutoResumeHeadlessSurfaceLostPinnedThreadStillPreparesNewThreadWhenWorkspaceMissing(t *testing.T) {
	now := time.Date(2026, 4, 29, 4, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "app-1", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendClaude, "devseek", "", "")

	_, result := svc.TryAutoResumeHeadlessSurface("surface-1", SurfaceResumeAttempt{
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

func TestTryAutoResumeHeadlessSurfaceDoesNotReuseCodexHeadlessForClaudeThread(t *testing.T) {
	now := time.Date(2026, 4, 30, 1, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "app-1", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendClaude, "devseek", "", "")
	svc.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{
		byIDByBackend: map[agentproto.Backend]map[string]state.ThreadRecord{
			agentproto.BackendClaude: {
				"thread-claude": {
					ThreadID:      "thread-claude",
					Name:          "Claude 恢复会话",
					CWD:           "/data/dl/repo",
					Loaded:        true,
					LastUsedAt:    now.Add(-time.Minute),
					ListOrder:     1,
					Preview:       "继续修复",
					RuntimeStatus: &agentproto.ThreadRuntimeStatus{Type: "idle"},
				},
			},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-codex-headless",
		DisplayName:   "repo-codex",
		WorkspaceRoot: "/data/dl/repo",
		WorkspaceKey:  "/data/dl/repo",
		ShortName:     "repo-codex",
		Backend:       agentproto.BackendCodex,
		Managed:       true,
		Source:        "headless",
		Online:        true,
	})

	events, result := svc.TryAutoResumeHeadlessSurface("surface-1", SurfaceResumeAttempt{
		ThreadID:       "thread-claude",
		ThreadTitle:    "Claude 恢复会话",
		ThreadCWD:      "/data/dl/repo",
		Backend:        agentproto.BackendClaude,
		ResumeHeadless: true,
	}, true)

	if result.Status != SurfaceResumeStatusStarting {
		t.Fatalf("expected claude resume to start a new headless instead of reusing codex one, got result=%#v events=%#v", result, events)
	}
	if len(events) != 1 {
		t.Fatalf("expected only start headless command for headless restore, got %#v", events)
	}
	if events[0].DaemonCommand == nil || events[0].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected start headless command, got %#v", events)
	}
	if events[0].DaemonCommand.InstanceID == "inst-codex-headless" {
		t.Fatalf("expected resume to avoid reusing codex headless instance, got %#v", events[0].DaemonCommand)
	}
	if events[0].DaemonCommand.ClaudeProfileID != "devseek" {
		t.Fatalf("expected resume start command to carry claude profile, got %#v", events[0].DaemonCommand)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "" {
		t.Fatalf("expected surface to stay unattached while starting claude headless, got %#v", surface)
	}
	if surface.PendingHeadless == nil || surface.PendingHeadless.InstanceID == "inst-codex-headless" {
		t.Fatalf("expected pending launch for fresh claude headless, got %#v", surface.PendingHeadless)
	}
	if surface.Backend != agentproto.BackendClaude {
		t.Fatalf("expected surface backend to remain claude, got %#v", surface)
	}
}
