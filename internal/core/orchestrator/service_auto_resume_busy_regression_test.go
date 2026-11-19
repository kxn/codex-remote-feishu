package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

// Reproduce startup with a persisted thread whose workspace is already owned
// by another bot and a compatible idle prewarm process available for reuse.
func TestAutoResumeHeadlessWorkspaceConflictDoesNotReportAttached(t *testing.T) {
	now := time.Date(2026, 10, 22, 7, 21, 35, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspace := testutil.CanonicalPath(t.TempDir())
	for _, id := range []string{"owner", "recovering"} {
		svc.MaterializeSurfaceResume(id, id, id, "user", state.ProductModeNormal, agentproto.BackendCodex, "", "", "")
	}
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "owner-instance", WorkspaceRoot: workspace, WorkspaceKey: workspace,
		Source: "headless", Backend: agentproto.BackendCodex, Managed: true, Online: true,
		Threads: map[string]*state.ThreadRecord{"owner-thread": {ThreadID: "owner-thread", CWD: workspace, Loaded: true}},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "owner", ChatID: "owner", ActorUserID: "user", ThreadID: "owner-thread"})
	owner := svc.root.Surfaces["owner"]
	if owner.AttachedInstanceID != "owner-instance" {
		t.Fatalf("fixture did not establish the owner's attachment: %#v", owner)
	}
	ownerWorkspace := owner.ClaimedWorkspaceKey
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "idle-pool", WorkspaceRoot: testutil.CanonicalPath(t.TempDir()),
		Source: "headless", Backend: agentproto.BackendCodex, Managed: true, Online: true,
		Threads: map[string]*state.ThreadRecord{},
	})
	thread := state.ThreadRecord{ThreadID: "memory-thread", Name: "Memory", CWD: workspace}
	svc.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{
		recent: []state.ThreadRecord{thread}, byID: map[string]state.ThreadRecord{thread.ThreadID: thread},
	})
	events, result := svc.TryAutoResumeHeadlessSurface("recovering", SurfaceResumeAttempt{
		ThreadID: thread.ThreadID, ThreadTitle: thread.Name, ThreadCWD: workspace,
		WorkspaceKey: workspace, Backend: agentproto.BackendCodex, ResumeHeadless: true,
	}, true)
	if result.Status != SurfaceResumeStatusFailed || result.FailureCode != "workspace_busy" {
		t.Fatalf("workspace rejection must report failed, not attached: result=%#v events=%#v", result, events)
	}
	if got := svc.SurfaceSnapshot("recovering").Attachment.InstanceID; got != "" {
		t.Fatalf("failed recovery must remain detached, got %q", got)
	}
	if owner.AttachedInstanceID != "owner-instance" || owner.ClaimedWorkspaceKey != ownerWorkspace {
		t.Fatal("failed recovery must preserve the other bot's workspace")
	}
}
