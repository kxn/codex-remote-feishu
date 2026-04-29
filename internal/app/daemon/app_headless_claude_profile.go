package daemon

import (
	"fmt"
	"os"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (a *App) applyClaudeHeadlessProfileEnv(baseEnv []string, backend agentproto.Backend, profileID, stateDir string) ([]string, error) {
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
	env, err = config.ApplyClaudeProfileLaunchEnv(env, profile, stateDir)
	if err != nil {
		return nil, err
	}
	if profile.BuiltIn {
		return env, nil
	}
	if err := os.MkdirAll(config.ClaudeProfileRuntimeConfigDir(stateDir, profile.ID), 0o700); err != nil {
		return nil, err
	}
	return env, nil
}
