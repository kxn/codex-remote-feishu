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

	if result.Status != SurfaceResumeStatusStarting {
		t.Fatalf("expected profile-mismatched visible workspace resume to start matching headless, got %#v", result)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "" || surface.PendingHeadless == nil {
		t.Fatalf("expected visible workspace resume to avoid attaching mismatched workspace and start headless instead, got %#v", surface)
	}
	if surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected fresh-start resume path to stay unbound until launch completes, got %#v", surface)
	}
	if !strings.EqualFold(surface.PendingHeadless.ThreadCWD, "/data/dl/repo") || !surface.PendingHeadless.PrepareNewThread || !strings.EqualFold(surface.ClaimedWorkspaceKey, "/data/dl/repo") {
		t.Fatalf("expected fresh-start resume path to preserve workspace intent in pending headless, got %#v", surface)
	}
	var sawWorkspaceStarting bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "workspace_create_starting" {
			sawWorkspaceStarting = true
		}
	}
	if !sawWorkspaceStarting {
		t.Fatalf("expected fresh-start resume to emit workspace_create_starting, got %#v", events)
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

func TestTryAutoResumeHeadlessSurfaceKeepsStableWorkspaceRootForSyntheticThreadRestore(t *testing.T) {
	now := time.Date(2026, 5, 12, 15, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "app-1", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendClaude, "devseek", "", "")

	events, result := svc.TryAutoResumeHeadlessSurface("surface-1", SurfaceResumeAttempt{
		ThreadID:       "thread-claude",
		ThreadTitle:    "Claude 恢复会话",
		WorkspaceKey:   "/data/dl/repo",
		ThreadCWD:      "/data/dl/repo/web",
		Backend:        agentproto.BackendClaude,
		ResumeHeadless: true,
	}, true)

	if result.Status != SurfaceResumeStatusStarting {
		t.Fatalf("expected synthetic claude restore to start a managed headless, got result=%#v events=%#v", result, events)
	}
	if len(events) != 1 || events[0].DaemonCommand == nil || events[0].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected only start headless command, got %#v", events)
	}
	if got := events[0].DaemonCommand; got.WorkspaceKey != "/data/dl/repo" || got.ThreadCWD != "/data/dl/repo/web" {
		t.Fatalf("expected start headless command to keep stable workspace root separate from last active cwd, got %#v", got)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.PendingHeadless == nil {
		t.Fatalf("expected pending headless launch after synthetic restore, got %#v", surface)
	}
	if surface.PendingHeadless.WorkspaceKey != "/data/dl/repo" || surface.PendingHeadless.ThreadCWD != "/data/dl/repo/web" {
		t.Fatalf("expected pending launch to keep stable workspace root separate from last active cwd, got %#v", surface.PendingHeadless)
	}
	if !strings.EqualFold(surface.ClaimedWorkspaceKey, "/data/dl/repo") {
		t.Fatalf("expected synthetic restore to claim the stable workspace root, got %#v", surface)
	}
}

func TestHeadlessRestoreFailureNoticeWorkspaceBusyUsesGenericRestoreText(t *testing.T) {
	notice := headlessRestoreFailureNotice("workspace_busy")
	if notice == nil {
		t.Fatal("expected notice")
	}
	if strings.Contains(notice.Text, "占用") || strings.Contains(notice.Text, "接管") {
		t.Fatalf("expected generic restore failure text, got %q", notice.Text)
	}
	if !strings.Contains(notice.Text, "暂时无法恢复") {
		t.Fatalf("expected generic restore failure text, got %q", notice.Text)
	}
}

func TestHeadlessRestoreFailureNoticeAcceptsCanonicalWorkspaceBusyCode(t *testing.T) {
	notice := NoticeForHeadlessRestoreFailure("headless_restore_workspace_busy")
	if notice == nil || notice.Code != "headless_restore_workspace_busy" {
		t.Fatalf("expected canonical workspace-busy code to retain its failure kind, got %#v", notice)
	}
}

func TestHeadlessLaunchFailurePreservesStructuredProfileReason(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessCodexSurfaceBackendContract("default"), state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	svc.root.Surfaces["surface-1"].PendingHeadless = &state.HeadlessLaunchRecord{
		InstanceID: "inst-headless-1",
		Purpose:    state.HeadlessLaunchPurposeThreadRestore,
	}

	events := svc.HandleHeadlessLaunchFailed("surface-1", "inst-headless-1", agentproto.ErrorInfo{
		Code:      "profile_secret_missing",
		Message:   "Codex Profile 缺少 API Key。",
		Retryable: false,
	})
	if len(events) != 1 || events[0].Notice == nil {
		t.Fatalf("expected failure notice, got %#v", events)
	}
	if got := events[0].Notice.Code; got != "profile_secret_missing" {
		t.Fatalf("notice code = %q, want profile_secret_missing", got)
	}
	if !strings.Contains(events[0].Notice.Text, "API Key") {
		t.Fatalf("notice text lost structured reason: %q", events[0].Notice.Text)
	}
}

func TestAutoRestoreFailurePreservesStructuredProfileReason(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessCodexSurfaceBackendContract("default"), state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	svc.root.Surfaces["surface-1"].PendingHeadless = &state.HeadlessLaunchRecord{
		InstanceID:  "inst-headless-1",
		AutoRestore: true,
		Purpose:     state.HeadlessLaunchPurposeThreadRestore,
	}

	events := svc.HandleHeadlessLaunchFailed("surface-1", "inst-headless-1", agentproto.ErrorInfo{
		Code:    "oauth_missing",
		Message: "本机 Codex 当前没有可用的 ChatGPT 登录。",
	})
	if len(events) != 1 || events[0].Notice == nil {
		t.Fatalf("expected restore failure notice, got %#v", events)
	}
	if got := events[0].Notice.Code; got != "oauth_missing" {
		t.Fatalf("notice code = %q, want oauth_missing", got)
	}
	if !strings.Contains(events[0].Notice.Text, "登录") {
		t.Fatalf("notice text lost OAuth reason: %q", events[0].Notice.Text)
	}
}

func TestTryAutoResumeHeadlessSurfaceReturnsFailureWhenVisibleAttachIsWorkspaceBusy(t *testing.T) {
	now := time.Date(2026, 7, 26, 18, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessCodexSurfaceBackendContract("default"), state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	svc.MaterializeSurfaceResumeContract("surface-2", "app-2", "chat-2", "user-2", state.HeadlessCodexSurfaceBackendContract("default"), state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceKey:  "/data/dl/repo",
		WorkspaceRoot: "/data/dl/repo",
		Backend:       agentproto.BackendCodex,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", CWD: "/data/dl/repo", Loaded: true},
		},
	})
	if !svc.claimWorkspace(svc.root.Surfaces["surface-2"], "/data/dl/repo") {
		t.Fatal("expected competing surface to claim workspace")
	}

	events, result := svc.TryAutoResumeHeadlessSurface("surface-1", SurfaceResumeAttempt{
		ThreadID:     "thread-1",
		ThreadCWD:    "/data/dl/repo",
		WorkspaceKey: "/data/dl/repo",
		Backend:      agentproto.BackendCodex,
	}, true)
	if result.Status != SurfaceResumeStatusFailed || result.FailureCode != "workspace_busy" {
		t.Fatalf("expected busy visible attach to report failure, got %#v events=%#v", result, events)
	}
}

func TestSurfaceResumeFailureNoticeWorkspaceBusyUsesGenericRestoreText(t *testing.T) {
	notice := surfaceResumeFailureNotice("workspace_busy")
	if notice == nil {
		t.Fatal("expected notice")
	}
	if strings.Contains(notice.Text, "占用") || strings.Contains(notice.Text, "接管") {
		t.Fatalf("expected generic restore failure text, got %q", notice.Text)
	}
	if !strings.Contains(notice.Text, "暂时无法恢复") {
		t.Fatalf("expected generic restore failure text, got %q", notice.Text)
	}
}
