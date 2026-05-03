package daemon

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

func TestMaterializeClaudeProfileRecordsCarriesSystemReasoningDefault(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Claude.Profiles = []config.ClaudeProfileConfig{
		{
			ID:   "devseek",
			Name: "DevSeek",
		},
		{
			ID:              "maxseek",
			Name:            "MaxSeek",
			ReasoningEffort: "max",
		},
	}

	records := materializeClaudeProfileRecords(cfg, " HIGH ")

	if len(records) != 3 {
		t.Fatalf("expected default + two custom profiles, got %#v", records)
	}
	if records[0].ID != config.ClaudeDefaultProfileID || records[0].ReasoningEffort != "high" {
		t.Fatalf("expected built-in default to carry system reasoning, got %#v", records[0])
	}
	if records[1].ID != "devseek" || records[1].ReasoningEffort != "high" {
		t.Fatalf("expected unset custom profile to inherit system reasoning, got %#v", records[1])
	}
	if records[2].ID != "maxseek" || records[2].ReasoningEffort != "max" {
		t.Fatalf("expected explicit profile reasoning to win, got %#v", records[2])
	}
}

func TestClaudeSystemReasoningEffortFromEnvUsesLastValidValue(t *testing.T) {
	env := []string{
		config.ClaudeEffortLevelEnv + "=medium",
		"OTHER=1",
		config.ClaudeEffortLevelEnv + "=MAX",
	}

	if got := claudeSystemReasoningEffortFromEnv(env); got != "max" {
		t.Fatalf("expected last valid env value, got %q", got)
	}
}
