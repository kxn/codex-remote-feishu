package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestTargetPickerWorktreeDraftProjectsCorePreview(t *testing.T) {
	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	repoRoot := createTargetPickerGitRepo(t)

	state := svc.buildTargetPickerWorktreeState(&activeTargetPickerRecord{
		PickerID:              "picker-1",
		Source:                control.TargetPickerRequestSourceWorktree,
		SelectedWorkspaceKey:  repoRoot,
		WorktreeDirectoryName: "draft-dir",
	})
	if !testutil.SamePath(state.FinalPath, filepath.Join(filepath.Dir(repoRoot), "draft-dir")) {
		t.Fatalf("FinalPath = %q, want %q", state.FinalPath, filepath.Join(filepath.Dir(repoRoot), "draft-dir"))
	}
	if state.CanConfirm {
		t.Fatalf("expected draft preview to block confirm, got %#v", state)
	}
	if len(state.Messages) != 0 {
		t.Fatalf("expected no messages for clean draft, got %#v", state.Messages)
	}
}

func TestTargetPickerWorktreeEmptyDraftStaysEmpty(t *testing.T) {
	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	repoRoot := createTargetPickerGitRepo(t)

	state := svc.buildTargetPickerWorktreeState(&activeTargetPickerRecord{
		PickerID:             "picker-1",
		Source:               control.TargetPickerRequestSourceWorktree,
		SelectedWorkspaceKey: repoRoot,
	})
	if state.FinalPath != "" || state.CanConfirm || len(state.Messages) != 0 {
		t.Fatalf("expected empty draft state, got %#v", state)
	}
}

func TestTargetPickerWorktreeCanConfirmProjectsCorePreview(t *testing.T) {
	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	repoRoot := createTargetPickerGitRepo(t)

	state := svc.buildTargetPickerWorktreeState(&activeTargetPickerRecord{
		PickerID:              "picker-1",
		Source:                control.TargetPickerRequestSourceWorktree,
		SelectedWorkspaceKey:  repoRoot,
		WorktreeBranchName:    "feat/login",
		WorktreeDirectoryName: "web-login",
	})
	if !state.CanConfirm {
		t.Fatalf("expected confirmable preview, got %#v", state)
	}
	if !testutil.SamePath(state.FinalPath, filepath.Join(filepath.Dir(repoRoot), "web-login")) {
		t.Fatalf("FinalPath = %q, want %q", state.FinalPath, filepath.Join(filepath.Dir(repoRoot), "web-login"))
	}
	if len(state.Messages) != 0 {
		t.Fatalf("expected no messages for valid preview, got %#v", state.Messages)
	}
}

func TestTargetPickerWorktreeProjectsGitmetaErrorText(t *testing.T) {
	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	repoRoot := createTargetPickerGitRepo(t)
	destinationPath := filepath.Join(filepath.Dir(repoRoot), "occupied-dir")
	if err := os.MkdirAll(destinationPath, 0o755); err != nil {
		t.Fatalf("mkdir occupied destination: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(destinationPath) })

	state := svc.buildTargetPickerWorktreeState(&activeTargetPickerRecord{
		PickerID:              "picker-1",
		Source:                control.TargetPickerRequestSourceWorktree,
		SelectedWorkspaceKey:  repoRoot,
		WorktreeBranchName:    "feat/occupied",
		WorktreeDirectoryName: "occupied-dir",
	})
	if state.CanConfirm {
		t.Fatalf("expected destination-exists to block confirm, got %#v", state)
	}
	if !testutil.SamePath(state.FinalPath, destinationPath) {
		t.Fatalf("FinalPath = %q, want %q", state.FinalPath, destinationPath)
	}
	wantText := gitmeta.WorktreeCreateErrorText(&gitmeta.WorktreeCreateError{Code: gitmeta.WorktreeCreateErrorDestinationExists})
	if len(state.Messages) != 1 || state.Messages[0].Text != wantText {
		t.Fatalf("expected picker message to equal gitmeta error text, got %#v want %q", state.Messages, wantText)
	}
}

func TestTargetPickerWorktreeNonGitWorkspaceProjectsGitmetaText(t *testing.T) {
	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	baseDir := createTargetPickerNonGitDir(t)

	state := svc.buildTargetPickerWorktreeState(&activeTargetPickerRecord{
		PickerID:              "picker-1",
		Source:                control.TargetPickerRequestSourceWorktree,
		SelectedWorkspaceKey:  baseDir,
		WorktreeBranchName:    "feat/login",
		WorktreeDirectoryName: "web-login",
	})
	if state.CanConfirm {
		t.Fatalf("expected non-git workspace to block confirm, got %#v", state)
	}
	wantText := gitmeta.WorktreeCreateErrorText(&gitmeta.WorktreeCreateError{Code: gitmeta.WorktreeCreateErrorBaseWorkspaceNotGit})
	if len(state.Messages) != 1 || state.Messages[0].Text != wantText {
		t.Fatalf("expected picker message to equal gitmeta non-git text, got %#v want %q", state.Messages, wantText)
	}
}

func createTargetPickerNonGitDir(t *testing.T) string {
	t.Helper()
	if dir := t.TempDir(); dir != "" {
		info, err := gitmeta.InspectWorkspace(dir, gitmeta.InspectOptions{})
		if err == nil && !info.InRepo() {
			return dir
		}
	}
	for _, base := range []string{"/var/tmp", "/dev/shm"} {
		base = filepath.Clean(base)
		if base == "" {
			continue
		}
		dir, err := os.MkdirTemp(base, "picker-nonrepo-")
		if err != nil {
			continue
		}
		info, locateErr := gitmeta.InspectWorkspace(dir, gitmeta.InspectOptions{})
		if locateErr == nil && !info.InRepo() {
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			return dir
		}
		_ = os.RemoveAll(dir)
	}
	t.Skip("could not allocate temp dir outside any git repo")
	return ""
}
