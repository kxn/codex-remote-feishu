package install

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRunPackagedInstallProbeFirstInstallReturnsEditableDefaultDir(t *testing.T) {
	t.Setenv(repoRootEnvVar, t.TempDir())
	baseDir := t.TempDir()

	var stdout bytes.Buffer
	if err := RunPackagedInstallProbe([]string{
		"-base-dir", baseDir,
		"-current-version", "v1.2.3",
		"-format", "json",
	}, bytes.NewBuffer(nil), &stdout, &bytes.Buffer{}, "vtest"); err != nil {
		t.Fatalf("RunPackagedInstallProbe first install: %v", err)
	}

	var result PackagedInstallProbeResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode probe result json: %v", err)
	}
	if !result.OK {
		t.Fatalf("result.OK = false, want true: %#v", result)
	}
	if result.Mode != string(packagedInstallModeFirstInstall) {
		t.Fatalf("result.Mode = %q, want %q", result.Mode, packagedInstallModeFirstInstall)
	}
	if !result.InstallLocationEditable {
		t.Fatalf("InstallLocationEditable = false, want true")
	}
	if result.SameVersion {
		t.Fatalf("SameVersion = true, want false")
	}
	if result.CurrentInstallBinDir != "" {
		t.Fatalf("CurrentInstallBinDir = %q, want empty", result.CurrentInstallBinDir)
	}
	wantDir := defaultInstallBinDirForInstance(runtime.GOOS, baseDir, defaultInstanceID)
	if result.SuggestedInstallBinDir != wantDir {
		t.Fatalf("SuggestedInstallBinDir = %q, want %q", result.SuggestedInstallBinDir, wantDir)
	}
	if manager, ok := managedServiceManagerForGOOS(runtime.GOOS); ok {
		if result.ServiceManager != string(manager) {
			t.Fatalf("ServiceManager = %q, want %q", result.ServiceManager, manager)
		}
	}
}

func TestRunPackagedInstallProbeRepairReturnsLockedDirAndSameVersion(t *testing.T) {
	t.Setenv(repoRootEnvVar, t.TempDir())
	baseDir := t.TempDir()
	statePath := defaultInstallStatePathForInstance(baseDir, defaultInstanceID)
	liveBinary := seedBinary(t, filepath.Join(baseDir, "installed-bin", executableName(runtime.GOOS)), "current-binary")
	if err := WriteState(statePath, InstallState{
		InstanceID:        defaultInstanceID,
		BaseDir:           baseDir,
		ConfigPath:        defaultConfigPathForInstance(baseDir, defaultInstanceID),
		StatePath:         statePath,
		ServiceManager:    ServiceManagerDetached,
		InstallSource:     InstallSourceRelease,
		CurrentTrack:      ReleaseTrackBeta,
		CurrentVersion:    "v2.0.0-beta.1",
		CurrentBinaryPath: liveBinary,
		InstalledBinary:   liveBinary,
		VersionsRoot:      filepath.Join(baseDir, "releases"),
		CurrentSlot:       "v2.0.0-beta.1",
	}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	var stdout bytes.Buffer
	if err := RunPackagedInstallProbe([]string{
		"-state-path", statePath,
		"-current-version", "v2.0.0-beta.1",
		"-format", "json",
	}, bytes.NewBuffer(nil), &stdout, &bytes.Buffer{}, "vtest"); err != nil {
		t.Fatalf("RunPackagedInstallProbe repair: %v", err)
	}

	var result PackagedInstallProbeResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode probe result json: %v", err)
	}
	if !result.OK {
		t.Fatalf("result.OK = false, want true: %#v", result)
	}
	if result.Mode != string(packagedInstallModeRepair) {
		t.Fatalf("result.Mode = %q, want %q", result.Mode, packagedInstallModeRepair)
	}
	if result.InstallLocationEditable {
		t.Fatalf("InstallLocationEditable = true, want false")
	}
	if !result.SameVersion {
		t.Fatalf("SameVersion = false, want true")
	}
	wantDir := filepath.Dir(liveBinary)
	if result.CurrentInstallBinDir != wantDir {
		t.Fatalf("CurrentInstallBinDir = %q, want %q", result.CurrentInstallBinDir, wantDir)
	}
	if result.SuggestedInstallBinDir != wantDir {
		t.Fatalf("SuggestedInstallBinDir = %q, want %q", result.SuggestedInstallBinDir, wantDir)
	}
	if result.CurrentVersion != "v2.0.0-beta.1" {
		t.Fatalf("CurrentVersion = %q, want v2.0.0-beta.1", result.CurrentVersion)
	}
	if result.CurrentTrack != string(ReleaseTrackBeta) {
		t.Fatalf("CurrentTrack = %q, want beta", result.CurrentTrack)
	}
	if result.ServiceManager != string(ServiceManagerDetached) {
		t.Fatalf("ServiceManager = %q, want detached", result.ServiceManager)
	}
}
