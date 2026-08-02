package config

import (
	"path/filepath"
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

func TestWriteAppConfigNormalizesCodexProviders(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := DefaultAppConfig()
	cfg.Codex.Providers = []CodexProviderConfig{{
		Name:            " Team Proxy ",
		BaseURL:         " https://proxy.example/v1 ",
		APIKey:          " secret ",
		Model:           " gpt-5.5 ",
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
	if provider.ID != "team-proxy" || provider.Name != "Team Proxy" || provider.BaseURL != "https://proxy.example/v1" || provider.APIKey != " secret " {
		t.Fatalf("unexpected provider after normalization: %#v", provider)
	}
	if provider.Model != "gpt-5.5" || provider.ReasoningEffort != "xhigh" {
		t.Fatalf("expected normalized model/reasoning, got %#v", provider)
	}
}
