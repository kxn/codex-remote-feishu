package worksettings

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestExplicitManualModelKeepsBackendAuthorityAndKnownLimits(t *testing.T) {
	model, effort := "custom-model", "high"
	for _, tt := range []struct {
		name    string
		manual  bool
		catalog *agentproto.ModelCatalogSnapshot
		wantErr bool
	}{
		{name: "manual with no catalog", manual: true},
		{name: "dropdown with no catalog", wantErr: true},
		{name: "manual absent from catalog", manual: true, catalog: &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{ID: "different", Model: "different"}}}},
		{name: "dropdown absent from catalog", catalog: &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{ID: "different", Model: "different"}}}, wantErr: true},
		{name: "manual known supported", manual: true, catalog: &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{ID: model, Model: model, SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{{ReasoningEffort: "high"}}}}}},
		{name: "manual known unsupported", manual: true, catalog: &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{ID: model, Model: model, SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{{ReasoningEffort: "low"}}}}}, wantErr: true},
		{name: "hidden model", manual: true, catalog: &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{ID: model, Model: model, Hidden: true}}}, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			record, _ := state.NormalizeBotCapabilitySettingsRecord(state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendCodex, CodexProfileID: state.NativeCodexProfileID})
			snapshot := Snapshot{Settings: record, Profiles: []Profile{{ID: state.NativeCodexProfileID, Backend: agentproto.BackendCodex, Available: true}}, CatalogBackend: agentproto.BackendCodex, CatalogProfileID: state.NativeCodexProfileID, Catalog: tt.catalog}
			got, err := nextSettings(snapshot, UpdateRequest{Model: &model, Reasoning: &effort, ManualSelection: tt.manual})
			if (err != nil) != tt.wantErr {
				t.Fatalf("selection error=%v wantErr=%v", err, tt.wantErr)
			}
			if err == nil && (got.PromptOverride.Model != model || got.PromptOverride.ReasoningEffort != effort) {
				t.Fatalf("explicit choice was changed: %+v", got.PromptOverride)
			}
		})
	}
}

func TestManualModelDoesNotBypassFixedProfileOrOpenCode(t *testing.T) {
	for _, backend := range []agentproto.Backend{agentproto.BackendCodex, agentproto.BackendOpenCode} {
		record, _ := state.NormalizeBotCapabilitySettingsRecord(state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: backend, CodexProfileID: "fixed", OpenCodeProfileID: "fixed"})
		snapshot := Snapshot{Settings: record, Profiles: []Profile{{ID: "fixed", Backend: backend, Available: true}}}
		if backend == agentproto.BackendCodex {
			snapshot.Profiles[0].FixedModel = "fixed-model"
		}
		custom := "custom-model"
		if _, err := nextSettings(snapshot, UpdateRequest{Model: &custom, ManualSelection: true}); err == nil {
			t.Fatalf("manual selection bypassed %s profile restriction", backend)
		}
	}
}

func TestManualModelWriteKeepsSourceCASAndUnknownReplay(t *testing.T) {
	service, port, source := settingsFixture(t)
	model, effort := "explicit-unlisted", "high"
	request := UpdateRequest{Key: "manual", RoleID: "role", ExpectedFingerprint: port.snapshot.Fingerprint, Model: &model, Reasoning: &effort, ManualSelection: true}
	if _, err := service.UpdateBot(t.Context(), "impostor", source, request); err == nil || port.calls != 0 {
		t.Fatal("manual input bypassed source authority")
	}
	port.failAfter = true
	first, err := service.UpdateBot(t.Context(), "human", source, request)
	if err == nil || first.Status != "pending_external" || port.calls != 1 {
		t.Fatalf("manual uncertain write=%+v %v", first, err)
	}
	replay, err := service.UpdateBot(t.Context(), "human", source, request)
	if err != nil || !replay.Replay || port.calls != 1 {
		t.Fatalf("manual unknown was repeated: %+v %v", replay, err)
	}
	if port.snapshot.Settings.PromptOverride.Model != model {
		t.Fatal("manual chosen model was silently changed")
	}
	request.ManualSelection = false
	if _, err = service.UpdateBot(t.Context(), "human", source, request); err == nil {
		t.Fatal("replayed request changed explicit-intent identity")
	}
}

func TestManualReasoningUsesActualDefaultModelLimits(t *testing.T) {
	_, port, _ := settingsFixture(t)
	port.snapshot.Catalog.Entries[0].SupportedReasoningEfforts = []agentproto.ReasoningEffortOption{{ReasoningEffort: "low"}}
	effort := "high"
	if _, err := nextSettings(port.snapshot, UpdateRequest{Reasoning: &effort, ManualSelection: true}); err == nil {
		t.Fatal("manual reasoning ignored current default model limits")
	}
	effort = "low"
	got, err := nextSettings(port.snapshot, UpdateRequest{Reasoning: &effort, ManualSelection: true})
	if err != nil || got.PromptOverride.Model != "" || got.PromptOverride.ReasoningEffort != "low" {
		t.Fatalf("manual reasoning changed default-model choice: %+v %v", got.PromptOverride, err)
	}
}
