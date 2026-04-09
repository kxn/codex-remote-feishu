package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type WrapperConfig struct {
	RelayServerURL  string
	CodexRealBinary string
	AgentType       string
	AgentBinary     string
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
	UnifiedConfigEnvPath = "CODEX_REMOTE_CONFIG"
	DebugRelayFlowEnv    = "CODEX_REMOTE_DEBUG_RELAY_FLOW"
	DebugRelayRawEnv     = "CODEX_REMOTE_DEBUG_RELAY_RAW"
)

func LoadEnvFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("invalid env line: %q", line)
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return values, scanner.Err()
}

func WriteEnvFile(path string, values map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	builder := strings.Builder{}
	keys := []string{
		"RELAY_SERVER_URL",
		"RELAY_HOST",
		"CODEX_REAL_BINARY",
		"CODEX_REMOTE_WRAPPER_NAME_MODE",
		"CODEX_REMOTE_WRAPPER_INTEGRATION_MODE",
		"RELAY_PORT",
		"RELAY_API_HOST",
		"RELAY_API_PORT",
		"FEISHU_APP_ID",
		"FEISHU_APP_SECRET",
		"FEISHU_USE_SYSTEM_PROXY",
		DebugRelayFlowEnv,
		DebugRelayRawEnv,
	}
	written := map[string]bool{}
	for _, key := range keys {
		value, ok := values[key]
		if !ok {
			continue
		}
		builder.WriteString(key)
		builder.WriteString("=")
		builder.WriteString(value)
		builder.WriteString("\n")
		written[key] = true
	}
	for key, value := range values {
		if written[key] {
			continue
		}
		builder.WriteString(key)
		builder.WriteString("=")
		builder.WriteString(value)
		builder.WriteString("\n")
	}
	return os.WriteFile(path, []byte(builder.String()), 0o600)
}

func LoadWrapperConfig() (WrapperConfig, error) {
	loaded, err := LoadAppConfig()
	if err != nil {
		return WrapperConfig{}, err
	}
	relayPort := chooseInt(os.Getenv("RELAY_PORT"), loaded.Config.Relay.ListenPort)
	agentType := chooseNonEmpty(
		os.Getenv("CODEX_REMOTE_AGENT_TYPE"),
		loaded.Config.Wrapper.AgentType,
		"codex",
	)
	defaultBinary := "codex"
	if agentType == "claude" {
		defaultBinary = "claude"
	}
	cfg := WrapperConfig{
		RelayServerURL: chooseNonEmpty(
			os.Getenv("RELAY_SERVER_URL"),
			loaded.Config.Relay.ServerURL,
			defaultRelayServerURL(relayPort),
		),
		CodexRealBinary: chooseNonEmpty(
			os.Getenv("CODEX_REAL_BINARY"),
			loaded.Config.Wrapper.CodexRealBinary,
			"codex",
		),
		AgentType: agentType,
		AgentBinary: chooseNonEmpty(
			os.Getenv("CODEX_REMOTE_AGENT_BINARY"),
			loaded.Config.Wrapper.AgentBinary,
			defaultBinary,
		),
		NameMode: chooseNonEmpty(
			os.Getenv("CODEX_REMOTE_WRAPPER_NAME_MODE"),
			loaded.Config.Wrapper.NameMode,
			"workspace_basename",
		),
		IntegrationMode: chooseNonEmpty(
			os.Getenv("CODEX_REMOTE_WRAPPER_INTEGRATION_MODE"),
			loaded.Config.Wrapper.IntegrationMode,
			"editor_settings",
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
		RelayHost:    chooseNonEmpty(os.Getenv("RELAY_HOST"), loaded.Config.Relay.ListenHost, defaultRelayListenHost),
		RelayPort:    strconv.Itoa(chooseInt(os.Getenv("RELAY_PORT"), loaded.Config.Relay.ListenPort)),
		RelayAPIHost: chooseNonEmpty(os.Getenv("RELAY_API_HOST"), loaded.Config.Admin.ListenHost, defaultAdminListenHost),
		RelayAPIPort: strconv.Itoa(chooseInt(os.Getenv("RELAY_API_PORT"), loaded.Config.Admin.ListenPort)),
		FeishuGatewayID: chooseNonEmpty(
			selectedApp.ID,
			defaultGatewayIDForCredentials(
				os.Getenv("FEISHU_APP_ID"),
				os.Getenv("FEISHU_APP_SECRET"),
				selectedApp.AppID,
				selectedApp.AppSecret,
			),
		),
		FeishuAppID: chooseNonEmpty(
			os.Getenv("FEISHU_APP_ID"),
			selectedApp.AppID,
		),
		FeishuAppSecret: chooseNonEmpty(
			os.Getenv("FEISHU_APP_SECRET"),
			selectedApp.AppSecret,
		),
		FeishuUseSystemProxy: chooseBool(
			os.Getenv("FEISHU_USE_SYSTEM_PROXY"),
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

func xdgConfigPath(parts ...string) string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(append([]string{base}, parts...)...)
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
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

func defaultGatewayIDForCredentials(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return "legacy-default"
		}
	}
	return ""
}
