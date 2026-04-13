package install

import (
	"bytes"
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

func TestRunMainDefaultsBinaryToCurrentExecutable(t *testing.T) {
	t.Setenv(repoRootEnvVar, t.TempDir())
	baseDir := t.TempDir()
	installBinDir := filepath.Join(baseDir, "installed-bin")
	selfBinary := filepath.Join(baseDir, "self", executableName(runtime.GOOS))
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

	raw, err := os.ReadFile(filepath.Join(installBinDir, executableName(runtime.GOOS)))
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
	binaryPath := filepath.Join(baseDir, "bin", executableName(runtime.GOOS))
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
	existingBinaryPath := seedBinary(t, filepath.Join(customInstallDir, executableName(runtime.GOOS)), "old-binary")
	sourceBinaryPath := seedBinary(t, filepath.Join(repoRoot, "bin", executableName(runtime.GOOS)), "new-binary")
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
		InstalledBinary:   existingBinaryPath,
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
	if updated.InstalledBinary != existingBinaryPath {
		t.Fatalf("InstalledBinary = %q, want %q", updated.InstalledBinary, existingBinaryPath)
	}
	raw, err := os.ReadFile(existingBinaryPath)
	if err != nil {
		t.Fatalf("ReadFile(existing binary): %v", err)
	}
	if string(raw) != "new-binary" {
		t.Fatalf("installed binary content = %q, want new-binary", string(raw))
	}
}
