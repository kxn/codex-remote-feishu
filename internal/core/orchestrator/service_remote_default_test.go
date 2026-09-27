package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestRemoteDefaultModelAppliesOnlyWithoutTopicSelection(t *testing.T) {
	now := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	svc := newReplyAutoSteerServiceFixture(&now)
	surface := svc.root.Surfaces["surface-1"]
	surface.CodexConnectionContract = &state.CodexConnectionContract{ConnectionContractID: "native", Kind: state.CodexProfileKindNative}
	surface.CodexThreadPolicy = &state.CodexThreadPolicy{ThreadPolicyID: "native-default", ModelMode: state.CodexThreadValueDefault, ReasoningMode: state.CodexThreadValueDefault}
	svc.SetCodexRemoteDefault("gpt-6-sol", "high")
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionNewThread, SurfaceSessionID: surface.SurfaceSessionID})

	events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: surface.SurfaceSessionID, MessageID: "msg-default", Text: "hello"})
	command := firstCodexCommand(events, agentproto.CommandPromptSend)
	if command == nil || command.Overrides.Model != "gpt-6-sol" || command.Overrides.ReasoningEffort != "high" {
		t.Fatalf("remote default missing from prompt: %#v", command)
	}
	queued := surface.QueueItems[surface.ActiveQueueItemID]
	if queued == nil {
		t.Fatal("missing queued prompt")
	}

	surface.CodexPromptOverride = state.CodexPromptOverrideRecord{Model: "gpt-6-astra", ReasoningEffort: "medium"}
	got := svc.resolveFrozenPromptOverride(svc.root.Instances[surface.AttachedInstanceID], surface, "", "", state.ModelConfigRecord{})
	if got.Model != "gpt-6-astra" || got.ReasoningEffort != "medium" {
		t.Fatalf("topic selection lost: %#v", got)
	}

	svc.SetCodexRemoteDefault("gpt-6-luna", "low")
	if queued.FrozenOverride.Model != "gpt-6-sol" || queued.FrozenOverride.ReasoningEffort != "high" {
		t.Fatalf("queued prompt changed after default update: %#v", queued.FrozenOverride)
	}
	surface.CodexPromptOverride = state.CodexPromptOverrideRecord{}
	got = svc.resolveFrozenPromptOverride(svc.root.Instances[surface.AttachedInstanceID], surface, "", "", state.ModelConfigRecord{})
	if got.Model != "gpt-6-luna" || got.ReasoningEffort != "low" {
		t.Fatalf("cleared topic did not inherit current default: %#v", got)
	}
	svc.MaterializeCodexProfiles([]state.CodexProfileSummary{{ID: "fixed", Kind: state.CodexProfileKindAPI, Model: "provider-fixed", Available: true}})
	surface.CodexProfileID = "fixed"
	got = svc.resolveFrozenPromptOverride(svc.root.Instances[surface.AttachedInstanceID], surface, "", "", state.ModelConfigRecord{})
	if got.Model != "" || got.ReasoningEffort != "" {
		t.Fatalf("remote default leaked into fixed profile: %#v", got)
	}
}
