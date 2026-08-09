package opencodeprofile

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
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
	if strings.Join(material.Args, "\x00") != strings.Join([]string{"acp", "--cwd", "/repo"}, "\x00") {
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

func TestCompilerAPIProfileProjectsOverlayAndRedactsSecrets(t *testing.T) {
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
		RuntimeDir:    "/tmp/codex-remote/opencode/op_team",
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
	if value, ok := lookupEnv(material.Env, "XDG_DATA_HOME"); !ok || value != "/tmp/codex-remote/opencode/op_team/data" {
		t.Fatalf("unexpected XDG_DATA_HOME: %#v", material.Env)
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
	if _, ok := configDoc["permission"]; ok {
		t.Fatalf("unsupported permission mode must not be written to OpenCode config overlay: %#v", configDoc)
	}
	provider, ok := configDoc["provider"].(map[string]any)["codex_remote_opencode_op_team"].(map[string]any)
	if !ok {
		t.Fatalf("missing generated provider config: %#v", configDoc)
	}
	models, ok := provider["models"].(map[string]any)
	if !ok {
		t.Fatalf("missing provider model metadata: %#v", provider)
	}
	model, ok := models["kimi-k2"].(map[string]any)
	if !ok || model["name"] != "kimi-k2" || model["tool_call"] != true {
		t.Fatalf("unexpected generated model metadata: %#v", models)
	}
	variants, ok := model["variants"].(map[string]any)
	if !ok {
		t.Fatalf("missing generated reasoning variants: %#v", model)
	}
	if _, ok := variants["high"]; !ok {
		t.Fatalf("reasoning effort was not represented as a model variant: %#v", variants)
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

func TestCompilerAPIProfileMapsSupportedPermissionMode(t *testing.T) {
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
	permission, ok := configDoc["permission"].(map[string]any)
	if !ok || permission["*"] != "ask" {
		t.Fatalf("permission mode = %#v, want ask in %#v", configDoc["permission"], configDoc)
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
