package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/pathscope"
)

type WrapperConfig struct {
	RelayServerURL  string
	CodexRealBinary string
	NameMode        string
	IntegrationMode string
	ConfigPath      string
	DebugRelayFlow  bool
	DebugRelayRaw   bool
}

type ServicesConfig struct {
	RelayHost            string
	RelayPort            string
	RelayAPIHost         string
	RelayAPIPort         string
	FeishuGatewayID      string
	FeishuAppID          string
	FeishuAppSecret      string
	FeishuUseSystemProxy bool
	ConfigPath           string
	DebugRelayFlow       bool
	DebugRelayRaw        bool
}

const (
	UnifiedConfigEnvPath          = "CODEX_REMOTE_CONFIG"
	DebugRelayFlowEnv             = "CODEX_REMOTE_DEBUG_RELAY_FLOW"
	DebugRelayRawEnv              = "CODEX_REMOTE_DEBUG_RELAY_RAW"
	ResumeThreadIDEnv             = "CODEX_REMOTE_RESUME_THREAD_ID"
	ExternalAccessHostEnv         = "EXTERNAL_ACCESS_HOST"
	ExternalAccessPortEnv         = "EXTERNAL_ACCESS_PORT"
	ExternalAccessProviderEnv     = "CODEX_REMOTE_EXTERNAL_ACCESS_PROVIDER"
	TryCloudflareBinaryEnv        = "CODEX_REMOTE_TRYCLOUDFLARE_BINARY"
	TryCloudflareLaunchTimeoutEnv = "CODEX_REMOTE_TRYCLOUDFLARE_LAUNCH_TIMEOUT"

	RelayPortEnv              = "RELAY_PORT"
	RelayServerURLEnv         = "RELAY_SERVER_URL"
	RelayHostEnv              = "RELAY_HOST"
	RelayAPIHostEnv           = "RELAY_API_HOST"
	RelayAPIPortEnv           = "RELAY_API_PORT"
	CodexRealBinaryEnv        = "CODEX_REAL_BINARY"
	WrapperNameModeEnv        = "CODEX_REMOTE_WRAPPER_NAME_MODE"
	WrapperIntegrationModeEnv = "CODEX_REMOTE_WRAPPER_INTEGRATION_MODE"
	FeishuGatewayIDEnv        = "FEISHU_GATEWAY_ID"
	FeishuAppIDEnv            = "FEISHU_APP_ID"
	FeishuAppSecretEnv        = "FEISHU_APP_SECRET"
	FeishuUseSystemProxyEnv   = "FEISHU_USE_SYSTEM_PROXY"
	XDGConfigHomeEnv          = "XDG_CONFIG_HOME"

	CodexRemoteRepoEnv             = "CODEX_REMOTE_REPO"
	CodexRemoteReleasesAPIURLEnv   = "CODEX_REMOTE_RELEASES_API_URL"
	CodexRemoteBaseURLEnv          = "CODEX_REMOTE_BASE_URL"
	CodexRemoteDevManifestURLEnv   = "CODEX_REMOTE_DEV_MANIFEST_URL"
	CodexRemoteInstanceIDEnv       = "CODEX_REMOTE_INSTANCE_ID"
	CodexRemoteInstanceDisplayName = "CODEX_REMOTE_INSTANCE_DISPLAY_NAME"
	CodexRemoteInstanceSourceEnv   = "CODEX_REMOTE_INSTANCE_SOURCE"
	CodexRemoteLifetimeEnv         = "CODEX_REMOTE_LIFETIME"
	CodexRemoteParentPIDEnv        = "CODEX_REMOTE_PARENT_PID"
	CodexRemoteInstanceBackendEnv  = "CODEX_REMOTE_INSTANCE_BACKEND"
)

func LoadWrapperConfig() (WrapperConfig, error) {
	loaded, err := LoadAppConfig()
	if err != nil {
		return WrapperConfig{}, err
	}
	relayPort := chooseInt(os.Getenv(RelayPortEnv), loaded.Config.Relay.ListenPort)
	cfg := WrapperConfig{
		RelayServerURL: chooseNonEmpty(
			os.Getenv(RelayServerURLEnv),
			loaded.Config.Relay.ServerURL,
			defaultRelayServerURL(relayPort),
		),
		CodexRealBinary: chooseNonEmpty(
			os.Getenv(CodexRealBinaryEnv),
			loaded.Config.Wrapper.CodexRealBinary,
			"codex",
		),
		NameMode: chooseNonEmpty(
			os.Getenv(WrapperNameModeEnv),
			loaded.Config.Wrapper.NameMode,
			"workspace_basename",
		),
		IntegrationMode: chooseNonEmpty(
			os.Getenv(WrapperIntegrationModeEnv),
			loaded.Config.Wrapper.IntegrationMode,
			"managed_shim",
		),
		ConfigPath: loaded.Path,
		DebugRelayFlow: chooseBool(
			os.Getenv(DebugRelayFlowEnv),
			boolString(loaded.Config.Debug.RelayFlow),
			false,
		),
		DebugRelayRaw: chooseBool(
			os.Getenv(DebugRelayRawEnv),
			boolString(loaded.Config.Debug.RelayRaw),
			false,
		),
	}
	return cfg, nil
}

func LoadServicesConfig() (ServicesConfig, error) {
	loaded, err := LoadAppConfig()
	if err != nil {
		return ServicesConfig{}, err
	}
	selectedApp := SelectRuntimeFeishuApp(loaded.Config.Feishu.Apps)
	cfg := ServicesConfig{
		RelayHost:    chooseNonEmpty(os.Getenv(RelayHostEnv), loaded.Config.Relay.ListenHost, defaultRelayListenHost),
		RelayPort:    strconv.Itoa(chooseInt(os.Getenv(RelayPortEnv), loaded.Config.Relay.ListenPort)),
		RelayAPIHost: chooseNonEmpty(os.Getenv(RelayAPIHostEnv), loaded.Config.Admin.ListenHost, defaultAdminListenHost),
		RelayAPIPort: strconv.Itoa(chooseInt(os.Getenv(RelayAPIPortEnv), loaded.Config.Admin.ListenPort)),
		FeishuGatewayID: chooseNonEmpty(
			os.Getenv(FeishuGatewayIDEnv),
			selectedApp.ID,
		),
		FeishuAppID: chooseNonEmpty(
			os.Getenv(FeishuAppIDEnv),
			selectedApp.AppID,
		),
		FeishuAppSecret: chooseNonEmpty(
			os.Getenv(FeishuAppSecretEnv),
			selectedApp.AppSecret,
		),
		FeishuUseSystemProxy: chooseBool(
			os.Getenv(FeishuUseSystemProxyEnv),
			boolString(loaded.Config.Feishu.UseSystemProxy),
			loaded.Config.Feishu.UseSystemProxy,
		),
		ConfigPath: loaded.Path,
		DebugRelayFlow: chooseBool(
			os.Getenv(DebugRelayFlowEnv),
			boolString(loaded.Config.Debug.RelayFlow),
			loaded.Config.Debug.RelayFlow,
		),
		DebugRelayRaw: chooseBool(
			os.Getenv(DebugRelayRawEnv),
			boolString(loaded.Config.Debug.RelayRaw),
			loaded.Config.Debug.RelayRaw,
		),
	}
	return cfg, nil
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func ResolveExternalAccessSettings(base ExternalAccessSettings) ExternalAccessSettings {
	base.ListenHost = chooseNonEmpty(os.Getenv(ExternalAccessHostEnv), base.ListenHost)
	base.ListenPort = chooseInt(os.Getenv(ExternalAccessPortEnv), base.ListenPort)
	base.Provider.Kind = chooseNonEmpty(os.Getenv(ExternalAccessProviderEnv), base.Provider.Kind)
	base.Provider.TryCloudflare.BinaryPath = chooseNonEmpty(os.Getenv(TryCloudflareBinaryEnv), base.Provider.TryCloudflare.BinaryPath)
	base.Provider.TryCloudflare.LaunchTimeoutSeconds = chooseInt(os.Getenv(TryCloudflareLaunchTimeoutEnv), base.Provider.TryCloudflare.LaunchTimeoutSeconds)
	return base
}

func xdgConfigPath(parts ...string) string {
	base := os.Getenv(XDGConfigHomeEnv)
	if base == "" {
		home, err := pathscope.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(append([]string{base}, parts...)...)
}

func chooseNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func chooseBool(primary, secondary string, fallback bool) bool {
	for _, value := range []string{primary, secondary} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
