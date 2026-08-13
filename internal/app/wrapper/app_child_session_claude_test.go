package wrapper

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

func TestResolveClaudeBinaryKeepsExplicitEnvPath(t *testing.T) {
	claudeDir := t.TempDir()
	claudePath := filepath.Join(claudeDir, executableNameForWrapperTest("claude-custom"))
	writeWrapperExecutableForTest(t, claudePath)

	t.Setenv(config.ClaudeBinaryEnv, claudePath)
	app := &App{}
	if got := app.resolveClaudeBinary(); normalizeExecutablePathForWrapperTest(t, got) != normalizeExecutablePathForWrapperTest(t, claudePath) {
		t.Fatalf("resolveClaudeBinary() = %q, want path equivalent to %q", got, claudePath)
	}
}

func TestBuildClaudeChildLaunchReviewForkUsesReviewerReadOnlyContract(t *testing.T) {
	workspaceRoot := t.TempDir()
	app := &App{config: Config{WorkspaceRoot: workspaceRoot}}

	args, _ := app.buildClaudeChildLaunch(&claudeLaunchResumeTarget{
		ThreadID:      "parent-session-1",
		CWD:           workspaceRoot,
		ForkEphemeral: true,
		ReviewerAgent: "reviewer",
	})

	assertArgPair(t, args, "--resume", "parent-session-1")
	assertContainsArg(t, args, "--fork-session")
	assertArgPair(t, args, "--agent", "reviewer")
	assertArgPairContains(t, args, "--agents", `"reviewer"`)
	assertArgPair(t, args, "--tools", "Read,Glob,Grep")
	assertArgPair(t, args, "--disallowedTools", "Edit,Write,MultiEdit,NotebookEdit,Task,Bash")
	assertArgPair(t, args, "--permission-mode", "plan")
	if containsArg(args, "--allow-dangerously-skip-permissions") {
		t.Fatalf("review fork args must not skip permissions: %v", args)
	}
}

func TestBuildClaudeChildLaunchNormalResumeDoesNotUseReviewForkFlags(t *testing.T) {
	workspaceRoot := t.TempDir()
	app := &App{config: Config{WorkspaceRoot: workspaceRoot}}

	args, _ := app.buildClaudeChildLaunch(&claudeLaunchResumeTarget{
		ThreadID: "parent-session-1",
		CWD:      workspaceRoot,
	})

	assertArgPair(t, args, "--resume", "parent-session-1")
	assertContainsArg(t, args, "--allow-dangerously-skip-permissions")
	assertNotContainsArg(t, args, "--fork-session")
	assertNotContainsArg(t, args, "--agent")
	assertNotContainsArg(t, args, "--tools")
	assertNotContainsArg(t, args, "--disallowedTools")
	assertNotContainsArg(t, args, "--permission-mode")
}

func normalizeExecutablePathForWrapperTest(t *testing.T, path string) string {
	t.Helper()
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return filepath.Clean(path)
}

func assertArgPair(t *testing.T, args []string, key string, want string) {
	t.Helper()
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key {
			if args[i+1] != want {
				t.Fatalf("%s arg value = %q, want %q in args %v", key, args[i+1], want, args)
			}
			return
		}
	}
	t.Fatalf("missing %s %q in args %v", key, want, args)
}

func assertArgPairContains(t *testing.T, args []string, key string, want string) {
	t.Helper()
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key {
			if !strings.Contains(args[i+1], want) {
				t.Fatalf("%s arg value = %q, want substring %q in args %v", key, args[i+1], want, args)
			}
			return
		}
	}
	t.Fatalf("missing %s value containing %q in args %v", key, want, args)
}

func assertContainsArg(t *testing.T, args []string, want string) {
	t.Helper()
	if !containsArg(args, want) {
		t.Fatalf("missing arg %q in args %v", want, args)
	}
}

func assertNotContainsArg(t *testing.T, args []string, unwanted string) {
	t.Helper()
	if containsArg(args, unwanted) {
		t.Fatalf("unexpected arg %q in args %v", unwanted, args)
	}
}

func writeWrapperExecutableForTest(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	content := []byte("#!/bin/sh\nexit 0\n")
	if runtime.GOOS == "windows" {
		content = []byte("echo off\r\n")
	}
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func executableNameForWrapperTest(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}
