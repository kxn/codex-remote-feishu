package daemon

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
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
	profileID := state.CodexProfileIDFromLegacyProviderID(providerID)
	if profileID != config.CodexNativeProfileID && len(loaded.Config.Codex.Profiles) > 0 {
		index := config.IndexOfCodexAPIProfile(loaded.Config.Codex.Profiles, profileID)
		if index < 0 {
			return nil, nil, fmt.Errorf("codex profile %q not found", profileID)
		}
		profile, ok := config.CurrentCodexAPIProfile(loaded.Config.Codex.Profiles[index])
		if !ok {
			return nil, nil, fmt.Errorf("profile_revision_unavailable: codex profile %q current revision is missing", profileID)
		}
		if status := config.CodexAPIProfileStatus(profile); status != "" {
			return nil, nil, fmt.Errorf("%s: codex profile %q is not launchable", status, profileID)
		}
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
