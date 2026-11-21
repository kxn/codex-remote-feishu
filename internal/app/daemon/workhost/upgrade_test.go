package workhost

import (
	"context"
	"github.com/kxn/codex-remote-feishu/internal/app/codexupgrade/contract"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestUpgradeFenceCatchesPreparationBeforeDurableLaunchReservation(t *testing.T) {
	calls := 0
	h, store := hostFixture(t, func(relayruntime.HeadlessLaunchOptions) (int, error) { calls++; return 123, nil })
	original := h.config.Settings
	entered, release := make(chan struct{}), make(chan struct{})
	h.config.Settings = settingsFunc(func(ctx context.Context, s work.WorkScope, r work.Role, c work.WorkContext) (LaunchSettings, error) {
		value, err := original.ResolveLaunch(ctx, s, r, c)
		value.Backend = work.BackendCodex
		value.CodexResume = &agentproto.CodexResumePolicy{ModelProviderID: "openai"}
		close(entered)
		<-release
		return value, err
	})
	done := make(chan error, 1)
	go func() {
		done <- h.Prepare(context.Background(), workengine.WorkerNeed{ScopeID: "scope", RoleID: "role"})
	}()
	<-entered
	if err := store.Within(context.Background(), func(tx *workstore.Tx) error {
		return tx.PutNativeUpgrade(workstore.NativeUpgrade{ID: "upgrade", HostID: "local", TargetVersion: "0.2.0", InstallationFingerprint: "actual-fingerprint", Stage: "draining", Revision: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("inflight prepare crossed upgrade admission")
	}
	if calls != 0 {
		t.Fatal("native spawned after freeze")
	}
	if err := store.Within(context.Background(), func(tx *workstore.Tx) error {
		workers, e := tx.NativeUpgradeWorkers("local")
		if len(workers) != 0 {
			t.Fatal("late launch reservation escaped frozen set")
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
}
func TestUpgradeFreezeIncludesLaunchWhoseStartHasNotReturned(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	h, store := hostFixture(t, func(relayruntime.HeadlessLaunchOptions) (int, error) { close(entered); <-release; return 123, nil })
	original := h.config.Settings
	h.config.Settings = settingsFunc(func(ctx context.Context, s work.WorkScope, r work.Role, c work.WorkContext) (LaunchSettings, error) {
		value, err := original.ResolveLaunch(ctx, s, r, c)
		value.Backend = work.BackendCodex
		value.CodexResume = &agentproto.CodexResumePolicy{ModelProviderID: "openai"}
		return value, err
	})
	done := make(chan error, 1)
	go func() {
		done <- h.Prepare(context.Background(), workengine.WorkerNeed{ScopeID: "scope", RoleID: "role"})
	}()
	<-entered
	if err := store.Within(context.Background(), func(tx *workstore.Tx) error {
		workers, e := tx.NativeUpgradeWorkers("local")
		if e != nil {
			return e
		}
		if len(workers) != 1 {
			t.Fatalf("sending launch absent from frozen pool: %d", len(workers))
		}
		unsent, e := tx.WorkerLaunchUnsent(workers[0])
		if e != nil {
			return e
		}
		if unsent {
			t.Fatal("started side effect treated as prepared")
		}
		return tx.PutNativeUpgrade(workstore.NativeUpgrade{ID: "upgrade", HostID: "local", TargetVersion: "0.2.0", InstallationFingerprint: "actual-fingerprint", Stage: "draining", FrozenWorkers: workers, Revision: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCompletedUpgradePinHonorsChangedConfiguredSource(t *testing.T) {
	h, store := hostFixture(t, func(relayruntime.HeadlessLaunchOptions) (int, error) { return 123, nil })
	source := filepath.Join(t.TempDir(), "explicit-codex")
	if runtime.GOOS == "windows" {
		t.Skip("shell version probe fixture")
	}
	if err := os.WriteFile(source, []byte("#!/bin/sh\nprintf 'codex-cli 0.3.0\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := store.Within(context.Background(), func(tx *workstore.Tx) error {
		return tx.PutNativeUpgrade(workstore.NativeUpgrade{ID: "previous-upgrade", HostID: "local", TargetVersion: "0.2.0", InstallationFingerprint: "fingerprint", Installation: contract.Installation{ConfiguredBinary: "old-source"}, ManagedBinary: "/old/pinned/codex", ManagedVersion: "0.2.0", Stage: "completed", Revision: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	h.config.ResolveConfiguredCodexBinary = func(context.Context) (string, error) { return source, nil }
	settings := LaunchSettings{Backend: work.BackendCodex}
	if err := h.freezeUpgradeBinary(context.Background(), &settings); err != nil {
		t.Fatal(err)
	}
	if settings.NativeBinaryPath != source || settings.NativeBinaryVersion != "0.3.0" {
		t.Fatalf("explicit source swallowed by previous pin: %+v", settings)
	}
	record := LaunchRecord{Snapshot: agentproto.RuntimeLaunchSnapshot{NativeBinaryPath: "/old/pinned/codex", NativeBinaryVersion: "0.2.0"}}
	if sameRuntimePolicy(record, settings) {
		t.Fatal("source change reused previous carrier")
	}
}
