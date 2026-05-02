package install

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

func TestResolveCurrentDaemonTargetInfoDefaultInstance(t *testing.T) {
	baseDir := t.TempDir()
	originalHome := serviceUserHomeDir
	serviceUserHomeDir = func() (string, error) { return baseDir, nil }
	defer func() { serviceUserHomeDir = originalHome }()

	configPath := defaultConfigPathForInstance(baseDir, defaultInstanceID)
	statePath := defaultInstallStatePathForInstance(baseDir, defaultInstanceID)
	cfg := config.DefaultAppConfig()
	cfg.Admin.ListenHost = "127.0.0.1"
	cfg.Admin.ListenPort = 9511
	cfg.Tool.ListenHost = "127.0.0.1"
	cfg.Tool.ListenPort = 9512
	cfg.ExternalAccess.ListenHost = "127.0.0.1"
	cfg.ExternalAccess.ListenPort = 9522
	cfg.Relay.ListenHost = "127.0.0.1"
	cfg.Relay.ListenPort = 9510
	cfg.Relay.ServerURL = "ws://127.0.0.1:9510/ws/agent"
	if cfg.Debug.Pprof == nil {
		cfg.Debug.Pprof = &config.PprofSettings{}
	}
	cfg.Debug.Pprof.Enabled = true
	cfg.Debug.Pprof.ListenHost = "127.0.0.1"
	cfg.Debug.Pprof.ListenPort = 17511
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	state := InstallState{
		InstanceID:        defaultInstanceID,
		BaseDir:           baseDir,
		ConfigPath:        configPath,
		StatePath:         statePath,
		CurrentVersion:    "vtest",
		CurrentBinaryPath: filepath.Join(baseDir, "bin", "codex-remote"),
		InstalledBinary:   filepath.Join(baseDir, "bin", "codex-remote"),
		VersionsRoot:      filepath.Join(baseDir, "versions"),
		ServiceManager:    ServiceManagerSystemdUser,
	}
	if err := WriteState(statePath, state); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	t.Setenv(currentDaemonConfigEnv, configPath)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(baseDir, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(baseDir, ".local", "share"))
	t.Setenv(currentDaemonRuntimeIDEnv, "inst-headless-self")
	t.Setenv(currentDaemonRuntimeSourceEnv, "headless")
	t.Setenv(currentDaemonRuntimeManagedEnv, "1")
	t.Setenv(currentDaemonRuntimeLifeEnv, "daemon-owned")

	info, err := ResolveCurrentDaemonTargetInfo()
	if err != nil {
		t.Fatalf("ResolveCurrentDaemonTargetInfo: %v", err)
	}
	if info.InstanceID != defaultInstanceID {
		t.Fatalf("InstanceID = %q, want %q", info.InstanceID, defaultInstanceID)
	}
	if info.RuntimeInstanceID != "inst-headless-self" || info.RuntimeInstanceSource != "headless" || !info.RuntimeManaged || info.RuntimeLifetime != "daemon-owned" {
		t.Fatalf("unexpected runtime metadata: %#v", info)
	}
	if info.StatePath != statePath || info.ConfigPath != configPath || !info.StateExists || !info.ConfigExists {
		t.Fatalf("unexpected state/config paths: %#v", info)
	}
	if info.Admin.URL != "http://127.0.0.1:9511" || info.Tool.URL != "http://127.0.0.1:9512" || info.ExternalAccess.URL != "http://127.0.0.1:9522" {
		t.Fatalf("unexpected endpoint urls: %#v", info)
	}
	if info.LocalUpgradeArtifact != filepath.Join(filepath.Dir(statePath), "local-upgrade", executableName(runtime.GOOS)) {
		t.Fatalf("LocalUpgradeArtifact = %q", info.LocalUpgradeArtifact)
	}
	wantServiceUnitPath := filepath.ToSlash(serviceUnitPathForInstallInstance(serviceRuntimeGOOS, baseDir, defaultInstanceID))
	if filepath.ToSlash(info.ServiceUnitPath) != wantServiceUnitPath {
		t.Fatalf("unexpected service unit path: %q", info.ServiceUnitPath)
	}
}

func TestResolveCurrentDaemonTargetInfoNamedInstance(t *testing.T) {
	baseDir := t.TempDir()
	instanceID := "beta"
	originalHome := serviceUserHomeDir
	serviceUserHomeDir = func() (string, error) { return baseDir, nil }
	defer func() { serviceUserHomeDir = originalHome }()

	configPath := defaultConfigPathForInstance(baseDir, instanceID)
	statePath := defaultInstallStatePathForInstance(baseDir, instanceID)
	cfg := config.DefaultAppConfig()
	cfg.Admin.ListenHost = "127.0.0.1"
	cfg.Admin.ListenPort = 9611
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	state := InstallState{
		InstanceID:      instanceID,
		BaseDir:         baseDir,
		ConfigPath:      configPath,
		StatePath:       statePath,
		VersionsRoot:    filepath.Join(baseDir, "versions-beta"),
		ServiceManager:  ServiceManagerSystemdUser,
		CurrentVersion:  "beta-v1",
		InstalledBinary: filepath.Join(baseDir, "bin", "codex-remote"),
	}
	if err := WriteState(statePath, state); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	t.Setenv(currentDaemonConfigEnv, configPath)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(baseDir, ".config", instanceNamespace(instanceID)))
	t.Setenv("XDG_DATA_HOME", filepath.Join(baseDir, ".local", "share", instanceNamespace(instanceID)))
	t.Setenv(currentDaemonRuntimeIDEnv, "inst-visible-beta")
	t.Setenv(currentDaemonRuntimeSourceEnv, "normal")
	t.Setenv(currentDaemonRuntimeManagedEnv, "")
	t.Setenv(currentDaemonRuntimeLifeEnv, "")

	info, err := ResolveCurrentDaemonTargetInfo()
	if err != nil {
		t.Fatalf("ResolveCurrentDaemonTargetInfo: %v", err)
	}
	if info.InstanceID != instanceID {
		t.Fatalf("InstanceID = %q, want %q", info.InstanceID, instanceID)
	}
	if info.StatePath != statePath || info.ConfigPath != configPath {
		t.Fatalf("unexpected state/config paths: %#v", info)
	}
	if info.LocalUpgradeArtifact != filepath.Join(filepath.Dir(statePath), "local-upgrade", executableName(runtime.GOOS)) {
		t.Fatalf("LocalUpgradeArtifact = %q", info.LocalUpgradeArtifact)
	}
	wantServiceUnitPath := filepath.ToSlash(serviceUnitPathForInstallInstance(serviceRuntimeGOOS, baseDir, instanceID))
	if filepath.ToSlash(info.ServiceUnitPath) != wantServiceUnitPath {
		t.Fatalf("unexpected service unit path: %q", info.ServiceUnitPath)
	}
}

func TestResolveCurrentDaemonTargetInfoRequiresRuntimeEnv(t *testing.T) {
	t.Setenv(currentDaemonConfigEnv, "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")

	_, err := ResolveCurrentDaemonTargetInfo()
	if err == nil || !strings.Contains(err.Error(), "current daemon target requires CODEX_REMOTE_CONFIG or XDG runtime env") {
		t.Fatalf("ResolveCurrentDaemonTargetInfo() error = %v", err)
	}
}
