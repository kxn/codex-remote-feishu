package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/config"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
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
	currentBinary := seedBinary(t, filepath.Join(dir, "bin", executableName(runtime.GOOS)), "old-binary")
	targetBinary := seedBinary(t, filepath.Join(dir, "releases", "v1.1.0", executableName(runtime.GOOS)), "new-binary")

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
		if len(args) > 0 && args[0] == "show" {
			return "inactive\n0\n", nil
		}
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
	if got, want := calls, []string{
		"stop codex-remote.service",
		"show --property=ActiveState --property=MainPID codex-remote.service",
		"start codex-remote.service",
	}; strings.Join(got, "\n") != strings.Join(want, "\n") {
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

func TestRunUpgradeHelperWithStatePathLaunchdUserRebootstrapsBeforeStart(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "install-state.json")
	configPath := filepath.Join(dir, ".config", "codex-remote", "config.json")
	currentBinary := seedBinary(t, filepath.Join(dir, "bin", executableName(runtime.GOOS)), "old-binary")
	targetBinary := seedBinary(t, filepath.Join(dir, "releases", "v1.1.0", executableName(runtime.GOOS)), "new-binary")

	cfg := config.DefaultAppConfig()
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	originalGOOS := serviceRuntimeGOOS
	originalHome := serviceUserHomeDir
	originalRunner := launchctlUserRunner
	originalObserve := upgradeHelperObserveFunc
	originalSleep := upgradeHelperSleepFunc
	serviceRuntimeGOOS = "darwin"
	serviceUserHomeDir = func() (string, error) { return dir, nil }
	defer func() {
		serviceRuntimeGOOS = originalGOOS
		serviceUserHomeDir = originalHome
		launchctlUserRunner = originalRunner
		upgradeHelperObserveFunc = originalObserve
		upgradeHelperSleepFunc = originalSleep
	}()

	stateValue := InstallState{
		BaseDir:           dir,
		ConfigPath:        configPath,
		StatePath:         statePath,
		ServiceManager:    ServiceManagerLaunchdUser,
		CurrentVersion:    "v1.0.0",
		CurrentBinaryPath: currentBinary,
		InstalledBinary:   currentBinary,
		VersionsRoot:      filepath.Join(dir, "releases"),
		PendingUpgrade: &PendingUpgrade{
			Phase:         PendingUpgradePhasePrepared,
			TargetVersion: "v1.1.0",
		},
	}
	ApplyStateMetadata(&stateValue, StateMetadataOptions{
		StatePath:      statePath,
		BaseDir:        dir,
		ServiceManager: ServiceManagerLaunchdUser,
	})
	if err := os.MkdirAll(filepath.Dir(stateValue.ServiceUnitPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(plist dir): %v", err)
	}
	rollbackCandidate, err := PrepareRollbackCandidate(stateValue, "v1.1.0")
	if err != nil {
		t.Fatalf("PrepareRollbackCandidate: %v", err)
	}
	stateValue.RollbackCandidate = rollbackCandidate
	if err := WriteState(statePath, stateValue); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	var calls []string
	launchctlUserRunner = func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if len(args) > 0 && args[0] == "print" {
			return "", fmt.Errorf("Could not find domain for label")
		}
		return "", nil
	}
	upgradeHelperObserveFunc = func(context.Context, config.LoadedAppConfig) error { return nil }
	upgradeHelperSleepFunc = func(time.Duration) {}

	if err := RunUpgradeHelperWithStatePath(context.Background(), statePath); err != nil {
		t.Fatalf("RunUpgradeHelperWithStatePath: %v", err)
	}
	wantTarget := "gui/" + strconv.Itoa(os.Getuid()) + "/com.codex-remote.service"
	wantPlist := filepath.Join(dir, "Library", "LaunchAgents", "com.codex-remote.service.plist")
	if got, want := calls, []string{
		"bootout " + wantTarget,
		"print " + wantTarget,
		"bootstrap gui/" + strconv.Itoa(os.Getuid()) + " " + wantPlist,
		"kickstart " + wantTarget,
	}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("launchctl calls = %#v, want %#v", got, want)
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
}

func TestRunUpgradeHelperWithStatePathTaskSchedulerLogonStopsInstallsAndStartsTask(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "install-state.json")
	configPath := filepath.Join(dir, ".config", "codex-remote", "config.json")
	currentBinary := seedBinary(t, filepath.Join(dir, "bin", "codex-remote.exe"), "old-binary")
	targetBinary := seedBinary(t, filepath.Join(dir, "releases", "v1.1.0", "codex-remote.exe"), "new-binary")

	cfg := config.DefaultAppConfig()
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	originalGOOS := serviceRuntimeGOOS
	originalRunner := taskSchedulerRunner
	originalObserve := upgradeHelperObserveFunc
	originalSleep := upgradeHelperSleepFunc
	serviceRuntimeGOOS = "windows"
	defer func() {
		serviceRuntimeGOOS = originalGOOS
		taskSchedulerRunner = originalRunner
		upgradeHelperObserveFunc = originalObserve
		upgradeHelperSleepFunc = originalSleep
	}()

	stateValue := InstallState{
		BaseDir:           dir,
		ConfigPath:        configPath,
		StatePath:         statePath,
		ServiceManager:    ServiceManagerTaskSchedulerLogon,
		CurrentVersion:    "v1.0.0",
		CurrentBinaryPath: currentBinary,
		InstalledBinary:   currentBinary,
		VersionsRoot:      filepath.Join(dir, "releases"),
		PendingUpgrade: &PendingUpgrade{
			Phase:            PendingUpgradePhasePrepared,
			TargetVersion:    "v1.1.0",
			TargetBinaryPath: targetBinary,
		},
	}
	ApplyStateMetadata(&stateValue, StateMetadataOptions{
		StatePath:      statePath,
		BaseDir:        dir,
		ServiceManager: ServiceManagerTaskSchedulerLogon,
	})
	rollbackCandidate, err := PrepareRollbackCandidate(stateValue, "v1.1.0")
	if err != nil {
		t.Fatalf("PrepareRollbackCandidate: %v", err)
	}
	stateValue.RollbackCandidate = rollbackCandidate
	if err := WriteState(statePath, stateValue); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	var calls []string
	taskSchedulerRunner = func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if len(args) > 0 && args[0] == "/Query" {
			return "Status: Ready\r\n", nil
		}
		return "", nil
	}
	upgradeHelperObserveFunc = func(context.Context, config.LoadedAppConfig) error { return nil }
	upgradeHelperSleepFunc = func(time.Duration) {}

	if err := RunUpgradeHelperWithStatePath(context.Background(), statePath); err != nil {
		t.Fatalf("RunUpgradeHelperWithStatePath: %v", err)
	}
	taskName := taskSchedulerTaskNameForInstance("stable")
	if got, want := calls, []string{
		"/End /TN " + taskName,
		"/Query /TN " + taskName + " /FO LIST /V",
		"/Create /TN " + taskName + " /XML " + stateValue.ServiceUnitPath + " /F",
		"/Run /TN " + taskName,
	}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("task scheduler calls = %#v, want %#v", got, want)
	}
	currentRaw, err := os.ReadFile(currentBinary)
	if err != nil {
		t.Fatalf("ReadFile current: %v", err)
	}
	if string(currentRaw) != "new-binary" {
		t.Fatalf("current binary content = %q, want new-binary", string(currentRaw))
	}
}

func TestRunUpgradeHelperWithStatePathDebugInstanceUsesDebugSystemdUnit(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, ".local", "share", "codex-remote-debug", "codex-remote", "install-state.json")
	configPath := filepath.Join(dir, ".config", "codex-remote-debug", "codex-remote", "config.json")
	currentBinary := seedBinary(t, filepath.Join(dir, "bin", executableName(runtime.GOOS)), "old-binary")
	seedBinary(t, filepath.Join(dir, ".local", "share", "codex-remote-debug", "codex-remote", "releases", "v1.1.0", executableName(runtime.GOOS)), "new-binary")

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
		if len(args) > 0 && args[0] == "show" {
			return "inactive\n0\n", nil
		}
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
	if got, want := calls, []string{
		"stop codex-remote-debug.service",
		"show --property=ActiveState --property=MainPID codex-remote-debug.service",
		"start codex-remote-debug.service",
	}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("systemctl calls = %#v, want %#v", got, want)
	}
}

func TestRunUpgradeHelperWithStatePathSystemdUserRollsBackOnObserveFailure(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "install-state.json")
	configPath := filepath.Join(dir, ".config", "codex-remote", "config.json")
	currentBinary := seedBinary(t, filepath.Join(dir, "bin", executableName(runtime.GOOS)), "old-binary")
	seedBinary(t, filepath.Join(dir, "releases", "v1.1.0", executableName(runtime.GOOS)), "new-binary")

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
		if len(args) > 0 && args[0] == "show" {
			return "inactive\n0\n", nil
		}
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
		"show --property=ActiveState --property=MainPID codex-remote.service",
		"start codex-remote.service",
		"stop codex-remote.service",
		"show --property=ActiveState --property=MainPID codex-remote.service",
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

func TestRunUpgradeHelperWithStatePathFailsWhenRollbackStopFails(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "install-state.json")
	configPath := filepath.Join(dir, ".config", "codex-remote", "config.json")
	currentBinary := seedBinary(t, filepath.Join(dir, "bin", executableName(runtime.GOOS)), "old-binary")
	seedBinary(t, filepath.Join(dir, "releases", "v1.1.0", executableName(runtime.GOOS)), "new-binary")

	cfg := config.DefaultAppConfig()
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	stateValue := InstallState{
		BaseDir:           dir,
		ConfigPath:        configPath,
		StatePath:         statePath,
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

	originalObserve := upgradeHelperObserveFunc
	originalSleep := upgradeHelperSleepFunc
	originalReadPID := upgradeHelperReadPIDFunc
	originalTerminate := upgradeHelperTerminateProcessFunc
	originalStart := upgradeHelperStartDetachedDaemonFunc
	defer func() {
		upgradeHelperObserveFunc = originalObserve
		upgradeHelperSleepFunc = originalSleep
		upgradeHelperReadPIDFunc = originalReadPID
		upgradeHelperTerminateProcessFunc = originalTerminate
		upgradeHelperStartDetachedDaemonFunc = originalStart
	}()

	upgradeHelperObserveFunc = func(context.Context, config.LoadedAppConfig) error {
		return errors.New("gateway unhealthy")
	}
	upgradeHelperSleepFunc = func(time.Duration) {}
	upgradeHelperReadPIDFunc = func(string) (int, error) { return 123, nil }
	stopCalls := 0
	upgradeHelperTerminateProcessFunc = func(int, time.Duration) error {
		stopCalls++
		if stopCalls == 1 {
			return nil
		}
		return errors.New("process still alive")
	}
	startCalls := 0
	upgradeHelperStartDetachedDaemonFunc = func(relayruntime.LaunchOptions) (int, error) {
		startCalls++
		return 999, nil
	}

	err = RunUpgradeHelperWithStatePath(context.Background(), statePath)
	if err == nil || !strings.Contains(err.Error(), "rollback stop failed") || !strings.Contains(err.Error(), "gateway unhealthy") {
		t.Fatalf("RunUpgradeHelperWithStatePath error = %v, want rollback stop failure after gateway unhealthy", err)
	}
	if startCalls != 1 {
		t.Fatalf("start calls = %d, want 1 initial start only", startCalls)
	}
	currentRaw, err := os.ReadFile(currentBinary)
	if err != nil {
		t.Fatalf("ReadFile current: %v", err)
	}
	if string(currentRaw) != "new-binary" {
		t.Fatalf("current binary content = %q, want new-binary because rollback copy must not run", string(currentRaw))
	}
	updated, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if updated.PendingUpgrade == nil || updated.PendingUpgrade.Phase != PendingUpgradePhaseFailed {
		t.Fatalf("pending upgrade = %#v, want failed", updated.PendingUpgrade)
	}
}
