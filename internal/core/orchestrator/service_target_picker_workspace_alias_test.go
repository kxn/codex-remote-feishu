package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestTargetPickerConfirmResolvesHostWorkspaceAliasBeforeLiveAcceptance(t *testing.T) {
	now := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspace := createTargetPickerGitRepo(t)
	alias := filepath.Join(t.TempDir(), "workspace-alias")
	if err := os.Symlink(workspace, alias); err != nil {
		t.Skipf("host cannot create a workspace alias: %v", err)
	}
	svc.UpsertInstance(&state.InstanceRecord{InstanceID: "git", WorkspaceRoot: workspace, WorkspaceKey: workspace, Online: true, Threads: map[string]*state.ThreadRecord{}})
	initial := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{Kind: control.ActionListInstances, SurfaceSessionID: "surface-1", ActorUserID: "user-1"}))
	canonical := state.ResolveWorkspaceClaimKey(workspace)
	if canonical == state.ResolveWorkspaceKey(alias) {
		t.Fatal("fixture alias must differ from the canonical workspace identity")
	}
	// The draft path accepts only identities that were actually emitted. It
	// must not visit the host to interpret an arbitrary alias supplied here.
	draft := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{Kind: control.ActionTargetPickerSelectWorkspace, SurfaceSessionID: "surface-1", ActorUserID: "user-1", PickerID: initial.PickerID, WorkspaceKey: alias}))
	if draft.SelectedWorkspaceKey != canonical || len(draft.Messages) == 0 {
		t.Fatal("an unissued alias must not replace the canonical draft selection")
	}
	// Confirmation is a different boundary: callers may supply a real host
	// alias, which is canonicalized before fresh eligibility is compared.
	events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionTargetPickerConfirm, SurfaceSessionID: "surface-1", ActorUserID: "user-1", PickerID: initial.PickerID, WorkspaceKey: alias, TargetPickerValue: targetPickerWorktreeCreateValue})
	view := singleTargetPickerEvent(t, events)
	if view.Page != control.FeishuTargetPickerPageWorktree || view.SelectedWorkspaceKey != canonical || !view.CanGoBack {
		t.Fatalf("live confirmation failed to resolve the workspace alias: %#v", view)
	}
	for _, event := range events {
		if event.DaemonCommand != nil {
			t.Fatal("opening the worktree form must not create a worktree")
		}
	}
}
