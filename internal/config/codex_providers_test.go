package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeCodexProvidersAddsStableIDs(t *testing.T) {
	providers := NormalizeCodexProviders([]CodexProviderConfig{
		{Name: "Team Alpha", BaseURL: "https://alpha.example/v1", APIKey: "alpha-key"},
		{Name: "Team Alpha", BaseURL: "https://alpha-2.example/v1", APIKey: "alpha-key-2"},
	})
	if len(providers) != 2 {
		t.Fatalf("len(providers) = %d, want 2", len(providers))
	}
	if providers[0].ID != "team-alpha" {
		t.Fatalf("provider[0].ID = %q, want team-alpha", providers[0].ID)
	}
	if providers[1].ID != "team-alpha-2" {
		t.Fatalf("provider[1].ID = %q, want team-alpha-2", providers[1].ID)
	}
}

func TestPrepareCodexProviderCreateRejectsReservedNames(t *testing.T) {
	for _, tc := range []CodexProviderConfig{
		{Name: "OpenAI", BaseURL: "https://proxy.example/v1", APIKey: "secret"},
		{ID: "openai", Name: "Proxy", BaseURL: "https://proxy.example/v1", APIKey: "secret"},
		{Name: "Default", ID: "default", BaseURL: "https://proxy.example/v1", APIKey: "secret"},
	} {
		if _, err := PrepareCodexProviderCreate(nil, tc); err == nil {
			t.Fatalf("PrepareCodexProviderCreate(%+v) expected error", tc)
		}
	}
}

func TestPrepareCodexProviderCreateRequiresAPIKey(t *testing.T) {
	_, err := PrepareCodexProviderCreate(nil, CodexProviderConfig{
		Name:    "Proxy",
		BaseURL: "https://proxy.example/v1",
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "apikey") {
		t.Fatalf("expected apiKey required error, got %v", err)
	}
}

func TestPrepareCodexProviderUpdatePreservesExistingAPIKey(t *testing.T) {
	existing := []CodexProviderConfig{{
		ID:              "proxy",
		Name:            "Proxy",
		BaseURL:         "https://proxy.example/v1",
		APIKey:          "saved-secret",
		Model:           "gpt-5.4",
		ReasoningEffort: "high",
	}}
	provider, err := PrepareCodexProviderUpdate(existing, "proxy", CodexProviderConfig{
		Name:            "Proxy",
		BaseURL:         "https://proxy-2.example/v1",
		Model:           "gpt-5.5",
		ReasoningEffort: "xhigh",
	})
	if err != nil {
		t.Fatalf("PrepareCodexProviderUpdate: %v", err)
	}
	if provider.APIKey != "saved-secret" {
		t.Fatalf("provider.APIKey = %q, want saved-secret", provider.APIKey)
	}
	if provider.BaseURL != "https://proxy-2.example/v1" {
		t.Fatalf("provider.BaseURL = %q", provider.BaseURL)
	}
	if provider.Model != "gpt-5.5" || provider.ReasoningEffort != "xhigh" {
		t.Fatalf("expected model/reasoning update, got %#v", provider)
	}
}

func TestWriteAppConfigNormalizesCodexProviders(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := DefaultAppConfig()
	cfg.Codex.Providers = []CodexProviderConfig{{
		Name:            " Team Proxy ",
		BaseURL:         " https://proxy.example/v1 ",
		APIKey:          " secret ",
		Model:           " gpt-5.4 ",
		ReasoningEffort: " XHIGH ",
	}}

	if err := WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	loaded, err := LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if len(loaded.Config.Codex.Providers) != 1 {
		t.Fatalf("len(loaded.Config.Codex.Providers) = %d, want 1", len(loaded.Config.Codex.Providers))
	}
	provider := loaded.Config.Codex.Providers[0]
	if provider.ID != "team-proxy" || provider.Name != "Team Proxy" || provider.BaseURL != "https://proxy.example/v1" || provider.APIKey != "secret" {
		t.Fatalf("unexpected provider after normalization: %#v", provider)
	}
	if provider.Model != "gpt-5.4" || provider.ReasoningEffort != "xhigh" {
		t.Fatalf("expected normalized model/reasoning, got %#v", provider)
	}
}

func TestCodexProviderLaunchOverridesForCustomProvider(t *testing.T) {
	overrides := CodexProviderLaunchOverrides(CodexProvider{
		CodexProviderConfig: CodexProviderConfig{
			ID:              "team-proxy",
			Name:            "Team Proxy",
			BaseURL:         "https://proxy.example/v1",
			Model:           "gpt-5.4",
			ReasoningEffort: "high",
		},
	})
	want := []string{
		"-c", `model_provider="team-proxy"`,
		"-c", `model_providers.team-proxy.name="Team Proxy"`,
		"-c", `model_providers.team-proxy.base_url="https://proxy.example/v1"`,
		"-c", `model_providers.team-proxy.env_key="CODEX_REMOTE_CODEX_PROVIDER_API_KEY"`,
		"-c", `model="gpt-5.4"`,
		"-c", `model_reasoning_effort="high"`,
	}
	if strings.Join(overrides, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("CodexProviderLaunchOverrides() = %#v, want %#v", overrides, want)
	}
}

func TestPrepareCodexProviderCreateRejectsInvalidReasoningEffort(t *testing.T) {
	_, err := PrepareCodexProviderCreate(nil, CodexProviderConfig{
		Name:            "Proxy",
		BaseURL:         "https://proxy.example/v1",
		APIKey:          "secret",
		ReasoningEffort: "turbo",
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "reasoningeffort") {
		t.Fatalf("expected reasoningEffort error, got %v", err)
	}
}
