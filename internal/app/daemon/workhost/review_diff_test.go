package workhost

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func reviewRepo(t *testing.T) string {
	t.Helper()
	cwd := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "test@example.invalid"}, {"config", "user.name", "Review Test"}} {
		reviewTestGit(t, cwd, args...)
	}
	if err := os.WriteFile(filepath.Join(cwd, "code.txt"), []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	reviewTestGit(t, cwd, "add", "code.txt")
	reviewTestGit(t, cwd, "commit", "-qm", "initial")
	return cwd
}
func reviewTestGit(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GIT_") {
			command.Env = append(command.Env, e)
		}
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("test Git failed: %v %s", err, output)
	}
	return strings.TrimSpace(string(output))
}

func TestReviewDiffUsesExplicitRepositoryAndNeverRunsExternalDiffOrTextconv(t *testing.T) {
	cwd := reviewRepo(t)
	other := reviewRepo(t)
	if err := os.WriteFile(filepath.Join(cwd, "code.txt"), []byte("after\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "new.txt"), []byte("not automatically uploaded\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		tool := filepath.Join(t.TempDir(), "must-not-run")
		if err := os.WriteFile(tool, []byte("#!/bin/sh\nexit 92\n"), 0700); err != nil {
			t.Fatal(err)
		}
		reviewTestGit(t, cwd, "config", "diff.external", tool)
		reviewTestGit(t, cwd, "config", "diff.forbidden.textconv", tool)
		if err := os.WriteFile(filepath.Join(cwd, ".gitattributes"), []byte("code.txt diff=forbidden\n"), 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GIT_EXTERNAL_DIFF", tool)
	}
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_WORK_TREE", other)
	input, err := buildReviewDiffInput(context.Background(), cwd, agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindUncommittedChanges})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(input.Text, "+after") || !strings.Contains(input.Text, "-before") || !strings.Contains(input.Text, "new.txt") || strings.Contains(input.Text, "not automatically uploaded") {
		t.Fatalf("review did not preserve explicit bounded Git scope: %s", input.Text)
	}
	if _, err = buildReviewDiffInput(context.Background(), cwd, agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindCommit, CommitSHA: "--output=/tmp/forbidden"}); err == nil {
		t.Fatal("Git option was accepted as a commit")
	}
}

func TestReviewDiffRejectsOversizeInsteadOfSilentlyReviewingTruncatedChanges(t *testing.T) {
	cwd := reviewRepo(t)
	if err := os.WriteFile(filepath.Join(cwd, "code.txt"), []byte(strings.Repeat("oversize-line\n", maxReviewDiffBytes/5)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := buildReviewDiffInput(context.Background(), cwd, agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindUncommittedChanges}); err == nil {
		t.Fatal("oversize diff was silently accepted")
	}
}
