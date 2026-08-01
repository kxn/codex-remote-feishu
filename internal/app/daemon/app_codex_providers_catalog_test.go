package daemon

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestMaterializeCodexProfileSummariesIncludesAPIModelPolicy(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Codex.Profiles = []config.CodexAPIProfileRecord{{
		ID:              "cp_deepseek",
		CurrentRevision: 2,
		Revisions: []config.CodexAPIProfileSecretConfig{{
			ID:                   "cp_deepseek",
			Revision:             2,
			CredentialGeneration: 1,
			ConnectionGeneration: 1,
			Kind:                 state.CodexProfileKindAPI,
			Name:                 "DeepseekV4Flash",
			BaseURL:              "https://sub.example/v1",
			APIKey:               "secret",
			Model:                "deepseek-v4-flash",
			ReviewModel:          "deepseek-v4-flash-review",
			ReasoningEffort:      "high",
		}},
	}}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")

	profiles := app.materializeCodexProfileSummariesLocked(cfg)
	var got state.CodexProfileSummary
	for _, profile := range profiles {
		if profile.ID == "cp_deepseek" {
			got = profile
			break
		}
	}
	if got.ID == "" {
		t.Fatalf("profile not materialized: %#v", profiles)
	}
	if got.Model != "deepseek-v4-flash" || got.ReviewModel != "deepseek-v4-flash-review" || got.ReasoningEffort != "high" {
		t.Fatalf("profile model policy was not materialized: %#v", got)
	}
}
