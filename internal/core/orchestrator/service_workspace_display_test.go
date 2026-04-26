package orchestrator

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
)

func TestTargetPickerWorkspaceMetaByKeyUsesBranchWithinRepoFamily(t *testing.T) {
	entries := []workspaceSelectionEntry{
		{
			workspaceKey: "/data/dl/repo-main",
			gitInfo: gitmeta.WorkspaceInfo{
				GitDir: "/git/repo/.git",
				Branch: "main",
			},
		},
		{
			workspaceKey: "/data/dl/repo-feature",
			gitInfo: gitmeta.WorkspaceInfo{
				GitDir:         "/git/repo/.git/worktrees/feature",
				Branch:         "feature/auth",
				LinkedWorktree: true,
			},
		},
	}

	metaByKey := targetPickerWorkspaceMetaByKey(entries)
	if got := metaByKey["/data/dl/repo-main"]; got != "main" {
		t.Fatalf("main workspace meta = %q, want %q", got, "main")
	}
	if got := metaByKey["/data/dl/repo-feature"]; got != "feature/auth" {
		t.Fatalf("feature workspace meta = %q, want %q", got, "feature/auth")
	}
}

func TestTargetPickerWorkspaceMetaByKeyUpgradesSameBranchToShortTail(t *testing.T) {
	entries := []workspaceSelectionEntry{
		{
			workspaceKey: "/Users/alice/repo",
			gitInfo: gitmeta.WorkspaceInfo{
				GitDir: "/git/repo/.git",
				Branch: "main",
			},
		},
		{
			workspaceKey: "/Users/bob/repo",
			gitInfo: gitmeta.WorkspaceInfo{
				GitDir:         "/git/repo/.git/worktrees/repo-b",
				Branch:         "main",
				LinkedWorktree: true,
			},
		},
	}

	metaByKey := targetPickerWorkspaceMetaByKey(entries)
	if got := metaByKey["/Users/alice/repo"]; got != "main@alice/repo" {
		t.Fatalf("alice workspace meta = %q, want %q", got, "main@alice/repo")
	}
	if got := metaByKey["/Users/bob/repo"]; got != "main@bob/repo" {
		t.Fatalf("bob workspace meta = %q, want %q", got, "main@bob/repo")
	}
}

func TestTargetPickerWorkspaceMetaByKeySkipsNonGitAndRecoverableEntries(t *testing.T) {
	entries := []workspaceSelectionEntry{
		{
			workspaceKey: "/data/dl/non-git",
		},
		{
			workspaceKey:    "/data/dl/recoverable",
			recoverableOnly: true,
			gitInfo: gitmeta.WorkspaceInfo{
				GitDir: "/git/repo/.git",
				Branch: "main",
			},
		},
		{
			workspaceKey: "/data/dl/repo-main",
			gitInfo: gitmeta.WorkspaceInfo{
				GitDir: "/git/repo/.git",
				Branch: "main",
			},
		},
		{
			workspaceKey: "/data/dl/repo-feature",
			gitInfo: gitmeta.WorkspaceInfo{
				GitDir:         "/git/repo/.git/worktrees/feature",
				Branch:         "feature/auth",
				LinkedWorktree: true,
			},
		},
	}

	metaByKey := targetPickerWorkspaceMetaByKey(entries)
	if got := metaByKey["/data/dl/non-git"]; got != "" {
		t.Fatalf("non-git workspace meta = %q, want empty", got)
	}
	if got := metaByKey["/data/dl/recoverable"]; got != "" {
		t.Fatalf("recoverable workspace meta = %q, want empty", got)
	}
}

func TestTargetPickerWorkspaceMetaByKeyLeavesMissingBranchEmpty(t *testing.T) {
	entries := []workspaceSelectionEntry{
		{
			workspaceKey: "/data/dl/repo-main",
			gitInfo: gitmeta.WorkspaceInfo{
				GitDir: "/git/repo/.git",
				Branch: "main",
			},
		},
		{
			workspaceKey: "/data/dl/repo-unknown",
			gitInfo: gitmeta.WorkspaceInfo{
				GitDir:         "/git/repo/.git/worktrees/unknown",
				LinkedWorktree: true,
			},
		},
	}

	metaByKey := targetPickerWorkspaceMetaByKey(entries)
	if got := metaByKey["/data/dl/repo-main"]; got != "main" {
		t.Fatalf("main workspace meta = %q, want %q", got, "main")
	}
	if got := metaByKey["/data/dl/repo-unknown"]; got != "" {
		t.Fatalf("missing-branch workspace meta = %q, want empty", got)
	}
}
