package config

import (
	"path/filepath"
	"testing"
)

func TestWriteAppConfigNormalizesClaudeProfiles(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := DefaultAppConfig()
	cfg.Claude.Profiles = []ClaudeProfileConfig{
		{
			ID:         " default ",
			Name:       " Proxy Profile ",
			AuthMode:   " AUTH_TOKEN ",
			BaseURL:    " https://proxy.internal/v1 ",
			AuthToken:  " secret-token ",
			Model:      " mimo-v2.5-pro ",
			SmallModel: " mimo-v2.5-haiku ",
		},
		{
			Name:     "Dev Seek",
			AuthMode: "unknown",
		},
		{
			Name: "Dev Seek",
		},
	}

	if err := WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	loaded, err := LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	profiles := loaded.Config.Claude.Profiles
	if len(profiles) != 3 {
		t.Fatalf("expected 3 normalized profiles, got %#v", profiles)
	}

	if profiles[0].ID != "default-2" || profiles[0].Name != "Proxy Profile" {
		t.Fatalf("unexpected normalized profile[0]: %#v", profiles[0])
	}
	if profiles[0].AuthMode != ClaudeAuthModeAuthToken || profiles[0].BaseURL != "https://proxy.internal/v1" || profiles[0].AuthToken != "secret-token" {
		t.Fatalf("unexpected normalized auth fields: %#v", profiles[0])
	}
	if profiles[0].Model != "mimo-v2.5-pro" || profiles[0].SmallModel != "mimo-v2.5-haiku" {
		t.Fatalf("unexpected normalized model fields: %#v", profiles[0])
	}

	if profiles[1].ID != "dev-seek" || profiles[1].AuthMode != ClaudeAuthModeInherit {
		t.Fatalf("unexpected normalized profile[1]: %#v", profiles[1])
	}
	if profiles[2].ID != "dev-seek-2" || profiles[2].Name != "Dev Seek" {
		t.Fatalf("unexpected normalized profile[2]: %#v", profiles[2])
	}
}

func TestClaudeProfileResolutionAndLaunchEnv(t *testing.T) {
	cfg := DefaultAppConfig()
	cfg.Claude.Profiles = []ClaudeProfileConfig{{
		ID:         "devseek",
		Name:       "DevSeek",
		AuthMode:   ClaudeAuthModeAuthToken,
		BaseURL:    "https://proxy.internal/v1",
		AuthToken:  "profile-token",
		Model:      "mimo-v2.5-pro",
		SmallModel: "mimo-v2.5-haiku",
	}}

	listed := ListClaudeProfiles(cfg)
	if len(listed) != 2 || !listed[0].BuiltIn || listed[0].ID != ClaudeDefaultProfileID {
		t.Fatalf("expected built-in default profile first, got %#v", listed)
	}

	defaultProfile, ok := ResolveClaudeProfile(cfg, "")
	if !ok || !defaultProfile.BuiltIn {
		t.Fatalf("expected empty profile id to resolve built-in default, got %#v ok=%t", defaultProfile, ok)
	}

	customProfile, ok := ResolveClaudeProfile(cfg, " DEVSEEK ")
	if !ok || customProfile.BuiltIn || customProfile.ID != "devseek" {
		t.Fatalf("expected custom profile resolution, got %#v ok=%t", customProfile, ok)
	}

	baseEnv := []string{
		"KEEP_ME=1",
		ClaudeConfigDirEnv + "=/tmp/old-claude",
		ClaudeBaseURLEnv + "=https://old.internal",
		ClaudeAuthTokenEnv + "=old-token",
		ClaudeModelEnv + "=old-model",
		ClaudeDefaultHaikuModelEnv + "=old-small-model",
	}
	updatedEnv, err := ApplyClaudeProfileLaunchEnv(baseEnv, customProfile, "/var/lib/codex-remote/state")
	if err != nil {
		t.Fatalf("ApplyClaudeProfileLaunchEnv(custom): %v", err)
	}

	if value, ok := lookupEnvValue(updatedEnv, "KEEP_ME"); !ok || value != "1" {
		t.Fatalf("expected unrelated env to survive, got %#v", updatedEnv)
	}
	if value, ok := lookupEnvValue(updatedEnv, ClaudeConfigDirEnv); !ok || value != "/var/lib/codex-remote/state/claude/profiles/devseek" {
		t.Fatalf("unexpected CLAUDE_CONFIG_DIR: %#v", updatedEnv)
	}
	if value, ok := lookupEnvValue(updatedEnv, ClaudeBaseURLEnv); !ok || value != "https://proxy.internal/v1" {
		t.Fatalf("unexpected base url env: %#v", updatedEnv)
	}
	if value, ok := lookupEnvValue(updatedEnv, ClaudeAuthTokenEnv); !ok || value != "profile-token" {
		t.Fatalf("unexpected auth token env: %#v", updatedEnv)
	}
	if value, ok := lookupEnvValue(updatedEnv, ClaudeModelEnv); !ok || value != "mimo-v2.5-pro" {
		t.Fatalf("unexpected model env: %#v", updatedEnv)
	}
	if value, ok := lookupEnvValue(updatedEnv, ClaudeDefaultHaikuModelEnv); !ok || value != "mimo-v2.5-haiku" {
		t.Fatalf("unexpected small model env: %#v", updatedEnv)
	}

	defaultEnv, err := ApplyClaudeProfileLaunchEnv(baseEnv, defaultProfile, "/var/lib/codex-remote/state")
	if err != nil {
		t.Fatalf("ApplyClaudeProfileLaunchEnv(default): %v", err)
	}
	if value, ok := lookupEnvValue(defaultEnv, ClaudeConfigDirEnv); !ok || value != "/tmp/old-claude" {
		t.Fatalf("expected built-in default profile to preserve current env, got %#v", defaultEnv)
	}
}
