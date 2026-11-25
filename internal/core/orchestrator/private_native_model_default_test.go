package orchestrator

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateCodexUnspecifiedModelUsesNativeDefault(t *testing.T) {
	for _, private := range []bool{false, true} {
		s := NewService(nil, Config{PrivateApplication: private}, nil)
		s.MaterializeSurface("surface", "gateway", "chat", "user")
		v := s.Surface("surface")
		inst := &state.InstanceRecord{InstanceID: "instance", Backend: agentproto.BackendCodex, Source: "headless", Managed: true, Online: true, Threads: map[string]*state.ThreadRecord{"thread": {ThreadID: "thread", CWD: "/work"}}}
		s.UpsertInstance(inst)
		res := s.resolvePromptConfig(inst, v, "", "/work", state.ModelConfigRecord{})
		frozen := s.resolveFrozenPromptOverride(inst, v, "", "/work", state.ModelConfigRecord{})
		if private {
			if res.EffectiveModel.Value != "" || res.EffectiveModel.Source != "native_default" || frozen.Model != "" {
				t.Fatalf("native default not preserved: %+v %+v", res, frozen)
			}
		} else if res.EffectiveModel.Value != defaultModel || frozen.Model != defaultModel {
			t.Fatalf("legacy fallback changed: %+v %+v", res, frozen)
		}
		explicit := s.resolveFrozenPromptOverride(inst, v, "", "/work", state.ModelConfigRecord{Model: "chosen-model"})
		if explicit.Model != "chosen-model" {
			t.Fatalf("explicit model lost: %+v", explicit)
		}
		inst.Threads["thread"].ExplicitModel = "observed-model"
		observed := s.resolveFrozenPromptOverride(inst, v, "thread", "/work", state.ModelConfigRecord{})
		if observed.Model != "observed-model" {
			t.Fatalf("confirmed thread model lost: %+v", observed)
		}
	}
}

func TestPrivateNativeModelDefaultDisplay(t *testing.T) {
	if got := displayConfigValue("", "native_default"); got != "跟随原生配置" {
		t.Fatalf("default display=%q", got)
	}
	if got := displayConfigValue("explicit", "native_default"); got != "explicit" {
		t.Fatalf("explicit display=%q", got)
	}
}

func TestPrivateCodexAPIProfileKeepsConfiguredModel(t *testing.T) {
	s := NewService(nil, Config{PrivateApplication: true}, nil)
	s.MaterializeCodexProfiles([]state.CodexProfileSummary{{ID: "api-profile", Kind: state.CodexProfileKindAPI, Name: "API", BaseURL: "https://example.invalid", Model: "profile-model", ReasoningEffort: "high", Available: true}})
	s.MaterializeSurfaceResumeWithCodexProfile("surface", "gateway", "chat", "user", state.ProductModeNormal, agentproto.BackendCodex, "api-profile", "", "", "")
	v := s.Surface("surface")
	inst := &state.InstanceRecord{InstanceID: "instance", Backend: agentproto.BackendCodex, Source: "headless", Managed: true, Online: true}
	got := s.resolveFrozenPromptOverride(inst, v, "", "/work", state.ModelConfigRecord{})
	if got.Model != "profile-model" {
		t.Fatalf("API profile model lost: %+v", got)
	}
}
