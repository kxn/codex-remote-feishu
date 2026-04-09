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
	helperBinary := seedBinary(t, filepath.Join(baseDir, "helper-bin", executableName(runtime.GOOS)), "helper-binary")

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
		HelperBinary: helperBinary,
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
	if string(helperRaw) != "helper-binary" {
		t.Fatalf("helper binary content = %q, want helper-binary", string(helperRaw))
	}
	if got, want := strings.Join(startedArgs, "\x00"), strings.Join([]string{"upgrade-helper", "-state-path", statePath}, "\x00"); got != want {
		t.Fatalf("helper args = %#v, want %#v", startedArgs, []string{"upgrade-helper", "-state-path", statePath})
	}

	updated, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if updated.PendingUpgrade == nil || updated.PendingUpgrade.Phase != PendingUpgradePhasePrepared {
		t.Fatalf("pending upgrade = %#v, want prepared", updated.PendingUpgrade)
	}
	if updated.PendingUpgrade.TargetVersion != slot {
		t.Fatalf("pending target version = %q, want %q", updated.PendingUpgrade.TargetVersion, slot)
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
	helperBinary := seedBinary(t, filepath.Join(baseDir, "helper-bin", executableName(runtime.GOOS)), "helper-binary")

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
		HelperBinary: helperBinary,
		Slot:         "local-test",
	})
	if err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("RunLocalBinaryUpgradeWithStatePath error = %v, want already in progress", err)
	}
}

func TestRunMainUpgradeSourceBinaryStartsLocalUpgradeTransaction(t *testing.T) {
	baseDir := t.TempDir()
	statePath := defaultInstallStatePath(baseDir)
	currentBinary := seedBinary(t, filepath.Join(baseDir, "installed-bin", executableName(runtime.GOOS)), "stable-binary")
	sourceBinary := seedBinary(t, filepath.Join(baseDir, "source-bin", executableName(runtime.GOOS)), "local-build")
	helperBinary := seedBinary(t, filepath.Join(baseDir, "helper-bin", executableName(runtime.GOOS)), "helper-binary")

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

	originalValidator := sourceBinaryValidator
	originalExec := executablePath
	originalStart := upgradeHelperStartDetachedCommandFunc
	sourceBinaryValidator = func(string) error { return nil }
	executablePath = func() (string, error) { return helperBinary, nil }
	upgradeHelperStartDetachedCommandFunc = func(opts relayruntime.DetachedCommandOptions) (int, error) {
		return 321, nil
	}
	defer func() {
		sourceBinaryValidator = originalValidator
		executablePath = originalExec
		upgradeHelperStartDetachedCommandFunc = originalStart
	}()

	var stdout bytes.Buffer
	if err := RunMain([]string{
		"-base-dir", baseDir,
		"-upgrade-source-binary", sourceBinary,
		"-upgrade-slot", "local-test",
	}, strings.NewReader(""), &stdout, &bytes.Buffer{}, "vtest"); err != nil {
		t.Fatalf("RunMain local upgrade: %v", err)
	}
	if !strings.Contains(stdout.String(), "slot: local-test") {
		t.Fatalf("stdout = %q, want local-test slot", stdout.String())
	}
	updated, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if updated.PendingUpgrade == nil || updated.PendingUpgrade.TargetVersion != "local-test" {
		t.Fatalf("pending upgrade = %#v, want local-test", updated.PendingUpgrade)
	}
}
