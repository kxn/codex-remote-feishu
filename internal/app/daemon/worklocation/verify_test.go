package worklocation

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

func testGit(t *testing.T, path string, args ...string) {
	t.Helper()
	command := execlaunch.Command("git", append([]string{"-C", path}, args...)...)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, "GIT_") {
			command.Env = append(command.Env, entry)
		}
	}
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}
func testRepository(t *testing.T) string {
	t.Helper()
	path := t.TempDir()
	testGit(t, path, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(path, "README"), []byte("initial"), 0600); err != nil {
		t.Fatal(err)
	}
	testGit(t, path, "add", "README")
	testGit(t, path, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "initial")
	return path
}
func rootLocation(path string) work.ExecutionLocation {
	return work.ExecutionLocation{ID: "chosen", HostID: "host", Cwd: path, Generation: 1, Revision: 1}
}

func TestVerifyAgentCreatedWorktreePreservesOriginalDirtyFiles(t *testing.T) {
	root := testRepository(t)
	prepared := filepath.Join(t.TempDir(), "agent-work")
	testGit(t, root, "worktree", "add", "-b", "agent-work", prepared)
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("human edits"), 0600); err != nil {
		t.Fatal(err)
	}
	proof, err := Verify(context.Background(), rootLocation(root), prepared)
	if err != nil {
		t.Fatal(err)
	}
	if proof.Location.Cwd != prepared || proof.BranchRef != "refs/heads/agent-work" || proof.RootLocationID != "chosen" {
		t.Fatalf("wrong location proof: %+v", proof)
	}
	if !slices.Contains(proof.Location.ResourceKeys, "directory:"+prepared) || len(proof.Location.ResourceKeys) != 2 {
		t.Fatalf("missing independent directory/ref ownership: %v", proof.Location.ResourceKeys)
	}
	data, err := os.ReadFile(filepath.Join(root, "README"))
	if err != nil || string(data) != "human edits" {
		t.Fatalf("verification changed original files: %q %v", data, err)
	}
}

func TestVerifyRejectsUnrelatedRepositoryEvenWithInjectedGitEnvironment(t *testing.T) {
	root := testRepository(t)
	unrelated := testRepository(t)
	t.Setenv("GIT_DIR", filepath.Join(root, ".git"))
	t.Setenv("GIT_WORK_TREE", root)
	if _, err := Verify(context.Background(), rootLocation(root), unrelated); err == nil {
		t.Fatal("unrelated repository authorized through inherited Git variables")
	}
}

func TestVerifyOrdinaryDirectoryCannotEscapeSelectedRoot(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "prepared")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(context.Background(), rootLocation(root), child); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if _, err := Verify(context.Background(), rootLocation(root), outside); err == nil {
		t.Fatal("unselected sibling directory authorized")
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Verify(context.Background(), rootLocation(root), link); err == nil {
		t.Fatal("symlink escaped selected resource")
	}
}

func TestVerifyBrokenRepositoryIsNotTreatedAsOrdinaryDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: /missing/repository"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(context.Background(), rootLocation(root), root); err == nil {
		t.Fatal("broken repository silently admitted")
	}
}
