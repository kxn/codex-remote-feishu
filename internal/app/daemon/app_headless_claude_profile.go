package daemon

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (a *App) applyClaudeHeadlessProfileEnv(baseEnv []string, backend agentproto.Backend, profileID string) ([]string, error) {
	env := append([]string{}, baseEnv...)
	if agentproto.NormalizeBackend(backend) != agentproto.BackendClaude {
		return env, nil
	}

	loaded, err := a.loadAdminConfig()
	if err != nil {
		return nil, err
	}
	profile, ok := config.ResolveClaudeProfile(loaded.Config, profileID)
	if !ok {
		return nil, fmt.Errorf("claude profile %q not found", strings.TrimSpace(profileID))
	}
	return config.ApplyClaudeProfileLaunchEnv(env, profile)
}
