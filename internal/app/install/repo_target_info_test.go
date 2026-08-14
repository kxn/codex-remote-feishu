package install

import (
	"github.com/kxn/codex-remote-feishu/internal/xutil"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

func TestResolveRepoInstallTargetInfoUsesBindingAndConfig(t *testing.T) {
	repoRoot := t.TempDir()
	baseDir := t.TempDir()
	stubServiceUserHome(t, baseDir)
	t.Setenv(repoRootEnvVar, repoRoot)
	if err := writeRepoInstallBinding(repoRoot, repoInstallBinding{
		InstanceID: "master",
		BaseDir:    baseDir,
	}); err != nil {
		t.Fatalf("writeRepoInstallBinding() error = %v", err)
	}

	cfg := config.DefaultAppConfig()
	cfg.Relay.ListenHost = "127.0.0.1"
	cfg.Relay.ListenPort = 9700
	cfg.Relay.ServerURL = "ws://127.0.0.1:9700/ws/agent"
	cfg.Admin.ListenPort = 9701
	cfg.Tool.ListenPort = 9702
	cfg.ExternalAccess.ListenPort = 9712
	cfg.Debug.Pprof = &config.PprofSettings{
		Enabled:    true,
		ListenHost: "127.0.0.1",
		ListenPort: 17601,
	}
	configPath := defaultConfigPathForInstance(baseDir, "master")
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig() error = %v", err)
	}

	statePath := defaultInstallStatePathForInstance(baseDir, "master")
	if err := WriteState(statePath, InstallState{
		InstanceID:        "master",
		BaseDir:           baseDir,
		ConfigPath:        configPath,
		StatePath:         statePath,
		CurrentVersion:    "v1.5.0-alpha4",
		CurrentBinaryPath: filepath.Join(baseDir, "bin", "codex-remote"),
	}); err != nil {
		t.Fatalf("WriteState() error = %v", err)
	}

	info, err := ResolveRepoInstallTargetInfo(RepoInstallTargetOptions{
		FallbackBaseDir: t.TempDir(),
		GOOS:            "linux",
		RequireBinding:  true,
	})
	if err != nil {
		t.Fatalf("ResolveRepoInstallTargetInfo() error = %v", err)
	}
	if info.InstanceID != "master" {
		t.Fatalf("InstanceID = %q, want master", info.InstanceID)
	}
	if info.BindingSource != string(repoInstallBindingSourceFile) {
		t.Fatalf("BindingSource = %q, want %q", info.BindingSource, repoInstallBindingSourceFile)
	}
	if info.StatePath != statePath {
		t.Fatalf("StatePath = %q, want %q", info.StatePath, statePath)
	}
	if info.LocalUpgradeArtifactPath != filepath.Join(baseDir, ".local", "share", "codex-remote-master", "codex-remote", "local-upgrade", xutil.ExecutableName(runtime.GOOS)) {
		t.Fatalf("LocalUpgradeArtifactPath = %q", info.LocalUpgradeArtifactPath)
	}
	if info.LogPath != filepath.Join(baseDir, ".local", "share", "codex-remote-master", "codex-remote", "logs", "codex-remote-relayd.log") {
		t.Fatalf("LogPath = %q", info.LogPath)
	}
	if info.Admin.URL != "http://127.0.0.1:9701" {
		t.Fatalf("Admin.URL = %q", info.Admin.URL)
	}
	if info.Relay.ServerURL != "ws://127.0.0.1:9700/ws/agent" {
		t.Fatalf("Relay.ServerURL = %q", info.Relay.ServerURL)
	}
	if !info.Pprof.Enabled || info.Pprof.URL != "http://127.0.0.1:17601/debug/pprof/" {
		t.Fatalf("Pprof = %#v", info.Pprof)
	}
	if info.CurrentVersion != "v1.5.0-alpha4" {
		t.Fatalf("CurrentVersion = %q", info.CurrentVersion)
	}
}

func TestResolveRepoInstallTargetInfoRejectsUnboundRepo(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv(repoRootEnvVar, repoRoot)

	_, err := ResolveRepoInstallTargetInfo(RepoInstallTargetOptions{
		FallbackBaseDir: t.TempDir(),
		GOOS:            "linux",
		RequireBinding:  true,
	})
	if err == nil || !strings.Contains(err.Error(), "not bound") {
		t.Fatalf("ResolveRepoInstallTargetInfo() error = %v, want not bound", err)
	}
}
