package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAppConfigRejectsCorruptCodexProfileCatalog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	raw := []byte(`{
  "version": 2,
  "codex": {
    "profileCatalogMigrationVersion": 1,
    "profiles": [{
      "id": "team-proxy",
      "currentRevision": 2,
      "revisions": [{
        "id": "team-proxy",
        "revision": 1,
        "credentialGeneration": 1,
        "connectionGeneration": 1,
        "kind": "api",
        "name": "Team Proxy",
        "baseURL": "https://proxy.example/v1",
        "apiKey": "secret",
        "model": "gpt-5.5",
        "reasoningEffort": "high"
      }]
    }]
  }
}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := LoadAppConfigAtPath(path); err == nil {
		t.Fatal("LoadAppConfigAtPath() accepted a missing current profile revision")
	}
}

func TestMigrateLegacyCodexProvidersCreatesRevisionOneProfiles(t *testing.T) {
	cfg := DefaultAppConfig()
	cfg.Codex.Providers = []LegacyCodexProviderConfig{{
		ID:              "team-proxy",
		Name:            " Team Proxy ",
		BaseURL:         " https://proxy.example/v1 ",
		APIKey:          " secret with spaces ",
		Model:           "gpt-5.5",
		ReasoningEffort: "high",
	}}

	migrated, changed, diagnostics := MigrateLegacyCodexProviders(cfg)
	if !changed || len(diagnostics) != 0 {
		t.Fatalf("MigrateLegacyCodexProviders() changed=%v diagnostics=%#v", changed, diagnostics)
	}
	if len(migrated.Codex.Providers) != 0 || len(migrated.Codex.Profiles) != 1 {
		t.Fatalf("unexpected migrated catalog: %#v", migrated.Codex)
	}
	profile, ok := CurrentCodexAPIProfile(migrated.Codex.Profiles[0])
	if !ok {
		t.Fatal("CurrentCodexAPIProfile() did not return migrated revision")
	}
	if profile.ID != "team-proxy" || profile.Kind != CodexProfileKindAPI || profile.Revision != 1 {
		t.Fatalf("unexpected migrated identity: %#v", profile)
	}
	if profile.CredentialGeneration != 1 || profile.ConnectionGeneration != 1 {
		t.Fatalf("unexpected migrated generations: %#v", profile)
	}
	if profile.APIKey != " secret with spaces " {
		t.Fatalf("migrated APIKey = %q, want exact legacy value", profile.APIKey)
	}
	if status := CodexAPIProfileStatus(profile); status != "" {
		t.Fatalf("CodexAPIProfileStatus() = %q, want available definition", status)
	}
}

func TestMigrateLegacyCodexProvidersKeepsIncompleteProfileVisible(t *testing.T) {
	cfg := DefaultAppConfig()
	cfg.Codex.Providers = []LegacyCodexProviderConfig{{
		ID:      "legacy",
		Name:    "Legacy",
		BaseURL: "https://proxy.example/v1",
		APIKey:  "secret",
	}}

	migrated, changed, diagnostics := MigrateLegacyCodexProviders(cfg)
	if !changed || len(diagnostics) != 1 || diagnostics[0].Code != "profile_definition_incomplete" {
		t.Fatalf("MigrateLegacyCodexProviders() changed=%v diagnostics=%#v", changed, diagnostics)
	}
	profile, ok := CurrentCodexAPIProfile(migrated.Codex.Profiles[0])
	if !ok {
		t.Fatal("CurrentCodexAPIProfile() did not return incomplete migrated revision")
	}
	if status := CodexAPIProfileStatus(profile); status != "profile_definition_incomplete" {
		t.Fatalf("CodexAPIProfileStatus() = %q", status)
	}
}

func TestPrepareCodexAPIProfileCreateRequiresModelAndReasoning(t *testing.T) {
	for _, input := range []CodexAPIProfileInput{
		{Name: "Missing Model", BaseURL: "https://proxy.example/v1", APIKey: "secret", ReasoningEffort: "high"},
		{Name: "Missing Reasoning", BaseURL: "https://proxy.example/v1", APIKey: "secret", Model: "gpt-5.5"},
	} {
		if _, err := PrepareCodexAPIProfileCreate(nil, input); err == nil {
			t.Fatalf("PrepareCodexAPIProfileCreate(%#v) expected validation error", input)
		}
	}
}

func TestPrepareCodexAPIProfileCreateAndUpdateCarriesSubagentModel(t *testing.T) {
	created, err := PrepareCodexAPIProfileCreate(nil, CodexAPIProfileInput{
		Name:            "Team Proxy",
		BaseURL:         "https://proxy.example/v1",
		APIKey:          "secret",
		Model:           "gpt-5.5",
		SubagentModel:   "weak-model",
		ReviewModel:     "review-model",
		ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatalf("PrepareCodexAPIProfileCreate: %v", err)
	}
	createdProfile, ok := CurrentCodexAPIProfile(created)
	if !ok {
		t.Fatal("CurrentCodexAPIProfile() did not return created revision")
	}
	if createdProfile.SubagentModel != "weak-model" {
		t.Fatalf("created SubagentModel = %q, want %q", createdProfile.SubagentModel, "weak-model")
	}

	updated, changed, err := PrepareCodexAPIProfileUpdate(created, CodexAPIProfileInput{
		Name:            "Team Proxy",
		BaseURL:         "https://proxy.example/v1",
		APIKey:          "secret",
		Model:           "gpt-5.5",
		SubagentModel:   "weak-model-2",
		ReviewModel:     "review-model",
		ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatalf("PrepareCodexAPIProfileUpdate: %v", err)
	}
	if !changed {
		t.Fatal("PrepareCodexAPIProfileUpdate() expected a change for new subagent model")
	}
	updatedProfile, ok := CurrentCodexAPIProfile(updated)
	if !ok {
		t.Fatal("CurrentCodexAPIProfile() did not return updated revision")
	}
	if updatedProfile.SubagentModel != "weak-model-2" {
		t.Fatalf("updated SubagentModel = %q, want %q", updatedProfile.SubagentModel, "weak-model-2")
	}

	unchanged, changed, err := PrepareCodexAPIProfileUpdate(updated, CodexAPIProfileInput{
		Name:            "Team Proxy",
		BaseURL:         "https://proxy.example/v1",
		APIKey:          "secret",
		Model:           "gpt-5.5",
		SubagentModel:   "weak-model-2",
		ReviewModel:     "review-model",
		ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatalf("PrepareCodexAPIProfileUpdate unchanged: %v", err)
	}
	if changed {
		t.Fatal("PrepareCodexAPIProfileUpdate() expected no change for identical input")
	}
	if current, _ := CurrentCodexAPIProfile(unchanged); current.SubagentModel != "weak-model-2" {
		t.Fatalf("unchanged SubagentModel = %q, want preserved %q", current.SubagentModel, "weak-model-2")
	}
}

func TestPrepareCodexAPIProfileCreateAndUpdateCarriesInstruction(t *testing.T) {
	created, err := PrepareCodexAPIProfileCreate(nil, CodexAPIProfileInput{
		Name:            "Team Proxy",
		BaseURL:         "https://proxy.example/v1",
		APIKey:          "secret",
		Model:           "gpt-5.5",
		ReasoningEffort: "high",
		Instruction:     "你是一个乐于助人的助手。",
	})
	if err != nil {
		t.Fatalf("PrepareCodexAPIProfileCreate: %v", err)
	}
	createdProfile, ok := CurrentCodexAPIProfile(created)
	if !ok {
		t.Fatal("CurrentCodexAPIProfile() did not return created revision")
	}
	if createdProfile.Instruction != "你是一个乐于助人的助手。" {
		t.Fatalf("created Instruction = %q, want role prompt", createdProfile.Instruction)
	}

	updated, changed, err := PrepareCodexAPIProfileUpdate(created, CodexAPIProfileInput{
		Name:            "Team Proxy",
		BaseURL:         "https://proxy.example/v1",
		APIKey:          "secret",
		Model:           "gpt-5.5",
		ReasoningEffort: "high",
		Instruction:     "你是一个严谨的工程师。",
	})
	if err != nil {
		t.Fatalf("PrepareCodexAPIProfileUpdate: %v", err)
	}
	if !changed {
		t.Fatal("PrepareCodexAPIProfileUpdate() expected a change for new instruction")
	}
	updatedProfile, ok := CurrentCodexAPIProfile(updated)
	if !ok {
		t.Fatal("CurrentCodexAPIProfile() did not return updated revision")
	}
	if updatedProfile.Instruction != "你是一个严谨的工程师。" {
		t.Fatalf("updated Instruction = %q, want new role prompt", updatedProfile.Instruction)
	}

	unchanged, changed, err := PrepareCodexAPIProfileUpdate(updated, CodexAPIProfileInput{
		Name:            "Team Proxy",
		BaseURL:         "https://proxy.example/v1",
		APIKey:          "secret",
		Model:           "gpt-5.5",
		ReasoningEffort: "high",
		Instruction:     "你是一个严谨的工程师。",
	})
	if err != nil {
		t.Fatalf("PrepareCodexAPIProfileUpdate unchanged: %v", err)
	}
	if changed {
		t.Fatal("PrepareCodexAPIProfileUpdate() expected no change for identical input")
	}
	if current, _ := CurrentCodexAPIProfile(unchanged); current.Instruction != "你是一个严谨的工程师。" {
		t.Fatalf("unchanged Instruction = %q, want preserved value", current.Instruction)
	}
}

func TestPrepareCodexAPIProfileRejectsInvalidInstruction(t *testing.T) {
	base := CodexAPIProfileInput{
		Name:            "Team Proxy",
		BaseURL:         "https://proxy.example/v1",
		APIKey:          "secret",
		Model:           "gpt-5.5",
		ReasoningEffort: "high",
	}
	for _, mutate := range []func(*CodexAPIProfileInput){
		func(input *CodexAPIProfileInput) { input.Instruction = strings.Repeat("a", InstructionMaxChars+1) },
		func(input *CodexAPIProfileInput) { input.Instruction = "bad\x00instruction" },
	} {
		input := base
		mutate(&input)
		if _, err := PrepareCodexAPIProfileCreate(nil, input); err == nil {
			t.Fatalf("PrepareCodexAPIProfileCreate accepted invalid instruction %q", input.Instruction)
		}
	}
}

func TestPrepareCodexAPIProfileCreateRejectsUnicodeCaseFoldDuplicateName(t *testing.T) {
	existing, err := PrepareCodexAPIProfileCreate(nil, CodexAPIProfileInput{
		Name: "Straße", BaseURL: "https://proxy.example/v1", APIKey: "secret", Model: "gpt-5.5", ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatalf("PrepareCodexAPIProfileCreate existing: %v", err)
	}
	_, err = PrepareCodexAPIProfileCreate([]CodexAPIProfileRecord{existing}, CodexAPIProfileInput{
		Name: "STRASSE", BaseURL: "https://other.example/v1", APIKey: "secret", Model: "gpt-5.5", ReasoningEffort: "high",
	})
	if err == nil {
		t.Fatal("PrepareCodexAPIProfileCreate accepted a Unicode case-fold duplicate profile name")
	}
}

func TestPrepareCodexAPIProfileCreateRejectsUnsafeBaseURL(t *testing.T) {
	for _, baseURL := range []string{
		"proxy.example/v1",
		"ftp://proxy.example/v1",
		"https://user:pass@proxy.example/v1",
		"https://proxy.example/v1?token=secret",
		"https://proxy.example/v1#fragment",
	} {
		_, err := PrepareCodexAPIProfileCreate(nil, CodexAPIProfileInput{
			Name:            "Team Proxy",
			BaseURL:         baseURL,
			APIKey:          "secret",
			Model:           "gpt-5.5",
			ReasoningEffort: "high",
		})
		if err == nil {
			t.Fatalf("PrepareCodexAPIProfileCreate() accepted unsafe baseURL %q", baseURL)
		}
	}
}

func TestValidateCodexAPIProfileRecordsRejectsUnsafeHistoricalRevision(t *testing.T) {
	record := CodexAPIProfileRecord{
		ID:              "team-proxy",
		CurrentRevision: 2,
		Revisions: []CodexAPIProfileSecretConfig{
			{
				ID: "team-proxy", Revision: 1, CredentialGeneration: 1, ConnectionGeneration: 1,
				Kind: CodexProfileKindAPI, Name: "Team\nProxy", BaseURL: "https://proxy.example/v1",
				APIKey: "secret", Model: "gpt-5.5", ReasoningEffort: "high",
			},
			{
				ID: "team-proxy", Revision: 2, CredentialGeneration: 1, ConnectionGeneration: 1,
				Kind: CodexProfileKindAPI, Name: "Team Proxy", BaseURL: "https://proxy.example/v1",
				APIKey: "secret", Model: "gpt-5.5", ReasoningEffort: "high",
			},
		},
	}
	if err := ValidateCodexAPIProfileRecords([]CodexAPIProfileRecord{record}); err == nil {
		t.Fatal("ValidateCodexAPIProfileRecords() accepted an unsafe historical revision")
	}
}

func TestValidateCodexAPIProfileRecordsRejectsGenerationDrift(t *testing.T) {
	base := CodexAPIProfileSecretConfig{
		ID: "team-proxy", Revision: 1, CredentialGeneration: 1, ConnectionGeneration: 1,
		Kind: CodexProfileKindAPI, Name: "Team Proxy", BaseURL: "https://proxy.example/v1",
		APIKey: "old-secret", Model: "gpt-5.5", ReasoningEffort: "high",
	}
	for _, mutate := range []func(*CodexAPIProfileSecretConfig){
		func(next *CodexAPIProfileSecretConfig) { next.APIKey = "new-secret" },
		func(next *CodexAPIProfileSecretConfig) { next.BaseURL = "https://other.example/v1" },
	} {
		next := base
		next.Revision = 2
		mutate(&next)
		record := CodexAPIProfileRecord{ID: base.ID, CurrentRevision: 2, Revisions: []CodexAPIProfileSecretConfig{base, next}}
		if err := ValidateCodexAPIProfileRecords([]CodexAPIProfileRecord{record}); err == nil {
			t.Fatalf("ValidateCodexAPIProfileRecords() accepted generation drift: %#v", record)
		}
	}
}

func TestPrepareCodexAPIProfileUpdateUsesImmutableRevisions(t *testing.T) {
	record, err := PrepareCodexAPIProfileCreate(nil, CodexAPIProfileInput{
		Name:            "Team Proxy",
		BaseURL:         "https://proxy.example/v1",
		APIKey:          " secret with spaces ",
		Model:           "gpt-5.5",
		ReviewModel:     "gpt-5.5-mini",
		ReasoningEffort: "custom-effort",
	})
	if err != nil {
		t.Fatalf("PrepareCodexAPIProfileCreate: %v", err)
	}
	created, _ := CurrentCodexAPIProfile(record)
	if created.APIKey != " secret with spaces " {
		t.Fatalf("created APIKey = %q, want exact input", created.APIKey)
	}

	unchanged, changed, err := PrepareCodexAPIProfileUpdate(record, CodexAPIProfileInput{
		Name:            "Team Proxy",
		BaseURL:         "https://proxy.example/v1",
		Model:           "gpt-5.5",
		ReviewModel:     "gpt-5.5-mini",
		ReasoningEffort: "custom-effort",
	})
	if err != nil || changed {
		t.Fatalf("no-op update changed=%v err=%v", changed, err)
	}
	current, _ := CurrentCodexAPIProfile(unchanged)
	if current.Revision != 1 || current.APIKey != " secret with spaces " {
		t.Fatalf("unexpected no-op revision: %#v", current)
	}

	updated, changed, err := PrepareCodexAPIProfileUpdate(record, CodexAPIProfileInput{
		Name:            "Team Proxy",
		BaseURL:         "https://proxy.example/v1",
		APIKey:          "replacement-secret",
		Model:           "gpt-5.5",
		ReviewModel:     "gpt-5.5-mini",
		ReasoningEffort: "custom-effort",
	})
	if err != nil || !changed {
		t.Fatalf("key update changed=%v err=%v", changed, err)
	}
	current, _ = CurrentCodexAPIProfile(updated)
	if current.Revision != 2 || current.CredentialGeneration != 2 || current.ConnectionGeneration != 2 {
		t.Fatalf("unexpected key update generations: %#v", current)
	}
	if len(updated.Revisions) != 2 || updated.Revisions[0].APIKey != " secret with spaces " {
		t.Fatalf("old immutable revision was not retained: %#v", updated.Revisions)
	}
	pruned := PruneCodexAPIProfileHistory(updated, nil)
	if len(pruned.Revisions) != 1 || pruned.Revisions[0].Revision != pruned.CurrentRevision {
		t.Fatalf("current definition revision was not protected during prune: %#v", pruned)
	}
}
