package daemon

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (a *App) applyClaudeHeadlessProfileEnv(baseEnv []string, backend agentproto.Backend, profileID string) ([]string, config.ClaudeRuntimeSettings, error) {
	env := append([]string{}, baseEnv...)
	if agentproto.NormalizeBackend(backend) != agentproto.BackendClaude {
		return env, config.ClaudeRuntimeSettings{}, nil
	}

	loaded, err := a.loadAdminConfig()
	if err != nil {
		return nil, config.ClaudeRuntimeSettings{}, err
	}
	profile, ok := config.ResolveClaudeProfile(loaded.Config, profileID)
	if !ok {
		return nil, config.ClaudeRuntimeSettings{}, fmt.Errorf("claude profile %q not found", strings.TrimSpace(profileID))
	}
	env, err = config.ApplyClaudeProfileLaunchEnv(env, profile)
	if err != nil {
		return nil, config.ClaudeRuntimeSettings{}, err
	}
	return env, config.ClaudeProfileRuntimeSettings(profile), nil
}
