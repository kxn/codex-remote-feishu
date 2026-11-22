package install

import (
	"bytes"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

func TestRunMainHelpReturnsNil(t *testing.T) {
	var stdout bytes.Buffer
	err := RunMain([]string{"-h"}, strings.NewReader(""), &stdout, &bytes.Buffer{}, "vtest")
	if err != nil {
		t.Fatalf("RunMain(-h): %v", err)
	}
	if !strings.Contains(stdout.String(), "-binary") {
		t.Fatalf("help output missing -binary flag: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "optional Codex path override") {
		t.Fatalf("help output missing optional Codex override wording: %q", stdout.String())
	}
}

func TestRunMainRejectsInteractiveBootstrapOnly(t *testing.T) {
	err := RunMain([]string{"-interactive", "-bootstrap-only"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "vtest")
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("RunMain interactive bootstrap-only error = %v", err)
	}
}

func TestRunMainBootstrapOnlyPreservesExistingRelayURLWhenFlagOmitted(t *testing.T) {
	t.Setenv(repoRootEnvVar, t.TempDir())
	baseDir := t.TempDir()
	installBinDir := filepath.Join(baseDir, "installed-bin")
	configPath := filepath.Join(baseDir, ".config", "codex-remote", "config.json")
	cfg := config.DefaultAppConfig()
	cfg.Relay.ServerURL = "ws://127.0.0.1:9910/ws/agent"
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	binaryPath := filepath.Join(baseDir, "bin", "codex-remote")
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		t.Fatalf("mkdir binary dir: %v", err)
	}
	if err := os.WriteFile(binaryPath, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	originalValidator := sourceBinaryValidator
	sourceBinaryValidator = func(string) error { return nil }
	defer func() { sourceBinaryValidator = originalValidator }()

	if err := RunMain([]string{
		"-bootstrap-only",
		"-base-dir", baseDir,
		"-install-bin-dir", installBinDir,
		"-binary", binaryPath,
	}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "vtest"); err != nil {
		t.Fatalf("RunMain bootstrap-only: %v", err)
	}

	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if loaded.Config.Relay.ServerURL != "ws://127.0.0.1:9910/ws/agent" {
		t.Fatalf("relay server url = %q, want preserved value", loaded.Config.Relay.ServerURL)
	}
}

func TestRunMainBootstrapOnlyPersistsExplicitCodexHome(t *testing.T) {
	t.Setenv(repoRootEnvVar, t.TempDir())
	baseDir := t.TempDir()
	codexHome := filepath.Join(baseDir, "custom-codex-home")
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatalf("MkdirAll CODEX_HOME: %v", err)
	}
	t.Setenv("CODEX_HOME", codexHome)
	binaryPath := seedBinary(t, filepath.Join(baseDir, "bin", "codex-remote"), "binary")

	originalValidator := sourceBinaryValidator
	sourceBinaryValidator = func(string) error { return nil }
	defer func() { sourceBinaryValidator = originalValidator }()

	if err := RunMain([]string{
		"-bootstrap-only",
		"-base-dir", baseDir,
		"-binary", binaryPath,
	}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "vtest"); err != nil {
		t.Fatalf("RunMain bootstrap-only: %v", err)
	}

	state, err := LoadState(defaultInstallStatePath(baseDir))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.CodexHome != codexHome {
		t.Fatalf("CodexHome = %q, want %q", state.CodexHome, codexHome)
	}
}

func TestRunMainBootstrapOnlyPreservesExistingInstallMetadataWhenFlagsOmitted(t *testing.T) {
	t.Setenv(repoRootEnvVar, t.TempDir())
	t.Setenv("CODEX_HOME", "")
	baseDir := t.TempDir()
	installBinDir := filepath.Join(baseDir, "installed-bin")
	statePath := defaultInstallStatePathForInstance(baseDir, defaultInstanceID)
	existingBinary := seedBinary(t, filepath.Join(installBinDir, xutil.ExecutableName("linux")), "old-binary")
	existingCodexHome := filepath.Join(baseDir, "existing-codex-home")
	if err := WriteState(statePath, InstallState{
		InstanceID:         defaultInstanceID,
		BaseDir:            baseDir,
		StatePath:          statePath,
		ServiceManager:     ServiceManagerSystemdUser,
		InstallSource:      InstallSourceRelease,
		CurrentTrack:       ReleaseTrackBeta,
		CurrentVersion:     "v1.4.0-beta.1",
		CurrentBinaryPath:  existingBinary,
		VersionsRoot:       filepath.Join(baseDir, "releases-cache"),
		CurrentSlot:        "v1.4.0-beta.1",
		VSCodeSettingsPath: filepath.Join(baseDir, "vscode", "settings.json"),
		BundleEntrypoint:   filepath.Join(baseDir, "bundle", "codex"),
		CodexHome:          existingCodexHome,
	}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	sourceBinary := seedBinary(t, filepath.Join(baseDir, "src", xutil.ExecutableName("linux")), "new-binary")

	originalValidator := sourceBinaryValidator
	sourceBinaryValidator = func(string) error { return nil }
	defer func() { sourceBinaryValidator = originalValidator }()

	if err := RunMain([]string{
		"-bootstrap-only",
		"-base-dir", baseDir,
		"-install-bin-dir", installBinDir,
		"-binary", sourceBinary,
	}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "vtest"); err != nil {
		t.Fatalf("RunMain bootstrap-only: %v", err)
	}

	updated, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if updated.ServiceManager != ServiceManagerSystemdUser {
		t.Fatalf("ServiceManager = %q, want %q", updated.ServiceManager, ServiceManagerSystemdUser)
	}
	if updated.InstallSource != InstallSourceRelease {
		t.Fatalf("InstallSource = %q, want %q", updated.InstallSource, InstallSourceRelease)
	}
	if updated.CurrentTrack != ReleaseTrackBeta {
		t.Fatalf("CurrentTrack = %q, want %q", updated.CurrentTrack, ReleaseTrackBeta)
	}
	// #808-C: layout facts (versionsRoot) are no longer persisted; they are
	// derived from the state file location at load time. currentSlot stays:
	// it identifies the active version slot and cannot be derived from the
	// state path alone.
	wantVersions := defaultVersionsRootForStatePath(statePath)
	if updated.VersionsRoot != wantVersions {
		t.Fatalf("VersionsRoot = %q, want derived %q", updated.VersionsRoot, wantVersions)
	}
	if updated.CurrentSlot != "v1.4.0-beta.1" {
		t.Fatalf("CurrentSlot = %q, want preserved value", updated.CurrentSlot)
	}
	if updated.VSCodeSettingsPath != filepath.Join(baseDir, "vscode", "settings.json") {
		t.Fatalf("VSCodeSettingsPath = %q, want preserved value", updated.VSCodeSettingsPath)
	}
	if updated.BundleEntrypoint != filepath.Join(baseDir, "bundle", "codex") {
		t.Fatalf("BundleEntrypoint = %q, want preserved value", updated.BundleEntrypoint)
	}
	if updated.CodexHome != existingCodexHome {
		t.Fatalf("CodexHome = %q, want preserved %q", updated.CodexHome, existingCodexHome)
	}
}

func TestRunMainDefaultsBinaryToCurrentExecutable(t *testing.T) {
	t.Setenv(repoRootEnvVar, t.TempDir())
	baseDir := t.TempDir()
	installBinDir := filepath.Join(baseDir, "installed-bin")
	selfBinary := filepath.Join(baseDir, "self", xutil.ExecutableName(runtime.GOOS))
	if err := os.MkdirAll(filepath.Dir(selfBinary), 0o755); err != nil {
		t.Fatalf("mkdir self binary dir: %v", err)
	}
	if err := os.WriteFile(selfBinary, []byte("self-binary"), 0o755); err != nil {
		t.Fatalf("write self binary: %v", err)
	}

	originalExec := executablePath
	executablePath = func() (string, error) { return selfBinary, nil }
	defer func() { executablePath = originalExec }()

	originalValidator := sourceBinaryValidator
	sourceBinaryValidator = func(string) error { return nil }
	defer func() { sourceBinaryValidator = originalValidator }()

	if err := RunMain([]string{
		"-bootstrap-only",
		"-base-dir", baseDir,
		"-install-bin-dir", installBinDir,
	}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "vtest"); err != nil {
		t.Fatalf("RunMain default binary source: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(installBinDir, xutil.ExecutableName(runtime.GOOS)))
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if string(raw) != "self-binary" {
		t.Fatalf("installed binary content = %q, want current executable content", string(raw))
	}
}

func TestRunMainRejectsUnrunnableBinarySource(t *testing.T) {
	t.Setenv(repoRootEnvVar, t.TempDir())
	baseDir := t.TempDir()
	binaryPath := filepath.Join(baseDir, "bin", xutil.ExecutableName(runtime.GOOS))
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		t.Fatalf("mkdir binary dir: %v", err)
	}
	if err := os.WriteFile(binaryPath, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	err := RunMain([]string{
		"-bootstrap-only",
		"-base-dir", baseDir,
		"-binary", binaryPath,
	}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "vtest")
	if err == nil || !strings.Contains(err.Error(), "validate binary source") {
		t.Fatalf("RunMain invalid binary error = %v, want validation failure", err)
	}
}

func TestRunMainUsesWorkspaceBindingBaseDirWhenFlagsOmitted(t *testing.T) {
	repoRoot := t.TempDir()
	boundBaseDir := t.TempDir()
	binaryPath := filepath.Join(repoRoot, "bin", "codex-remote")
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		t.Fatalf("mkdir binary dir: %v", err)
	}
	if err := os.WriteFile(binaryPath, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	if err := writeRepoInstallBinding(repoRoot, repoInstallBinding{
		InstanceID: "master",
		BaseDir:    boundBaseDir,
	}); err != nil {
		t.Fatalf("writeRepoInstallBinding: %v", err)
	}
	t.Setenv(repoRootEnvVar, repoRoot)

	originalValidator := sourceBinaryValidator
	sourceBinaryValidator = func(string) error { return nil }
	defer func() { sourceBinaryValidator = originalValidator }()

	if err := RunMain([]string{
		"-bootstrap-only",
		"-binary", binaryPath,
	}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "vtest"); err != nil {
		t.Fatalf("RunMain bootstrap-only: %v", err)
	}

	statePath := defaultInstallStatePathForInstance(boundBaseDir, "master")
	state, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.BaseDir != boundBaseDir {
		t.Fatalf("BaseDir = %q, want %q", state.BaseDir, boundBaseDir)
	}
	if state.InstanceID != "master" {
		t.Fatalf("InstanceID = %q, want master", state.InstanceID)
	}
}

func TestRunMainReusesExistingInstalledBinaryDirWhenInstallBinDirOmitted(t *testing.T) {
	repoRoot := t.TempDir()
	baseDir := t.TempDir()
	statePath := defaultInstallStatePathForInstance(baseDir, defaultInstanceID)
	customInstallDir := filepath.Join(baseDir, "systemd-dev", "bin")
	existingBinaryPath := seedBinary(t, filepath.Join(customInstallDir, xutil.ExecutableName(runtime.GOOS)), "old-binary")
	sourceBinaryPath := seedBinary(t, filepath.Join(repoRoot, "bin", xutil.ExecutableName(runtime.GOOS)), "new-binary")
	if err := writeRepoInstallBinding(repoRoot, repoInstallBinding{
		InstanceID: defaultInstanceID,
		BaseDir:    baseDir,
	}); err != nil {
		t.Fatalf("writeRepoInstallBinding: %v", err)
	}
	if err := WriteState(statePath, InstallState{
		InstanceID:        defaultInstanceID,
		BaseDir:           baseDir,
		StatePath:         statePath,
		CurrentBinaryPath: existingBinaryPath,
	}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	t.Setenv(repoRootEnvVar, repoRoot)

	originalValidator := sourceBinaryValidator
	sourceBinaryValidator = func(string) error { return nil }
	defer func() { sourceBinaryValidator = originalValidator }()

	if err := RunMain([]string{
		"-bootstrap-only",
		"-binary", sourceBinaryPath,
	}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "vtest"); err != nil {
		t.Fatalf("RunMain bootstrap-only: %v", err)
	}

	updated, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if updated.CurrentBinaryPath != existingBinaryPath {
		t.Fatalf("CurrentBinaryPath = %q, want %q", updated.CurrentBinaryPath, existingBinaryPath)
	}
	raw, err := os.ReadFile(existingBinaryPath)
	if err != nil {
		t.Fatalf("ReadFile(existing binary): %v", err)
	}
	if string(raw) != "new-binary" {
		t.Fatalf("installed binary content = %q, want new-binary", string(raw))
	}
}

func TestResolveTargetInstallBinDirMigratesVersionScopedPath(t *testing.T) {
	baseDir := t.TempDir()
	statePath := defaultInstallStatePathForInstance(baseDir, defaultInstanceID)
	// Use the derived versions root so LoadState can re-derive it from the state path.
	derivedVersionsRoot := defaultVersionsRootForStatePath(statePath)
	versionScopedBinary := seedBinary(t, filepath.Join(derivedVersionsRoot, "v1.8.4", xutil.ExecutableName(runtime.GOOS)), "old-binary")
	if err := WriteState(statePath, InstallState{
		InstanceID:        defaultInstanceID,
		BaseDir:           baseDir,
		StatePath:         statePath,
		CurrentBinaryPath: versionScopedBinary,
		VersionsRoot:      derivedVersionsRoot,
	}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	selection := installInstanceSelection{
		InstanceID:    defaultInstanceID,
		BaseDir:       baseDir,
		InstallBinDir: defaultInstallBinDirForInstance(runtime.GOOS, baseDir, defaultInstanceID),
		StatePath:     statePath,
	}
	got := resolveTargetInstallBinDir(selection, "")
	want := defaultInstallBinDirForInstance(runtime.GOOS, baseDir, defaultInstanceID)
	if got != want {
		t.Fatalf("resolveTargetInstallBinDir = %q, want %q (canonical bin dir)", got, want)
	}
}

func TestResolveTargetInstallBinDirPreservesCustomDir(t *testing.T) {
	baseDir := t.TempDir()
	customBinDir := filepath.Join(baseDir, "my-custom-bin")
	statePath := defaultInstallStatePathForInstance(baseDir, defaultInstanceID)
	existingBinary := seedBinary(t, filepath.Join(customBinDir, xutil.ExecutableName(runtime.GOOS)), "binary")
	if err := WriteState(statePath, InstallState{
		InstanceID:        defaultInstanceID,
		BaseDir:           baseDir,
		StatePath:         statePath,
		CurrentBinaryPath: existingBinary,
		VersionsRoot:      filepath.Join(baseDir, "releases"),
	}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	selection := installInstanceSelection{
		InstanceID:    defaultInstanceID,
		BaseDir:       baseDir,
		InstallBinDir: defaultInstallBinDirForInstance(runtime.GOOS, baseDir, defaultInstanceID),
		StatePath:     statePath,
	}
	got := resolveTargetInstallBinDir(selection, "")
	if got != customBinDir {
		t.Fatalf("resolveTargetInstallBinDir = %q, want %q (custom dir preserved)", got, customBinDir)
	}
}

func TestResolveTargetInstallBinDirExplicitValueOverridesMigration(t *testing.T) {
	baseDir := t.TempDir()
	versionsRoot := filepath.Join(baseDir, "releases")
	statePath := defaultInstallStatePathForInstance(baseDir, defaultInstanceID)
	versionScopedBinary := seedBinary(t, filepath.Join(versionsRoot, "v1.8.4", xutil.ExecutableName(runtime.GOOS)), "old-binary")
	if err := WriteState(statePath, InstallState{
		InstanceID:        defaultInstanceID,
		BaseDir:           baseDir,
		StatePath:         statePath,
		CurrentBinaryPath: versionScopedBinary,
		VersionsRoot:      versionsRoot,
	}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	explicitDir := filepath.Join(baseDir, "explicit-bin")
	selection := installInstanceSelection{
		InstanceID:    defaultInstanceID,
		BaseDir:       baseDir,
		InstallBinDir: defaultInstallBinDirForInstance(runtime.GOOS, baseDir, defaultInstanceID),
		StatePath:     statePath,
	}
	got := resolveTargetInstallBinDir(selection, explicitDir)
	if got != explicitDir {
		t.Fatalf("resolveTargetInstallBinDir = %q, want %q (explicit value overrides)", got, explicitDir)
	}
}
