package wrapper

import (
	"context"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWorkLaunchStartsOnlyFrozenVerifiedNativeRelease(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("version fixture uses shell; Windows build checked")
	}
	app := newTestWorkApp(t, agentproto.BackendCodex)
	binary := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf 'codex-cli 0.2.0\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	app.work.snapshot.NativeBinaryPath = binary
	app.work.snapshot.NativeBinaryVersion = "0.2.0"
	calls := 0
	ensure := func(context.Context, string, string, []string) (string, error) {
		calls++
		return "unrelated-latest", nil
	}
	selected, err := app.selectCodexChildBinary(context.Background(), os.Environ(), ensure)
	if err != nil {
		t.Fatal(err)
	}
	if selected != binary || calls != 0 {
		t.Fatalf("frozen native replaced: %q", selected)
	}
	app.work.snapshot.NativeBinaryVersion = "0.3.0"
	if _, err = app.selectCodexChildBinary(context.Background(), os.Environ(), ensure); err == nil {
		t.Fatal("wrong frozen native version accepted")
	}
}
