package install

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunPackagedInstallFirstInstallWritesStateAndJSONResult(t *testing.T) {
	t.Setenv(repoRootEnvVar, t.TempDir())
	baseDir := t.TempDir()
	sourceBinary := seedBinary(t, filepath.Join(baseDir, "pkg", executableName(runtime.GOOS)), "package-binary")
	resultFile := filepath.Join(baseDir, "result", "packaged-install.ini")

	originalValidator := sourceBinaryValidator
	sourceBinaryValidator = func(string) error { return nil }
	defer func() { sourceBinaryValidator = originalValidator }()

	originalEnsureReady := packagedInstallEnsureReadyFunc
	packagedInstallEnsureReadyFunc = func(_ context.Context, _ string, _ string) (DaemonReadyStatus, error) {
		return DaemonReadyStatus{
			AdminURL:      "http://localhost:9501/admin/",
			SetupURL:      "http://localhost:9501/setup",
			SetupRequired: true,
			LogPath:       filepath.Join(baseDir, "logs", "daemon.log"),
		}, nil
	}
	defer func() { packagedInstallEnsureReadyFunc = originalEnsureReady }()

	var stdout bytes.Buffer
	if err := RunPackagedInstall([]string{
		"-base-dir", baseDir,
		"-binary", sourceBinary,
		"-current-version", "v1.2.3",
		"-format", "json",
		"-result-file", resultFile,
	}, bytes.NewBuffer(nil), &stdout, &bytes.Buffer{}, "vtest"); err != nil {
		t.Fatalf("RunPackagedInstall first install: %v", err)
	}

	var result PackagedInstallResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode result json: %v", err)
	}
	if !result.OK {
		t.Fatalf("result.OK = false, want true: %#v", result)
	}
	if result.Mode != string(packagedInstallModeFirstInstall) {
		t.Fatalf("result.Mode = %q, want %q", result.Mode, packagedInstallModeFirstInstall)
	}
	if result.CurrentSlot != "v1.2.3" {
		t.Fatalf("result.CurrentSlot = %q, want v1.2.3", result.CurrentSlot)
	}
	if result.SetupURL != "http://localhost:9501/setup" {
		t.Fatalf("result.SetupURL = %q, want setup url", result.SetupURL)
	}
	assertPackagedInstallResultFileContains(t, resultFile,
		"ok=true",
		"mode=first_install",
		"currentVersion=v1.2.3",
		"setupRequired=true",
		"setupURL=http://localhost:9501/setup",
	)

	statePath := defaultInstallStatePathForInstance(baseDir, defaultInstanceID)
	state, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.CurrentVersion != "v1.2.3" {
		t.Fatalf("CurrentVersion = %q, want v1.2.3", state.CurrentVersion)
	}
	if state.CurrentSlot != "v1.2.3" {
		t.Fatalf("CurrentSlot = %q, want v1.2.3", state.CurrentSlot)
	}
	if state.InstallSource != InstallSourceRelease {
		t.Fatalf("InstallSource = %q, want release", state.InstallSource)
	}
}

func TestRunPackagedInstallRepairOverwritesLiveBinaryAndClearsUpgradeState(t *testing.T) {
	t.Setenv(repoRootEnvVar, t.TempDir())
	baseDir := t.TempDir()
	statePath := defaultInstallStatePathForInstance(baseDir, defaultInstanceID)
	resultFile := filepath.Join(baseDir, "result", "packaged-install.ini")
	liveBinary := seedBinary(t, filepath.Join(baseDir, "installed-bin", executableName(runtime.GOOS)), "old-binary")
	sourceBinary := seedBinary(t, filepath.Join(baseDir, "pkg", executableName(runtime.GOOS)), "new-binary")
	versionsRoot := filepath.Join(baseDir, "releases")
	if err := WriteState(statePath, InstallState{
		InstanceID:        defaultInstanceID,
		BaseDir:           baseDir,
		ConfigPath:        defaultConfigPathForInstance(baseDir, defaultInstanceID),
		StatePath:         statePath,
		ServiceManager:    ServiceManagerDetached,
		InstallSource:     InstallSourceRelease,
		CurrentTrack:      ReleaseTrackProduction,
		CurrentVersion:    "v1.0.0",
		CurrentBinaryPath: liveBinary,
		InstalledBinary:   liveBinary,
		VersionsRoot:      versionsRoot,
		CurrentSlot:       "v1.0.0",
		PendingUpgrade: &PendingUpgrade{
			Phase: PendingUpgradePhasePrepared,
		},
		RollbackCandidate: &RollbackCandidate{BinaryPath: liveBinary},
	}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	originalValidator := sourceBinaryValidator
	sourceBinaryValidator = func(string) error { return nil }
	defer func() { sourceBinaryValidator = originalValidator }()

	originalEnsureReady := packagedInstallEnsureReadyFunc
	packagedInstallEnsureReadyFunc = func(_ context.Context, _ string, _ string) (DaemonReadyStatus, error) {
		return DaemonReadyStatus{
			AdminURL:      "http://localhost:9501/admin/",
			SetupRequired: false,
			LogPath:       filepath.Join(baseDir, "logs", "daemon.log"),
		}, nil
	}
	defer func() { packagedInstallEnsureReadyFunc = originalEnsureReady }()

	var stdout bytes.Buffer
	if err := RunPackagedInstall([]string{
		"-state-path", statePath,
		"-binary", sourceBinary,
		"-current-version", "v1.2.0-beta.1",
		"-format", "json",
		"-result-file", resultFile,
	}, bytes.NewBuffer(nil), &stdout, &bytes.Buffer{}, "vtest"); err != nil {
		t.Fatalf("RunPackagedInstall repair: %v", err)
	}

	var result PackagedInstallResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode result json: %v", err)
	}
	if !result.OK {
		t.Fatalf("result.OK = false, want true: %#v", result)
	}
	if result.Mode != string(packagedInstallModeRepair) {
		t.Fatalf("result.Mode = %q, want repair", result.Mode)
	}
	if result.CurrentSlot != "v1.2.0-beta.1" {
		t.Fatalf("result.CurrentSlot = %q, want new slot", result.CurrentSlot)
	}
	if result.CurrentTrack != string(ReleaseTrackBeta) {
		t.Fatalf("result.CurrentTrack = %q, want beta", result.CurrentTrack)
	}
	assertPackagedInstallResultFileContains(t, resultFile,
		"ok=true",
		"mode=repair",
		"currentTrack=beta",
		"currentVersion=v1.2.0-beta.1",
	)

	updated, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState updated: %v", err)
	}
	if updated.PendingUpgrade != nil {
		t.Fatalf("PendingUpgrade = %#v, want nil", updated.PendingUpgrade)
	}
	if updated.RollbackCandidate != nil {
		t.Fatalf("RollbackCandidate = %#v, want nil", updated.RollbackCandidate)
	}
	if updated.CurrentTrack != ReleaseTrackBeta {
		t.Fatalf("CurrentTrack = %q, want beta", updated.CurrentTrack)
	}
	if updated.CurrentVersion != "v1.2.0-beta.1" {
		t.Fatalf("CurrentVersion = %q, want new version", updated.CurrentVersion)
	}

	raw, err := os.ReadFile(liveBinary)
	if err != nil {
		t.Fatalf("read live binary: %v", err)
	}
	if string(raw) != "new-binary" {
		t.Fatalf("live binary content = %q, want new-binary", string(raw))
	}
	if _, err := os.Stat(filepath.Join(versionsRoot, "v1.2.0-beta.1", executableName(runtime.GOOS))); err != nil {
		t.Fatalf("expected staged binary: %v", err)
	}
}

func TestRunPackagedInstallWritesResultFileOnRepairFailure(t *testing.T) {
	t.Setenv(repoRootEnvVar, t.TempDir())
	baseDir := t.TempDir()
	statePath := defaultInstallStatePathForInstance(baseDir, defaultInstanceID)
	resultFile := filepath.Join(baseDir, "result", "packaged-install.ini")
	liveBinary := seedBinary(t, filepath.Join(baseDir, "installed-bin", executableName(runtime.GOOS)), "old-binary")
	sourceBinary := seedBinary(t, filepath.Join(baseDir, "pkg", executableName(runtime.GOOS)), "new-binary")

	if err := WriteState(statePath, InstallState{
		InstanceID:        defaultInstanceID,
		BaseDir:           baseDir,
		ConfigPath:        defaultConfigPathForInstance(baseDir, defaultInstanceID),
		StatePath:         statePath,
		ServiceManager:    ServiceManagerDetached,
		InstallSource:     InstallSourceRelease,
		CurrentTrack:      ReleaseTrackProduction,
		CurrentVersion:    "v1.0.0",
		CurrentBinaryPath: liveBinary,
		InstalledBinary:   liveBinary,
		VersionsRoot:      filepath.Join(baseDir, "releases"),
		CurrentSlot:       "v1.0.0",
	}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	originalValidator := sourceBinaryValidator
	sourceBinaryValidator = func(string) error { return nil }
	defer func() { sourceBinaryValidator = originalValidator }()

	var stdout bytes.Buffer
	err := RunPackagedInstall([]string{
		"-state-path", statePath,
		"-binary", sourceBinary,
		"-install-bin-dir", filepath.Join(baseDir, "ignored"),
		"-result-file", resultFile,
	}, bytes.NewBuffer(nil), &stdout, &bytes.Buffer{}, "vtest")
	if err == nil {
		t.Fatal("RunPackagedInstall error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "-install-bin-dir cannot be used for existing installs") {
		t.Fatalf("RunPackagedInstall error = %v, want existing-install install-bin-dir rejection", err)
	}
	assertPackagedInstallResultFileContains(t, resultFile,
		"ok=false",
		"mode=repair",
		"serviceManager=detached",
		"error=-install-bin-dir cannot be used for existing installs",
	)
}

func assertPackagedInstallResultFileContains(t *testing.T, path string, wantFragments ...string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	got := string(raw)
	for _, fragment := range wantFragments {
		if !strings.Contains(got, fragment) {
			t.Fatalf("result file %q missing %q in:\n%s", path, fragment, got)
		}
	}
}
