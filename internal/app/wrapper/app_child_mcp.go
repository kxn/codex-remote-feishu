package wrapper

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/toolservicecontract"
)

const (
	feishuMCPServerID      = "codex_remote_feishu"
	feishuMCPBearerEnvName = "CODEX_REMOTE_FEISHU_MCP_BEARER"
)

func (a *App) buildCodexChildLaunch(baseArgs []string) ([]string, []string) {
	args := append([]string{}, baseArgs...)
	env := childEnvWithProxy(a.config.ChildProxyEnv, args)
	args, env = a.applyCodexFeishuMCPPublication(args, env)
	return args, env
}

func (a *App) applyCodexFeishuMCPPublication(baseArgs, baseEnv []string) ([]string, []string) {
	args := append([]string{}, baseArgs...)
	env := append([]string{}, baseEnv...)
	info, ok := a.readFeishuMCPPublicationInfo()
	if !ok {
		return args, env
	}
	args = append(
		args,
		"-c", codexMCPOverride("url", info.URL),
		"-c", codexMCPOverride("bearer_token_env_var", feishuMCPBearerEnvName),
	)
	env = upsertEnvValue(env, feishuMCPBearerEnvName, strings.TrimSpace(info.Token))
	return args, env
}

func (a *App) applyClaudeFeishuMCPPublication(baseArgs, baseEnv []string) ([]string, []string) {
	args := append([]string{}, baseArgs...)
	env := append([]string{}, baseEnv...)
	info, ok := a.readFeishuMCPPublicationInfo()
	if !ok {
		return args, env
	}
	configPath, err := a.writeClaudeFeishuMCPConfig(info)
	if err != nil {
		a.debugf("feishu mcp publication skipped: write claude config failed path=%s err=%v", a.claudeFeishuMCPConfigPath(), err)
		return args, env
	}
	args = append(args, "--mcp-config", configPath)
	env = upsertEnvValue(env, feishuMCPBearerEnvName, strings.TrimSpace(info.Token))
	return args, env
}

func (a *App) readFeishuMCPPublicationInfo() (toolservicecontract.ServiceInfo, bool) {
	if !a.feishuMCPPublicationEligible() {
		return toolservicecontract.ServiceInfo{}, false
	}

	info, err := toolservicecontract.ReadServiceInfo(a.config.RuntimePaths.ToolServiceFile)
	if err != nil {
		a.debugf("feishu mcp publication skipped: read state failed path=%s err=%v", a.config.RuntimePaths.ToolServiceFile, err)
		return toolservicecontract.ServiceInfo{}, false
	}
	if strings.TrimSpace(info.URL) == "" || strings.TrimSpace(info.Token) == "" {
		a.debugf("feishu mcp publication skipped: incomplete state path=%s", a.config.RuntimePaths.ToolServiceFile)
		return toolservicecontract.ServiceInfo{}, false
	}
	urlWithCaller, ok := appendToolCallerInstanceParam(info.URL, a.config.InstanceID)
	if !ok {
		a.debugf("feishu mcp publication skipped: caller identity unavailable instance=%s url=%s", a.config.InstanceID, info.URL)
		return toolservicecontract.ServiceInfo{}, false
	}
	info.URL = urlWithCaller
	if tokenType := strings.TrimSpace(info.TokenType); tokenType != "" && !strings.EqualFold(tokenType, "bearer") {
		a.debugf("feishu mcp publication skipped: unsupported token type=%s", tokenType)
		return toolservicecontract.ServiceInfo{}, false
	}
	return info, true
}

func (a *App) feishuMCPPublicationEligible() bool {
	return !state.IsInstanceSource(a.config.Source, state.InstanceSourceVSCode)
}

func codexMCPOverride(field, value string) string {
	return fmt.Sprintf("mcp_servers.%s.%s=%s", feishuMCPServerID, strings.TrimSpace(field), strconv.Quote(strings.TrimSpace(value)))
}

func appendToolCallerInstanceParam(rawURL, instanceID string) (string, bool) {
	rawURL = strings.TrimSpace(rawURL)
	instanceID = strings.TrimSpace(instanceID)
	if rawURL == "" || instanceID == "" {
		return "", false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
		return "", false
	}
	values := parsed.Query()
	values.Set(toolservicecontract.CallerInstanceIDQueryParam, instanceID)
	parsed.RawQuery = values.Encode()
	return parsed.String(), true
}

func (a *App) writeClaudeFeishuMCPConfig(info toolservicecontract.ServiceInfo) (string, error) {
	path := a.claudeFeishuMCPConfigPath()
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("claude mcp config path is empty")
	}
	payload := map[string]any{
		"mcpServers": map[string]any{
			feishuMCPServerID: map[string]any{
				"type": "http",
				"url":  strings.TrimSpace(info.URL),
				"headers": map[string]string{
					"Authorization": "Bearer ${" + feishuMCPBearerEnvName + "}",
				},
			},
		},
	}
	if err := toolservicecontract.WriteJSONFileAtomic(path, payload, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func (a *App) claudeFeishuMCPConfigPath() string {
	if path := strings.TrimSpace(a.config.RuntimePaths.ClaudeMCPConfigFile); path != "" {
		return path
	}
	if stateDir := strings.TrimSpace(a.config.RuntimePaths.StateDir); stateDir != "" {
		return filepath.Join(stateDir, "codex-remote-claude-mcp.json")
	}
	return ""
}

func upsertEnvValue(env []string, key, value string) []string {
	key = strings.TrimSpace(key)
	if key == "" {
		return append([]string{}, env...)
	}
	entry := key + "=" + value
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, item := range env {
		k, _, ok := strings.Cut(item, "=")
		if ok && k == key {
			if !replaced {
				out = append(out, entry)
				replaced = true
			}
			continue
		}
		out = append(out, item)
	}
	if !replaced {
		out = append(out, entry)
	}
	return out
}
