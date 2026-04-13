package install

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestRunLocalBinaryUpgradeWithStatePathImportsBinaryAndStartsHelper(t *testing.T) {
	baseDir := t.TempDir()
	statePath := defaultInstallStatePath(baseDir)
	currentBinary := seedBinary(t, filepath.Join(baseDir, "installed-bin", executableName(runtime.GOOS)), "stable-binary")
	sourceBinary := seedBinary(t, filepath.Join(baseDir, "source-bin", executableName(runtime.GOOS)), "local-build")

	stateValue := InstallState{
		BaseDir:           baseDir,
		StatePath:         statePath,
		CurrentTrack:      ReleaseTrackAlpha,
		CurrentVersion:    "dev-old",
		CurrentBinaryPath: currentBinary,
		InstalledBinary:   currentBinary,
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

	targetBinary := filepath.Join(stateValue.VersionsRoot, slot, executableName(runtime.GOOS))
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
	sidecarRaw, err := os.ReadFile(UpgradeShimSidecarPath(startedBinary))
	if err != nil {
		t.Fatalf("ReadFile sidecar: %v", err)
	}
	if !bytes.Contains(sidecarRaw, []byte(statePath)) {
		t.Fatalf("sidecar = %q, want state path", string(sidecarRaw))
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
	if updated.PendingUpgrade.TargetBinaryPath != targetBinary {
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

func TestRunLocalBinaryUpgradeWithStatePathRejectsBusyPendingUpgrade(t *testing.T) {
	baseDir := t.TempDir()
	statePath := defaultInstallStatePath(baseDir)
	currentBinary := seedBinary(t, filepath.Join(baseDir, "installed-bin", executableName(runtime.GOOS)), "stable-binary")
	sourceBinary := seedBinary(t, filepath.Join(baseDir, "source-bin", executableName(runtime.GOOS)), "local-build")

	stateValue := InstallState{
		BaseDir:           baseDir,
		StatePath:         statePath,
		CurrentVersion:    "dev-old",
		CurrentBinaryPath: currentBinary,
		InstalledBinary:   currentBinary,
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
	currentBinary := seedBinary(t, filepath.Join(baseDir, "installed-bin", executableName(runtime.GOOS)), "stable-binary")
	artifactBinary := seedBinary(t, filepath.Join(baseDir, ".local", "share", "codex-remote", "local-upgrade", executableName(runtime.GOOS)), "local-build")

	stateValue := InstallState{
		BaseDir:           baseDir,
		StatePath:         statePath,
		CurrentTrack:      ReleaseTrackAlpha,
		CurrentVersion:    "dev-old",
		CurrentBinaryPath: currentBinary,
		InstalledBinary:   currentBinary,
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
	if sidecarRaw, err := os.ReadFile(UpgradeShimSidecarPath(startedBinary)); err != nil {
		t.Fatalf("ReadFile sidecar: %v", err)
	} else if !bytes.Contains(sidecarRaw, []byte(statePath)) {
		t.Fatalf("sidecar = %q, want state path", string(sidecarRaw))
	}
}

func TestRunLocalUpgradeDebugInstanceUsesDebugStatePath(t *testing.T) {
	baseDir := t.TempDir()
	statePath := defaultInstallStatePathForInstance(baseDir, debugInstanceID)
	currentBinary := seedBinary(t, filepath.Join(baseDir, "installed-bin", executableName(runtime.GOOS)), "stable-binary")
	artifactBinary := seedBinary(t, filepath.Join(baseDir, ".local", "share", "codex-remote-debug", "codex-remote", "local-upgrade", executableName(runtime.GOOS)), "local-build")

	stateValue := InstallState{
		InstanceID:        debugInstanceID,
		BaseDir:           baseDir,
		StatePath:         statePath,
		CurrentTrack:      ReleaseTrackAlpha,
		CurrentVersion:    "dev-old",
		CurrentBinaryPath: currentBinary,
		InstalledBinary:   currentBinary,
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
	currentBinary := seedBinary(t, filepath.Join(baseDir, "installed-bin", executableName(runtime.GOOS)), "stable-binary")
	artifactBinary := seedBinary(t, filepath.Join(baseDir, ".local", "share", "codex-remote-master", "codex-remote", "local-upgrade", executableName(runtime.GOOS)), "local-build")

	stateValue := InstallState{
		InstanceID:        "master",
		BaseDir:           baseDir,
		StatePath:         statePath,
		CurrentTrack:      ReleaseTrackAlpha,
		CurrentVersion:    "dev-old",
		CurrentBinaryPath: currentBinary,
		InstalledBinary:   currentBinary,
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
	if !strings.Contains(err.Error(), "requires a bound repo target or explicit -instance/-base-dir/-state-path") {
		t.Fatalf("RunLocalUpgrade() error = %v", err)
	}
}
