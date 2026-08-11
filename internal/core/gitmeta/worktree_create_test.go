package gitmeta

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviewWorktreeEmptyBranchExplicitDirectoryReturnsDraftPreview(t *testing.T) {
	ensureGitForTest(t)
	repoRoot := createGitRepoForTest(t)

	preview, err := PreviewWorktree(WorktreeCreateRequest{
		BaseWorkspacePath: repoRoot,
		DirectoryName:     "draft-dir",
	})
	if err != nil {
		t.Fatalf("PreviewWorktree(draft) error = %v", err)
	}
	if preview.CanConfirm {
		t.Fatalf("expected draft preview to block confirm, got %#v", preview)
	}
	if preview.DirectoryName != "draft-dir" {
		t.Fatalf("DirectoryName = %q, want %q", preview.DirectoryName, "draft-dir")
	}
	if preview.DestinationPath != filepath.Join(filepath.Dir(repoRoot), "draft-dir") {
		t.Fatalf("DestinationPath = %q, want %q", preview.DestinationPath, filepath.Join(filepath.Dir(repoRoot), "draft-dir"))
	}
}

func TestPreviewWorktreeEmptyBranchEmptyDirectoryReturnsEmptyDraft(t *testing.T) {
	ensureGitForTest(t)
	repoRoot := createGitRepoForTest(t)

	preview, err := PreviewWorktree(WorktreeCreateRequest{BaseWorkspacePath: repoRoot})
	if err != nil {
		t.Fatalf("PreviewWorktree(empty draft) error = %v", err)
	}
	if preview.CanConfirm {
		t.Fatalf("expected empty draft to block confirm, got %#v", preview)
	}
	if preview.DestinationPath != "" || preview.DirectoryName != "" {
		t.Fatalf("expected empty draft destination, got %#v", preview)
	}
	if preview.BaseWorkspacePath != repoRoot {
		t.Fatalf("BaseWorkspacePath = %q, want %q", preview.BaseWorkspacePath, repoRoot)
	}
}

func TestPreviewWorktreeNonEmptyBranchCanConfirm(t *testing.T) {
	ensureGitForTest(t)
	repoRoot := createGitRepoForTest(t)

	preview, err := PreviewWorktree(WorktreeCreateRequest{
		BaseWorkspacePath: repoRoot,
		BranchName:        "feat/confirmable",
	})
	if err != nil {
		t.Fatalf("PreviewWorktree() error = %v", err)
	}
	if !preview.CanConfirm {
		t.Fatalf("expected confirmable preview, got %#v", preview)
	}
}

func TestPreviewWorktreeDraftAndBranchAgreeOnDestination(t *testing.T) {
	ensureGitForTest(t)
	repoRoot := createGitRepoForTest(t)

	draft, err := PreviewWorktree(WorktreeCreateRequest{
		BaseWorkspacePath: repoRoot,
		DirectoryName:     "same-dir",
	})
	if err != nil {
		t.Fatalf("PreviewWorktree(draft) error = %v", err)
	}
	full, err := PreviewWorktree(WorktreeCreateRequest{
		BaseWorkspacePath: repoRoot,
		BranchName:        "feat/same",
		DirectoryName:     "same-dir",
	})
	if err != nil {
		t.Fatalf("PreviewWorktree(full) error = %v", err)
	}
	if draft.DestinationPath != full.DestinationPath {
		t.Fatalf("draft destination %q != full destination %q", draft.DestinationPath, full.DestinationPath)
	}
}

func TestPreviewWorktreeDestinationExistsCarriesPathAndConsistentText(t *testing.T) {
	ensureGitForTest(t)
	repoRoot := createGitRepoForTest(t)
	destinationPath := filepath.Join(filepath.Dir(repoRoot), "occupied-dir")
	if err := os.MkdirAll(destinationPath, 0o755); err != nil {
		t.Fatalf("mkdir occupied destination: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(destinationPath) })

	_, err := PreviewWorktree(WorktreeCreateRequest{
		BaseWorkspacePath: repoRoot,
		BranchName:        "feat/occupied",
		DirectoryName:     "occupied-dir",
	})
	var worktreeErr *WorktreeCreateError
	if !errors.As(err, &worktreeErr) {
		t.Fatalf("error = %#v, want WorktreeCreateError", err)
	}
	if worktreeErr.Code != WorktreeCreateErrorDestinationExists {
		t.Fatalf("code = %q, want %q", worktreeErr.Code, WorktreeCreateErrorDestinationExists)
	}
	if worktreeErr.DestinationPath != destinationPath {
		t.Fatalf("DestinationPath = %q, want %q", worktreeErr.DestinationPath, destinationPath)
	}
	if worktreeErr.DirectoryName != "occupied-dir" {
		t.Fatalf("DirectoryName = %q, want %q", worktreeErr.DirectoryName, "occupied-dir")
	}
	if got := WorktreeCreateErrorText(worktreeErr); !strings.Contains(got, "已经存在") {
		t.Fatalf("error text = %q, want destination-exists text", got)
	}
}

func TestPreviewWorktreeEmptyDirectoryIsNotProtocolError(t *testing.T) {
	ensureGitForTest(t)
	repoRoot := createGitRepoForTest(t)

	preview, err := PreviewWorktree(WorktreeCreateRequest{
		BaseWorkspacePath: repoRoot,
		BranchName:        "feat/no-dir",
	})
	if err != nil {
		t.Fatalf("PreviewWorktree(empty directory) error = %v", err)
	}
	if preview.DirectoryName != "feat-no-dir" {
		t.Fatalf("DirectoryName = %q, want inferred %q", preview.DirectoryName, "feat-no-dir")
	}
	if preview.DestinationPath != filepath.Join(filepath.Dir(repoRoot), "feat-no-dir") {
		t.Fatalf("DestinationPath = %q, want %q", preview.DestinationPath, filepath.Join(filepath.Dir(repoRoot), "feat-no-dir"))
	}
	if !preview.CanConfirm {
		t.Fatalf("expected empty directory to keep confirm enabled, got %#v", preview)
	}
}

func TestPreviewWorktreeDraftDestinationExistsCarriesPath(t *testing.T) {
	ensureGitForTest(t)
	repoRoot := createGitRepoForTest(t)
	destinationPath := filepath.Join(filepath.Dir(repoRoot), "occupied-dir")
	if err := os.MkdirAll(destinationPath, 0o755); err != nil {
		t.Fatalf("mkdir occupied destination: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(destinationPath) })

	_, err := PreviewWorktree(WorktreeCreateRequest{
		BaseWorkspacePath: repoRoot,
		DirectoryName:     "occupied-dir",
	})
	var worktreeErr *WorktreeCreateError
	if !errors.As(err, &worktreeErr) {
		t.Fatalf("error = %#v, want WorktreeCreateError", err)
	}
	if worktreeErr.Code != WorktreeCreateErrorDestinationExists {
		t.Fatalf("code = %q, want %q", worktreeErr.Code, WorktreeCreateErrorDestinationExists)
	}
	if worktreeErr.DestinationPath != destinationPath {
		t.Fatalf("DestinationPath = %q, want %q", worktreeErr.DestinationPath, destinationPath)
	}
}
