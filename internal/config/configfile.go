package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	currentConfigVersion = 1

	defaultRelayListenHost = "127.0.0.1"
	defaultRelayListenPort = 9500
	defaultAdminListenHost = "127.0.0.1"
	defaultAdminListenPort = 9501
	defaultToolListenHost  = "127.0.0.1"
	defaultToolListenPort  = 9502
	defaultPprofListenHost = "127.0.0.1"
	defaultPprofListenPort = 17501
	defaultPreviewRootName = "Codex Remote Previews"
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
	ListenHost      string `json:"listenHost,omitempty"`
	ListenPort      int    `json:"listenPort,omitempty"`
	AutoOpenBrowser *bool  `json:"autoOpenBrowser,omitempty"`
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

type FeishuAppWizardState struct {
	CredentialsSavedAt   *time.Time `json:"credentialsSavedAt,omitempty"`
	ConnectionVerifiedAt *time.Time `json:"connectionVerifiedAt,omitempty"`
	ScopesExportedAt     *time.Time `json:"scopesExportedAt,omitempty"`
	EventsConfirmedAt    *time.Time `json:"eventsConfirmedAt,omitempty"`
	CallbacksConfirmedAt *time.Time `json:"callbacksConfirmedAt,omitempty"`
	MenusConfirmedAt     *time.Time `json:"menusConfirmedAt,omitempty"`
	PublishedAt          *time.Time `json:"publishedAt,omitempty"`
}

type FeishuAppConfig struct {
	ID         string               `json:"id,omitempty"`
	Name       string               `json:"name,omitempty"`
	AppID      string               `json:"appId,omitempty"`
	AppSecret  string               `json:"appSecret,omitempty"`
	Enabled    *bool                `json:"enabled,omitempty"`
	VerifiedAt *time.Time           `json:"verifiedAt,omitempty"`
	Wizard     FeishuAppWizardState `json:"wizard,omitempty"`
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
					LaunchTimeoutSeconds: 20,
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
	targetPath := DefaultConfigPath()
	return loadAppConfig(targetPath, defaultLegacyCandidates(targetPath))
}

func LoadAppConfigAtPath(targetPath string, legacyCandidates ...string) (LoadedAppConfig, error) {
	targetPath = chooseNonEmpty(targetPath, xdgConfigPath("codex-remote", "config.json"))
	if len(legacyCandidates) == 0 {
		legacyCandidates = defaultLegacyCandidates(targetPath)
	}
	return loadAppConfig(targetPath, legacyCandidates)
}

func WriteAppConfig(path string, cfg AppConfig) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("config path is required")
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

func loadAppConfig(targetPath string, legacyCandidates []string) (LoadedAppConfig, error) {
	targetPath = strings.TrimSpace(targetPath)
	if targetPath == "" {
		return LoadedAppConfig{}, fmt.Errorf("config path is required")
	}

	if fileExists(targetPath) {
		cfg, kind, err := readConfigFile(targetPath)
		if err != nil {
			return LoadedAppConfig{}, err
		}
		if kind == configFileJSON {
			return LoadedAppConfig{Path: targetPath, Config: cfg}, nil
		}
		return migrateLegacyConfig(targetPath, cfg, []string{targetPath})
	}

	legacyValues, usedPaths, err := loadLegacyConfigValues(legacyCandidates)
	if err != nil {
		return LoadedAppConfig{}, err
	}
	if len(usedPaths) == 0 {
		return LoadedAppConfig{Path: targetPath, Config: DefaultAppConfig()}, nil
	}
	cfg := legacyValuesToAppConfig(legacyValues)
	return migrateLegacyConfig(targetPath, cfg, usedPaths)
}

type configFileKind int

const (
	configFileUnknown configFileKind = iota
	configFileJSON
	configFileLegacyEnv
)

func readConfigFile(path string) (AppConfig, configFileKind, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return AppConfig{}, configFileUnknown, err
	}
	var cfg AppConfig
	jsonErr := json.Unmarshal(raw, &cfg)
	if jsonErr == nil {
		return cfg.normalized(), configFileJSON, nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return AppConfig{}, configFileJSON, fmt.Errorf("parse json config %s: %w", path, jsonErr)
	}
	values, err := LoadEnvFile(path)
	if err != nil {
		return AppConfig{}, configFileLegacyEnv, err
	}
	return legacyValuesToAppConfig(values), configFileLegacyEnv, nil
}

func defaultLegacyCandidates(targetPath string) []string {
	candidates := []string{targetPath}
	if dir := filepath.Dir(strings.TrimSpace(targetPath)); dir != "" && dir != "." {
		candidates = append(candidates,
			filepath.Join(dir, "config.env"),
			filepath.Join(dir, "wrapper.env"),
			filepath.Join(dir, "services.env"),
		)
	}
	candidates = append(candidates,
		os.Getenv("CODEX_REMOTE_WRAPPER_CONFIG"),
		os.Getenv("CODEX_REMOTE_SERVICES_CONFIG"),
		xdgConfigPath("codex-remote", "config.env"),
		xdgConfigPath("codex-remote", "wrapper.env"),
		xdgConfigPath("codex-remote", "services.env"),
	)
	return dedupePaths(candidates)
}

func loadLegacyConfigValues(candidates []string) (map[string]string, []string, error) {
	merged := map[string]string{}
	used := []string{}
	for _, candidate := range dedupePaths(candidates) {
		if !fileExists(candidate) {
			continue
		}
		values, err := LoadEnvFile(candidate)
		if err != nil {
			return nil, nil, fmt.Errorf("load legacy env %s: %w", candidate, err)
		}
		used = append(used, candidate)
		for key, value := range values {
			if _, exists := merged[key]; exists {
				continue
			}
			merged[key] = value
		}
	}
	return merged, used, nil
}

func legacyValuesToAppConfig(values map[string]string) AppConfig {
	cfg := DefaultAppConfig()

	if value := strings.TrimSpace(values["RELAY_SERVER_URL"]); value != "" {
		cfg.Relay.ServerURL = value
	}
	if value := strings.TrimSpace(values["RELAY_HOST"]); value != "" {
		cfg.Relay.ListenHost = value
	}
	cfg.Relay.ListenPort = chooseInt(values["RELAY_PORT"], cfg.Relay.ListenPort)

	if value := strings.TrimSpace(values["RELAY_API_HOST"]); value != "" {
		cfg.Admin.ListenHost = value
	}
	cfg.Admin.ListenPort = chooseInt(values["RELAY_API_PORT"], cfg.Admin.ListenPort)

	if value := strings.TrimSpace(values["CODEX_REAL_BINARY"]); value != "" {
		cfg.Wrapper.CodexRealBinary = value
	}
	if value := strings.TrimSpace(values["CODEX_REMOTE_WRAPPER_NAME_MODE"]); value != "" {
		cfg.Wrapper.NameMode = value
	}
	if value := strings.TrimSpace(values["CODEX_REMOTE_WRAPPER_INTEGRATION_MODE"]); value != "" {
		cfg.Wrapper.IntegrationMode = value
	}

	cfg.Feishu.UseSystemProxy = chooseBool(values["FEISHU_USE_SYSTEM_PROXY"], "", false)
	appID := strings.TrimSpace(values["FEISHU_APP_ID"])
	appSecret := strings.TrimSpace(values["FEISHU_APP_SECRET"])
	if appID != "" || appSecret != "" {
		cfg.Feishu.Apps = []FeishuAppConfig{{
			ID:        "legacy-default",
			Name:      "Legacy Default",
			AppID:     appID,
			AppSecret: appSecret,
			Enabled:   boolPtr(true),
		}}
	}

	cfg.Debug.RelayFlow = chooseBool(values[DebugRelayFlowEnv], "", false)
	cfg.Debug.RelayRaw = chooseBool(values[DebugRelayRawEnv], "", false)

	return cfg.normalized()
}

func migrateLegacyConfig(targetPath string, cfg AppConfig, usedPaths []string) (LoadedAppConfig, error) {
	targetPath = strings.TrimSpace(targetPath)
	usedPaths = dedupePaths(usedPaths)
	targetBackupPath := ""
	targetMatched := false

	for _, legacyPath := range usedPaths {
		if samePathString(legacyPath, targetPath) {
			targetMatched = true
			break
		}
	}
	if targetMatched {
		var err error
		targetBackupPath, err = archiveLegacyPath(targetPath)
		if err != nil {
			return LoadedAppConfig{}, err
		}
	}

	if err := WriteAppConfig(targetPath, cfg); err != nil {
		if targetBackupPath != "" {
			_ = os.Rename(targetBackupPath, targetPath)
		}
		return LoadedAppConfig{}, err
	}

	for _, legacyPath := range usedPaths {
		if samePathString(legacyPath, targetPath) {
			continue
		}
		if _, err := archiveLegacyPath(legacyPath); err != nil {
			return LoadedAppConfig{}, err
		}
	}
	if err := syncInstallStateConfigPath(targetPath, usedPaths); err != nil {
		return LoadedAppConfig{}, err
	}
	return LoadedAppConfig{Path: targetPath, Config: cfg.normalized()}, nil
}

func archiveLegacyPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || !fileExists(path) {
		return "", nil
	}
	backupPath := fmt.Sprintf("%s.migrated-%s.bak", path, time.Now().UTC().Format("20060102T150405Z"))
	if err := os.Rename(path, backupPath); err != nil {
		return "", err
	}
	return backupPath, nil
}

func syncInstallStateConfigPath(targetPath string, legacyPaths []string) error {
	legacySet := map[string]bool{}
	for _, path := range legacyPaths {
		legacySet[filepath.Clean(path)] = true
	}

	for _, statePath := range candidateInstallStatePaths(targetPath) {
		if !fileExists(statePath) {
			continue
		}
		raw, err := os.ReadFile(statePath)
		if err != nil {
			return err
		}
		var state map[string]any
		if err := json.Unmarshal(raw, &state); err != nil {
			return fmt.Errorf("parse install state %s: %w", statePath, err)
		}

		updated := false
		for _, field := range []string{"configPath", "wrapperConfigPath", "servicesConfigPath"} {
			value, _ := state[field].(string)
			if !shouldUpdateInstallStateField(value, targetPath, legacySet) {
				continue
			}
			state[field] = targetPath
			updated = true
		}
		if !updated {
			continue
		}
		raw, err = json.MarshalIndent(state, "", "  ")
		if err != nil {
			return err
		}
		raw = append(raw, '\n')
		if err := os.WriteFile(statePath, raw, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func candidateInstallStatePaths(targetPath string) []string {
	candidates := []string{xdgDataPath("codex-remote", "install-state.json")}
	if baseDir, ok := baseDirForConfigPath(targetPath); ok {
		candidates = append(candidates, filepath.Join(baseDir, ".local", "share", "codex-remote", "install-state.json"))
	}
	return dedupePaths(candidates)
}

func baseDirForConfigPath(path string) (string, bool) {
	dir := filepath.Dir(filepath.Clean(path))
	if filepath.Base(dir) != "codex-remote" {
		return "", false
	}
	configParent := filepath.Dir(dir)
	if filepath.Base(configParent) != ".config" {
		return "", false
	}
	return filepath.Dir(configParent), true
}

func shouldUpdateInstallStateField(currentValue, targetPath string, legacySet map[string]bool) bool {
	currentValue = strings.TrimSpace(currentValue)
	if currentValue == "" {
		return false
	}
	cleanCurrent := filepath.Clean(currentValue)
	if legacySet[cleanCurrent] {
		return true
	}
	if filepath.Dir(cleanCurrent) != filepath.Dir(filepath.Clean(targetPath)) {
		return false
	}
	switch filepath.Base(cleanCurrent) {
	case "config.env", "wrapper.env", "services.env":
		return true
	default:
		return false
	}
}

func dedupePaths(paths []string) []string {
	seen := map[string]bool{}
	deduped := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		cleaned := filepath.Clean(path)
		if seen[cleaned] {
			continue
		}
		seen[cleaned] = true
		deduped = append(deduped, cleaned)
	}
	return deduped
}

func samePathString(left, right string) bool {
	return filepath.Clean(left) == filepath.Clean(right)
}

func xdgDataPath(parts ...string) string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(append([]string{base}, parts...)...)
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
