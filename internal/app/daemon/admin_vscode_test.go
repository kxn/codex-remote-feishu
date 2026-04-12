package daemon

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/editor"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestVSCodeDetectApplyAndReinstallManagedShim(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("VSCODE_SERVER_EXTENSIONS_DIR", filepath.Join(home, ".vscode-server", "extensions"))

	binaryPath := filepath.Join(home, "bin", "codex-remote")
	writeExecutableFile(t, binaryPath, "wrapper-binary")

	entrypointV1 := filepath.Join(home, ".vscode-server", "extensions", "openai.chatgpt-1", "bin", "linux-x86_64", "codex")
	windowsSibling := filepath.Join(home, ".vscode-server", "extensions", "openai.chatgpt-1", "bin", "windows-x86_64", "codex.exe")
	writeExecutableFile(t, entrypointV1, "orig-v1")
	writeExecutableFile(t, windowsSibling, "orig-win-v1")

	app, configPath, installStatePath := newVSCodeAdminTestApp(t, home, binaryPath, true)

	rec := performAdminRequest(t, app, http.MethodGet, "/api/admin/vscode/detect", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("detect status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var detect vscodeDetectResponse
	if err := json.NewDecoder(rec.Body).Decode(&detect); err != nil {
		t.Fatalf("decode detect: %v", err)
	}
	if detect.RecommendedMode != "managed_shim" {
		t.Fatalf("recommended mode = %q, want managed_shim", detect.RecommendedMode)
	}
	if detect.LatestBundleEntrypoint != entrypointV1 {
		t.Fatalf("latest bundle entrypoint = %q, want %q", detect.LatestBundleEntrypoint, entrypointV1)
	}

	rec = performAdminRequest(t, app, http.MethodPost, "/api/admin/vscode/apply", `{"mode":"managed_shim"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(entrypointV1 + ".real"); err != nil {
		t.Fatalf("expected .real backup after shim install: %v", err)
	}
	if readFileString(t, entrypointV1) == "wrapper-binary" {
		t.Fatalf("expected entrypoint to be tiny shim, not copied main binary")
	}

	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if loaded.Config.Wrapper.IntegrationMode != "managed_shim" {
		t.Fatalf("wrapper integration mode = %q, want managed_shim", loaded.Config.Wrapper.IntegrationMode)
	}
	if loaded.Config.Wrapper.CodexRealBinary != "codex" {
		t.Fatalf("expected shared codex path to stay unchanged, got %q", loaded.Config.Wrapper.CodexRealBinary)
	}

	entrypointV2 := filepath.Join(home, ".vscode-server", "extensions", "openai.chatgpt-2", "bin", "linux-x86_64", "codex")
	windowsSiblingV2 := filepath.Join(home, ".vscode-server", "extensions", "openai.chatgpt-2", "bin", "windows-x86_64", "codex.exe")
	writeExecutableFile(t, entrypointV2, "orig-v2")
	writeExecutableFile(t, windowsSiblingV2, "orig-win-v2")
	now := time.Now().Add(time.Minute)
	if err := os.Chtimes(filepath.Dir(filepath.Dir(filepath.Dir(entrypointV2))), now, now); err != nil {
		t.Fatalf("Chtimes(new extension dir): %v", err)
	}

	rec = performAdminRequest(t, app, http.MethodGet, "/api/admin/vscode/detect", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("detect after upgrade status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&detect); err != nil {
		t.Fatalf("decode detect after upgrade: %v", err)
	}
	if detect.LatestBundleEntrypoint != entrypointV2 {
		t.Fatalf("latest bundle entrypoint after upgrade = %q, want %q", detect.LatestBundleEntrypoint, entrypointV2)
	}
	if !detect.NeedsShimReinstall {
		t.Fatalf("expected shim reinstall to be required after extension upgrade, got %#v", detect)
	}

	rec = performAdminRequest(t, app, http.MethodPost, "/api/admin/vscode/apply", `{"mode":"managed_shim"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("re-apply status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(entrypointV2 + ".real"); err != nil {
		t.Fatalf("expected .real backup on latest entrypoint: %v", err)
	}
	if readFileString(t, entrypointV2) == "wrapper-binary" {
		t.Fatalf("expected latest entrypoint to be tiny shim, not copied main binary")
	}
	if err := json.NewDecoder(rec.Body).Decode(&detect); err != nil {
		t.Fatalf("decode re-apply detect: %v", err)
	}
	if detect.NeedsShimReinstall {
		t.Fatalf("did not expect reinstall flag after re-apply, got %#v", detect)
	}

	loaded, err = config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath after re-apply: %v", err)
	}
	if loaded.Config.Wrapper.CodexRealBinary != "codex" {
		t.Fatalf("expected shared codex path to remain unchanged, got %q", loaded.Config.Wrapper.CodexRealBinary)
	}

	rec = performAdminRequest(t, app, http.MethodPost, "/api/admin/vscode/reinstall-shim", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reinstall status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	rawState, err := os.ReadFile(installStatePath)
	if err != nil {
		t.Fatalf("read install-state: %v", err)
	}
	if !strings.Contains(string(rawState), entrypointV2) {
		t.Fatalf("expected install-state to record latest bundle entrypoint, got %s", string(rawState))
	}
	if _, err := os.Stat(editor.ManagedShimRealBinaryPath(windowsSiblingV2)); !os.IsNotExist(err) {
		t.Fatalf("expected non-current-platform sibling to stay untouched, stat err=%v", err)
	}
}

func TestVSCodeDetectAndApplyManagedShimUseWindowsEntrypoint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	binaryPath := filepath.Join(home, "bin", "codex-remote.exe")
	writeExecutableFile(t, binaryPath, "wrapper-binary")

	linuxEntrypoint := filepath.Join(home, ".vscode", "extensions", "openai.chatgpt-1", "bin", "linux-x86_64", "codex")
	windowsEntrypoint := filepath.Join(home, ".vscode", "extensions", "openai.chatgpt-1", "bin", "windows-x86_64", "codex.exe")
	writeExecutableFile(t, linuxEntrypoint, "wrapper-binary")
	writeExecutableFile(t, linuxEntrypoint+".real", "orig-linux")
	writeExecutableFile(t, windowsEntrypoint, "orig-windows")

	app, configPath, installStatePath := newVSCodeAdminTestApp(t, home, binaryPath, true)
	settingsPath := filepath.Join(home, "AppData", "Roaming", "Code", "User", "settings.json")
	app.detectPlatformDefaults = func() (install.PlatformDefaults, error) {
		return install.PlatformDefaults{
			GOOS:                       "windows",
			HomeDir:                    home,
			BaseDir:                    home,
			VSCodeSettingsPath:         settingsPath,
			CandidateBundleEntrypoints: []string{windowsEntrypoint},
			DefaultIntegrations:        install.DefaultIntegrations("windows"),
		}, nil
	}
	if err := install.WriteState(installStatePath, install.InstallState{
		StatePath:              installStatePath,
		VSCodeSettingsPath:     settingsPath,
		BundleEntrypoint:       linuxEntrypoint,
		Integrations:           []install.WrapperIntegrationMode{install.IntegrationManagedShim},
		InstalledBinary:        binaryPath,
		CurrentBinaryPath:      binaryPath,
		InstalledWrapperBinary: binaryPath,
	}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	rec := performAdminRequest(t, app, http.MethodGet, "/api/admin/vscode/detect", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("detect status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var detect vscodeDetectResponse
	if err := json.NewDecoder(rec.Body).Decode(&detect); err != nil {
		t.Fatalf("decode detect: %v", err)
	}
	if detect.LatestBundleEntrypoint != windowsEntrypoint {
		t.Fatalf("latest bundle entrypoint = %q, want %q", detect.LatestBundleEntrypoint, windowsEntrypoint)
	}
	if detect.RecordedBundleEntrypoint != linuxEntrypoint {
		t.Fatalf("recorded bundle entrypoint = %q, want %q", detect.RecordedBundleEntrypoint, linuxEntrypoint)
	}
	if !detect.NeedsShimReinstall {
		t.Fatalf("expected detect to require reinstall when recorded linux entrypoint differs, got %#v", detect)
	}

	rec = performAdminRequest(t, app, http.MethodPost, "/api/admin/vscode/apply", `{"mode":"managed_shim"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	windowsRealBinary := editor.ManagedShimRealBinaryPath(windowsEntrypoint)
	if _, err := os.Stat(windowsRealBinary); err != nil {
		t.Fatalf("expected .real.exe backup after windows shim apply: %v", err)
	}
	if readFileString(t, windowsEntrypoint) != "wrapper-binary" {
		t.Fatalf("expected windows entrypoint to match wrapper binary")
	}
	if readFileString(t, linuxEntrypoint) != "wrapper-binary" {
		t.Fatalf("expected stale linux entrypoint to remain unchanged")
	}

	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if loaded.Config.Wrapper.CodexRealBinary != windowsRealBinary {
		t.Fatalf("expected codexRealBinary to point at windows managed shim backup, got %q", loaded.Config.Wrapper.CodexRealBinary)
	}

	state, err := install.LoadState(installStatePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.BundleEntrypoint != windowsEntrypoint {
		t.Fatalf("expected install-state to move to windows entrypoint, got %q", state.BundleEntrypoint)
	}
}

func TestVSCodeApplyEditorSettingsRejected(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	binaryPath := filepath.Join(home, "bin", "codex-remote")
	writeExecutableFile(t, binaryPath, "wrapper-binary")

	app, _, _ := newVSCodeAdminTestApp(t, home, binaryPath, false)

	rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/vscode/apply", `{"mode":"editor_settings"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("apply status = %d, want 400 body=%s", rec.Code, rec.Body.String())
	}
}

func TestVSCodeDetectRecommendsManagedShimOutsideSSH(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	binaryPath := filepath.Join(home, "bin", "codex-remote")
	writeExecutableFile(t, binaryPath, "wrapper-binary")

	app, _, _ := newVSCodeAdminTestApp(t, home, binaryPath, false)

	rec := performAdminRequest(t, app, http.MethodGet, "/api/admin/vscode/detect", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("detect status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var detect vscodeDetectResponse
	if err := json.NewDecoder(rec.Body).Decode(&detect); err != nil {
		t.Fatalf("decode detect: %v", err)
	}
	if detect.RecommendedMode != "managed_shim" {
		t.Fatalf("recommended mode = %q, want managed_shim", detect.RecommendedMode)
	}
}

func TestVSCodeApplyAllAliasesBoth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("VSCODE_SERVER_EXTENSIONS_DIR", filepath.Join(home, ".vscode-server", "extensions"))
	binaryPath := filepath.Join(home, "bin", "codex-remote")
	writeExecutableFile(t, binaryPath, "wrapper-binary")

	entrypoint := filepath.Join(home, ".vscode-server", "extensions", "openai.chatgpt-1", "bin", "linux-x86_64", "codex")
	writeExecutableFile(t, entrypoint, "orig")

	app, configPath, _ := newVSCodeAdminTestApp(t, home, binaryPath, false)

	rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/vscode/apply", `{"mode":"all"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply all status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if loaded.Config.Wrapper.IntegrationMode != "managed_shim" {
		t.Fatalf("wrapper integration mode = %q, want managed_shim", loaded.Config.Wrapper.IntegrationMode)
	}
}

func TestVSCodeDetectSupportsJSONCSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	binaryPath := filepath.Join(home, "bin", "codex-remote")
	writeExecutableFile(t, binaryPath, "wrapper-binary")

	defaults, err := install.DetectPlatformDefaults()
	if err != nil {
		t.Fatalf("DetectPlatformDefaults: %v", err)
	}
	settingsPath := defaults.VSCodeSettingsPath
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(settings dir): %v", err)
	}
	rawSettings := "{\n  // existing vscode config\n  \"chatgpt.cliExecutable\": \"" + binaryPath + "\",\n}\n"
	if err := os.WriteFile(settingsPath, []byte(rawSettings), 0o644); err != nil {
		t.Fatalf("WriteFile(settings): %v", err)
	}

	app, _, _ := newVSCodeAdminTestApp(t, home, binaryPath, false)

	rec := performAdminRequest(t, app, http.MethodGet, "/api/admin/vscode/detect", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("detect status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var detect vscodeDetectResponse
	if err := json.NewDecoder(rec.Body).Decode(&detect); err != nil {
		t.Fatalf("decode detect: %v", err)
	}
	if detect.Settings.CLIExecutable != binaryPath {
		t.Fatalf("settings cli executable = %q, want %q", detect.Settings.CLIExecutable, binaryPath)
	}
	if !detect.Settings.MatchesBinary {
		t.Fatalf("expected settings to match current binary, got %#v", detect.Settings)
	}
}

func TestVSCodeDetectAndReinstallMigrateRecordedHistoricalManagedShim(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("VSCODE_SERVER_EXTENSIONS_DIR", filepath.Join(home, ".vscode-server", "extensions"))

	binaryPath := filepath.Join(home, "bin", "codex-remote")
	writeExecutableFile(t, binaryPath, "wrapper-binary")

	entrypointV1 := filepath.Join(home, ".vscode-server", "extensions", "openai.chatgpt-1", "bin", "linux-x86_64", "codex")
	writeExecutableFile(t, entrypointV1, "orig-v1")

	app, configPath, installStatePath := newVSCodeAdminTestApp(t, home, binaryPath, true)

	rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/vscode/apply", `{"mode":"managed_shim"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply v1 status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	entrypointV2 := filepath.Join(home, ".vscode-server", "extensions", "openai.chatgpt-2", "bin", "linux-x86_64", "codex")
	writeExecutableFile(t, entrypointV2, "orig-v2")
	now := time.Now().Add(time.Minute)
	if err := os.Chtimes(filepath.Dir(filepath.Dir(filepath.Dir(entrypointV2))), now, now); err != nil {
		t.Fatalf("Chtimes(v2 extension dir): %v", err)
	}
	if err := editor.PatchBundleEntrypoint(editor.PatchBundleEntrypointOptions{
		EntrypointPath:   entrypointV2,
		InstallStatePath: installStatePath,
		ConfigPath:       configPath,
		InstanceID:       "stable",
	}); err != nil {
		t.Fatalf("PatchBundleEntrypoint(v2): %v", err)
	}

	if err := os.Remove(editor.ManagedShimSidecarPath(entrypointV1)); err != nil {
		t.Fatalf("remove v1 sidecar: %v", err)
	}
	writeExecutableFile(t, entrypointV1, "wrapper-binary")

	rec = performAdminRequest(t, app, http.MethodGet, "/api/admin/vscode/detect", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("detect status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var detect vscodeDetectResponse
	if err := json.NewDecoder(rec.Body).Decode(&detect); err != nil {
		t.Fatalf("decode detect: %v", err)
	}
	if detect.LatestBundleEntrypoint != entrypointV2 {
		t.Fatalf("latest bundle entrypoint = %q, want %q", detect.LatestBundleEntrypoint, entrypointV2)
	}
	if !detect.NeedsShimReinstall {
		t.Fatalf("expected historical recorded legacy shim to require reinstall, got %#v", detect)
	}

	rec = performAdminRequest(t, app, http.MethodPost, "/api/admin/vscode/reinstall-shim", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reinstall status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	statusV1, err := editor.DetectManagedShim(entrypointV1, binaryPath)
	if err != nil {
		t.Fatalf("DetectManagedShim(v1): %v", err)
	}
	if statusV1.Kind != editor.ManagedShimKindTiny || !statusV1.SidecarValid || !statusV1.MatchesBinary {
		t.Fatalf("expected recorded historical shim to migrate back to tiny shim, got %#v", statusV1)
	}
}

func newVSCodeAdminTestApp(t *testing.T, home, binaryPath string, sshSession bool) (*App, string, string) {
	t.Helper()
	return newVSCodeAdminTestAppWithGateway(t, &recordingGateway{}, home, binaryPath, sshSession)
}

func newVSCodeAdminTestAppWithGateway(t *testing.T, gateway feishu.Gateway, home, binaryPath string, sshSession bool) (*App, string, string) {
	t.Helper()

	cfg := config.DefaultAppConfig()
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	dataDir := filepath.Join(home, ".local", "share", "codex-remote")
	installStatePath := filepath.Join(dataDir, "install-state.json")

	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		BinaryIdentity: agentproto.BinaryIdentity{Version: "dev"},
	})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: binaryPath,
		Paths: relayruntime.Paths{
			DataDir:  dataDir,
			StateDir: filepath.Join(home, ".local", "state", "codex-remote"),
		},
	})
	app.ConfigureAdmin(AdminRuntimeOptions{
		ConfigPath:      configPath,
		Services:        defaultFeishuServices(),
		AdminListenHost: "127.0.0.1",
		AdminListenPort: "9501",
		AdminURL:        "http://localhost:9501/",
		SetupURL:        "http://localhost:9501/setup",
		SSHSession:      sshSession,
	})
	return app, configPath, installStatePath
}

func writeExecutableFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(raw)
}
