package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestAutoWhipDispatchInheritsParentMessagePreset(t *testing.T) {
	now := time.Date(2026, 8, 26, 10, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.PromptOverride.AccessMode = agentproto.AccessModeConfirm
	surface.CodexPromptOverride = state.CodexPromptOverrideRecord{Model: "gpt-6-sol", ReasoningEffort: "high"}
	parent := &state.QueueItemRecord{
		CodexMessagePreset: "terra",
		FrozenOverride: state.ModelConfigRecord{
			Model:           "gpt-5.6-terra",
			ReasoningEffort: "high",
			AccessMode:      agentproto.AccessModeConfirm,
		},
		ReplyToMessageID: "msg-1",
	}

	svc.scheduleAutoWhip(surface, parent, "turn-1", state.AutoWhipReasonIncompleteStop)
	now = now.Add(3 * time.Second)
	svc.maybeDispatchPendingAutoWhip(surface, now)

	item := surface.QueueItems[surface.ActiveQueueItemID]
	if item == nil {
		t.Fatal("expected autowhip queue item")
	}
	if item.CodexMessagePreset != "terra" || item.FrozenOverride != parent.FrozenOverride {
		t.Fatalf("expected autowhip to inherit parent preset and override, got preset=%q override=%#v", item.CodexMessagePreset, item.FrozenOverride)
	}
	if surface.AutoWhip.PendingPreset != "" || surface.AutoWhip.PendingOverride != (state.ModelConfigRecord{}) {
		t.Fatalf("expected dispatched pending preset state to clear, got %#v", surface.AutoWhip)
	}
}

func TestAutoWhipMissingMessagePresetInheritsCodexDefault(t *testing.T) {
	now := time.Date(2026, 8, 26, 10, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.PromptOverride.AccessMode = agentproto.AccessModeConfirm
	surface.AutoWhip.PendingReason = state.AutoWhipReasonIncompleteStop
	surface.AutoWhip.PendingDueAt = now

	svc.maybeDispatchPendingAutoWhip(surface, now)

	item := surface.QueueItems[surface.ActiveQueueItemID]
	if item == nil {
		t.Fatal("expected autowhip queue item")
	}
	want := state.ModelConfigRecord{AccessMode: agentproto.AccessModeConfirm}
	if item.CodexMessagePreset != "" || item.FrozenOverride != want {
		t.Fatalf("expected native Codex defaults with current access, got preset=%q override=%#v", item.CodexMessagePreset, item.FrozenOverride)
	}
}

func TestAutoWhipMissingMessagePresetDoesNotForceGPT(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*Service, *state.SurfaceConsoleRecord)
	}{
		{
			name: "fixed Codex profile",
			setup: func(svc *Service, surface *state.SurfaceConsoleRecord) {
				svc.MaterializeCodexProfiles([]state.CodexProfileSummary{{ID: "fixed", Kind: state.CodexProfileKindAPI, Model: "provider-fixed", Available: true}})
				surface.CodexProfileID = "fixed"
			},
		},
		{
			name: "dynamic non-GPT Codex profile",
			setup: func(svc *Service, surface *state.SurfaceConsoleRecord) {
				svc.MaterializeCodexProfiles([]state.CodexProfileSummary{{ID: "deepseek", Kind: state.CodexProfileKindAPI, BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", Available: true}})
				surface.CodexProfileID = "deepseek"
				surface.CodexPromptOverride = state.CodexPromptOverrideRecord{Model: "deepseek-v4-flash", ReasoningEffort: "high"}
			},
		},
		{
			name: "non-Codex backend",
			setup: func(svc *Service, surface *state.SurfaceConsoleRecord) {
				surface.Backend = agentproto.BackendClaude
				svc.root.Instances[surface.AttachedInstanceID].Backend = agentproto.BackendClaude
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 8, 26, 10, 12, 0, 0, time.UTC)
			svc := newServiceForTest(&now)
			surface := setupAutoWhipSurface(t, svc)
			tt.setup(svc, surface)
			surface.AutoWhip.PendingReason = state.AutoWhipReasonIncompleteStop
			surface.AutoWhip.PendingDueAt = now

			svc.maybeDispatchPendingAutoWhip(surface, now)

			item := surface.QueueItems[surface.ActiveQueueItemID]
			if item == nil {
				t.Fatal("expected autowhip queue item")
			}
			if item.CodexMessagePreset != "" || strings.HasPrefix(item.FrozenOverride.Model, "gpt-") {
				t.Fatalf("expected profile to stay off GPT fallback, got preset=%q override=%#v", item.CodexMessagePreset, item.FrozenOverride)
			}
		})
	}
}

func TestAutoContinuePreservesFrozenOverride(t *testing.T) {
	now := time.Date(2026, 8, 26, 10, 15, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.AutoWhip.Enabled = false
	surface.AutoContinue.Enabled = true
	surface.PromptOverride.AccessMode = agentproto.AccessModeConfirm

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "[terra] 继续处理", "turn-1")
	original := surface.QueueItems[surface.ActiveQueueItemID]
	if original == nil {
		t.Fatal("expected original queue item")
	}
	surface.CodexPromptOverride = state.CodexPromptOverrideRecord{Model: "gpt-6-sol", ReasoningEffort: "high"}
	completeRemoteTurnWithFinalText(t, svc, "turn-1", "interrupted", "upstream stream closed", "", &agentproto.ErrorInfo{
		Code:      "responseStreamDisconnected",
		Layer:     "codex",
		Stage:     "runtime_error",
		Message:   "upstream stream closed",
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Retryable: false,
	})

	active := surface.QueueItems[surface.ActiveQueueItemID]
	if active == nil || active.SourceKind != state.QueueItemSourceAutoContinue {
		t.Fatalf("expected active autocontinue queue item, got %#v", active)
	}
	want := state.ModelConfigRecord{Model: "gpt-5.6-terra", ReasoningEffort: "high", AccessMode: agentproto.AccessModeConfirm}
	if original.FrozenOverride != want || active.FrozenOverride != want {
		t.Fatalf("expected autocontinue to preserve frozen Terra override, original=%#v active=%#v", original.FrozenOverride, active.FrozenOverride)
	}
}
