package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAppConfigRejectsCorruptOpenCodeProfileCatalog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	raw := []byte(`{
  "version": 2,
  "openCode": {
    "profiles": [{
      "id": "op_team",
      "currentRevision": 2,
      "revisions": [{
        "id": "op_team",
        "revision": 1,
        "credentialGeneration": 1,
        "connectionGeneration": 1,
        "name": "Team OpenCode",
        "baseURL": "https://proxy.example/v1",
        "apiKey": "secret",
        "model": "kimi-k2"
      }]
    }]
  }
}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := LoadAppConfigAtPath(path); err == nil {
		t.Fatal("LoadAppConfigAtPath() accepted a missing current OpenCode profile revision")
	}
}

func TestWriteAppConfigNormalizesAndResolvesOpenCodeProfiles(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := DefaultAppConfig()
	cfg.OpenCode.BinaryPath = " /opt/opencode "
	cfg.OpenCode.Profiles = []OpenCodeAPIProfileRecord{{
		ID:              " op_team ",
		CurrentRevision: 1,
		Revisions: []OpenCodeAPIProfileSecretConfig{{
			ID:                   "op_team",
			Revision:             1,
			CredentialGeneration: 1,
			ConnectionGeneration: 1,
			Name:                 " Team OpenCode ",
			BaseURL:              " https://proxy.example/v1 ",
			APIKey:               "secret",
			Model:                " kimi-k2 ",
			SmallModel:           " kimi-small ",
			ReviewModel:          " kimi-review ",
			SubagentModel:        " kimi-subagent ",
			Instruction:          "  keep answers concise  ",
			ReasoningEffort:      " HIGH ",
			ProjectConfigMode:    " DISABLE ",
			DataIsolationMode:    " PROCESS ",
			PermissionMode:       " plan ",
		}},
	}}

	if err := WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	loaded, err := LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if loaded.Config.OpenCode.BinaryPath != "/opt/opencode" {
		t.Fatalf("OpenCode.BinaryPath = %q", loaded.Config.OpenCode.BinaryPath)
	}
	profiles := loaded.Config.OpenCode.Profiles
	if len(profiles) != 1 {
		t.Fatalf("expected one normalized profile, got %#v", profiles)
	}
	current, ok := CurrentOpenCodeAPIProfile(profiles[0])
	if !ok {
		t.Fatal("CurrentOpenCodeAPIProfile() did not return current revision")
	}
	if current.ID != "op_team" || current.Name != "Team OpenCode" || current.BaseURL != "https://proxy.example/v1" {
		t.Fatalf("unexpected normalized identity fields: %#v", current)
	}
	if current.Model != "kimi-k2" || current.SmallModel != "kimi-small" || current.ReviewModel != "kimi-review" || current.SubagentModel != "kimi-subagent" {
		t.Fatalf("unexpected normalized model fields: %#v", current)
	}
	if current.Instruction != "keep answers concise" || current.ReasoningEffort != "high" {
		t.Fatalf("unexpected normalized instruction/reasoning: %#v", current)
	}
	if current.ProjectConfigMode != OpenCodeProjectConfigDisable || current.DataIsolationMode != OpenCodeDataIsolationProcess || current.PermissionMode != "plan" {
		t.Fatalf("unexpected normalized modes: %#v", current)
	}

	listed := ListOpenCodeProfiles(loaded.Config)
	if len(listed) != 2 || !listed[0].BuiltIn || listed[0].ID != OpenCodeDefaultProfileID {
		t.Fatalf("expected built-in OpenCode profile first, got %#v", listed)
	}
	defaultProfile, ok := ResolveOpenCodeProfile(loaded.Config, "")
	if !ok || !defaultProfile.BuiltIn {
		t.Fatalf("expected empty OpenCode profile id to resolve built-in default, got %#v ok=%t", defaultProfile, ok)
	}
	customProfile, ok := ResolveOpenCodeProfile(loaded.Config, " OP_TEAM ")
	if !ok || customProfile.BuiltIn || customProfile.ID != "op_team" {
		t.Fatalf("expected custom OpenCode profile resolution, got %#v ok=%t", customProfile, ok)
	}
}

func TestPrepareOpenCodeAPIProfileCreateAndUpdateTracksGenerations(t *testing.T) {
	created, err := PrepareOpenCodeAPIProfileCreate(nil, OpenCodeAPIProfileInput{
		Name:            "Team OpenCode",
		BaseURL:         "https://proxy.example/v1",
		APIKey:          "secret-v1",
		Model:           "kimi-k2",
		SmallModel:      "kimi-small",
		ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatalf("PrepareOpenCodeAPIProfileCreate: %v", err)
	}
	current, ok := CurrentOpenCodeAPIProfile(created)
	if !ok {
		t.Fatal("CurrentOpenCodeAPIProfile() did not return created revision")
	}
	if !strings.HasPrefix(current.ID, "op_") || current.Revision != 1 || current.CredentialGeneration != 1 || current.ConnectionGeneration != 1 {
		t.Fatalf("unexpected created profile identity: %#v", current)
	}

	modelUpdated, changed, err := PrepareOpenCodeAPIProfileUpdate(created, OpenCodeAPIProfileInput{
		Name:            "Team OpenCode",
		BaseURL:         "https://proxy.example/v1",
		Model:           "kimi-k2-pro",
		SmallModel:      "kimi-small",
		ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatalf("PrepareOpenCodeAPIProfileUpdate(model): %v", err)
	}
	if !changed {
		t.Fatal("expected model update to create a new revision")
	}
	current, _ = CurrentOpenCodeAPIProfile(modelUpdated)
	if current.APIKey != "secret-v1" || current.CredentialGeneration != 1 || current.ConnectionGeneration != 1 {
		t.Fatalf("model update should preserve secret generations, got %#v", current)
	}

	keyUpdated, changed, err := PrepareOpenCodeAPIProfileUpdate(modelUpdated, OpenCodeAPIProfileInput{
		Name:            "Team OpenCode",
		BaseURL:         "https://proxy.example/v1",
		APIKey:          "secret-v2",
		Model:           "kimi-k2-pro",
		SmallModel:      "kimi-small",
		ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatalf("PrepareOpenCodeAPIProfileUpdate(key): %v", err)
	}
	if !changed {
		t.Fatal("expected key update to create a new revision")
	}
	current, _ = CurrentOpenCodeAPIProfile(keyUpdated)
	if current.CredentialGeneration != 2 || current.ConnectionGeneration != 2 || current.APIKey != "secret-v2" {
		t.Fatalf("key update should advance credential and connection generations, got %#v", current)
	}

	baseURLUpdated, changed, err := PrepareOpenCodeAPIProfileUpdate(keyUpdated, OpenCodeAPIProfileInput{
		Name:            "Team OpenCode",
		BaseURL:         "https://proxy2.example/v1",
		Model:           "kimi-k2-pro",
		SmallModel:      "kimi-small",
		ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatalf("PrepareOpenCodeAPIProfileUpdate(baseURL): %v", err)
	}
	if !changed {
		t.Fatal("expected baseURL update to create a new revision")
	}
	current, _ = CurrentOpenCodeAPIProfile(baseURLUpdated)
	if current.CredentialGeneration != 2 || current.ConnectionGeneration != 3 || current.APIKey != "secret-v2" {
		t.Fatalf("baseURL update should advance only connection generation, got %#v", current)
	}
}
