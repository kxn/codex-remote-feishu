package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestTargetPickerUsesResolvedPendingWorkspaceForHighlightAndNewThreadSuccess(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 7, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-repo",
		DisplayName:   "repo",
		WorkspaceRoot: "/data/dl/repo",
		WorkspaceKey:  "/data/dl/repo",
		ShortName:     "repo",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-other",
		DisplayName:   "other",
		WorkspaceRoot: "/data/dl/other",
		WorkspaceKey:  "/data/dl/other",
		ShortName:     "other",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
	surface.ProductMode = state.ProductModeNormal
	surface.PendingHeadless = &state.HeadlessLaunchRecord{
		WorkspaceKey:     "/data/dl/repo",
		ThreadCWD:        "/data/dl/other",
		PrepareNewThread: true,
	}

	record, err := svc.newTargetPickerRecord(surface, control.TargetPickerRequestSourceList, targetPickerOpenOptions{AllowNewThread: true})
	if err != nil {
		t.Fatalf("newTargetPickerRecord error = %v", err)
	}
	view, err := svc.buildTargetPickerView(surface, record)
	if err != nil {
		t.Fatalf("buildTargetPickerView error = %v", err)
	}
	if view.SelectedWorkspaceKey != "/data/dl/other" {
		t.Fatalf("expected picker to highlight resolved pending thread cwd workspace, got %#v", view)
	}
	if view.SelectedSessionValue != targetPickerNewThreadValue || view.ConfirmLabel != "新建会话" || !view.CanConfirm {
		t.Fatalf("expected resolved pending workspace to confirm as new-thread target, got %#v", view)
	}
	if !targetPickerNewThreadSucceeded(surface, "/data/dl/other") {
		t.Fatalf("expected pending new-thread success to match resolved thread cwd workspace, got %#v", surface.PendingHeadless)
	}
	if targetPickerNewThreadSucceeded(surface, "/data/dl/repo") {
		t.Fatalf("did not expect pending new-thread success to match stale workspaceKey, got %#v", surface.PendingHeadless)
	}
}
