package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeLegacyCodexProvidersAddsStableIDs(t *testing.T) {
	providers := NormalizeLegacyCodexProviders([]LegacyCodexProviderConfig{
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

func TestWriteAppConfigDropsLegacyCodexProviders(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := DefaultAppConfig()
	cfg.Codex.Providers = []LegacyCodexProviderConfig{{
		Name:            " Team Proxy ",
		BaseURL:         " https://proxy.example/v1 ",
		APIKey:          " secret ",
		Model:           " gpt-5.5 ",
		ReasoningEffort: " XHIGH ",
	}}

	if err := WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(raw), `"providers"`) {
		t.Fatalf("WriteAppConfig wrote legacy providers: %s", raw)
	}
	loaded, err := LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if len(loaded.Config.Codex.Providers) != 0 {
		t.Fatalf("len(loaded.Config.Codex.Providers) = %d, want 0", len(loaded.Config.Codex.Providers))
	}
}

func TestLoadAppConfigReadsLegacyCodexProvidersForMigration(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	raw := []byte(`{
  "version": 2,
  "codex": {
    "providers": [
      {
        "name": " Team Proxy ",
        "baseURL": " https://proxy.example/v1 ",
        "apiKey": " secret ",
        "model": " gpt-5.5 ",
        "reasoningEffort": " XHIGH "
      }
    ]
  }
}`)
	if err := os.WriteFile(configPath, raw, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
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
		t.Fatalf("unexpected loaded provider: %#v", provider)
	}
	if provider.Model != "gpt-5.5" || provider.ReasoningEffort != "xhigh" {
		t.Fatalf("expected normalized model/reasoning, got %#v", provider)
	}
}
