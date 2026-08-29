package orchestrator

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestTargetPickerAddWorkspacePathPickerFallsBackFromDeletedCurrentWorkspace(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 37, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	parent := t.TempDir()
	deletedWorkspace := filepath.Join(parent, "deleted-workspace")
	surface := svc.ensureSurface(control.Action{
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	surface.ProductMode = state.ProductModeNormal
	surface.ClaimedWorkspaceKey = deletedWorkspace

	addMode := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionWorkspaceNewDir,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	pathView := singlePathPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerOpenPathPicker,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          addMode.PickerID,
		TargetPickerValue: control.FeishuTargetPickerPathFieldLocalDirectory,
	}))

	if !testutil.SamePath(pathView.CurrentPath, parent) || !testutil.SamePath(pathView.SelectedPath, parent) {
		t.Fatalf("expected deleted current workspace to fall back to parent directory, got %#v", pathView)
	}
	if !pathView.CanConfirm || !pathView.CanGoUp {
		t.Fatalf("expected fallback directory picker to remain usable, got %#v", pathView)
	}
}
