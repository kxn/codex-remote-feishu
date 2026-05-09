package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/pathscope"
)

const (
	currentConfigVersion = 1

	defaultRelayListenHost                   = "127.0.0.1"
	defaultRelayListenPort                   = 9500
	defaultAdminListenHost                   = "127.0.0.1"
	defaultAdminListenPort                   = 9501
	defaultToolListenHost                    = "127.0.0.1"
	defaultToolListenPort                    = 9502
	defaultPprofListenHost                   = "127.0.0.1"
	defaultPprofListenPort                   = 17501
	defaultPreviewRootName                   = "Codex Remote Previews"
	defaultTryCloudflareLaunchTimeoutSeconds = 60
)

type LoadedAppConfig struct {
	Path   string
	Config AppConfig
}

type AppConfig struct {
	Version        int                    `json:"version"`
	Relay          RelaySettings          `json:"relay"`
	Admin          AdminSettings          `json:"admin"`
	Tool           ToolSettings           `json:"tool,omitempty"`
	ExternalAccess ExternalAccessSettings `json:"externalAccess,omitempty"`
	Wrapper        WrapperSettings        `json:"wrapper"`
	Codex          CodexSettings          `json:"codex,omitempty"`
	Claude         ClaudeSettings         `json:"claude,omitempty"`
	Feishu         FeishuSettings         `json:"feishu"`
	Debug          DebugSettings          `json:"debug"`
	Storage        StorageSettings        `json:"storage,omitempty"`
}

type RelaySettings struct {
	ServerURL  string `json:"serverURL,omitempty"`
	ListenHost string `json:"listenHost,omitempty"`
	ListenPort int    `json:"listenPort,omitempty"`
}

type AdminSettings struct {
	ListenHost      string                  `json:"listenHost,omitempty"`
	ListenPort      int                     `json:"listenPort,omitempty"`
	AutoOpenBrowser *bool                   `json:"autoOpenBrowser,omitempty"`
	Onboarding      AdminOnboardingSettings `json:"onboarding,omitempty"`
}

type AdminOnboardingSettings struct {
	Apps              map[string]FeishuAppOnboardingState `json:"apps,omitempty"`
	AutostartDecision *OnboardingDecision                 `json:"autostartDecision,omitempty"`
	VSCodeDecision    *OnboardingDecision                 `json:"vscodeDecision,omitempty"`
}

type OnboardingDecision struct {
	Value     string     `json:"value,omitempty"`
	DecidedAt *time.Time `json:"decidedAt,omitempty"`
}

type FeishuAppOnboardingState struct {
	PermissionDecision *OnboardingDecision `json:"permissionDecision,omitempty"`
	AutoConfigDecision *OnboardingDecision `json:"autoConfigDecision,omitempty"`
	MenuDecision       *OnboardingDecision `json:"menuDecision,omitempty"`
}

type ToolSettings struct {
	ListenHost string `json:"listenHost,omitempty"`
	ListenPort int    `json:"listenPort,omitempty"`
}

type ExternalAccessSettings struct {
	ListenHost               string                         `json:"listenHost,omitempty"`
	ListenPort               int                            `json:"listenPort,omitempty"`
	DefaultLinkTTLSeconds    int                            `json:"defaultLinkTTLSeconds,omitempty"`
	DefaultSessionTTLSeconds int                            `json:"defaultSessionTTLSeconds,omitempty"`
	Provider                 ExternalAccessProviderSettings `json:"provider,omitempty"`
}

type ExternalAccessProviderSettings struct {
	Kind          string                `json:"kind,omitempty"`
	LazyStart     *bool                 `json:"lazyStart,omitempty"`
	TryCloudflare TryCloudflareSettings `json:"tryCloudflare,omitempty"`
}

type TryCloudflareSettings struct {
	BinaryPath           string `json:"binaryPath,omitempty"`
	LaunchTimeoutSeconds int    `json:"launchTimeoutSeconds,omitempty"`
	MetricsPort          int    `json:"metricsPort,omitempty"`
	LogPath              string `json:"logPath,omitempty"`
}

type WrapperSettings struct {
	CodexRealBinary string `json:"codexRealBinary,omitempty"`
	NameMode        string `json:"nameMode,omitempty"`
	IntegrationMode string `json:"integrationMode,omitempty"`
}

type FeishuSettings struct {
	UseSystemProxy bool              `json:"useSystemProxy,omitempty"`
	Apps           []FeishuAppConfig `json:"apps,omitempty"`
}

type FeishuAppConfig struct {
	ID         string     `json:"id,omitempty"`
	Name       string     `json:"name,omitempty"`
	AppID      string     `json:"appId,omitempty"`
	AppSecret  string     `json:"appSecret,omitempty"`
	Enabled    *bool      `json:"enabled,omitempty"`
	VerifiedAt *time.Time `json:"verifiedAt,omitempty"`
}

type DebugSettings struct {
	RelayFlow bool           `json:"relayFlow,omitempty"`
	RelayRaw  bool           `json:"relayRaw,omitempty"`
	Pprof     *PprofSettings `json:"pprof,omitempty"`
}

type PprofSettings struct {
	Enabled    bool   `json:"enabled,omitempty"`
	ListenHost string `json:"listenHost,omitempty"`
	ListenPort int    `json:"listenPort,omitempty"`
}

type StorageSettings struct {
	ImageStagingDir       string `json:"imageStagingDir,omitempty"`
	PreviewStatePath      string `json:"previewStatePath,omitempty"`
	PreviewRootFolderName string `json:"previewRootFolderName,omitempty"`
}

func DefaultConfigPath() string {
	return chooseNonEmpty(
		os.Getenv(UnifiedConfigEnvPath),
		xdgConfigPath("codex-remote", "config.json"),
	)
}

func DefaultAppConfig() AppConfig {
	return AppConfig{
		Version: currentConfigVersion,
		Relay: RelaySettings{
			ServerURL:  defaultRelayServerURL(defaultRelayListenPort),
			ListenHost: defaultRelayListenHost,
			ListenPort: defaultRelayListenPort,
		},
		Admin: AdminSettings{
			ListenHost:      defaultAdminListenHost,
			ListenPort:      defaultAdminListenPort,
			AutoOpenBrowser: boolPtr(true),
		},
		Tool: ToolSettings{
			ListenHost: defaultToolListenHost,
			ListenPort: defaultToolListenPort,
		},
		ExternalAccess: ExternalAccessSettings{
			ListenHost:               defaultAdminListenHost,
			ListenPort:               9512,
			DefaultLinkTTLSeconds:    600,
			DefaultSessionTTLSeconds: 1800,
			Provider: ExternalAccessProviderSettings{
				Kind:      "trycloudflare",
				LazyStart: boolPtr(true),
				TryCloudflare: TryCloudflareSettings{
					LaunchTimeoutSeconds: defaultTryCloudflareLaunchTimeoutSeconds,
				},
			},
		},
		Wrapper: WrapperSettings{
			CodexRealBinary: "codex",
			NameMode:        "workspace_basename",
			IntegrationMode: "managed_shim",
		},
		Storage: StorageSettings{
			PreviewRootFolderName: defaultPreviewRootName,
		},
	}
}

func LoadAppConfig() (LoadedAppConfig, error) {
	return LoadAppConfigAtPath(DefaultConfigPath())
}

func LoadAppConfigAtPath(targetPath string) (LoadedAppConfig, error) {
	targetPath = chooseNonEmpty(targetPath, xdgConfigPath("codex-remote", "config.json"))
	return loadAppConfig(targetPath)
}

func WriteAppConfig(path string, cfg AppConfig) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("config path is required")
	}
	if err := pathscope.EnsureWritePath(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	cfg = cfg.normalized()
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmpFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if err := tmpFile.Chmod(0o600); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if _, err := tmpFile.Write(raw); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func SelectRuntimeFeishuApp(apps []FeishuAppConfig) FeishuAppConfig {
	for _, app := range apps {
		if feishuAppEnabled(app) {
			return app
		}
	}
	if len(apps) > 0 {
		return apps[0]
	}
	return FeishuAppConfig{}
}

func loadAppConfig(targetPath string) (LoadedAppConfig, error) {
	targetPath = strings.TrimSpace(targetPath)
	if targetPath == "" {
		return LoadedAppConfig{}, fmt.Errorf("config path is required")
	}
	if isLegacyConfigFilename(filepath.Base(targetPath)) {
		return LoadedAppConfig{}, unsupportedLegacyConfigPathError(targetPath)
	}

	if fileExists(targetPath) {
		cfg, err := readConfigFile(targetPath)
		if err != nil {
			return LoadedAppConfig{}, err
		}
		return LoadedAppConfig{Path: targetPath, Config: cfg}, nil
	}
	if legacyPaths := detectLegacyConfigFiles(filepath.Dir(targetPath)); len(legacyPaths) > 0 {
		return LoadedAppConfig{}, unsupportedLegacyConfigFilesError(legacyPaths)
	}
	return LoadedAppConfig{Path: targetPath, Config: DefaultAppConfig()}, nil
}

func readConfigFile(path string) (AppConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return AppConfig{}, err
	}
	var cfg AppConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		if isLegacyConfigFilename(filepath.Base(path)) {
			return AppConfig{}, unsupportedLegacyConfigPathError(path)
		}
		return AppConfig{}, fmt.Errorf("parse json config %s: %w", path, err)
	}
	return cfg.normalized(), nil
}

func detectLegacyConfigFiles(dir string) []string {
	dir = strings.TrimSpace(dir)
	if dir == "" || dir == "." {
		return nil
	}
	candidates := []string{
		filepath.Join(dir, "config.env"),
		filepath.Join(dir, "wrapper.env"),
		filepath.Join(dir, "services.env"),
	}
	found := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if fileExists(candidate) {
			found = append(found, candidate)
		}
	}
	return found
}

func isLegacyConfigFilename(name string) bool {
	switch strings.TrimSpace(name) {
	case "config.env", "wrapper.env", "services.env":
		return true
	default:
		return false
	}
}

func unsupportedLegacyConfigPathError(path string) error {
	return fmt.Errorf("legacy env config file %s is no longer supported; move settings into config.json", filepath.Clean(path))
}

func unsupportedLegacyConfigFilesError(paths []string) error {
	cleaned := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		cleaned = append(cleaned, filepath.Clean(path))
	}
	return fmt.Errorf("legacy env config files are no longer supported; remove or migrate them to config.json: %s", strings.Join(cleaned, ", "))
}

func chooseInt(value string, fallback int) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func defaultRelayServerURL(port int) string {
	if port <= 0 {
		port = defaultRelayListenPort
	}
	return fmt.Sprintf("ws://127.0.0.1:%d/ws/agent", port)
}

func feishuAppEnabled(app FeishuAppConfig) bool {
	return app.Enabled == nil || *app.Enabled
}

func boolPtr(value bool) *bool {
	return &value
}

func (cfg AppConfig) normalized() AppConfig {
	defaults := DefaultAppConfig()

	if cfg.Version <= 0 {
		cfg.Version = defaults.Version
	}

	if strings.TrimSpace(cfg.Relay.ListenHost) == "" {
		cfg.Relay.ListenHost = defaults.Relay.ListenHost
	}
	if cfg.Relay.ListenPort <= 0 {
		cfg.Relay.ListenPort = defaults.Relay.ListenPort
	}
	if strings.TrimSpace(cfg.Relay.ServerURL) == "" {
		cfg.Relay.ServerURL = defaultRelayServerURL(cfg.Relay.ListenPort)
	}

	if strings.TrimSpace(cfg.Admin.ListenHost) == "" {
		cfg.Admin.ListenHost = defaults.Admin.ListenHost
	}
	if cfg.Admin.ListenPort <= 0 {
		cfg.Admin.ListenPort = defaults.Admin.ListenPort
	}
	if cfg.Admin.AutoOpenBrowser == nil {
		cfg.Admin.AutoOpenBrowser = boolPtr(*defaults.Admin.AutoOpenBrowser)
	}

	if strings.TrimSpace(cfg.Tool.ListenHost) == "" {
		cfg.Tool.ListenHost = defaults.Tool.ListenHost
	}
	if cfg.Tool.ListenPort <= 0 {
		cfg.Tool.ListenPort = defaults.Tool.ListenPort
	}

	if strings.TrimSpace(cfg.ExternalAccess.ListenHost) == "" {
		cfg.ExternalAccess.ListenHost = defaults.ExternalAccess.ListenHost
	}
	if cfg.ExternalAccess.ListenPort <= 0 {
		cfg.ExternalAccess.ListenPort = defaults.ExternalAccess.ListenPort
	}
	if cfg.ExternalAccess.DefaultLinkTTLSeconds <= 0 {
		cfg.ExternalAccess.DefaultLinkTTLSeconds = defaults.ExternalAccess.DefaultLinkTTLSeconds
	}
	if cfg.ExternalAccess.DefaultSessionTTLSeconds <= 0 {
		cfg.ExternalAccess.DefaultSessionTTLSeconds = defaults.ExternalAccess.DefaultSessionTTLSeconds
	}
	if strings.TrimSpace(cfg.ExternalAccess.Provider.Kind) == "" {
		cfg.ExternalAccess.Provider.Kind = defaults.ExternalAccess.Provider.Kind
	}
	if cfg.ExternalAccess.Provider.LazyStart == nil {
		cfg.ExternalAccess.Provider.LazyStart = boolPtr(*defaults.ExternalAccess.Provider.LazyStart)
	}
	if cfg.ExternalAccess.Provider.TryCloudflare.LaunchTimeoutSeconds <= 0 {
		cfg.ExternalAccess.Provider.TryCloudflare.LaunchTimeoutSeconds = defaults.ExternalAccess.Provider.TryCloudflare.LaunchTimeoutSeconds
	}

	if strings.TrimSpace(cfg.Wrapper.CodexRealBinary) == "" {
		cfg.Wrapper.CodexRealBinary = defaults.Wrapper.CodexRealBinary
	}
	if strings.TrimSpace(cfg.Wrapper.NameMode) == "" {
		cfg.Wrapper.NameMode = defaults.Wrapper.NameMode
	}
	if strings.TrimSpace(cfg.Wrapper.IntegrationMode) == "" {
		cfg.Wrapper.IntegrationMode = defaults.Wrapper.IntegrationMode
	}

	cfg.Codex.Providers = NormalizeCodexProviders(cfg.Codex.Providers)
	cfg.Claude.Profiles = NormalizeClaudeProfiles(cfg.Claude.Profiles)

	if cfg.Debug.Pprof != nil {
		normalized := cfg.Debug.Pprof.normalized()
		if normalized.isZero() {
			cfg.Debug.Pprof = nil
		} else {
			cfg.Debug.Pprof = &normalized
		}
	}

	if strings.TrimSpace(cfg.Storage.PreviewRootFolderName) == "" {
		cfg.Storage.PreviewRootFolderName = defaults.Storage.PreviewRootFolderName
	}

	return cfg
}

func (cfg PprofSettings) normalized() PprofSettings {
	if !cfg.Enabled {
		return cfg
	}
	if strings.TrimSpace(cfg.ListenHost) == "" {
		cfg.ListenHost = defaultPprofListenHost
	}
	if cfg.ListenPort <= 0 {
		cfg.ListenPort = defaultPprofListenPort
	}
	return cfg
}

func (cfg PprofSettings) isZero() bool {
	return !cfg.Enabled && strings.TrimSpace(cfg.ListenHost) == "" && cfg.ListenPort <= 0
}
