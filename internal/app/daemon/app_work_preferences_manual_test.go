package daemon

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestManualModelFormRetainsCurrentChoiceAndExplicitBackendBoundary(t *testing.T) {
	snapshot := worksettings.Snapshot{Settings: state.BotCapabilitySettingsRecord{Backend: agentproto.BackendCodex, CodexProfileID: state.NativeCodexProfileID, PromptOverride: state.ModelConfigRecord{Model: "custom", ReasoningEffort: "high"}}, Profiles: []worksettings.Profile{{ID: state.NativeCodexProfileID, Backend: agentproto.BackendCodex, Available: true}}}
	raw, err := teamSettingsCard("model-view", snapshot, work.ContextPreferences{}, "model", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"settings.model-manual", "manual_model", "custom high", "select_static", "由执行后端确认是否支持"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing %s in model form", want)
		}
	}
	var card struct {
		Body struct {
			Elements []teamCardElement `json:"elements"`
		} `json:"body"`
	}
	if err = json.Unmarshal(raw, &card); err != nil {
		t.Fatal(err)
	}
	values := map[string]any{"team_view": "model-view", "action": "settings.model-manual"}
	if teamFrozenCallback(card.Body.Elements, values, "settings.model-manual") == nil {
		t.Fatal("manual callback not frozen in card")
	}
	parts, err := teamSettingsSubmission(card.Body.Elements, map[string]any{"manual_model": "custom high"}, "settings.model-manual", "model")
	if err != nil || strings.Join(parts, " ") != "settings custom high" {
		t.Fatalf("manual args=%v %v", parts, err)
	}
	if _, err = teamSettingsSubmission(card.Body.Elements, map[string]any{"selection": "custom"}, "settings.apply", "model"); err == nil {
		t.Fatal("dropdown accepted unlisted manual value")
	}
	snapshot.Profiles[0].FixedModel = "fixed-model"
	raw, err = teamSettingsCard("fixed", snapshot, work.ContextPreferences{}, "model", "")
	if err != nil || strings.Contains(string(raw), "settings.model-manual") {
		t.Fatalf("fixed profile offered arbitrary model: %s %v", raw, err)
	}
}
