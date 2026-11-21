package daemon

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
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
	profile, _, err := a.resolveClaudeLaunchProfile(loaded.Config, profileID, state.ModelConfigRecord{})
	if err != nil {
		return nil, config.ClaudeRuntimeSettings{}, err
	}
	return applyResolvedClaudeLaunchProfile(env, profile)
}

func applyResolvedClaudeLaunchProfile(env []string, profile config.ClaudeProfile) ([]string, config.ClaudeRuntimeSettings, error) {
	if profile.BuiltIn {
		// Native defaults inherit provider and authentication. Explicit model
		// and effort still need to reach the child process and settings overlay.
		settings := config.ClaudeReasoningRuntimeSettings(profile.ReasoningEffort)
		if profile.Model != "" {
			settings = config.MergeClaudeRuntimeSettings(settings, config.ClaudeRuntimeSettings{Env: map[string]string{config.ClaudeModelEnv: profile.Model}})
		}
		return config.ApplyClaudeRuntimeSettingsEnv(env, settings), settings, nil
	}
	env, err := config.ApplyClaudeProfileLaunchEnv(env, profile)
	if err != nil {
		return nil, config.ClaudeRuntimeSettings{}, err
	}
	return env, config.ClaudeProfileRuntimeSettings(profile), nil
}

func (a *App) resolveClaudeLaunchProfile(cfg config.AppConfig, profileID string, overrides state.ModelConfigRecord) (config.ClaudeProfile, state.ProfileContextPreference, error) {
	profile, ok := config.ResolveClaudeProfile(cfg, profileID)
	if !ok {
		return profile, state.ProfileContextPreference{}, fmt.Errorf("claude profile %q not found", strings.TrimSpace(profileID))
	}
	preferenceStore, err := a.profileContextPreferenceStore()
	if err != nil {
		return profile, state.ProfileContextPreference{}, err
	}
	preference, ok := preferenceStore.ClaudeCurrent(profile.ID)
	if !ok {
		return profile, preference, fmt.Errorf("claude profile %q context preference not found", profile.ID)
	}
	if model := strings.TrimSpace(overrides.Model); model != "" {
		profile.Model = model
	}
	if effort := config.NormalizeClaudeReasoningEffort(overrides.ReasoningEffort); effort != "" {
		profile.ReasoningEffort = effort
	}
	if preference.Mode == state.ClaudeContextModeExtended {
		model, _ := config.SplitClaudeExtendedContextSuffix(profile.Model)
		if profile.BuiltIn && model == "" {
			model = "sonnet"
		}
		if model != "" {
			profile.Model = model + "[1m]"
		}
	}
	return profile, preference, nil
}
