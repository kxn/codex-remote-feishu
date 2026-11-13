package daemon

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestAutomaticResumeWorkspaceConflictDoesNotFloodNotices(t *testing.T) {
	stateDir, workspace := testutil.CanonicalPath(t.TempDir()), t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
		SurfaceSessionID: "recovering", GatewayID: "recovering", ChatID: "recovering", ActorUserID: "user",
		ProductMode: "normal", Backend: "codex", ResumeThreadID: "memory-thread", ResumeThreadTitle: "Memory",
		ResumeThreadCWD: workspace, ResumeWorkspaceKey: workspace, ResumeRouteMode: "pinned", ResumeHeadless: true,
	})
	app := newRestoreHintTestApp(stateDir)
	app.service.MaterializeSurfaceResume("owner", "owner", "owner", "user", state.ProductModeNormal, agentproto.BackendCodex, "", "", "")
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID: "owner-instance", WorkspaceRoot: workspace, WorkspaceKey: workspace,
		Source: "headless", Backend: agentproto.BackendCodex, Managed: true, Online: true,
		Threads: map[string]*state.ThreadRecord{
			"owner-thread": {ThreadID: "owner-thread", CWD: workspace, Loaded: true},
		},
	})
	app.service.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "owner", ChatID: "owner", ActorUserID: "user", ThreadID: "owner-thread"})
	if got := app.service.SurfaceSnapshot("owner").Attachment.InstanceID; got != "owner-instance" {
		t.Fatalf("fixture did not establish the other bot's ownership: %q", got)
	}
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID: "idle-pool", WorkspaceRoot: testutil.CanonicalPath(t.TempDir()),
		Source: "headless", Backend: agentproto.BackendCodex, Managed: true, Online: true,
		Threads: map[string]*state.ThreadRecord{},
	})
	now := time.Date(2026, 10, 22, 7, 21, 35, 0, time.UTC)
	notices := 0
	for i := 0; i < 20; i++ {
		for _, event := range app.maybeRecoverHeadlessSurfacesLocked(now.Add(time.Duration(i) * 100 * time.Millisecond)) {
			if event.Notice != nil {
				notices++
			}
		}
	}
	if notices != 0 {
		t.Fatalf("automatic workspace conflict must back off silently, emitted %d notices in 2 seconds", notices)
	}
	recovery := app.surfaceResumeRuntime.recovery["recovering"]
	if recovery == nil || recovery.NextAttemptAt.Before(now.Add(29*time.Second)) {
		t.Fatalf("workspace conflict must preserve retry backoff, got %#v", recovery)
	}
}
