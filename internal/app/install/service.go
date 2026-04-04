package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"fschannel/internal/adapter/editor"
	"fschannel/internal/config"
)

type WrapperIntegrationMode string

const (
	IntegrationEditorSettings WrapperIntegrationMode = "editor_settings"
	IntegrationManagedShim    WrapperIntegrationMode = "managed_shim"
)

type Options struct {
	BaseDir            string
	WrapperBinary      string
	RelayServerURL     string
	CodexRealBinary    string
	IntegrationMode    WrapperIntegrationMode
	VSCodeSettingsPath string
	BundleEntrypoint   string
	FeishuAppID        string
	FeishuAppSecret    string
	UseSystemProxy     bool
}

type InstallState struct {
	WrapperConfigPath  string                 `json:"wrapperConfigPath"`
	ServicesConfigPath string                 `json:"servicesConfigPath"`
	StatePath          string                 `json:"statePath"`
	IntegrationMode    WrapperIntegrationMode `json:"integrationMode"`
	VSCodeSettingsPath string                 `json:"vscodeSettingsPath,omitempty"`
	BundleEntrypoint   string                 `json:"bundleEntrypoint,omitempty"`
}

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) Bootstrap(opts Options) (InstallState, error) {
	configDir := filepath.Join(opts.BaseDir, ".config", "codex-relay")
	stateDir := filepath.Join(opts.BaseDir, ".local", "share", "codex-relay")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return InstallState{}, err
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return InstallState{}, err
	}

	wrapperConfigPath := filepath.Join(configDir, "wrapper.env")
	servicesConfigPath := filepath.Join(configDir, "services.env")
	statePath := filepath.Join(stateDir, "install-state.json")
	existingServices, err := config.LoadEnvFile(servicesConfigPath)
	if err != nil && !os.IsNotExist(err) {
		return InstallState{}, err
	}

	if err := config.WriteEnvFile(wrapperConfigPath, map[string]string{
		"RELAY_SERVER_URL":                     opts.RelayServerURL,
		"CODEX_REAL_BINARY":                    opts.CodexRealBinary,
		"CODEX_RELAY_WRAPPER_NAME_MODE":        "workspace_basename",
		"CODEX_RELAY_WRAPPER_INTEGRATION_MODE": string(opts.IntegrationMode),
	}); err != nil {
		return InstallState{}, err
	}
	if err := config.WriteEnvFile(servicesConfigPath, map[string]string{
		"RELAY_PORT":              "9500",
		"RELAY_API_PORT":          "9501",
		"FEISHU_APP_ID":           choosePreservedValue(opts.FeishuAppID, existingServices["FEISHU_APP_ID"]),
		"FEISHU_APP_SECRET":       choosePreservedValue(opts.FeishuAppSecret, existingServices["FEISHU_APP_SECRET"]),
		"FEISHU_USE_SYSTEM_PROXY": boolString(opts.UseSystemProxy),
	}); err != nil {
		return InstallState{}, err
	}

	if opts.IntegrationMode == IntegrationEditorSettings && opts.VSCodeSettingsPath != "" {
		if err := editor.PatchVSCodeSettings(opts.VSCodeSettingsPath, opts.WrapperBinary); err != nil {
			return InstallState{}, err
		}
	}
	if opts.IntegrationMode == IntegrationManagedShim {
		if err := editor.PatchBundleEntrypoint(opts.BundleEntrypoint, opts.WrapperBinary); err != nil {
			return InstallState{}, err
		}
	}

	state := InstallState{
		WrapperConfigPath:  wrapperConfigPath,
		ServicesConfigPath: servicesConfigPath,
		StatePath:          statePath,
		IntegrationMode:    opts.IntegrationMode,
		VSCodeSettingsPath: opts.VSCodeSettingsPath,
		BundleEntrypoint:   opts.BundleEntrypoint,
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
