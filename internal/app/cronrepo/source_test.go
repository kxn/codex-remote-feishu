package cronrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseSourceInputSupportsTreeURLAndRefFragment(t *testing.T) {
	spec, err := ParseSourceInput("https://github.com/org/repo/tree/main")
	if err != nil {
		t.Fatalf("ParseSourceInput(tree url): %v", err)
	}
	if spec.RepoURL != "https://github.com/org/repo.git" || spec.Ref != "main" {
		t.Fatalf("unexpected tree url parse: %#v", spec)
	}

	spec, err = ParseSourceInput("https://github.com/org/repo.git#ref=release/1.5")
	if err != nil {
		t.Fatalf("ParseSourceInput(ref fragment): %v", err)
	}
	if spec.RepoURL != "https://github.com/org/repo.git" || spec.Ref != "release/1.5" {
		t.Fatalf("unexpected ref fragment parse: %#v", spec)
	}
}

func TestManagerMaterializeAndCleanupRun(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	repoURL, ref := createGitTestRepo(t)
	manager := NewManager(t.TempDir())
	result, err := manager.Materialize(t.Context(), "inst-cron-1", SourceSpec{
		RawInput: repoURL + "#ref=" + ref,
		RepoURL:  repoURL,
		Ref:      ref,
	})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if _, err := os.Stat(filepath.Join(result.RunDirectory, "README.md")); err != nil {
		t.Fatalf("materialized worktree missing file: %v", err)
	}
	if err := manager.CleanupRun(t.Context(), result.SourceKey, result.RunRoot); err != nil {
		t.Fatalf("CleanupRun: %v", err)
	}
	if _, err := os.Stat(result.RunRoot); !os.IsNotExist(err) {
		t.Fatalf("expected run root to be removed, got err=%v", err)
	}
}

func TestManagerMaterializeUsesRepoDefaultBranchWhenRefMissing(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	repoURL, defaultRef := createGitTestRepoWithBranch(t, "trunk")
	manager := NewManager(t.TempDir())
	result, err := manager.Materialize(t.Context(), "inst-cron-default", SourceSpec{
		RawInput: repoURL,
		RepoURL:  repoURL,
	})
	if err != nil {
		t.Fatalf("Materialize(default branch): %v", err)
	}
	if result.ResolvedRef != defaultRef {
		t.Fatalf("resolved ref = %q, want %q", result.ResolvedRef, defaultRef)
	}
	if _, err := os.Stat(filepath.Join(result.RunDirectory, "README.md")); err != nil {
		t.Fatalf("materialized default branch worktree missing file: %v", err)
	}
}

func runGitTestCommand(t *testing.T, dir string, args ...string) {
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

func createGitTestRepo(t *testing.T) (string, string) {
	t.Helper()
	return createGitTestRepoWithBranch(t, "main")
}

func createGitTestRepoWithBranch(t *testing.T, branch string) (string, string) {
	t.Helper()
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}
	runGitTestCommand(t, repoRoot, "init", "-q")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write repo file: %v", err)
	}
	runGitTestCommand(t, repoRoot, "add", "README.md")
	runGitTestCommand(t, repoRoot, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "init")
	runGitTestCommand(t, repoRoot, "branch", "-M", branch)
	return "file://" + filepath.ToSlash(repoRoot), branch
}
