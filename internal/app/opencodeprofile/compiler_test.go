package opencodeprofile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/pathcanon"
)

func TestCompilerBuiltInProfileInheritsSystemOpenCodeConfig(t *testing.T) {
	material, err := CompileLaunchMaterial(CompileInput{
		Profile:       config.BuiltInOpenCodeProfile(),
		WorkspaceRoot: "/repo",
		BaseEnv: []string{
			"KEEP_ME=1",
			config.OpenCodeConfigContentEnv + "=old-config",
			config.OpenCodeAuthContentEnv + "=old-auth",
			config.OpenCodeDisableProjectConfigEnv + "=1",
		},
	})
	if err != nil {
		t.Fatalf("CompileLaunchMaterial(default): %v", err)
	}
	if strings.Join(material.Args, "\x00") != strings.Join([]string{"acp", "--cwd", pathcanon.Native("/repo")}, "\x00") {
		t.Fatalf("unexpected default args: %#v", material.Args)
	}
	if value, ok := lookupEnv(material.Env, "KEEP_ME"); !ok || value != "1" {
		t.Fatalf("expected unrelated env to survive, got %#v", material.Env)
	}
	for _, key := range []string{config.OpenCodeConfigContentEnv, config.OpenCodeAuthContentEnv, config.OpenCodeDisableProjectConfigEnv} {
		if _, ok := lookupEnv(material.Env, key); ok {
			t.Fatalf("built-in profile should inherit system OpenCode config and clear stale %s, got %#v", key, material.Env)
		}
	}
	if material.AdmissionRef == nil || material.AdmissionRef.ProfileRef.ID != state.DefaultOpenCodeProfileID || material.AdmissionRef.ProfileRef.Revision != 1 {
		t.Fatalf("unexpected built-in admission ref: %#v", material.AdmissionRef)
	}
}

func TestCompilerBuiltInProfileProjectsRecentSystemModelForACP(t *testing.T) {
	root := t.TempDir()
	configHome := filepath.Join(root, ".config")
	stateHome := filepath.Join(root, ".local", "state")
	if err := os.MkdirAll(filepath.Join(configHome, "opencode"), 0o755); err != nil {
		t.Fatalf("MkdirAll config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(stateHome, "opencode"), 0o755); err != nil {
		t.Fatalf("MkdirAll state: %v", err)
	}
	configRaw := []byte(`{
  "$schema": "https://opencode.ai/config.json",
  // No top-level model: ACP should inherit the recent TUI selection.
  "provider": {
    "mimo": {
      "models": {
        "mimo-v2.5-pro": { "name": "mimo-v2.5-pro" },
      },
    },
  },
}`)
	if err := os.WriteFile(filepath.Join(configHome, "opencode", "opencode.jsonc"), configRaw, 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}
	stateRaw := []byte(`{
  "recent": [
    { "providerID": "mimo", "modelID": "mimo-v2.5-pro" },
    { "providerID": "mimo", "modelID": "mimo-v2.5" }
  ]
}`)
	if err := os.WriteFile(filepath.Join(stateHome, "opencode", "model.json"), stateRaw, 0o644); err != nil {
		t.Fatalf("WriteFile state: %v", err)
	}

	material, err := CompileLaunchMaterial(CompileInput{
		Profile:       config.BuiltInOpenCodeProfile(),
		WorkspaceRoot: "/repo",
		BaseEnv: []string{
			"KEEP_ME=1",
			"XDG_CONFIG_HOME=" + configHome,
			"XDG_STATE_HOME=" + stateHome,
			config.OpenCodeConfigContentEnv + "=old-config",
			config.OpenCodeAuthContentEnv + "=old-auth",
			config.OpenCodeDisableProjectConfigEnv + "=1",
		},
	})
	if err != nil {
		t.Fatalf("CompileLaunchMaterial(default): %v", err)
	}
	configOverlayRaw, ok := lookupEnv(material.Env, config.OpenCodeConfigContentEnv)
	if !ok {
		t.Fatalf("missing projected recent model config overlay in %#v", material.Env)
	}
	var configDoc map[string]any
	if err := json.Unmarshal([]byte(configOverlayRaw), &configDoc); err != nil {
		t.Fatalf("config overlay is not JSON: %v\n%s", err, configOverlayRaw)
	}
	if configDoc["model"] != "mimo/mimo-v2.5-pro" {
		t.Fatalf("projected model = %#v, want mimo/mimo-v2.5-pro in %#v", configDoc["model"], configDoc)
	}
	if _, ok := lookupEnv(material.Env, config.OpenCodeAuthContentEnv); ok {
		t.Fatalf("built-in profile must not project auth overlay, got %#v", material.Env)
	}
	if _, ok := lookupEnv(material.Env, config.OpenCodeDisableProjectConfigEnv); ok {
		t.Fatalf("built-in profile must not retain project config disable overlay, got %#v", material.Env)
	}
	if value, ok := lookupEnv(material.Env, "KEEP_ME"); !ok || value != "1" {
		t.Fatalf("expected unrelated env to survive, got %#v", material.Env)
	}
}

func TestCompilerBuiltInProfileProjectsRuntimeAccessMode(t *testing.T) {
	material, err := CompileLaunchMaterial(CompileInput{
		Profile:           config.BuiltInOpenCodeProfile(),
		WorkspaceRoot:     "/repo",
		RuntimeAccessMode: "confirm",
		BaseEnv: []string{
			"KEEP_ME=1",
			config.OpenCodeConfigContentEnv + "=old-config",
			config.OpenCodeAuthContentEnv + "=old-auth",
			config.OpenCodeDisableProjectConfigEnv + "=1",
		},
	})
	if err != nil {
		t.Fatalf("CompileLaunchMaterial(default): %v", err)
	}
	configOverlayRaw, ok := lookupEnv(material.Env, config.OpenCodeConfigContentEnv)
	if !ok {
		t.Fatalf("missing projected runtime access config overlay in %#v", material.Env)
	}
	var configDoc map[string]any
	if err := json.Unmarshal([]byte(configOverlayRaw), &configDoc); err != nil {
		t.Fatalf("config overlay is not JSON: %v\n%s", err, configOverlayRaw)
	}
	permission, ok := configDoc["permission"].(map[string]any)
	if !ok || permission["*"] != "ask" {
		t.Fatalf("runtime access mode permission = %#v, want ask in %#v", configDoc["permission"], configDoc)
	}
	if _, ok := lookupEnv(material.Env, config.OpenCodeAuthContentEnv); ok {
		t.Fatalf("built-in profile must not project auth overlay, got %#v", material.Env)
	}
	if _, ok := lookupEnv(material.Env, config.OpenCodeDisableProjectConfigEnv); ok {
		t.Fatalf("built-in profile must not retain project config disable overlay, got %#v", material.Env)
	}
	if value, ok := lookupEnv(material.Env, "KEEP_ME"); !ok || value != "1" {
		t.Fatalf("expected unrelated env to survive, got %#v", material.Env)
	}
}

func TestCompilerBuiltInProfileMergesRecentModelWithRuntimeAccessMode(t *testing.T) {
	root := t.TempDir()
	configHome := filepath.Join(root, ".config")
	stateHome := filepath.Join(root, ".local", "state")
	if err := os.MkdirAll(filepath.Join(configHome, "opencode"), 0o755); err != nil {
		t.Fatalf("MkdirAll config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(stateHome, "opencode"), 0o755); err != nil {
		t.Fatalf("MkdirAll state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configHome, "opencode", "opencode.jsonc"), []byte(`{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "mimo": {
      "models": {
        "mimo-v2.5-pro": { "name": "mimo-v2.5-pro" }
      }
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateHome, "opencode", "model.json"), []byte(`{"recent":[{"providerID":"mimo","modelID":"mimo-v2.5-pro"}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile state: %v", err)
	}

	material, err := CompileLaunchMaterial(CompileInput{
		Profile:           config.BuiltInOpenCodeProfile(),
		WorkspaceRoot:     "/repo",
		RuntimeAccessMode: "confirm",
		BaseEnv: []string{
			"XDG_CONFIG_HOME=" + configHome,
			"XDG_STATE_HOME=" + stateHome,
		},
	})
	if err != nil {
		t.Fatalf("CompileLaunchMaterial(default): %v", err)
	}
	configOverlayRaw, ok := lookupEnv(material.Env, config.OpenCodeConfigContentEnv)
	if !ok {
		t.Fatalf("missing projected config overlay in %#v", material.Env)
	}
	var configDoc map[string]any
	if err := json.Unmarshal([]byte(configOverlayRaw), &configDoc); err != nil {
		t.Fatalf("config overlay is not JSON: %v\n%s", err, configOverlayRaw)
	}
	if configDoc["model"] != "mimo/mimo-v2.5-pro" {
		t.Fatalf("projected model = %#v, want mimo/mimo-v2.5-pro in %#v", configDoc["model"], configDoc)
	}
	permission, ok := configDoc["permission"].(map[string]any)
	if !ok || permission["*"] != "ask" {
		t.Fatalf("runtime access mode permission = %#v, want ask in %#v", configDoc["permission"], configDoc)
	}
}

func TestCompilerBuiltInProfileKeepsExplicitSystemModelAuthoritative(t *testing.T) {
	root := t.TempDir()
	configHome := filepath.Join(root, ".config")
	stateHome := filepath.Join(root, ".local", "state")
	if err := os.MkdirAll(filepath.Join(configHome, "opencode"), 0o755); err != nil {
		t.Fatalf("MkdirAll config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(stateHome, "opencode"), 0o755); err != nil {
		t.Fatalf("MkdirAll state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configHome, "opencode", "opencode.jsonc"), []byte(`{"model":"mimo/mimo-v2.5"}`), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateHome, "opencode", "model.json"), []byte(`{"recent":[{"providerID":"mimo","modelID":"mimo-v2.5-pro"}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile state: %v", err)
	}

	material, err := CompileLaunchMaterial(CompileInput{
		Profile: config.BuiltInOpenCodeProfile(),
		BaseEnv: []string{
			"XDG_CONFIG_HOME=" + configHome,
			"XDG_STATE_HOME=" + stateHome,
		},
	})
	if err != nil {
		t.Fatalf("CompileLaunchMaterial(default): %v", err)
	}
	if _, ok := lookupEnv(material.Env, config.OpenCodeConfigContentEnv); ok {
		t.Fatalf("explicit system model should remain authoritative without overlay, got %#v", material.Env)
	}
}

func TestCompilerAPIProfileProjectsOverlayAndRedactsSecrets(t *testing.T) {
	runtimeDir := filepath.Join(string(filepath.Separator), "tmp", "codex-remote", "opencode", "op_team")
	profile := config.OpenCodeProfile{
		OpenCodeAPIProfileSecretConfig: config.OpenCodeAPIProfileSecretConfig{
			ID:                   "op_team",
			Revision:             7,
			CredentialGeneration: 3,
			ConnectionGeneration: 4,
			Name:                 "Team OpenCode",
			BaseURL:              "https://proxy.example/v1",
			APIKey:               "secret-token",
			Model:                "kimi-k2",
			SmallModel:           "kimi-small",
			ReviewModel:          "kimi-review",
			SubagentModel:        "kimi-subagent",
			Instruction:          "be precise",
			ReasoningEffort:      "high",
			ProjectConfigMode:    config.OpenCodeProjectConfigDisable,
			DataIsolationMode:    config.OpenCodeDataIsolationProcess,
			PermissionMode:       "plan",
		},
	}
	material, err := CompileLaunchMaterial(CompileInput{
		Profile:       profile,
		WorkspaceRoot: "/repo",
		RuntimeDir:    runtimeDir,
		BaseEnv: []string{
			"KEEP_ME=1",
			config.OpenCodeConfigContentEnv + "=old-config",
			config.OpenCodeAuthContentEnv + "=old-auth",
			config.OpenCodeDisableProjectConfigEnv + "=old-disable",
			"XDG_DATA_HOME=/old-data",
			"XDG_STATE_HOME=/old-state",
			"XDG_CACHE_HOME=/old-cache",
		},
	})
	if err != nil {
		t.Fatalf("CompileLaunchMaterial(api): %v", err)
	}
	if value, ok := lookupEnv(material.Env, config.OpenCodeDisableProjectConfigEnv); !ok || value != "1" {
		t.Fatalf("expected project config disable env, got %#v", material.Env)
	}
	for _, tc := range []struct {
		key  string
		want string
	}{
		{key: "XDG_DATA_HOME", want: filepath.Join(runtimeDir, "data")},
		{key: "XDG_STATE_HOME", want: filepath.Join(runtimeDir, "state")},
		{key: "XDG_CACHE_HOME", want: filepath.Join(runtimeDir, "cache")},
	} {
		if value, ok := lookupEnv(material.Env, tc.key); !ok || value != tc.want {
			t.Fatalf("%s = %q, want %q in %#v", tc.key, value, tc.want, material.Env)
		}
	}
	configRaw, ok := lookupEnv(material.Env, config.OpenCodeConfigContentEnv)
	if !ok {
		t.Fatalf("missing %s in %#v", config.OpenCodeConfigContentEnv, material.Env)
	}
	authRaw, ok := lookupEnv(material.Env, config.OpenCodeAuthContentEnv)
	if !ok {
		t.Fatalf("missing %s in %#v", config.OpenCodeAuthContentEnv, material.Env)
	}
	if strings.Contains(configRaw, "secret-token") {
		t.Fatalf("config overlay leaked API key: %s", configRaw)
	}
	if !strings.Contains(authRaw, "secret-token") {
		t.Fatalf("auth overlay did not include profile API key: %s", authRaw)
	}
	var authDoc map[string]any
	if err := json.Unmarshal([]byte(authRaw), &authDoc); err != nil {
		t.Fatalf("auth overlay is not JSON: %v\n%s", err, authRaw)
	}
	authProvider, ok := authDoc["codex_remote_opencode_op_team"].(map[string]any)
	if !ok {
		t.Fatalf("auth overlay must be keyed by provider id, got %#v", authDoc)
	}
	if authProvider["type"] != "api" || authProvider["key"] != "secret-token" {
		t.Fatalf("unexpected auth provider overlay: %#v", authProvider)
	}
	if _, ok := authDoc["provider"]; ok {
		t.Fatalf("auth overlay used stale nested provider shape: %#v", authDoc)
	}

	var configDoc map[string]any
	if err := json.Unmarshal([]byte(configRaw), &configDoc); err != nil {
		t.Fatalf("config overlay is not JSON: %v\n%s", err, configRaw)
	}
	if configDoc["model"] != "codex_remote_opencode_op_team/kimi-k2" {
		t.Fatalf("unexpected model projection: %#v", configDoc)
	}
	if configDoc["small_model"] != "codex_remote_opencode_op_team/kimi-small" {
		t.Fatalf("unexpected small model projection: %#v", configDoc)
	}
	if _, ok := configDoc["review_model"]; ok {
		t.Fatalf("OpenCode 1.18.15 has no top-level review_model overlay: %#v", configDoc)
	}
	if _, ok := configDoc["subagent_model"]; ok {
		t.Fatalf("OpenCode 1.18.15 has no top-level subagent_model overlay: %#v", configDoc)
	}
	if _, ok := configDoc["permission"]; ok {
		t.Fatalf("unsupported permission mode must not be written to OpenCode config overlay: %#v", configDoc)
	}
	agent, ok := configDoc["agent"].(map[string]any)
	if !ok {
		t.Fatalf("missing subagent model agent overrides: %#v", configDoc)
	}
	for _, agentName := range []string{"general", "explore"} {
		entry, ok := agent[agentName].(map[string]any)
		if !ok || entry["model"] != "codex_remote_opencode_op_team/kimi-subagent" {
			t.Fatalf("agent.%s model override = %#v in %#v", agentName, agent[agentName], agent)
		}
	}
	provider, ok := configDoc["provider"].(map[string]any)["codex_remote_opencode_op_team"].(map[string]any)
	if !ok {
		t.Fatalf("missing generated provider config: %#v", configDoc)
	}
	models, ok := provider["models"].(map[string]any)
	if !ok {
		t.Fatalf("missing provider model metadata: %#v", provider)
	}
	for _, modelID := range []string{"kimi-k2", "kimi-small", "kimi-subagent"} {
		if _, ok := models[modelID].(map[string]any); !ok {
			t.Fatalf("missing generated metadata for %s: %#v", modelID, models)
		}
	}
	model, ok := models["kimi-k2"].(map[string]any)
	if !ok || model["name"] != "kimi-k2" || model["tool_call"] != true {
		t.Fatalf("unexpected generated model metadata: %#v", models)
	}
	variants, ok := model["variants"].(map[string]any)
	if !ok {
		t.Fatalf("missing generated reasoning variants: %#v", model)
	}
	for _, effort := range []string{"low", "medium", "high", "xhigh", "max"} {
		if _, ok := variants[effort]; !ok {
			t.Fatalf("reasoning effort %q was not represented as a model variant: %#v", effort, variants)
		}
	}
	if _, ok := configDoc["reasoning"]; ok {
		t.Fatalf("OpenCode 1.18.15 rejects top-level reasoning config: %#v", configDoc)
	}
	options, ok := provider["options"].(map[string]any)
	if !ok || options["baseURL"] != "https://proxy.example/v1" {
		t.Fatalf("unexpected provider options: %#v", provider)
	}
	if material.RedactedSummary == "" || strings.Contains(material.RedactedSummary, "secret-token") {
		t.Fatalf("redacted summary missing or leaked secret: %q", material.RedactedSummary)
	}
	if material.AdmissionRef == nil || material.AdmissionRef.ProfileRef.ID != "op_team" || material.AdmissionRef.ProfileRef.Revision != 7 {
		t.Fatalf("unexpected admission ref: %#v", material.AdmissionRef)
	}
}

func TestCompilerAPIProfileIgnoresLegacyPermissionMode(t *testing.T) {
	material, err := CompileLaunchMaterial(CompileInput{
		Profile: config.OpenCodeProfile{
			OpenCodeAPIProfileSecretConfig: config.OpenCodeAPIProfileSecretConfig{
				ID:             "op_team",
				Revision:       7,
				Name:           "Team OpenCode",
				BaseURL:        "https://proxy.example/v1",
				APIKey:         "secret-token",
				Model:          "kimi-k2",
				PermissionMode: " ASK ",
			},
		},
		WorkspaceRoot: "/repo",
	})
	if err != nil {
		t.Fatalf("CompileLaunchMaterial(api): %v", err)
	}
	configRaw, ok := lookupEnv(material.Env, config.OpenCodeConfigContentEnv)
	if !ok {
		t.Fatalf("missing %s in %#v", config.OpenCodeConfigContentEnv, material.Env)
	}
	var configDoc map[string]any
	if err := json.Unmarshal([]byte(configRaw), &configDoc); err != nil {
		t.Fatalf("config overlay is not JSON: %v\n%s", err, configRaw)
	}
	if _, ok := configDoc["permission"]; ok {
		t.Fatalf("legacy profile permission mode must not be projected into OpenCode config: %#v", configDoc)
	}
}

func TestCompilerGoogleGeminiProfileProjectsGoogleProvider(t *testing.T) {
	profile := config.OpenCodeProfile{
		OpenCodeAPIProfileSecretConfig: config.OpenCodeAPIProfileSecretConfig{
			ID:           "op_gemini",
			Revision:     3,
			Name:         "Gemini",
			ProviderType: " GOOGLE_GEMINI ",
			APIKey:       "gemini-secret",
			Model:        "gemini-2.5-pro",
			SmallModel:   "gemini-2.5-flash",
		},
	}
	material, err := CompileLaunchMaterial(CompileInput{
		Profile:       profile,
		WorkspaceRoot: "/repo",
	})
	if err != nil {
		t.Fatalf("CompileLaunchMaterial(gemini): %v", err)
	}
	configRaw, ok := lookupEnv(material.Env, config.OpenCodeConfigContentEnv)
	if !ok {
		t.Fatalf("missing %s in %#v", config.OpenCodeConfigContentEnv, material.Env)
	}
	authRaw, ok := lookupEnv(material.Env, config.OpenCodeAuthContentEnv)
	if !ok {
		t.Fatalf("missing %s in %#v", config.OpenCodeAuthContentEnv, material.Env)
	}
	if strings.Contains(configRaw, "gemini-secret") {
		t.Fatalf("config overlay leaked API key: %s", configRaw)
	}
	var configDoc map[string]any
	if err := json.Unmarshal([]byte(configRaw), &configDoc); err != nil {
		t.Fatalf("config overlay is not JSON: %v\n%s", err, configRaw)
	}
	if configDoc["model"] != "codex_remote_opencode_op_gemini/gemini-2.5-pro" {
		t.Fatalf("unexpected model projection: %#v", configDoc)
	}
	provider, ok := configDoc["provider"].(map[string]any)["codex_remote_opencode_op_gemini"].(map[string]any)
	if !ok {
		t.Fatalf("missing generated provider config: %#v", configDoc)
	}
	if provider["npm"] != "@ai-sdk/google" {
		t.Fatalf("provider npm = %#v, want @ai-sdk/google in %#v", provider["npm"], provider)
	}
	options, ok := provider["options"].(map[string]any)
	if ok {
		if _, exists := options["baseURL"]; exists {
			t.Fatalf("Gemini provider without baseURL must not project baseURL option: %#v", provider)
		}
	}
	var authDoc map[string]any
	if err := json.Unmarshal([]byte(authRaw), &authDoc); err != nil {
		t.Fatalf("auth overlay is not JSON: %v\n%s", err, authRaw)
	}
	authProvider, ok := authDoc["codex_remote_opencode_op_gemini"].(map[string]any)
	if !ok {
		t.Fatalf("auth overlay must be keyed by provider id, got %#v", authDoc)
	}
	if authProvider["type"] != "api" || authProvider["key"] != "gemini-secret" {
		t.Fatalf("unexpected auth provider overlay: %#v", authProvider)
	}
}

func TestCompilerGoogleGeminiProfileProjectsOptionalBaseURLOverride(t *testing.T) {
	profile := config.OpenCodeProfile{
		OpenCodeAPIProfileSecretConfig: config.OpenCodeAPIProfileSecretConfig{
			ID:           "op_gemini",
			Revision:     3,
			Name:         "Gemini",
			ProviderType: config.OpenCodeProviderTypeGoogleGemini,
			BaseURL:      "https://generativelanguage.googleapis.com/v1beta",
			APIKey:       "gemini-secret",
			Model:        "gemini-2.5-pro",
		},
	}
	material, err := CompileLaunchMaterial(CompileInput{Profile: profile})
	if err != nil {
		t.Fatalf("CompileLaunchMaterial(gemini): %v", err)
	}
	configRaw, ok := lookupEnv(material.Env, config.OpenCodeConfigContentEnv)
	if !ok {
		t.Fatalf("missing %s in %#v", config.OpenCodeConfigContentEnv, material.Env)
	}
	var configDoc map[string]any
	if err := json.Unmarshal([]byte(configRaw), &configDoc); err != nil {
		t.Fatalf("config overlay is not JSON: %v\n%s", err, configRaw)
	}
	provider, ok := configDoc["provider"].(map[string]any)["codex_remote_opencode_op_gemini"].(map[string]any)
	if !ok {
		t.Fatalf("missing generated provider config: %#v", configDoc)
	}
	options, ok := provider["options"].(map[string]any)
	if !ok || options["baseURL"] != "https://generativelanguage.googleapis.com/v1beta" {
		t.Fatalf("Gemini baseURL override options = %#v, want baseURL override", provider["options"])
	}
}

func TestCompilerRuntimeAccessModeProjectsPermissionOverlay(t *testing.T) {
	material, err := CompileLaunchMaterial(CompileInput{
		Profile: config.OpenCodeProfile{
			OpenCodeAPIProfileSecretConfig: config.OpenCodeAPIProfileSecretConfig{
				ID:       "op_team",
				Revision: 7,
				Name:     "Team OpenCode",
				BaseURL:  "https://proxy.example/v1",
				APIKey:   "secret-token",
				Model:    "kimi-k2",
			},
		},
		WorkspaceRoot:     "/repo",
		RuntimeAccessMode: "confirm",
	})
	if err != nil {
		t.Fatalf("CompileLaunchMaterial(api): %v", err)
	}
	configRaw, ok := lookupEnv(material.Env, config.OpenCodeConfigContentEnv)
	if !ok {
		t.Fatalf("missing %s in %#v", config.OpenCodeConfigContentEnv, material.Env)
	}
	var configDoc map[string]any
	if err := json.Unmarshal([]byte(configRaw), &configDoc); err != nil {
		t.Fatalf("config overlay is not JSON: %v\n%s", err, configRaw)
	}
	permission, ok := configDoc["permission"].(map[string]any)
	if !ok || permission["*"] != "ask" {
		t.Fatalf("runtime access mode permission = %#v, want ask in %#v", configDoc["permission"], configDoc)
	}
}

func lookupEnv(env []string, key string) (string, bool) {
	for _, entry := range env {
		currentKey, value, ok := strings.Cut(entry, "=")
		if ok && currentKey == key {
			return value, true
		}
	}
	return "", false
}
