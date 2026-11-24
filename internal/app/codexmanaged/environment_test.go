package codexmanaged

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPrivateCommandsUseSuppliedPathAndEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell tools are Unix-only")
	}
	tools := t.TempDir()
	argsPath := filepath.Join(tools, "args")
	for name, script := range map[string]string{
		"codex": "#!/bin/sh\n[ \"$CODEX_HOME\" = \"/captured/codex-home\" ] || exit 2\n[ \"$HTTPS_PROXY\" = \"http://captured-proxy:3\" ] || exit 3\nprintf 'codex-cli 0.159.3\\n'\n",
		"npm":   "#!/bin/sh\n[ \"$CODEX_HOME\" = \"/captured/codex-home\" ] || exit 2\n[ \"$HTTPS_PROXY\" = \"http://captured-proxy:3\" ] || exit 3\nprintf '%s\\n' \"$@\" > \"$CODEX_MANAGED_TEST_ARGS\"\n",
	} {
		if err := os.WriteFile(filepath.Join(tools, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	env := []string{"PATH=" + tools, "CODEX_HOME=/captured/codex-home", "HTTPS_PROXY=http://captured-proxy:3", "CODEX_MANAGED_TEST_ARGS=" + argsPath}
	t.Setenv("PATH", "/unavailable-global-path")
	t.Setenv("CODEX_HOME", "/global-codex-home")
	actual, err := readBinaryVersionWithEnv(context.Background(), "codex", env)
	if err != nil || actual != "0.159.3" {
		t.Fatalf("version=%s err=%v", actual, err)
	}
	if err := installPrivateWithEnv(context.Background(), t.TempDir(), "0.159.3", env); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(argsPath); err != nil || !strings.Contains(string(raw), "@openai/codex@0.159.3") {
		t.Fatalf("args=%q err=%v", raw, err)
	}
	if os.Getenv("CODEX_HOME") != "/global-codex-home" {
		t.Fatal("global environment changed")
	}
}

func TestVersionOutputLimitCannotBeBypassedByReaderFrom(t *testing.T) {
	var output versionOutput
	if _, err := io.Copy(&output, strings.NewReader(strings.Repeat("x", 8192))); err != nil {
		t.Fatal(err)
	}
	if !output.exceeded || len(output.String()) != 1024 {
		t.Fatalf("output=%d exceeded=%v", len(output.String()), output.exceeded)
	}
}
