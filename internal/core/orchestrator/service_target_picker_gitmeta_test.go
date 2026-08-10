package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestTargetPickerListWorkspaceMetaDoesNotInvokeGit(t *testing.T) {
	now := time.Date(2026, 4, 26, 10, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	repoRoot, worktreeRoot := createTargetPickerManualGitRepoFamily(t)
	marker := filepath.Join(t.TempDir(), "git-invoked")
	t.Setenv("PATH", fakeTargetPickerFailingGitBin(t, marker))

	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-main",
		DisplayName:   "main",
		WorkspaceRoot: repoRoot,
		WorkspaceKey:  repoRoot,
		ShortName:     filepath.Base(repoRoot),
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-main": {ThreadID: "thread-main", Name: "主线修复", CWD: repoRoot, Loaded: true, LastUsedAt: now.Add(-2 * time.Minute)},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-feature",
		DisplayName:   "feature",
		WorkspaceRoot: worktreeRoot,
		WorkspaceKey:  worktreeRoot,
		ShortName:     filepath.Base(worktreeRoot),
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-feature": {ThreadID: "thread-feature", Name: "特性开发", CWD: worktreeRoot, Loaded: true, LastUsedAt: now.Add(-1 * time.Minute)},
		},
	})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	mainOption, ok := targetPickerWorkspaceOption(view, repoRoot)
	if !ok || mainOption.MetaText != "main" {
		t.Fatalf("expected main branch meta from HEAD file, option=%#v view=%#v", mainOption, view)
	}
	featureOption, ok := targetPickerWorkspaceOption(view, worktreeRoot)
	if !ok || featureOption.MetaText != "feature/auth" {
		t.Fatalf("expected worktree branch meta from HEAD file, option=%#v view=%#v", featureOption, view)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("/list workspace meta should not invoke git, marker err=%v", err)
	}
}

func createTargetPickerManualGitRepoFamily(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	repoRoot := filepath.Join(root, "repo")
	repoGitDir := filepath.Join(repoRoot, ".git")
	if err := os.MkdirAll(repoGitDir, 0o755); err != nil {
		t.Fatalf("mkdir manual repo git dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoGitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatalf("write manual repo HEAD: %v", err)
	}
	worktreeRoot := filepath.Join(root, "feature-worktree")
	worktreeGitDir := filepath.Join(repoGitDir, "worktrees", "feature-worktree")
	if err := os.MkdirAll(worktreeGitDir, 0o755); err != nil {
		t.Fatalf("mkdir manual worktree git dir: %v", err)
	}
	if err := os.MkdirAll(worktreeRoot, 0o755); err != nil {
		t.Fatalf("mkdir manual worktree root: %v", err)
	}
	relGitDir, err := filepath.Rel(worktreeRoot, worktreeGitDir)
	if err != nil {
		t.Fatalf("rel git dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktreeRoot, ".git"), []byte("gitdir: "+filepath.ToSlash(relGitDir)+"\n"), 0o644); err != nil {
		t.Fatalf("write manual worktree git pointer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktreeGitDir, "HEAD"), []byte("ref: refs/heads/feature/auth\n"), 0o644); err != nil {
		t.Fatalf("write manual worktree HEAD: %v", err)
	}
	return repoRoot, worktreeRoot
}

func fakeTargetPickerFailingGitBin(t *testing.T, marker string) string {
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
