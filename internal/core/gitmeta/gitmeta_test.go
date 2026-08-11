package gitmeta

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestParseGitDirFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".git")

	if err := os.WriteFile(path, []byte("gitdir: ../.git/modules/demo\n"), 0o600); err != nil {
		t.Fatalf("write git pointer: %v", err)
	}
	got, err := ParseGitDirFile(path)
	if err != nil {
		t.Fatalf("ParseGitDirFile() error = %v", err)
	}
	if got != "../.git/modules/demo" {
		t.Fatalf("ParseGitDirFile() = %q, want %q", got, "../.git/modules/demo")
	}
}

func TestResolveGitDirPath(t *testing.T) {
	base := filepath.Join(t.TempDir(), "repo", "worktree")
	wantRelative := filepath.Clean(filepath.Join(base, "..", ".git", "modules", "demo"))
	if got := ResolveGitDirPath(base, "../.git/modules/demo"); !testutil.SamePath(got, wantRelative) {
		t.Fatalf("ResolveGitDirPath(relative) = %q, want %q", got, wantRelative)
	}
	abs := filepath.Join(t.TempDir(), "var", "tmp", "repo.git")
	if got := ResolveGitDirPath(base, abs); !testutil.SamePath(got, abs) {
		t.Fatalf("ResolveGitDirPath(abs) = %q, want %q", got, abs)
	}
}

func TestFileHasExactTrimmedLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exclude")
	body := "# comments\n /.codex-remote/ \nfoo\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write exclude: %v", err)
	}
	if !FileHasExactTrimmedLine(path, "/.codex-remote/") {
		t.Fatal("expected exact trimmed line match")
	}
	if FileHasExactTrimmedLine(path, "/not-found/") {
		t.Fatal("unexpected match for absent line")
	}
}

func TestInspectWorkspaceRegularRepo(t *testing.T) {
	ensureGitForTest(t)
	repoRoot := createGitRepoForTest(t)
	info, err := InspectWorkspace(repoRoot, InspectOptions{})
	if err != nil {
		t.Fatalf("InspectWorkspace() error = %v", err)
	}
	if !info.InRepo() {
		t.Fatalf("expected git repo info, got %#v", info)
	}
	if info.RepoRoot != repoRoot {
		t.Fatalf("RepoRoot = %q, want %q", info.RepoRoot, repoRoot)
	}
	if info.GitDir != filepath.Join(repoRoot, ".git") {
		t.Fatalf("GitDir = %q, want %q", info.GitDir, filepath.Join(repoRoot, ".git"))
	}
	if info.LinkedWorktree {
		t.Fatalf("expected regular repo, got linked worktree: %#v", info)
	}
	if info.Detached || info.Branch != "main" {
		t.Fatalf("unexpected branch info: %#v", info)
	}
}

func TestInspectWorkspaceReadsHeadWithoutInvokingGit(t *testing.T) {
	repoRoot := createManualGitRepoForTest(t, "main")
	marker := filepath.Join(t.TempDir(), "git-invoked")
	t.Setenv("PATH", fakeFailingGitBinForTest(t, marker))

	info, err := InspectWorkspace(repoRoot, InspectOptions{})
	if err != nil {
		t.Fatalf("InspectWorkspace() error = %v", err)
	}
	if !info.InRepo() || info.Branch != "main" || info.Detached {
		t.Fatalf("unexpected file-only git info: %#v", info)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("InspectWorkspace should not invoke git, marker err=%v", err)
	}
}

func TestInspectWorkspaceLinkedWorktree(t *testing.T) {
	ensureGitForTest(t)
	repoRoot := createGitRepoForTest(t)
	worktreeRoot := filepath.Join(t.TempDir(), "feature-worktree")
	runGitTestCommand(t, repoRoot, "worktree", "add", "-b", "feature/worktree", worktreeRoot, "HEAD")

	info, err := InspectWorkspace(worktreeRoot, InspectOptions{})
	if err != nil {
		t.Fatalf("InspectWorkspace(worktree) error = %v", err)
	}
	if !info.InRepo() {
		t.Fatalf("expected linked worktree info, got %#v", info)
	}
	if info.RepoRoot != worktreeRoot {
		t.Fatalf("RepoRoot = %q, want %q", info.RepoRoot, worktreeRoot)
	}
	if !info.LinkedWorktree {
		t.Fatalf("expected linked worktree, got %#v", info)
	}
	if !strings.Contains(info.GitDir, filepath.ToSlash(filepath.Join(".git", "worktrees"))) &&
		!strings.Contains(info.GitDir, string(filepath.Separator)+filepath.Join(".git", "worktrees")+string(filepath.Separator)) {
		t.Fatalf("expected worktree gitdir, got %q", info.GitDir)
	}
	if info.Branch != "feature/worktree" || info.Detached {
		t.Fatalf("unexpected branch info: %#v", info)
	}
}

func TestWorkspaceInfoRepoFamilyKeyMatchesLinkedWorktreeFamily(t *testing.T) {
	ensureGitForTest(t)
	repoRoot := createGitRepoForTest(t)
	worktreeRoot := filepath.Join(t.TempDir(), "feature-worktree")
	runGitTestCommand(t, repoRoot, "worktree", "add", "-b", "feature/worktree", worktreeRoot, "HEAD")

	repoInfo, err := InspectWorkspace(repoRoot, InspectOptions{})
	if err != nil {
		t.Fatalf("InspectWorkspace(repo) error = %v", err)
	}
	worktreeInfo, err := InspectWorkspace(worktreeRoot, InspectOptions{})
	if err != nil {
		t.Fatalf("InspectWorkspace(worktree) error = %v", err)
	}
	if repoInfo.RepoFamilyKey() == "" {
		t.Fatalf("expected repo family key, got %#v", repoInfo)
	}
	if !testutil.SamePath(repoInfo.RepoFamilyKey(), worktreeInfo.RepoFamilyKey()) {
		t.Fatalf("RepoFamilyKey mismatch: repo=%q worktree=%q", repoInfo.RepoFamilyKey(), worktreeInfo.RepoFamilyKey())
	}
}

func TestInspectWorkspaceDetachedHeadFallback(t *testing.T) {
	ensureGitForTest(t)
	repoRoot := createGitRepoForTest(t)
	shortHead := strings.TrimSpace(runGitOutput(t, repoRoot, "rev-parse", "--short", "HEAD"))
	runGitTestCommand(t, repoRoot, "checkout", "--detach", "HEAD")

	info, err := InspectWorkspace(repoRoot, InspectOptions{})
	if err != nil {
		t.Fatalf("InspectWorkspace(detached) error = %v", err)
	}
	if !info.Detached {
		t.Fatalf("expected detached head info, got %#v", info)
	}
	if info.Branch != shortHead {
		t.Fatalf("Branch = %q, want %q", info.Branch, shortHead)
	}
}

func TestInspectWorkspaceOutsideGitRepo(t *testing.T) {
	info, err := InspectWorkspace(createNonRepoDirForTest(t), InspectOptions{})
	if err != nil {
		t.Fatalf("InspectWorkspace(non-git) error = %v", err)
	}
	if info.InRepo() {
		t.Fatalf("expected non-git result, got %#v", info)
	}
	if info.RepoRoot != "" || info.GitDir != "" || info.Branch != "" || info.Detached || info.LinkedWorktree {
		t.Fatalf("unexpected non-git info: %#v", info)
	}
}

func TestInspectWorkspaceUnbornHeadRepo(t *testing.T) {
	ensureGitForTest(t)
	repoRoot := createUnbornGitRepoForTest(t)

	info, err := InspectWorkspace(repoRoot, InspectOptions{IncludeStatus: true})
	if err != nil {
		t.Fatalf("InspectWorkspace(unborn) error = %v", err)
	}
	if !info.InRepo() {
		t.Fatalf("expected git repo info, got %#v", info)
	}
	if info.Detached {
		t.Fatalf("expected unborn branch to stay attached, got %#v", info)
	}
	if info.Branch != unbornBranchNameForTest(t, repoRoot) {
		t.Fatalf("Branch = %q, want %q", info.Branch, unbornBranchNameForTest(t, repoRoot))
	}
	if info.Status.Dirty {
		t.Fatalf("expected unborn repo to be clean, got %#v", info.Status)
	}
}

func TestInspectWorkspaceStatusAvoidsRecursiveUntrackedExpansion(t *testing.T) {
	ensureGitForTest(t)
	repoRoot := createGitRepoForTest(t)
	if err := os.MkdirAll(filepath.Join(repoRoot, "scratch", "nested"), 0o755); err != nil {
		t.Fatalf("mkdir scratch dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "scratch", "nested", "note.txt"), []byte("note\n"), 0o644); err != nil {
		t.Fatalf("write scratch file: %v", err)
	}
	t.Setenv("PATH", fakeGitBlockingStatusAllForTest(t))

	info, err := InspectWorkspace(repoRoot, InspectOptions{IncludeStatus: true})
	if err != nil {
		t.Fatalf("InspectWorkspace() error = %v", err)
	}
	if !info.Status.Dirty || info.Status.UntrackedCount == 0 {
		t.Fatalf("expected untracked directory to mark status dirty, got %#v", info.Status)
	}
}

func TestPreviewWorktreeInfersDirectoryNameFromBranch(t *testing.T) {
	ensureGitForTest(t)
	repoRoot := createGitRepoForTest(t)

	preview, err := PreviewWorktree(WorktreeCreateRequest{
		BaseWorkspacePath: repoRoot,
		BranchName:        "feat/worktree-preview",
	})
	if err != nil {
		t.Fatalf("PreviewWorktree() error = %v", err)
	}
	if preview.BranchName != "feat/worktree-preview" {
		t.Fatalf("BranchName = %q, want %q", preview.BranchName, "feat/worktree-preview")
	}
	if preview.DirectoryName != "feat-worktree-preview" {
		t.Fatalf("DirectoryName = %q, want %q", preview.DirectoryName, "feat-worktree-preview")
	}
	if preview.ParentDir != filepath.Dir(repoRoot) {
		t.Fatalf("ParentDir = %q, want %q", preview.ParentDir, filepath.Dir(repoRoot))
	}
	if preview.DestinationPath != filepath.Join(filepath.Dir(repoRoot), "feat-worktree-preview") {
		t.Fatalf("DestinationPath = %q", preview.DestinationPath)
	}
}

func TestPreviewWorktreeRejectsNonGitBaseBeforeGitAvailability(t *testing.T) {
	baseDir := createNonRepoDirForTest(t)
	t.Setenv("PATH", filepath.Join(t.TempDir(), "empty-bin"))

	_, err := PreviewWorktree(WorktreeCreateRequest{
		BaseWorkspacePath: baseDir,
		BranchName:        "feat/no-git-base",
	})

	var worktreeErr *WorktreeCreateError
	if !errors.As(err, &worktreeErr) {
		t.Fatalf("PreviewWorktree() error = %#v, want WorktreeCreateError", err)
	}
	if worktreeErr.Code != WorktreeCreateErrorBaseWorkspaceNotGit {
		t.Fatalf("PreviewWorktree() code = %q, want %q", worktreeErr.Code, WorktreeCreateErrorBaseWorkspaceNotGit)
	}
}

func TestPreviewWorktreeAcceptsLinkedWorktreeBase(t *testing.T) {
	ensureGitForTest(t)
	repoRoot := createGitRepoForTest(t)
	worktreeRoot := filepath.Join(t.TempDir(), "feature-worktree")
	runGitTestCommand(t, repoRoot, "worktree", "add", "-b", "feature/worktree", worktreeRoot, "HEAD")

	preview, err := PreviewWorktree(WorktreeCreateRequest{
		BaseWorkspacePath: worktreeRoot,
		BranchName:        "feat/from-linked",
		DirectoryName:     "feat-linked",
	})
	if err != nil {
		t.Fatalf("PreviewWorktree(linked) error = %v", err)
	}
	if preview.BaseWorkspacePath != worktreeRoot {
		t.Fatalf("BaseWorkspacePath = %q, want %q", preview.BaseWorkspacePath, worktreeRoot)
	}
	if preview.DestinationPath != filepath.Join(filepath.Dir(worktreeRoot), "feat-linked") {
		t.Fatalf("DestinationPath = %q", preview.DestinationPath)
	}
}

func ensureGitForTest(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
}

func createGitRepoForTest(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}
	runGitTestCommand(t, repoRoot, "init", "-q")
	disableGitAutoMaintenanceForTest(t, repoRoot)
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write repo file: %v", err)
	}
	runGitTestCommand(t, repoRoot, "add", "README.md")
	runGitTestCommand(t, repoRoot, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "init")
	runGitTestCommand(t, repoRoot, "branch", "-M", "main")
	return repoRoot
}

func createManualGitRepoForTest(t *testing.T, branch string) string {
	t.Helper()
	repoRoot := filepath.Join(t.TempDir(), "repo")
	gitDir := filepath.Join(repoRoot, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("mkdir manual git dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/"+branch+"\n"), 0o644); err != nil {
		t.Fatalf("write manual HEAD: %v", err)
	}
	return repoRoot
}

func createUnbornGitRepoForTest(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}
	runGitTestCommand(t, repoRoot, "init", "-q")
	disableGitAutoMaintenanceForTest(t, repoRoot)
	return repoRoot
}

func createNonRepoDirForTest(t *testing.T) string {
	t.Helper()
	if dir := t.TempDir(); dir != "" {
		info, err := LocateWorkspace(dir)
		if err == nil && !info.InRepo() {
			return dir
		}
	}
	for _, base := range []string{"/var/tmp", "/dev/shm"} {
		base = strings.TrimSpace(base)
		if base == "" {
			continue
		}
		dir, err := os.MkdirTemp(base, "gitmeta-nonrepo-")
		if err != nil {
			continue
		}
		info, locateErr := LocateWorkspace(dir)
		if locateErr == nil && !info.InRepo() {
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			return dir
		}
		_ = os.RemoveAll(dir)
	}
	t.Skip("could not allocate temp dir outside any git repo")
	return ""
}

func fakeFailingGitBinForTest(t *testing.T, marker string) string {
	t.Helper()
	bin := t.TempDir()
	if runtime.GOOS == "windows" {
		path := filepath.Join(bin, "git.bat")
		body := fmt.Sprintf("@echo off\r\necho invoked>>%q\r\nexit /b 1\r\n", marker)
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatalf("write fake git: %v", err)
		}
		return bin
	}
	path := filepath.Join(bin, "git")
	body := fmt.Sprintf("#!/bin/sh\nprintf 'invoked\\n' >> %q\nexit 1\n", marker)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	return bin
}

func fakeGitBlockingStatusAllForTest(t *testing.T) string {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not available")
	}
	t.Setenv("GITMETA_REAL_GIT_FOR_TEST", realGit)
	bin := t.TempDir()
	if runtime.GOOS == "windows" {
		path := filepath.Join(bin, "git.bat")
		body := "@echo off\r\n" +
			"set saw_status=\r\n" +
			"set saw_all=\r\n" +
			"for %%A in (%*) do (\r\n" +
			"  if \"%%~A\"==\"status\" set saw_status=1\r\n" +
			"  if \"%%~A\"==\"--untracked-files=all\" set saw_all=1\r\n" +
			")\r\n" +
			"if \"%saw_status%%saw_all%\"==\"11\" (\r\n" +
			"  echo git status all blocked 1>&2\r\n" +
			"  exit /b 99\r\n" +
			")\r\n" +
			"\"%GITMETA_REAL_GIT_FOR_TEST%\" %*\r\n" +
			"exit /b %ERRORLEVEL%\r\n"
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatalf("write fake git: %v", err)
		}
		return bin
	}
	path := filepath.Join(bin, "git")
	body := "#!/bin/sh\n" +
		"saw_status=0\n" +
		"saw_all=0\n" +
		"for arg in \"$@\"; do\n" +
		"  if [ \"$arg\" = \"status\" ]; then saw_status=1; fi\n" +
		"  if [ \"$arg\" = \"--untracked-files=all\" ]; then saw_all=1; fi\n" +
		"done\n" +
		"if [ \"$saw_status\" = \"1\" ] && [ \"$saw_all\" = \"1\" ]; then\n" +
		"  echo 'git status all blocked' >&2\n" +
		"  exit 99\n" +
		"fi\n" +
		"exec \"$GITMETA_REAL_GIT_FOR_TEST\" \"$@\"\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	return bin
}

func disableGitAutoMaintenanceForTest(t *testing.T, repoRoot string) {
	t.Helper()
	runGitTestCommand(t, repoRoot, "config", "gc.auto", "0")
	runGitTestCommand(t, repoRoot, "config", "gc.autoDetach", "false")
	runGitTestCommand(t, repoRoot, "config", "maintenance.auto", "false")
}

func unbornBranchNameForTest(t *testing.T, repoRoot string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRoot, ".git", "HEAD"))
	if err != nil {
		t.Fatalf("read HEAD: %v", err)
	}
	line := strings.TrimSpace(string(body))
	if !strings.HasPrefix(line, "ref:") {
		t.Fatalf("unexpected HEAD content: %q", line)
	}
	return strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(line, "ref:")), "refs/heads/")
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

func runGitOutput(t *testing.T, dir string, args ...string) string {
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
