package wrapper

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

func TestResolveClaudeBinaryKeepsExplicitEnvPath(t *testing.T) {
	claudeDir := t.TempDir()
	claudePath := filepath.Join(claudeDir, executableNameForWrapperTest("claude-custom"))
	writeWrapperExecutableForTest(t, claudePath)

	t.Setenv(config.ClaudeBinaryEnv, claudePath)
	app := &App{}
	if got := app.resolveClaudeBinary(); got != claudePath {
		t.Fatalf("resolveClaudeBinary() = %q, want %q", got, claudePath)
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
