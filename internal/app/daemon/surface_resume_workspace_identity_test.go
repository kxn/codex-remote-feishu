package daemon

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestDaemonPendingWorkspaceRouteResumeTargetUsesPendingWorkspaceRootWhenClaimConflicts(t *testing.T) {
	app := newRestoreHintTestApp(t.TempDir())
	app.service.MaterializeSurfaceResumeContract("surface-1", "", "chat-1", "user-1", state.HeadlessCodexSurfaceBackendContract("default"), "", "")
	surface := app.service.Surface("surface-1")
	surface.ClaimedWorkspaceKey = "/data/dl/old"
	surface.PendingHeadless = &state.HeadlessLaunchRecord{
		WorkspaceKey:     "/data/dl/repo",
		ThreadCWD:        "/data/dl/repo/pkg",
		PrepareNewThread: true,
		Purpose:          state.HeadlessLaunchPurposeWorkspaceRouteRestart,
	}

	target, _, ok := app.currentSurfaceResumeTargetAndWorkspaceLocked(surface)
	if !ok {
		t.Fatal("expected pending workspace-route restart to produce a resume target")
	}
	want := state.ResolveHeadlessResumeWorkspaceKey("/data/dl/repo", "/data/dl/repo/pkg")
	if target.ResumeWorkspaceKey != want {
		t.Fatalf("expected pending workspace-route restart resume key %q, got %#v", want, target)
	}
}

func TestDaemonPendingThreadRestoreResumeTargetUsesPendingWorkspaceRootWhenClaimConflicts(t *testing.T) {
	app := newRestoreHintTestApp(t.TempDir())
	app.service.MaterializeSurfaceResumeContract("surface-1", "", "chat-1", "user-1", state.HeadlessCodexSurfaceBackendContract("default"), "", "")
	surface := app.service.Surface("surface-1")
	surface.ClaimedWorkspaceKey = "/data/dl/old"
	surface.PendingHeadless = &state.HeadlessLaunchRecord{
		ThreadID:     "thread-1",
		WorkspaceKey: "/data/dl/repo",
		ThreadCWD:    "/data/dl/repo/pkg",
		Purpose:      state.HeadlessLaunchPurposeThreadRestore,
	}

	target, _, ok := app.currentSurfaceResumeTargetAndWorkspaceLocked(surface)
	if !ok {
		t.Fatal("expected pending thread restore to produce a resume target")
	}
	want := state.ResolveHeadlessResumeWorkspaceKey("/data/dl/repo", "/data/dl/repo/pkg")
	if target.ResumeWorkspaceKey != want {
		t.Fatalf("expected pending thread restore resume key %q, got %#v", want, target)
	}
}
