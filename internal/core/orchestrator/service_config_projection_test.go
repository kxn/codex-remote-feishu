package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestVSCodeConfigViewCarriesSharedAuthorityProjectionState(t *testing.T) {
	now := time.Date(2026, 5, 3, 17, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Source:                  "vscode",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:        agentproto.EventConfigObserved,
		ThreadID:    "thread-1",
		CWD:         "/data/dl/droid",
		ConfigScope: "thread",
		PlanMode:    "on",
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionPlanCommand, SurfaceSessionID: "surface-1", Text: "/plan clear"})

	view := svc.buildConfigCommandView(svc.root.Surfaces["surface-1"], control.FeishuCommandPlan)
	if view.Config == nil {
		t.Fatal("expected config view")
	}
	if !view.Config.UsesLocalRequestedOverrides {
		t.Fatalf("expected vscode config view to carry shared-authority flag, got %#v", view.Config)
	}
	if view.Config.PlanModeOverrideSet {
		t.Fatalf("expected /plan clear to report no explicit plan override, got %#v", view.Config)
	}
	if view.Config.EffectiveValue != "on" || view.Config.CurrentValue != "off" {
		t.Fatalf("expected observed plan plus cleared local surface projection, got %#v", view.Config)
	}
}
