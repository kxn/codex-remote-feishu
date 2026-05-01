package wrapper

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

func TestBuildCodexChildLaunchAddsConfiguredCodexProviderOverrides(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.DefaultAppConfig()
	cfg.Codex.Providers = []config.CodexProviderConfig{{
		ID:      "team-proxy",
		Name:    "Team Proxy",
		BaseURL: "https://proxy.example/v1",
		APIKey:  "provider-secret",
	}}
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	app := New(Config{
		Backend:         "codex",
		CodexProviderID: "team-proxy",
		ConfigPath:      configPath,
	})

	args, env := app.buildCodexChildLaunch([]string{"app-server"})
	if !strings.Contains(strings.Join(args, "\n"), `model_provider="team-proxy"`) {
		t.Fatalf("expected provider override args, got %#v", args)
	}
	if got := lookupEnv(env, config.CodexProviderAPIKeyEnv); got != "provider-secret" {
		t.Fatalf("expected provider secret env, got %q", got)
	}
}
