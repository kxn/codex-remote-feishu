package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

func TestGatewayRecovered(t *testing.T) {
	tests := []struct {
		name  string
		state upgradeHelperBootstrapState
		want  bool
	}{
		{name: "no gateways", state: upgradeHelperBootstrapState{}, want: true},
		{
			name: "connected gateway",
			state: upgradeHelperBootstrapState{Gateways: []struct {
				State string `json:"state"`
			}{{State: "connected"}}},
			want: true,
		},
		{
			name: "disabled only",
			state: upgradeHelperBootstrapState{Gateways: []struct {
				State string `json:"state"`
			}{{State: "disabled"}}},
			want: false,
		},
		{
			name: "degraded gateway",
			state: upgradeHelperBootstrapState{Gateways: []struct {
				State string `json:"state"`
			}{{State: "degraded"}}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := gatewayRecovered(tt.state); got != tt.want {
				t.Fatalf("gatewayRecovered() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestRunUpgradeHelperWithStatePathSystemdUserUsesSystemctlStopStart(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "install-state.json")
	configPath := filepath.Join(dir, ".config", "codex-remote", "config.json")
	currentBinary := seedBinary(t, filepath.Join(dir, "bin", executableName("linux")), "old-binary")
	targetBinary := seedBinary(t, filepath.Join(dir, "releases", "v1.1.0", executableName("linux")), "new-binary")

	cfg := config.DefaultAppConfig()
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	stateValue := InstallState{
		BaseDir:           dir,
		ConfigPath:        configPath,
		StatePath:         statePath,
		ServiceManager:    ServiceManagerSystemdUser,
		CurrentVersion:    "v1.0.0",
		CurrentBinaryPath: currentBinary,
		VersionsRoot:      filepath.Join(dir, "releases"),
		PendingUpgrade: &PendingUpgrade{
			Phase:         PendingUpgradePhasePrepared,
			TargetVersion: "v1.1.0",
		},
	}
	rollbackCandidate, err := PrepareRollbackCandidate(stateValue, "v1.1.0")
	if err != nil {
		t.Fatalf("PrepareRollbackCandidate: %v", err)
	}
	stateValue.RollbackCandidate = rollbackCandidate
	if err := WriteState(statePath, stateValue); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	originalRunner := systemctlUserRunner
	originalObserve := upgradeHelperObserveFunc
	originalSleep := upgradeHelperSleepFunc
	var calls []string
	systemctlUserRunner = func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	}
	upgradeHelperObserveFunc = func(context.Context, config.LoadedAppConfig) error { return nil }
	upgradeHelperSleepFunc = func(time.Duration) {}
	defer func() {
		systemctlUserRunner = originalRunner
		upgradeHelperObserveFunc = originalObserve
		upgradeHelperSleepFunc = originalSleep
	}()

	if err := RunUpgradeHelperWithStatePath(context.Background(), statePath); err != nil {
		t.Fatalf("RunUpgradeHelperWithStatePath: %v", err)
	}
	if got, want := calls, []string{"stop codex-remote.service", "start codex-remote.service"}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("systemctl calls = %#v, want %#v", got, want)
	}
	raw, err := os.ReadFile(targetBinary)
	if err != nil {
		t.Fatalf("ReadFile target: %v", err)
	}
	if string(raw) != "new-binary" {
		t.Fatalf("target binary content = %q, want new-binary", string(raw))
	}
	currentRaw, err := os.ReadFile(currentBinary)
	if err != nil {
		t.Fatalf("ReadFile current: %v", err)
	}
	if string(currentRaw) != "new-binary" {
		t.Fatalf("current binary content = %q, want new-binary", string(currentRaw))
	}
	updated, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if updated.PendingUpgrade == nil || updated.PendingUpgrade.Phase != PendingUpgradePhaseCommitted {
		t.Fatalf("pending upgrade = %#v, want committed", updated.PendingUpgrade)
	}
	if updated.CurrentVersion != "v1.1.0" {
		t.Fatalf("current version = %q, want v1.1.0", updated.CurrentVersion)
	}
}

func TestRunUpgradeHelperWithStatePathDebugInstanceUsesDebugSystemdUnit(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, ".local", "share", "codex-remote-debug", "codex-remote", "install-state.json")
	configPath := filepath.Join(dir, ".config", "codex-remote-debug", "codex-remote", "config.json")
	currentBinary := seedBinary(t, filepath.Join(dir, "bin", executableName("linux")), "old-binary")
	seedBinary(t, filepath.Join(dir, ".local", "share", "codex-remote-debug", "codex-remote", "releases", "v1.1.0", executableName("linux")), "new-binary")

	cfg := config.DefaultAppConfig()
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	stateValue := InstallState{
		InstanceID:        debugInstanceID,
		BaseDir:           dir,
		ConfigPath:        configPath,
		StatePath:         statePath,
		ServiceManager:    ServiceManagerSystemdUser,
		CurrentVersion:    "v1.0.0",
		CurrentBinaryPath: currentBinary,
		VersionsRoot:      filepath.Join(dir, ".local", "share", "codex-remote-debug", "codex-remote", "releases"),
		PendingUpgrade: &PendingUpgrade{
			Phase:         PendingUpgradePhasePrepared,
			TargetVersion: "v1.1.0",
		},
	}
	rollbackCandidate, err := PrepareRollbackCandidate(stateValue, "v1.1.0")
	if err != nil {
		t.Fatalf("PrepareRollbackCandidate: %v", err)
	}
	stateValue.RollbackCandidate = rollbackCandidate
	if err := WriteState(statePath, stateValue); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	originalRunner := systemctlUserRunner
	originalObserve := upgradeHelperObserveFunc
	originalSleep := upgradeHelperSleepFunc
	var calls []string
	systemctlUserRunner = func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	}
	upgradeHelperObserveFunc = func(context.Context, config.LoadedAppConfig) error { return nil }
	upgradeHelperSleepFunc = func(time.Duration) {}
	defer func() {
		systemctlUserRunner = originalRunner
		upgradeHelperObserveFunc = originalObserve
		upgradeHelperSleepFunc = originalSleep
	}()

	if err := RunUpgradeHelperWithStatePath(context.Background(), statePath); err != nil {
		t.Fatalf("RunUpgradeHelperWithStatePath: %v", err)
	}
	if got, want := calls, []string{"stop codex-remote-debug.service", "start codex-remote-debug.service"}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("systemctl calls = %#v, want %#v", got, want)
	}
}

func TestRunUpgradeHelperWithStatePathSystemdUserRollsBackOnObserveFailure(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "install-state.json")
	configPath := filepath.Join(dir, ".config", "codex-remote", "config.json")
	currentBinary := seedBinary(t, filepath.Join(dir, "bin", executableName("linux")), "old-binary")
	seedBinary(t, filepath.Join(dir, "releases", "v1.1.0", executableName("linux")), "new-binary")

	cfg := config.DefaultAppConfig()
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	stateValue := InstallState{
		BaseDir:           dir,
		ConfigPath:        configPath,
		StatePath:         statePath,
		ServiceManager:    ServiceManagerSystemdUser,
		CurrentVersion:    "v1.0.0",
		CurrentBinaryPath: currentBinary,
		VersionsRoot:      filepath.Join(dir, "releases"),
		PendingUpgrade: &PendingUpgrade{
			Phase:         PendingUpgradePhasePrepared,
			TargetVersion: "v1.1.0",
		},
	}
	rollbackCandidate, err := PrepareRollbackCandidate(stateValue, "v1.1.0")
	if err != nil {
		t.Fatalf("PrepareRollbackCandidate: %v", err)
	}
	stateValue.RollbackCandidate = rollbackCandidate
	if err := WriteState(statePath, stateValue); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	originalRunner := systemctlUserRunner
	originalObserve := upgradeHelperObserveFunc
	originalSleep := upgradeHelperSleepFunc
	var calls []string
	systemctlUserRunner = func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	}
	upgradeHelperObserveFunc = func(context.Context, config.LoadedAppConfig) error {
		return errors.New("gateway unhealthy")
	}
	upgradeHelperSleepFunc = func(time.Duration) {}
	defer func() {
		systemctlUserRunner = originalRunner
		upgradeHelperObserveFunc = originalObserve
		upgradeHelperSleepFunc = originalSleep
	}()

	err = RunUpgradeHelperWithStatePath(context.Background(), statePath)
	if err == nil || !strings.Contains(err.Error(), "gateway unhealthy") {
		t.Fatalf("RunUpgradeHelperWithStatePath error = %v, want gateway unhealthy", err)
	}
	if got, want := calls, []string{
		"stop codex-remote.service",
		"start codex-remote.service",
		"stop codex-remote.service",
		"start codex-remote.service",
	}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("systemctl calls = %#v, want %#v", got, want)
	}
	currentRaw, err := os.ReadFile(currentBinary)
	if err != nil {
		t.Fatalf("ReadFile current: %v", err)
	}
	if string(currentRaw) != "old-binary" {
		t.Fatalf("current binary content = %q, want old-binary", string(currentRaw))
	}
	updated, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if updated.PendingUpgrade == nil || updated.PendingUpgrade.Phase != PendingUpgradePhaseRolledBack {
		t.Fatalf("pending upgrade = %#v, want rolled_back", updated.PendingUpgrade)
	}
	if updated.CurrentVersion != "v1.0.0" {
		t.Fatalf("current version = %q, want v1.0.0", updated.CurrentVersion)
	}
}
