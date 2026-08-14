package gitmeta

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildUncommittedReviewContextIncludesStagedUnstagedAndUntracked(t *testing.T) {
	root := initReviewContextRepo(t)
	writeReviewContextFile(t, root, "staged.txt", "base\n")
	writeReviewContextFile(t, root, "unstaged.txt", "base\n")
	runGitTestCommand(t, root, "add", ".")
	runGitTestCommand(t, root, "commit", "-q", "-m", "base")
	writeReviewContextFile(t, root, "staged.txt", "staged change\n")
	runGitTestCommand(t, root, "add", "staged.txt")
	writeReviewContextFile(t, root, "unstaged.txt", "unstaged change\n")
	writeReviewContextFile(t, root, "untracked.txt", "untracked content\n")

	context, err := BuildUncommittedReviewContext(root, 128*1024)
	if err != nil {
		t.Fatalf("BuildUncommittedReviewContext: %v", err)
	}
	for _, want := range []string{"## Changed files", "## Staged changes", "staged.txt", "staged change", "## Unstaged changes", "unstaged.txt", "unstaged change", "## Untracked files", "untracked.txt", "untracked content"} {
		if !strings.Contains(context.Text, want) {
			t.Fatalf("review context missing %q:\n%s", want, context.Text)
		}
	}
	if context.Truncated {
		t.Fatalf("small context must not be truncated: %#v", context)
	}
}

func TestBuildCommitReviewContextIncludesMetadataAndPatch(t *testing.T) {
	root := initReviewContextRepo(t)
	writeReviewContextFile(t, root, "feature.txt", "feature\n")
	runGitTestCommand(t, root, "add", "feature.txt")
	runGitTestCommand(t, root, "commit", "-q", "-m", "add feature")

	context, err := BuildCommitReviewContext(root, "HEAD", 128*1024)
	if err != nil {
		t.Fatalf("BuildCommitReviewContext: %v", err)
	}
	for _, want := range []string{"add feature", "feature.txt", "+feature"} {
		if !strings.Contains(context.Text, want) {
			t.Fatalf("commit context missing %q:\n%s", want, context.Text)
		}
	}
}

func TestReviewContextMarksTruncationExplicitly(t *testing.T) {
	root := initReviewContextRepo(t)
	writeReviewContextFile(t, root, "large.txt", strings.Repeat("0123456789", 200))

	context, err := BuildUncommittedReviewContext(root, 256)
	if err != nil {
		t.Fatalf("BuildUncommittedReviewContext: %v", err)
	}
	if !context.Truncated || !strings.Contains(context.Text, "[review context truncated") {
		t.Fatalf("expected explicit truncation marker, got %#v", context)
	}
}

func TestBuildUncommittedReviewContextFromSubdirectoryCoversWholeRepo(t *testing.T) {
	root := initReviewContextRepo(t)
	writeReviewContextFile(t, root, "subdir/kept.txt", "base\n")
	writeReviewContextFile(t, root, "sibling.txt", "base\n")
	runGitTestCommand(t, root, "add", ".")
	runGitTestCommand(t, root, "commit", "-q", "-m", "base")
	writeReviewContextFile(t, root, "sibling.txt", "sibling change\n")
	writeReviewContextFile(t, root, "outside-untracked.txt", "outside content\n")

	context, err := BuildUncommittedReviewContext(filepath.Join(root, "subdir"), 128*1024)
	if err != nil {
		t.Fatalf("BuildUncommittedReviewContext(subdir): %v", err)
	}
	for _, want := range []string{"sibling.txt", "sibling change", "outside-untracked.txt", "outside content"} {
		if !strings.Contains(context.Text, want) {
			t.Fatalf("repo-wide review context missing %q:\n%s", want, context.Text)
		}
	}
}

func initReviewContextRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGitTestCommand(t, root, "init", "-q")
	runGitTestCommand(t, root, "config", "user.email", "review-context@example.com")
	runGitTestCommand(t, root, "config", "user.name", "Review Context")
	return root
}

func writeReviewContextFile(t *testing.T, root, relativePath, body string) {
	t.Helper()
	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
