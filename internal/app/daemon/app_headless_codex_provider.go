package daemon

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (a *App) applyCodexHeadlessProviderConfig(baseEnv, baseArgs []string, backend agentproto.Backend, providerID string) ([]string, []string, error) {
	env := append([]string{}, baseEnv...)
	args := append([]string{}, baseArgs...)
	if agentproto.NormalizeBackend(backend) != agentproto.BackendCodex {
		return env, args, nil
	}

	loaded, err := a.loadAdminConfig()
	if err != nil {
		return nil, nil, err
	}
	provider, ok := config.ResolveCodexProvider(loaded.Config, providerID)
	if !ok {
		return nil, nil, fmt.Errorf("codex provider %q not found", strings.TrimSpace(providerID))
	}
	if provider.BuiltIn {
		return env, args, nil
	}
	env = config.UpsertEnvValue(env, config.CodexProviderAPIKeyEnv, strings.TrimSpace(provider.APIKey))
	args = append(args, config.CodexProviderLaunchOverrides(provider)...)
	return env, args, nil
}
