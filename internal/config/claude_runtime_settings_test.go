package config

import "testing"

func TestClaudeProfileRuntimeSettings(t *testing.T) {
	custom := ClaudeProfile{
		ClaudeProfileConfig: ClaudeProfileConfig{
			ID:              "devseek",
			Name:            "DevSeek",
			AuthMode:        ClaudeAuthModeAuthToken,
			BaseURL:         "https://proxy.internal/v1",
			AuthToken:       "profile-token",
			Model:           "mimo-v2.5-pro",
			SmallModel:      "mimo-v2.5-haiku",
			ReasoningEffort: "high",
		},
	}
	settings := ClaudeProfileRuntimeSettings(custom)

	assertRuntimeSettingEnvValue(t, settings, ClaudeBaseURLEnv, "https://proxy.internal/v1")
	assertRuntimeSettingEnvValue(t, settings, ClaudeAuthTokenEnv, "profile-token")
	assertRuntimeSettingEnvValue(t, settings, ClaudeModelEnv, "mimo-v2.5-pro")
	assertRuntimeSettingEnvValue(t, settings, ClaudeDefaultHaikuModelEnv, "mimo-v2.5-haiku")
	assertRuntimeSettingEnvValue(t, settings, ClaudeEffortLevelEnv, "high")
	assertRuntimeSettingEnvValue(t, settings, ClaudeDisableAdaptiveEnv, "1")
	assertRuntimeSettingEnvValue(t, settings, ClaudeDisableThinkingEnv, "")

	builtIn := ClaudeProfileRuntimeSettings(BuiltInClaudeProfile())
	if !builtIn.Empty() {
		t.Fatalf("expected built-in profile to produce no managed runtime settings, got %#v", builtIn)
	}
}

func TestClaudeReasoningRuntimeSettings(t *testing.T) {
	high := ClaudeReasoningRuntimeSettings(" HIGH ")
	assertRuntimeSettingEnvValue(t, high, ClaudeEffortLevelEnv, "high")
	assertRuntimeSettingEnvValue(t, high, ClaudeDisableAdaptiveEnv, "1")
	assertRuntimeSettingEnvValue(t, high, ClaudeDisableThinkingEnv, "")

	medium := ClaudeReasoningRuntimeSettings("medium")
	assertRuntimeSettingEnvValue(t, medium, ClaudeEffortLevelEnv, "medium")
	assertRuntimeSettingEnvValue(t, medium, ClaudeDisableAdaptiveEnv, "")
	assertRuntimeSettingEnvValue(t, medium, ClaudeDisableThinkingEnv, "")

	empty := ClaudeReasoningRuntimeSettings("")
	if !empty.Empty() {
		t.Fatalf("expected empty reasoning runtime settings for unset effort, got %#v", empty)
	}
}

func TestMergeClaudeRuntimeSettingsOverlayWins(t *testing.T) {
	base := ClaudeRuntimeSettings{
		Env: map[string]string{
			ClaudeModelEnv:       "old-model",
			ClaudeBaseURLEnv:     "https://old.internal/v1",
			ClaudeEffortLevelEnv: "medium",
		},
	}
	overlay := ClaudeRuntimeSettings{
		Env: map[string]string{
			ClaudeModelEnv:           "new-model",
			ClaudeDisableAdaptiveEnv: "1",
		},
	}
	merged := MergeClaudeRuntimeSettings(base, overlay)

	assertRuntimeSettingEnvValue(t, merged, ClaudeModelEnv, "new-model")
	assertRuntimeSettingEnvValue(t, merged, ClaudeBaseURLEnv, "https://old.internal/v1")
	assertRuntimeSettingEnvValue(t, merged, ClaudeEffortLevelEnv, "medium")
	assertRuntimeSettingEnvValue(t, merged, ClaudeDisableAdaptiveEnv, "1")

	base.Env[ClaudeModelEnv] = "mutated-base"
	overlay.Env[ClaudeDisableAdaptiveEnv] = "mutated-overlay"
	assertRuntimeSettingEnvValue(t, merged, ClaudeModelEnv, "new-model")
	assertRuntimeSettingEnvValue(t, merged, ClaudeDisableAdaptiveEnv, "1")
}

func assertRuntimeSettingEnvValue(t *testing.T, settings ClaudeRuntimeSettings, key, want string) {
	t.Helper()
	if got, ok := settings.Env[key]; !ok || got != want {
		t.Fatalf("runtime setting env %q = %q, %t; want %q", key, got, ok, want)
	}
}
