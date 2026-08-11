package install

import (
	"bytes"
	"context"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/installshim"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
	"github.com/kxn/codex-remote-feishu/internal/shim"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestRunLocalBinaryUpgradeWithStatePathImportsBinaryAndStartsHelper(t *testing.T) {
	baseDir := t.TempDir()
	statePath := defaultInstallStatePath(baseDir)
	currentBinary := seedBinary(t, filepath.Join(baseDir, "installed-bin", xutil.ExecutableName(runtime.GOOS)), "stable-binary")
	sourceBinary := seedBinary(t, filepath.Join(baseDir, "source-bin", xutil.ExecutableName(runtime.GOOS)), "local-build")

	stateValue := InstallState{
		BaseDir:           baseDir,
		StatePath:         statePath,
		CurrentTrack:      ReleaseTrackAlpha,
		CurrentVersion:    "dev-old",
		CurrentBinaryPath: currentBinary,
		VersionsRoot:      filepath.Join(baseDir, ".local", "share", "codex-remote", "releases"),
	}
	if err := WriteState(statePath, stateValue); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	originalStart := upgradeHelperStartDetachedCommandFunc
	var startedBinary string
	var startedArgs []string
	upgradeHelperStartDetachedCommandFunc = func(opts relayruntime.DetachedCommandOptions) (int, error) {
		startedBinary = opts.BinaryPath
		startedArgs = append([]string(nil), opts.Args...)
		return 123, nil
	}
	defer func() { upgradeHelperStartDetachedCommandFunc = originalStart }()

	slot, err := RunLocalBinaryUpgradeWithStatePath(LocalBinaryUpgradeOptions{
		StatePath:    statePath,
		SourceBinary: sourceBinary,
	})
	if err != nil {
		t.Fatalf("RunLocalBinaryUpgradeWithStatePath: %v", err)
	}
	if !strings.HasPrefix(slot, "local-") {
		t.Fatalf("slot = %q, want local-*", slot)
	}

	targetBinary := filepath.Join(stateValue.VersionsRoot, slot, xutil.ExecutableName(runtime.GOOS))
	targetRaw, err := os.ReadFile(targetBinary)
	if err != nil {
		t.Fatalf("ReadFile target: %v", err)
	}
	if string(targetRaw) != "local-build" {
		t.Fatalf("target binary content = %q, want local-build", string(targetRaw))
	}
	if startedBinary == "" {
		t.Fatal("expected detached helper start to be invoked")
	}
	helperRaw, err := os.ReadFile(startedBinary)
	if err != nil {
		t.Fatalf("ReadFile helper: %v", err)
	}
	if len(helperRaw) == 0 {
		t.Fatal("expected helper shim binary to be non-empty")
	}
	if len(startedArgs) != 0 {
		t.Fatalf("helper args = %#v, want empty direct-exec shim", startedArgs)
	}
	sidecar, err := shim.ReadSidecar(installshim.UpgradeShimSidecarPath(startedBinary))
	if err != nil {
		t.Fatalf("ReadSidecar: %v", err)
	}
	if !testutil.SamePath(sidecar.InstallStatePath, statePath) {
		t.Fatalf("sidecar installStatePath = %q, want %q", sidecar.InstallStatePath, statePath)
	}

	updated, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if updated.PendingUpgrade == nil || updated.PendingUpgrade.Phase != PendingUpgradePhasePrepared {
		t.Fatalf("pending upgrade = %#v, want prepared", updated.PendingUpgrade)
	}
	if updated.PendingUpgrade.Source != UpgradeSourceLocal {
		t.Fatalf("pending source = %q, want local", updated.PendingUpgrade.Source)
	}
	if updated.PendingUpgrade.TargetSlot != slot {
		t.Fatalf("pending target slot = %q, want %q", updated.PendingUpgrade.TargetSlot, slot)
	}
	if !testutil.SamePath(updated.PendingUpgrade.TargetBinaryPath, targetBinary) {
		t.Fatalf("pending target binary = %q, want %q", updated.PendingUpgrade.TargetBinaryPath, targetBinary)
	}
	if updated.PendingUpgrade.HelperUnitName != "" {
		t.Fatalf("pending helper unit = %q, want empty for detached helper", updated.PendingUpgrade.HelperUnitName)
	}
	if updated.PendingUpgrade.TargetVersion == "" {
		t.Fatalf("pending target version = empty, want non-empty")
	}
	if updated.RollbackCandidate == nil || strings.TrimSpace(updated.RollbackCandidate.BinaryPath) == "" {
		t.Fatalf("rollback candidate = %#v, want binary backup", updated.RollbackCandidate)
	}
}

func TestRunLocalBinaryUpgradeWithStatePathRepairsCrossPlatformServiceManager(t *testing.T) {
	baseDir := t.TempDir()
	homeDir := t.TempDir()
	statePath := defaultInstallStatePath(baseDir)
	currentBinary := seedBinary(t, filepath.Join(baseDir, "installed-bin", xutil.ExecutableName(runtime.GOOS)), "stable-binary")
	sourceBinary := seedBinary(t, filepath.Join(baseDir, "source-bin", xutil.ExecutableName(runtime.GOOS)), "local-build")
	systemdUnitPath := filepath.Join(homeDir, ".config", "systemd", "user", "codex-remote.service")
	if err := os.MkdirAll(filepath.Dir(systemdUnitPath), 0o755); err != nil {
		t.Fatalf("MkdirAll systemd unit dir: %v", err)
	}
	if err := os.WriteFile(systemdUnitPath, []byte("[Service]\nExecStart=/new/bin/codex-remote daemon\n"), 0o644); err != nil {
		t.Fatalf("WriteFile systemd unit: %v", err)
	}

	originalGOOS := serviceRuntimeGOOS
	serviceRuntimeGOOS = "linux"
	defer func() { serviceRuntimeGOOS = originalGOOS }()
	originalHome := serviceUserHomeDir
	serviceUserHomeDir = func() (string, error) { return homeDir, nil }
	defer func() { serviceUserHomeDir = originalHome }()
	originalSystemctl := systemctlUserRunner
	systemctlUserRunner = func(_ context.Context, args ...string) (string, error) {
		if len(args) >= 1 && args[0] == "is-enabled" {
			return "enabled\n", nil
		}
		return "", nil
	}
	defer func() { systemctlUserRunner = originalSystemctl }()

	stateValue := InstallState{
		InstanceID:        defaultInstanceID,
		BaseDir:           baseDir,
		StatePath:         statePath,
		ServiceManager:    ServiceManagerLaunchdUser,
		ServiceUnitPath:   filepath.Join(homeDir, "Library", "LaunchAgents", "com.codex-remote.service.plist"),
		CurrentTrack:      ReleaseTrackAlpha,
		CurrentVersion:    "dev-old",
		CurrentBinaryPath: currentBinary,
		VersionsRoot:      filepath.Join(baseDir, ".local", "share", "codex-remote", "releases"),
	}
	if err := WriteState(statePath, stateValue); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	originalStart := upgradeHelperStartSystemdUserTransientFunc
	var helperUnit string
	upgradeHelperStartSystemdUserTransientFunc = func(_ context.Context, opts systemdUserTransientCommandOptions) (string, error) {
		helperUnit = opts.UnitName
		return "", nil
	}
	defer func() { upgradeHelperStartSystemdUserTransientFunc = originalStart }()

	if _, err := RunLocalBinaryUpgradeWithStatePath(LocalBinaryUpgradeOptions{
		StatePath:    statePath,
		SourceBinary: sourceBinary,
	}); err != nil {
		t.Fatalf("RunLocalBinaryUpgradeWithStatePath: %v", err)
	}
	if helperUnit == "" {
		t.Fatal("expected local upgrade helper to be launched through systemd")
	}

	updated, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if updated.ServiceManager != ServiceManagerSystemdUser {
		t.Fatalf("ServiceManager = %q, want %q", updated.ServiceManager, ServiceManagerSystemdUser)
	}
	if updated.ServiceUnitPath != systemdUnitPath {
		t.Fatalf("ServiceUnitPath = %q, want %q", updated.ServiceUnitPath, systemdUnitPath)
	}
}

func TestRunLocalBinaryUpgradeWithStatePathRejectsBusyPendingUpgrade(t *testing.T) {
	baseDir := t.TempDir()
	statePath := defaultInstallStatePath(baseDir)
	currentBinary := seedBinary(t, filepath.Join(baseDir, "installed-bin", xutil.ExecutableName(runtime.GOOS)), "stable-binary")
	sourceBinary := seedBinary(t, filepath.Join(baseDir, "source-bin", xutil.ExecutableName(runtime.GOOS)), "local-build")

	stateValue := InstallState{
		BaseDir:           baseDir,
		StatePath:         statePath,
		CurrentVersion:    "dev-old",
		CurrentBinaryPath: currentBinary,
		VersionsRoot:      filepath.Join(baseDir, ".local", "share", "codex-remote", "releases"),
		PendingUpgrade: &PendingUpgrade{
			Phase:         PendingUpgradePhaseObserving,
			TargetVersion: "v1.2.3",
		},
	}
	if err := WriteState(statePath, stateValue); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	_, err := RunLocalBinaryUpgradeWithStatePath(LocalBinaryUpgradeOptions{
		StatePath:    statePath,
		SourceBinary: sourceBinary,
		Slot:         "local-test",
	})
	if err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("RunLocalBinaryUpgradeWithStatePath error = %v, want already in progress", err)
	}
}

func TestRunLocalUpgradeStartsLocalUpgradeTransaction(t *testing.T) {
	t.Setenv(repoRootEnvVar, t.TempDir())
	baseDir := t.TempDir()
	statePath := defaultInstallStatePath(baseDir)
	currentBinary := seedBinary(t, filepath.Join(baseDir, "installed-bin", xutil.ExecutableName(runtime.GOOS)), "stable-binary")
	artifactBinary := seedBinary(t, filepath.Join(baseDir, ".local", "share", "codex-remote", "local-upgrade", xutil.ExecutableName(runtime.GOOS)), "local-build")

	stateValue := InstallState{
		BaseDir:           baseDir,
		StatePath:         statePath,
		CurrentTrack:      ReleaseTrackAlpha,
		CurrentVersion:    "dev-old",
		CurrentBinaryPath: currentBinary,
		VersionsRoot:      filepath.Join(baseDir, ".local", "share", "codex-remote", "releases"),
	}
	if err := WriteState(statePath, stateValue); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	if got, want := LocalUpgradeArtifactPath(stateValue), artifactBinary; got != want {
		t.Fatalf("artifact path = %q, want %q", got, want)
	}

	originalStart := upgradeHelperStartDetachedCommandFunc
	var startedBinary string
	upgradeHelperStartDetachedCommandFunc = func(opts relayruntime.DetachedCommandOptions) (int, error) {
		startedBinary = opts.BinaryPath
		return 321, nil
	}
	defer func() {
		upgradeHelperStartDetachedCommandFunc = originalStart
	}()

	var stdout bytes.Buffer
	if err := RunLocalUpgrade([]string{
		"-base-dir", baseDir,
		"-slot", "local-test",
	}, strings.NewReader(""), &stdout, &bytes.Buffer{}, "vtest"); err != nil {
		t.Fatalf("RunLocalUpgrade: %v", err)
	}
	if !strings.Contains(stdout.String(), "slot: local-test") {
		t.Fatalf("stdout = %q, want local-test slot", stdout.String())
	}
	updated, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if updated.PendingUpgrade == nil || updated.PendingUpgrade.TargetSlot != "local-test" {
		t.Fatalf("pending upgrade = %#v, want local-test", updated.PendingUpgrade)
	}
	helperRaw, err := os.ReadFile(startedBinary)
	if err != nil {
		t.Fatalf("ReadFile helper: %v", err)
	}
	if len(helperRaw) == 0 {
		t.Fatal("expected helper shim binary to be non-empty")
	}
	if sidecar, err := shim.ReadSidecar(installshim.UpgradeShimSidecarPath(startedBinary)); err != nil {
		t.Fatalf("ReadSidecar: %v", err)
	} else if !testutil.SamePath(sidecar.InstallStatePath, statePath) {
		t.Fatalf("sidecar installStatePath = %q, want %q", sidecar.InstallStatePath, statePath)
	}
}

func TestRunLocalUpgradeDebugInstanceUsesDebugStatePath(t *testing.T) {
	baseDir := t.TempDir()
	statePath := defaultInstallStatePathForInstance(baseDir, debugInstanceID)
	currentBinary := seedBinary(t, filepath.Join(baseDir, "installed-bin", xutil.ExecutableName(runtime.GOOS)), "stable-binary")
	artifactBinary := seedBinary(t, filepath.Join(baseDir, ".local", "share", "codex-remote-debug", "codex-remote", "local-upgrade", xutil.ExecutableName(runtime.GOOS)), "local-build")

	stateValue := InstallState{
		InstanceID:        debugInstanceID,
		BaseDir:           baseDir,
		StatePath:         statePath,
		CurrentTrack:      ReleaseTrackAlpha,
		CurrentVersion:    "dev-old",
		CurrentBinaryPath: currentBinary,
		VersionsRoot:      filepath.Join(baseDir, ".local", "share", "codex-remote-debug", "codex-remote", "releases"),
	}
	if err := WriteState(statePath, stateValue); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	if got, want := LocalUpgradeArtifactPath(stateValue), artifactBinary; got != want {
		t.Fatalf("artifact path = %q, want %q", got, want)
	}

	originalStart := upgradeHelperStartDetachedCommandFunc
	var startedBinary string
	upgradeHelperStartDetachedCommandFunc = func(opts relayruntime.DetachedCommandOptions) (int, error) {
		startedBinary = opts.BinaryPath
		return 321, nil
	}
	defer func() {
		upgradeHelperStartDetachedCommandFunc = originalStart
	}()

	var stdout bytes.Buffer
	if err := RunLocalUpgrade([]string{
		"-instance", debugInstanceID,
		"-base-dir", baseDir,
		"-slot", "local-test",
	}, strings.NewReader(""), &stdout, &bytes.Buffer{}, "vtest"); err != nil {
		t.Fatalf("RunLocalUpgrade: %v", err)
	}
	if !strings.Contains(stdout.String(), statePath) {
		t.Fatalf("stdout = %q, want debug state path", stdout.String())
	}
	if startedBinary == "" {
		t.Fatal("expected helper launcher to run")
	}
	helperRaw, err := os.ReadFile(startedBinary)
	if err != nil {
		t.Fatalf("ReadFile helper: %v", err)
	}
	if len(helperRaw) == 0 {
		t.Fatal("expected helper shim binary to be non-empty")
	}
}

func TestRunLocalUpgradeUsesWorkspaceBindingWhenFlagsOmitted(t *testing.T) {
	repoRoot := t.TempDir()
	baseDir := t.TempDir()
	statePath := defaultInstallStatePathForInstance(baseDir, "master")
	currentBinary := seedBinary(t, filepath.Join(baseDir, "installed-bin", xutil.ExecutableName(runtime.GOOS)), "stable-binary")
	artifactBinary := seedBinary(t, filepath.Join(baseDir, ".local", "share", "codex-remote-master", "codex-remote", "local-upgrade", xutil.ExecutableName(runtime.GOOS)), "local-build")

	stateValue := InstallState{
		InstanceID:        "master",
		BaseDir:           baseDir,
		StatePath:         statePath,
		CurrentTrack:      ReleaseTrackAlpha,
		CurrentVersion:    "dev-old",
		CurrentBinaryPath: currentBinary,
		VersionsRoot:      filepath.Join(baseDir, ".local", "share", "codex-remote-master", "codex-remote", "releases"),
	}
	if err := WriteState(statePath, stateValue); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	if got, want := LocalUpgradeArtifactPath(stateValue), artifactBinary; got != want {
		t.Fatalf("artifact path = %q, want %q", got, want)
	}
	if err := writeRepoInstallBinding(repoRoot, repoInstallBinding{
		InstanceID: "master",
		BaseDir:    baseDir,
	}); err != nil {
		t.Fatalf("writeRepoInstallBinding: %v", err)
	}
	t.Setenv(repoRootEnvVar, repoRoot)

	originalStart := upgradeHelperStartDetachedCommandFunc
	var startedBinary string
	upgradeHelperStartDetachedCommandFunc = func(opts relayruntime.DetachedCommandOptions) (int, error) {
		startedBinary = opts.BinaryPath
		return 321, nil
	}
	defer func() {
		upgradeHelperStartDetachedCommandFunc = originalStart
	}()

	var stdout bytes.Buffer
	if err := RunLocalUpgrade([]string{
		"-slot", "binding-test",
	}, strings.NewReader(""), &stdout, &bytes.Buffer{}, "vtest"); err != nil {
		t.Fatalf("RunLocalUpgrade: %v", err)
	}
	if !strings.Contains(stdout.String(), statePath) {
		t.Fatalf("stdout = %q, want bound state path", stdout.String())
	}
	if startedBinary == "" {
		t.Fatal("expected helper launcher to run")
	}
	helperRaw, err := os.ReadFile(startedBinary)
	if err != nil {
		t.Fatalf("ReadFile helper: %v", err)
	}
	if len(helperRaw) == 0 {
		t.Fatal("expected helper shim binary to be non-empty")
	}
}

func TestRunLocalUpgradeRequiresExplicitTargetWithoutWorkspaceBinding(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv(repoRootEnvVar, repoRoot)

	var stdout bytes.Buffer
	err := RunLocalUpgrade([]string{
		"-slot", "binding-test",
	}, strings.NewReader(""), &stdout, &bytes.Buffer{}, "vtest")
	if err == nil {
		t.Fatal("RunLocalUpgrade() error = nil, want missing target error")
	}
	if !strings.Contains(err.Error(), "requires a repo install target or explicit -instance/-base-dir/-state-path") {
		t.Fatalf("RunLocalUpgrade() error = %v", err)
	}
}
