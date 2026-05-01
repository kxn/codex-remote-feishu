package wrapper

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

const (
	feishuMCPServerID      = "codex_remote_feishu"
	feishuMCPBearerEnvName = "CODEX_REMOTE_FEISHU_MCP_BEARER"
)

type childToolServiceInfo struct {
	URL       string `json:"url"`
	Token     string `json:"token"`
	TokenType string `json:"tokenType"`
}

func (a *App) buildCodexChildLaunch(baseArgs []string) ([]string, []string) {
	args := append([]string{}, baseArgs...)
	env := childEnvWithProxy(a.config.ChildProxyEnv, args)
	args, env = a.applyCodexProviderLaunch(args, env)
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

func (a *App) readFeishuMCPPublicationInfo() (childToolServiceInfo, bool) {
	if !a.feishuMCPPublicationEligible() {
		return childToolServiceInfo{}, false
	}

	info, err := readChildToolServiceInfo(a.config.RuntimePaths.ToolServiceFile)
	if err != nil {
		a.debugf("feishu mcp publication skipped: read state failed path=%s err=%v", a.config.RuntimePaths.ToolServiceFile, err)
		return childToolServiceInfo{}, false
	}
	if strings.TrimSpace(info.URL) == "" || strings.TrimSpace(info.Token) == "" {
		a.debugf("feishu mcp publication skipped: incomplete state path=%s", a.config.RuntimePaths.ToolServiceFile)
		return childToolServiceInfo{}, false
	}
	if tokenType := strings.TrimSpace(info.TokenType); tokenType != "" && !strings.EqualFold(tokenType, "bearer") {
		a.debugf("feishu mcp publication skipped: unsupported token type=%s", tokenType)
		return childToolServiceInfo{}, false
	}
	return info, true
}

func (a *App) applyCodexProviderLaunch(baseArgs, baseEnv []string) ([]string, []string) {
	args := append([]string{}, baseArgs...)
	env := append([]string{}, baseEnv...)
	if agentproto.NormalizeBackend(a.config.Backend) != agentproto.BackendCodex {
		return args, env
	}
	loaded, err := config.LoadAppConfigAtPath(a.config.ConfigPath)
	if err != nil {
		a.debugf("codex provider launch skipped: load config failed path=%s err=%v", a.config.ConfigPath, err)
		return args, env
	}
	provider, ok := config.ResolveCodexProvider(loaded.Config, a.config.CodexProviderID)
	if !ok || provider.BuiltIn {
		return args, env
	}
	args = append(args, config.CodexProviderLaunchOverrides(provider)...)
	env = upsertEnvValue(env, config.CodexProviderAPIKeyEnv, strings.TrimSpace(provider.APIKey))
	return args, env
}

func (a *App) feishuMCPPublicationEligible() bool {
	return !strings.EqualFold(strings.TrimSpace(a.config.Source), "vscode")
}

func codexMCPOverride(field, value string) string {
	return fmt.Sprintf("mcp_servers.%s.%s=%s", feishuMCPServerID, strings.TrimSpace(field), strconv.Quote(strings.TrimSpace(value)))
}

func (a *App) writeClaudeFeishuMCPConfig(info childToolServiceInfo) (string, error) {
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
	if err := writeJSONFileAtomic(path, payload, 0o600); err != nil {
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

func readChildToolServiceInfo(path string) (childToolServiceInfo, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return childToolServiceInfo{}, fmt.Errorf("tool service state path is empty")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return childToolServiceInfo{}, err
	}
	var info childToolServiceInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return childToolServiceInfo{}, err
	}
	return info, nil
}

func writeJSONFileAtomic(path string, payload any, mode os.FileMode) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
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
	if err := tmpFile.Chmod(mode); err != nil {
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
