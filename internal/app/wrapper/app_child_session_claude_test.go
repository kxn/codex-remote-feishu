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
