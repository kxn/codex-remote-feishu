package gitmeta

import (
	"os"
	"path/filepath"
	"testing"
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
	base := "/repo/worktree"
	if got := ResolveGitDirPath(base, "../.git/modules/demo"); got != "/repo/.git/modules/demo" {
		t.Fatalf("ResolveGitDirPath(relative) = %q", got)
	}
	if got := ResolveGitDirPath(base, "/var/tmp/repo.git"); got != "/var/tmp/repo.git" {
		t.Fatalf("ResolveGitDirPath(abs) = %q", got)
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
