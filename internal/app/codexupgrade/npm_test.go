package codexupgrade

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLookupLatestVersionReadsLatestDistTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"dist-tags":{"latest":"0.124.0","alpha":"0.124.0-alpha.3"}}`))
	}))
	defer server.Close()

	got, err := LookupLatestVersion(context.Background(), LatestVersionOptions{
		RegistryURL: server.URL,
	})
	if err != nil {
		t.Fatalf("LookupLatestVersion: %v", err)
	}
	if got != "0.124.0" {
		t.Fatalf("latest = %q", got)
	}
}

func TestInstallGlobalRunsExpectedNPMCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test fixture uses unix shell scripts")
	}

	root := t.TempDir()
	logPath := filepath.Join(root, "npm.log")
	scriptPath := filepath.Join(root, "npm")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > \"" + logPath + "\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile npm: %v", err)
	}

	if err := InstallGlobal(context.Background(), "0.124.0", InstallOptions{NPMCommand: scriptPath}); err != nil {
		t.Fatalf("InstallGlobal: %v", err)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile log: %v", err)
	}
	got := strings.TrimSpace(string(raw))
	if got != "install -g @openai/codex@0.124.0" {
		t.Fatalf("command = %q", got)
	}
}
