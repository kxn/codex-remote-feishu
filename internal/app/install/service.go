package install

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/editor"
	"github.com/kxn/codex-remote-feishu/internal/config"
)

type Options struct {
	BaseDir            string
	InstallBinDir      string
	BinaryPath         string
	WrapperBinary      string
	RelaydBinary       string
	RelayServerURL     string
	CodexRealBinary    string
	IntegrationMode    WrapperIntegrationMode
	Integrations       []WrapperIntegrationMode
	VSCodeSettingsPath string
	BundleEntrypoint   string
	FeishuAppID        string
	FeishuAppSecret    string
	UseSystemProxy     bool
	BootstrapOnly      bool
}

type InstallState struct {
	ConfigPath             string                   `json:"configPath,omitempty"`
	WrapperConfigPath      string                   `json:"wrapperConfigPath"`
	ServicesConfigPath     string                   `json:"servicesConfigPath"`
	StatePath              string                   `json:"statePath"`
	InstalledBinary        string                   `json:"installedBinary,omitempty"`
	InstalledWrapperBinary string                   `json:"installedWrapperBinary,omitempty"`
	InstalledRelaydBinary  string                   `json:"installedRelaydBinary,omitempty"`
	Integrations           []WrapperIntegrationMode `json:"integrations"`
	VSCodeSettingsPath     string                   `json:"vscodeSettingsPath,omitempty"`
	BundleEntrypoint       string                   `json:"bundleEntrypoint,omitempty"`
}

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) Bootstrap(opts Options) (InstallState, error) {
	configDir := filepath.Join(opts.BaseDir, ".config", "codex-remote")
	stateDir := filepath.Join(opts.BaseDir, ".local", "share", "codex-remote")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return InstallState{}, err
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return InstallState{}, err
	}
	sourceBinary, err := resolveBinaryPath(opts)
	if err != nil {
		return InstallState{}, err
	}
	installedBinary, err := installBinary(sourceBinary, opts.InstallBinDir)
	if err != nil {
		return InstallState{}, err
	}

	integrations := opts.Integrations
	if !opts.BootstrapOnly && len(integrations) == 0 && opts.IntegrationMode != "" {
		integrations = []WrapperIntegrationMode{opts.IntegrationMode}
	}
	integrations = normalizeIntegrations(integrations)

	configPath := filepath.Join(configDir, "config.json")
	legacyConfigPath := filepath.Join(configDir, "config.env")
	legacyWrapperConfigPath := filepath.Join(configDir, "wrapper.env")
	legacyServicesConfigPath := filepath.Join(configDir, "services.env")
	statePath := filepath.Join(stateDir, "install-state.json")
	existing, err := config.LoadAppConfigAtPath(configPath, legacyConfigPath, legacyWrapperConfigPath, legacyServicesConfigPath)
	if err != nil {
		return InstallState{}, err
	}
	cfg := existing.Config

	codexRealBinary := opts.CodexRealBinary
	if codexRealBinary == "" && hasIntegration(integrations, IntegrationManagedShim) && opts.BundleEntrypoint != "" {
		codexRealBinary = editor.ManagedShimRealBinaryPath(opts.BundleEntrypoint)
	}
	if installedBinary == "" {
		installedBinary = sourceBinary
	}

	emptyIntegrationMode := string(IntegrationEditorSettings)
	if opts.BootstrapOnly {
		emptyIntegrationMode = "none"
	}

	cfg.Relay.ServerURL = firstNonEmpty(opts.RelayServerURL, cfg.Relay.ServerURL)
	cfg.Wrapper.CodexRealBinary = choosePreservedValue(codexRealBinary, cfg.Wrapper.CodexRealBinary)
	cfg.Wrapper.NameMode = firstNonEmpty(cfg.Wrapper.NameMode, "workspace_basename")
	cfg.Wrapper.IntegrationMode = integrationsConfigValueOr(integrations, emptyIntegrationMode)
	cfg.Feishu.UseSystemProxy = opts.UseSystemProxy
	cfg.Feishu.Apps = mergePrimaryFeishuApp(cfg.Feishu.Apps, opts.FeishuAppID, opts.FeishuAppSecret)

	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		return InstallState{}, err
	}

	if hasIntegration(integrations, IntegrationEditorSettings) && opts.VSCodeSettingsPath != "" {
		if err := editor.PatchVSCodeSettings(opts.VSCodeSettingsPath, installedBinary); err != nil {
			return InstallState{}, err
		}
	}
	if hasIntegration(integrations, IntegrationManagedShim) {
		if err := editor.PatchBundleEntrypoint(opts.BundleEntrypoint, installedBinary); err != nil {
			return InstallState{}, err
		}
	}

	state := InstallState{
		ConfigPath:             configPath,
		WrapperConfigPath:      configPath,
		ServicesConfigPath:     configPath,
		StatePath:              statePath,
		InstalledBinary:        installedBinary,
		InstalledWrapperBinary: installedBinary,
		InstalledRelaydBinary:  installedBinary,
		Integrations:           integrations,
		VSCodeSettingsPath:     opts.VSCodeSettingsPath,
		BundleEntrypoint:       opts.BundleEntrypoint,
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return InstallState{}, err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(statePath, raw, 0o644); err != nil {
		return InstallState{}, err
	}

	return state, nil
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func choosePreservedValue(incoming, existing string) string {
	if strings.TrimSpace(incoming) != "" {
		return incoming
	}
	return existing
}

func installBinary(sourcePath, installDir string) (string, error) {
	if strings.TrimSpace(sourcePath) == "" {
		return "", nil
	}
	if strings.TrimSpace(installDir) == "" {
		return sourcePath, nil
	}
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return "", err
	}
	targetPath := filepath.Join(installDir, filepath.Base(sourcePath))
	if samePath(sourcePath, targetPath) {
		return targetPath, nil
	}
	if err := copyFile(sourcePath, targetPath); err != nil {
		return "", err
	}
	return targetPath, nil
}

func samePath(left, right string) bool {
	return filepath.Clean(left) == filepath.Clean(right)
}

func copyFile(sourcePath, targetPath string) error {
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	info, err := sourceFile.Stat()
	if err != nil {
		return err
	}
	targetFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer targetFile.Close()

	if _, err := io.Copy(targetFile, sourceFile); err != nil {
		return err
	}
	return targetFile.Chmod(info.Mode().Perm())
}

func resolveBinaryPath(opts Options) (string, error) {
	binaryPath := strings.TrimSpace(opts.BinaryPath)
	wrapperBinary := strings.TrimSpace(opts.WrapperBinary)
	relaydBinary := strings.TrimSpace(opts.RelaydBinary)

	if binaryPath != "" {
		return binaryPath, nil
	}
	if wrapperBinary != "" && relaydBinary != "" && !samePath(wrapperBinary, relaydBinary) {
		return "", fmt.Errorf("single-binary install requires -binary, or matching deprecated -wrapper-binary and -relayd-binary")
	}
	if wrapperBinary != "" {
		return wrapperBinary, nil
	}
	if relaydBinary != "" {
		return relaydBinary, nil
	}
	return "", fmt.Errorf("binary path is required")
}

func mergePrimaryFeishuApp(apps []config.FeishuAppConfig, incomingAppID, incomingSecret string) []config.FeishuAppConfig {
	if len(apps) == 0 && strings.TrimSpace(incomingAppID) == "" && strings.TrimSpace(incomingSecret) == "" {
		return nil
	}

	merged := append([]config.FeishuAppConfig(nil), apps...)
	index := 0
	for i, app := range merged {
		if app.Enabled == nil || *app.Enabled {
			index = i
			break
		}
	}
	if len(merged) == 0 {
		merged = append(merged, config.FeishuAppConfig{
			ID:      "legacy-default",
			Name:    "Legacy Default",
			Enabled: boolPtr(true),
		})
		index = 0
	}

	app := merged[index]
	if strings.TrimSpace(app.ID) == "" {
		app.ID = "legacy-default"
	}
	if strings.TrimSpace(app.Name) == "" {
		app.Name = "Legacy Default"
	}
	if app.Enabled == nil {
		app.Enabled = boolPtr(true)
	}
	app.AppID = choosePreservedValue(incomingAppID, app.AppID)
	app.AppSecret = choosePreservedValue(incomingSecret, app.AppSecret)
	merged[index] = app
	return merged
}

func boolPtr(value bool) *bool {
	return &value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
