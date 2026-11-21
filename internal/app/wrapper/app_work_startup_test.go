package wrapper

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestWorkInitialBootstrapRejectionRecordsExitedNativeScope(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native fixture uses a POSIX shell")
	}
	base := newTestWorkApp(t, agentproto.BackendCodex)
	cfg := base.config
	cfg.RuntimePaths.StateDir = t.TempDir()
	cfg.Args = []string{"app-server"}
	snapshot := *cfg.WorkLaunchSnapshot
	snapshot.JournalPath = filepath.Join(cfg.RuntimePaths.StateDir, "workers", snapshot.Owner.WorkerSessionID, "runtime.db")
	snapshot.CarrierIncarnation = "startup-failure-carrier"
	snapshot.InstanceGeneration = 1
	snapshot.ProcessGeneration = 1
	snapshot.LocationGeneration = 1
	cfg.WorkLaunchSnapshot = &snapshot
	cfg.CodexRealBinary = filepath.Join(t.TempDir(), "codex")
	script := `#!/bin/sh
read -r request
printf '%s\n' '{"id":"relay-bootstrap-initialize","error":{"message":"test initialize rejected"}}'
while read -r ignored; do :; done
`
	if err := os.WriteFile(cfg.CodexRealBinary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(filepath.Dir(snapshot.JournalPath), "2"), 0700); err != nil {
		t.Fatal(err)
	}
	app := New(cfg)
	if app.workErr != nil {
		t.Fatal(app.workErr)
	}
	t.Cleanup(func() { cleanupBackendRuntime(app.runtime) })
	child, err := app.launchInitialChild(t.Context(), nil, func(agentproto.ErrorInfo) {})
	if child != nil || err == nil || !strings.Contains(err.Error(), "test initialize rejected") {
		t.Fatalf("native startup error did not propagate: %v, %v", child, err)
	}
	receipt, err := runtimejournal.ReadStartupFailure(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Process.PID != os.Getpid() || receipt.Stage != "native_bootstrap" || len(receipt.Scopes) != 1 || receipt.Scopes[0].RootAlive || !receipt.Scopes[0].Complete {
		t.Fatalf("invalid exact pre-Hello failure proof: %+v", receipt)
	}
}

func TestWorkJournalInitializationFailureRecordsPreNativeExit(t *testing.T) {
	base := newTestWorkApp(t, agentproto.BackendCodex)
	cfg := base.config
	cfg.RuntimePaths.StateDir = t.TempDir()
	snapshot := *cfg.WorkLaunchSnapshot
	snapshot.JournalPath = filepath.Join(cfg.RuntimePaths.StateDir, "workers", snapshot.Owner.WorkerSessionID, "runtime.db")
	snapshot.CarrierIncarnation = "journal-failure-carrier"
	snapshot.InstanceGeneration = 1
	snapshot.ProcessGeneration = 1
	snapshot.LocationGeneration = 1
	cfg.WorkLaunchSnapshot = &snapshot
	if err := os.MkdirAll(filepath.Join(filepath.Dir(snapshot.JournalPath), "2"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snapshot.JournalPath, []byte("not an SQLite database"), 0600); err != nil {
		t.Fatal(err)
	}
	app := New(cfg)
	if app.workErr == nil {
		t.Fatal("expected journal initialization failure")
	}
	code, err := app.Run(t.Context(), nil, nil, nil)
	if code != 1 || err == nil {
		t.Fatalf("expected pre-native exit: code=%d err=%v", code, err)
	}
	receipt, err := runtimejournal.ReadStartupFailure(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Process.PID != os.Getpid() || receipt.Stage != "work_state_init" || len(receipt.Scopes) != 0 {
		t.Fatalf("invalid pre-native failure proof: %+v", receipt)
	}
}
