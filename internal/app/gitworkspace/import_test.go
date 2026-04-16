package gitworkspace

import "testing"

func TestResolveDirectoryNameInfersRepoName(t *testing.T) {
	name, err := resolveDirectoryName("https://github.com/kxn/codex-remote-feishu.git", "")
	if err != nil {
		t.Fatalf("expected inferred directory name, got err=%v", err)
	}
	if name != "codex-remote-feishu" {
		t.Fatalf("expected inferred repo name, got %q", name)
	}
}

func TestResolveDirectoryNameRejectsInvalidCustomName(t *testing.T) {
	if _, err := resolveDirectoryName("https://github.com/kxn/codex-remote-feishu.git", "bad/name"); err == nil {
		t.Fatal("expected invalid custom directory name to fail")
	}
}
