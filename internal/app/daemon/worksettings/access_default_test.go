package worksettings

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"testing"
)

func TestModelOrProfileSelectionPreservesExplicitPrivateAccessDefault(t *testing.T) {
	for _, access := range []string{agentproto.AccessModeConfirm, agentproto.AccessModeFullAccess, agentproto.AccessModeAcceptEdits} {
		snapshot := Snapshot{Settings: state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendCodex, CodexProfileID: state.NativeCodexProfileID, PlanMode: state.PlanModeSettingOn, PlanModeOverrideSet: true, PromptOverride: state.ModelConfigRecord{AccessMode: access}}, Profiles: []Profile{{ID: state.NativeCodexProfileID, Backend: agentproto.BackendCodex, Available: true}, {ID: "fixed", Backend: agentproto.BackendCodex, Available: true, FixedModel: "fixed-model"}}, CatalogBackend: agentproto.BackendCodex, CatalogProfileID: state.NativeCodexProfileID, Catalog: &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{ID: "actual-model", Model: "actual-model"}}}}
		model := "actual-model"
		changed, err := nextSettings(snapshot, UpdateRequest{Model: &model})
		if err != nil {
			t.Fatal(err)
		}
		if changed.PlanMode != state.PlanModeSettingOn || !changed.PlanModeOverrideSet {
			t.Fatal("selection erased explicit private plan default")
		}
		if changed.PromptOverride.AccessMode != access {
			t.Fatal("model change erased private access default")
		}
		profile := "fixed"
		changed, err = nextSettings(snapshot, UpdateRequest{ProfileID: &profile})
		if err != nil {
			t.Fatal(err)
		}
		if changed.PlanMode != state.PlanModeSettingOn || !changed.PlanModeOverrideSet {
			t.Fatal("selection erased explicit private plan default")
		}
		if changed.PromptOverride.AccessMode != access {
			t.Fatal("profile change erased private access default")
		}
	}
}
