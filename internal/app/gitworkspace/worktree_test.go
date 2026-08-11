package gitworkspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestCreateWorktreeUsesPreviewDestination(t *testing.T) {
	ensureGitWorkspaceTestGit(t)
	repoRoot := createGitWorkspaceTestRepo(t)
	dirName := "feature-login"

	preview, err := gitmeta.PreviewWorktree(gitmeta.WorktreeCreateRequest{
		BaseWorkspacePath: repoRoot,
		BranchName:        "feat/login",
		DirectoryName:     dirName,
	})
	if err != nil {
		t.Fatalf("PreviewWorktree() error = %v", err)
	}

	result, err := CreateWorktree(context.Background(), WorktreeRequest{
		BaseWorkspacePath: repoRoot,
		BranchName:        "feat/login",
		DirectoryName:     dirName,
	})
	if err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	if !testutil.SamePath(result.WorkspacePath, preview.DestinationPath) {
		t.Fatalf("WorkspacePath = %q, want preview destination %q", result.WorkspacePath, preview.DestinationPath)
	}
	if result.BranchName != preview.BranchName || result.DirectoryName != preview.DirectoryName {
		t.Fatalf("unexpected result: %#v, preview: %#v", result, preview)
	}
	if _, err := os.Stat(result.WorkspacePath); err != nil {
		t.Fatalf("created worktree missing: %v", err)
	}
	// 实际 `git worktree add` 使用 preview 的 cwd/branch/destination：新 worktree
	// 应落在 preview 的 destination 且 checkout 了 preview 的 branch。
	head := strings.TrimSpace(runGitWorkspaceTestOutput(t, result.WorkspacePath, "symbolic-ref", "--short", "HEAD"))
	if head != "feat/login" {
		t.Fatalf("created worktree HEAD = %q, want %q", head, "feat/login")
	}
}

func TestCreateWorktreeRejectsDraftPreviewWithoutBranch(t *testing.T) {
	ensureGitWorkspaceTestGit(t)
	repoRoot := createGitWorkspaceTestRepo(t)

	_, err := CreateWorktree(context.Background(), WorktreeRequest{
		BaseWorkspacePath: repoRoot,
		DirectoryName:     "draft-dir",
	})
	var worktreeErr *gitmeta.WorktreeCreateError
	if !errors.As(err, &worktreeErr) {
		t.Fatalf("error = %#v, want WorktreeCreateError", err)
	}
	if worktreeErr.Code != gitmeta.WorktreeCreateErrorInvalidBranchName {
		t.Fatalf("code = %q, want %q", worktreeErr.Code, gitmeta.WorktreeCreateErrorInvalidBranchName)
	}
}

func ensureGitWorkspaceTestGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
}

func createGitWorkspaceTestRepo(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}
	runGitWorkspaceTestCommand(t, repoRoot, "init", "-q")
	runGitWorkspaceTestCommand(t, repoRoot, "config", "gc.auto", "0")
	runGitWorkspaceTestCommand(t, repoRoot, "config", "maintenance.auto", "false")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write repo file: %v", err)
	}
	runGitWorkspaceTestCommand(t, repoRoot, "add", "README.md")
	runGitWorkspaceTestCommand(t, repoRoot, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "init")
	runGitWorkspaceTestCommand(t, repoRoot, "branch", "-M", "main")
	return repoRoot
}

func runGitWorkspaceTestCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}

func runGitWorkspaceTestOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
	return string(output)
}
