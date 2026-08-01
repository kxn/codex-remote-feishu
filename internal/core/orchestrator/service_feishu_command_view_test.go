package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestBuildConfigCommandViewStateRewritesLegacyVariantToSurfaceContext(t *testing.T) {
	now := time.Date(2026, 4, 28, 11, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
	surface.Backend = agentproto.BackendClaude

	flow, ok := control.FeishuConfigFlowDefinitionByCommandID(control.FeishuCommandMode)
	if !ok {
		t.Fatal("expected mode config flow")
	}
	view := svc.buildConfigCommandViewState(surface, flow, control.FeishuCatalogConfigView{
		CatalogFamilyID:  control.FeishuCommandMode,
		CatalogVariantID: "mode.default",
	})
	if view.Config == nil {
		t.Fatal("expected config view")
	}
	if view.Config.CatalogBackend != agentproto.BackendClaude {
		t.Fatalf("catalog backend = %q, want %q", view.Config.CatalogBackend, agentproto.BackendClaude)
	}
	if view.Config.CatalogVariantID != "mode.claude.normal" {
		t.Fatalf("catalog variant id = %q, want %q", view.Config.CatalogVariantID, "mode.claude.normal")
	}
}

func TestBuildConfigCommandViewStatePopulatesClaudeProfileOptions(t *testing.T) {
	now := time.Date(2026, 4, 29, 10, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendClaude, "devseek", "", "")
	svc.MaterializeClaudeProfiles([]state.ClaudeProfileRecord{
		{ID: "devseek", Name: "DevSeek"},
		{ID: "devseek-max", Name: "DevSeek"},
	})

	flow, ok := control.FeishuConfigFlowDefinitionByCommandID(control.FeishuCommandClaudeProfile)
	if !ok {
		t.Fatal("expected claude profile config flow")
	}
	view := svc.buildConfigCommandViewState(svc.root.Surfaces["surface-1"], flow, control.FeishuCatalogConfigView{})
	if view.Config == nil {
		t.Fatal("expected config view")
	}
	if view.Config.CurrentValue != "devseek" {
		t.Fatalf("current value = %q, want %q", view.Config.CurrentValue, "devseek")
	}
	if view.Config.FormDefaultValue != "devseek" {
		t.Fatalf("default value = %q, want %q", view.Config.FormDefaultValue, "devseek")
	}
	if got := view.Config.FormOptions; len(got) != 3 {
		t.Fatalf("expected default + 2 custom profiles, got %#v", got)
	} else {
		if got[0].Label != state.DefaultClaudeProfileName || got[0].Value != state.DefaultClaudeProfileID {
			t.Fatalf("unexpected built-in default option: %#v", got[0])
		}
		if got[1].Label != "DevSeek" || got[1].Value != "devseek" {
			t.Fatalf("unexpected first custom option: %#v", got[1])
		}
		if got[2].Label != "DevSeek（devseek-max）" || got[2].Value != "devseek-max" {
			t.Fatalf("unexpected second custom option: %#v", got[2])
		}
	}
}

func TestBuildConfigCommandViewStatePopulatesCodexProfileOptions(t *testing.T) {
	now := time.Date(2026, 5, 1, 10, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeWithCodexProvider("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "team-proxy", "", "", "")
	svc.MaterializeCodexProfiles([]state.CodexProfileSummary{
		{ID: state.NativeCodexProfileID, Kind: state.CodexProfileKindNative, Name: "本机默认", Available: true},
		{ID: "team-proxy", Kind: state.CodexProfileKindAPI, Name: "Team Proxy", Available: true},
		{ID: "team-proxy-2", Kind: state.CodexProfileKindAPI, Name: "Team Proxy", Available: true},
	})

	flow, ok := control.FeishuConfigFlowDefinitionByCommandID(control.FeishuCommandCodexProvider)
	if !ok {
		t.Fatal("expected codex profile config flow")
	}
	view := svc.buildConfigCommandViewState(svc.root.Surfaces["surface-1"], flow, control.FeishuCatalogConfigView{})
	if view.Config == nil {
		t.Fatal("expected config view")
	}
	if view.Config.CurrentValue != "team-proxy" {
		t.Fatalf("current value = %q, want %q", view.Config.CurrentValue, "team-proxy")
	}
	if view.Config.FormDefaultValue != "team-proxy" {
		t.Fatalf("default value = %q, want %q", view.Config.FormDefaultValue, "team-proxy")
	}
	if got := view.Config.FormOptions; len(got) != 3 {
		t.Fatalf("expected native + 2 API profiles, got %#v", got)
	} else {
		if got[0].Label != "本机默认" || got[0].Value != state.NativeCodexProfileID {
			t.Fatalf("unexpected built-in default option: %#v", got[0])
		}
		if got[1].Label != "Team Proxy（team-proxy）" || got[1].Value != "team-proxy" {
			t.Fatalf("unexpected first custom option: %#v", got[1])
		}
		if got[2].Label != "Team Proxy（team-proxy-2）" || got[2].Value != "team-proxy-2" {
			t.Fatalf("unexpected second custom option: %#v", got[2])
		}
	}
}

func TestBuildConfigCommandViewStatePopulatesCodexProfileOptionsAndUnavailableStatus(t *testing.T) {
	now := time.Date(2026, 8, 1, 10, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeWithCodexProvider("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "default", "", "", "")
	svc.MaterializeCodexProfiles([]state.CodexProfileSummary{
		{ID: state.NativeCodexProfileID, Kind: state.CodexProfileKindNative, Name: "ignored", Available: true},
		{ID: state.OAuthCodexProfileID, Kind: state.CodexProfileKindOAuth, Name: "ChatGPT 登录", Available: false, StatusCode: "missing"},
		{ID: "team-proxy", Kind: state.CodexProfileKindAPI, Name: "Team Proxy", Available: true},
	})

	flow, ok := control.FeishuConfigFlowDefinitionByCommandID(control.FeishuCommandCodexProvider)
	if !ok {
		t.Fatal("expected codex profile config flow")
	}
	view := svc.buildConfigCommandViewState(svc.root.Surfaces["surface-1"], flow, control.FeishuCatalogConfigView{})
	if view.Config == nil {
		t.Fatal("expected config view")
	}
	if view.Config.CurrentValue != state.NativeCodexProfileID || view.Config.FormDefaultValue != state.NativeCodexProfileID {
		t.Fatalf("current/default profile = %q/%q, want native", view.Config.CurrentValue, view.Config.FormDefaultValue)
	}
	if got := view.Config.FormOptions; len(got) != 2 || got[0].Value != state.NativeCodexProfileID || got[1].Value != "team-proxy" {
		t.Fatalf("expected only available profiles in submit options, got %#v", got)
	}
	if view.Config.StatusKind != "info" || !strings.Contains(view.Config.StatusText, "ChatGPT 登录：missing") {
		t.Fatalf("expected unavailable OAuth status row, got kind=%q text=%q", view.Config.StatusKind, view.Config.StatusText)
	}
	if !view.Config.FormPagination {
		t.Fatal("expected codex profile config flow to enable pagination")
	}
}

func TestApplyModelCatalogUpdatedStoresSnapshotAndPreservesOnFailure(t *testing.T) {
	now := time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-1",
		Online:     true,
		Threads:    map[string]*state.ThreadRecord{},
	})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind: agentproto.EventModelCatalogUpdated,
		ModelCatalog: &agentproto.ModelCatalogSnapshot{
			Entries: []agentproto.ModelCatalogEntry{{
				Model:                  "gpt-5.6",
				DisplayName:            "GPT 5.6",
				DefaultReasoningEffort: "high",
				SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{
					{ReasoningEffort: "low"},
					{ReasoningEffort: "high"},
				},
			}},
		},
	})
	inst := svc.Instance("inst-1")
	if inst.ModelCatalog == nil || len(inst.ModelCatalog.Entries) != 1 {
		t.Fatalf("expected model catalog snapshot, got %#v", inst.ModelCatalog)
	}
	if got := inst.ModelCatalog.Entries[0].SupportedReasoningEfforts; len(got) != 2 || got[0].ReasoningEffort != "low" || got[1].ReasoningEffort != "high" {
		t.Fatalf("expected reasoning metadata to be preserved, got %#v", got)
	}

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind: agentproto.EventModelCatalogUpdated,
		ModelCatalog: &agentproto.ModelCatalogSnapshot{
			ErrorMessage: "temporary failure",
			Unsupported:  false,
		},
	})
	if inst.ModelCatalog == nil || len(inst.ModelCatalog.Entries) != 1 || inst.ModelCatalog.ErrorMessage != "temporary failure" {
		t.Fatalf("expected failed refresh to preserve entries and record error, got %#v", inst.ModelCatalog)
	}

	svc.ApplyInstanceDisconnected("inst-1")
	if inst.ModelCatalog != nil {
		t.Fatalf("expected disconnect to clear catalog, got %#v", inst.ModelCatalog)
	}
}

func TestBuildConfigCommandViewStatePopulatesModelCatalogOptions(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	svc.root.Surfaces["surface-1"].AttachedInstanceID = "inst-1"
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-1",
		Online:     true,
		ModelCatalog: &agentproto.ModelCatalogSnapshot{
			Entries: []agentproto.ModelCatalogEntry{
				{Model: "first-model", DisplayName: "First"},
				{Model: "hidden-model", DisplayName: "Hidden", Hidden: true},
				{Model: "second-model", DisplayName: "Second"},
			},
			RefreshedAt: now,
		},
		Threads: map[string]*state.ThreadRecord{},
	})

	flow, ok := control.FeishuConfigFlowDefinitionByCommandID(control.FeishuCommandModel)
	if !ok {
		t.Fatal("expected model config flow")
	}
	view := svc.buildConfigCommandViewState(svc.root.Surfaces["surface-1"], flow, control.FeishuCatalogConfigView{})
	if view.Config == nil {
		t.Fatal("expected config view")
	}
	if got := view.Config.FormOptions; len(got) != 2 || got[0].Value != "first-model" || got[1].Value != "second-model" {
		t.Fatalf("expected visible dynamic model options in upstream order, got %#v", got)
	}
}
