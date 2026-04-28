package gitmeta

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestListRecentCommitsReturnsNewestFirst(t *testing.T) {
	ensureGitForTest(t)
	repoRoot := createGitRepoForTest(t)
	gitmetaWriteAndCommitFile(t, repoRoot, "docs/one.md", "one\n", "first follow-up")
	gitmetaWriteAndCommitFile(t, repoRoot, "docs/two.md", "two\n", "second follow-up")

	commits, err := ListRecentCommits(repoRoot, 3)
	if err != nil {
		t.Fatalf("ListRecentCommits() error = %v", err)
	}
	if len(commits) != 3 {
		t.Fatalf("ListRecentCommits() len = %d, want 3", len(commits))
	}
	if commits[0].Subject != "second follow-up" || commits[1].Subject != "first follow-up" || commits[2].Subject != "init" {
		t.Fatalf("unexpected commit order: %#v", commits)
	}
	if len(commits[0].SHA) != 40 || len(commits[0].ShortSHA) == 0 {
		t.Fatalf("expected normalized commit sha fields, got %#v", commits[0])
	}
}

func TestResolveCommitPrefixFound(t *testing.T) {
	ensureGitForTest(t)
	repoRoot := createGitRepoForTest(t)
	gitmetaWriteAndCommitFile(t, repoRoot, "docs/guide.md", "guide\n", "review target")

	commits, err := ListRecentCommits(repoRoot, 1)
	if err != nil {
		t.Fatalf("ListRecentCommits() error = %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("expected one commit, got %#v", commits)
	}

	result, err := ResolveCommitPrefix(repoRoot, commits[0].ShortSHA)
	if err != nil {
		t.Fatalf("ResolveCommitPrefix() error = %v", err)
	}
	if result.Status != CommitResolveFound {
		t.Fatalf("ResolveCommitPrefix() status = %q, want %q", result.Status, CommitResolveFound)
	}
	if result.Commit.SHA != commits[0].SHA || result.Commit.Subject != "review target" {
		t.Fatalf("unexpected resolved commit: %#v", result.Commit)
	}
}

func TestResolveCommitPrefixNotFound(t *testing.T) {
	ensureGitForTest(t)
	repoRoot := createGitRepoForTest(t)

	result, err := ResolveCommitPrefix(repoRoot, "deadbeef")
	if err != nil {
		t.Fatalf("ResolveCommitPrefix() error = %v", err)
	}
	if result.Status != CommitResolveNotFound {
		t.Fatalf("ResolveCommitPrefix() status = %q, want %q", result.Status, CommitResolveNotFound)
	}
}

func TestResolveCommitPrefixAmbiguous(t *testing.T) {
	ensureGitForTest(t)
	repoRoot := createGitRepoForTest(t)
	prefix := gitmetaEnsureAmbiguousPrefix(t, repoRoot, 2)

	result, err := ResolveCommitPrefix(repoRoot, prefix)
	if err != nil {
		t.Fatalf("ResolveCommitPrefix() error = %v", err)
	}
	if result.Status != CommitResolveAmbiguous {
		t.Fatalf("ResolveCommitPrefix() status = %q, want %q", result.Status, CommitResolveAmbiguous)
	}
}

func TestMatchRecentCommitPrefixRequiresUniqueMatch(t *testing.T) {
	commits := []CommitSummary{
		{SHA: "abcdef0123456789abcdef0123456789abcdef01", ShortSHA: "abcdef0", Subject: "first"},
		{SHA: "1234567012345670123456701234567012345670", ShortSHA: "1234567", Subject: "second"},
	}
	if match, ok := MatchRecentCommitPrefix(commits, "abcdef0"); !ok || match.Subject != "first" {
		t.Fatalf("expected unique recent commit match, got match=%#v ok=%v", match, ok)
	}
	if _, ok := MatchRecentCommitPrefix(commits, "deadbee"); ok {
		t.Fatal("did not expect non-existent recent commit match")
	}
	if _, ok := MatchRecentCommitPrefix([]CommitSummary{
		{SHA: "abcdef0123456789abcdef0123456789abcdef01"},
		{SHA: "abcdef1123456789abcdef0123456789abcdef01"},
	}, "abcdef"); ok {
		t.Fatal("did not expect ambiguous recent commit match")
	}
}

func gitmetaWriteAndCommitFile(t *testing.T, repoRoot, relativePath, content, message string) {
	t.Helper()
	fullPath := filepath.Join(repoRoot, relativePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", fullPath, err)
	}
	runGitTestCommand(t, repoRoot, "add", relativePath)
	runGitTestCommand(t, repoRoot, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-q", "-m", message)
}

func gitmetaEnsureAmbiguousPrefix(t *testing.T, repoRoot string, prefixLen int) string {
	t.Helper()
	for i := 0; i < 300; i++ {
		commits, err := ListRecentCommits(repoRoot, 400)
		if err != nil {
			t.Fatalf("ListRecentCommits() error = %v", err)
		}
		seen := map[string]struct{}{}
		for _, commit := range commits {
			if len(commit.SHA) < prefixLen {
				continue
			}
			prefix := commit.SHA[:prefixLen]
			if _, ok := seen[prefix]; ok {
				return prefix
			}
			seen[prefix] = struct{}{}
		}
		runGitTestCommand(t, repoRoot, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-q", "-m", fmt.Sprintf("empty-%03d", i))
	}
	t.Fatal("failed to create ambiguous commit prefix")
	return ""
}
