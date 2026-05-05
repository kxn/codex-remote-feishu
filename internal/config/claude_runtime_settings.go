package config

import (
	"encoding/json"
	"strings"
)

type ClaudeRuntimeSettings struct {
	Env map[string]string `json:"env,omitempty"`
}

func (s ClaudeRuntimeSettings) Empty() bool {
	return len(s.Env) == 0
}

func MergeClaudeRuntimeSettings(base, overlay ClaudeRuntimeSettings) ClaudeRuntimeSettings {
	if base.Empty() {
		return cloneClaudeRuntimeSettings(overlay)
	}
	if overlay.Empty() {
		return cloneClaudeRuntimeSettings(base)
	}
	merged := ClaudeRuntimeSettings{Env: map[string]string{}}
	for key, value := range base.Env {
		merged.Env[key] = value
	}
	for key, value := range overlay.Env {
		merged.Env[key] = value
	}
	return merged
}

func MarshalClaudeRuntimeSettings(settings ClaudeRuntimeSettings) (string, error) {
	raw, err := json.Marshal(settings)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func ApplyClaudeRuntimeSettingsEnv(baseEnv []string, settings ClaudeRuntimeSettings) []string {
	env := append([]string{}, baseEnv...)
	if settings.Empty() {
		return env
	}
	for key, value := range settings.Env {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if value == "" {
			env = removeEnvKeys(env, key)
			continue
		}
		env = upsertEnvValue(env, key, value)
	}
	return env
}

func ClaudeProfileRuntimeSettings(profile ClaudeProfile) ClaudeRuntimeSettings {
	if profile.BuiltIn {
		return ClaudeRuntimeSettings{}
	}
	settings := ClaudeRuntimeSettings{Env: map[string]string{}}
	if value := strings.TrimSpace(profile.BaseURL); value != "" {
		settings.Env[ClaudeBaseURLEnv] = value
	}
	if NormalizeClaudeAuthMode(profile.AuthMode) == ClaudeAuthModeAuthToken {
		if value := strings.TrimSpace(profile.AuthToken); value != "" {
			settings.Env[ClaudeAuthTokenEnv] = value
		}
	}
	if value := strings.TrimSpace(profile.Model); value != "" {
		settings.Env[ClaudeModelEnv] = value
	}
	if value := strings.TrimSpace(profile.SmallModel); value != "" {
		settings.Env[ClaudeDefaultHaikuModelEnv] = value
	}
	return MergeClaudeRuntimeSettings(settings, ClaudeReasoningRuntimeSettings(profile.ReasoningEffort))
}

func ClaudeReasoningRuntimeSettings(effort string) ClaudeRuntimeSettings {
	effort = NormalizeClaudeReasoningEffort(effort)
	if effort == "" {
		return ClaudeRuntimeSettings{}
	}
	settings := ClaudeRuntimeSettings{
		Env: map[string]string{
			ClaudeEffortLevelEnv:     effort,
			ClaudeDisableThinkingEnv: "",
		},
	}
	if effort == "high" || effort == "max" {
		settings.Env[ClaudeDisableAdaptiveEnv] = "1"
	} else {
		settings.Env[ClaudeDisableAdaptiveEnv] = ""
	}
	return settings
}

func cloneClaudeRuntimeSettings(settings ClaudeRuntimeSettings) ClaudeRuntimeSettings {
	if settings.Empty() {
		return ClaudeRuntimeSettings{}
	}
	cloned := ClaudeRuntimeSettings{Env: map[string]string{}}
	for key, value := range settings.Env {
		cloned.Env[key] = value
	}
	return cloned
}
