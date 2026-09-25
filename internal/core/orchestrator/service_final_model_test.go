package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestFinalTurnModelUsesStartedTurnEvidence(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		fromTranslator          bool
		eventModel, eventEffort string
		effective               *agentproto.CodexEffectiveThreadContract
		reroute                 string
		wantModel, wantEffort   string
	}{
		{name: "observed", effective: &agentproto.CodexEffectiveThreadContract{Model: " runtime-model ", ReasoningEffort: " high "}, wantModel: "runtime-model", wantEffort: "high"},
		{name: "unknown"},
		{name: "changed request without fresh runtime evidence", fromTranslator: true},
		{name: "explicit turn fields", eventModel: "event-model", eventEffort: "medium", wantModel: "event-model", wantEffort: "medium"},
		{name: "empty evidence", eventModel: "unconfirmed", eventEffort: "high", effective: &agentproto.CodexEffectiveThreadContract{}},
		{name: "rerouted", effective: &agentproto.CodexEffectiveThreadContract{Model: "runtime-model", ReasoningEffort: "high"}, reroute: "fallback-model", wantModel: "fallback-model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
			svc := newServiceForTest(&now)
			svc.UpsertInstance(&state.InstanceRecord{
				InstanceID: "inst-1", Online: true, WorkspaceRoot: "/tmp/workspace", WorkspaceKey: "/tmp/workspace",
				ObservedFocusedThreadID: "thread-1",
				Threads:                 map[string]*state.ThreadRecord{"thread-1": {ThreadID: "thread-1", Loaded: true, CWD: "/tmp/workspace", ExplicitModel: "old-model", ExplicitReasoningEffort: "low"}},
			})
			svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
			svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface-1", MessageID: "msg-1", Text: "continue"})
			started := agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "thread-1", TurnID: "turn-1", Model: tc.eventModel, ReasoningEffort: tc.eventEffort, CodexEffectiveThread: tc.effective}
			if tc.fromTranslator {
				started = startedTurnAfterModelChange(t)
			}
			svc.ApplyAgentEvent("inst-1", started)
			binding := svc.lookupRemoteTurn("inst-1", "thread-1", "turn-1")
			if binding == nil {
				t.Fatal("missing running turn")
			}
			svc.ApplyAgentEvent("inst-1", agentproto.Event{Kind: agentproto.EventConfigObserved, ConfigScope: "thread", ThreadID: "thread-1", Model: "next-model", ReasoningEffort: "low"})
			// Duplicate starts must not overwrite the first accepted turn evidence.
			svc.ApplyAgentEvent("inst-1", agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "thread-1", TurnID: "turn-1", CodexEffectiveThread: &agentproto.CodexEffectiveThreadContract{Model: "next-model", ReasoningEffort: "low"}})
			if tc.reroute != "" {
				svc.ApplyAgentEvent("inst-1", agentproto.Event{Kind: agentproto.EventTurnModelRerouted, ThreadID: "thread-1", TurnID: "turn-1", ModelReroute: &agentproto.TurnModelReroute{ThreadID: "thread-1", TurnID: "turn-1", ToModel: tc.reroute}})
			}
			now = now.Add(time.Second)
			events := completeRemoteTurnWithFinalText(t, svc, "turn-1", "completed", "", "done", nil)
			for _, event := range events {
				if event.FinalTurnSummary != nil {
					got := event.FinalTurnSummary
					if got.Model != tc.wantModel || got.ReasoningEffort != tc.wantEffort {
						t.Fatalf("model/effort = %q/%q, want %q/%q", got.Model, got.ReasoningEffort, tc.wantModel, tc.wantEffort)
					}
					return
				}
			}
			t.Fatal("missing final summary")
		})
	}
}

func startedTurnAfterModelChange(t *testing.T) agentproto.Event {
	t.Helper()
	tr := codex.NewTranslator("inst-1")
	if _, err := tr.ObserveServer([]byte(`{"method":"thread/started","params":{"thread":{"id":"thread-1","modelProvider":"provider","model":"old-model","config":{"model_reasoning_effort":"low"}}}}`)); err != nil {
		t.Fatal(err)
	}
	_, err := tr.TranslateCommand(agentproto.Command{
		Kind:        agentproto.CommandPromptSend,
		Origin:      agentproto.Origin{Surface: "surface-1"},
		Target:      agentproto.Target{ThreadID: "thread-1"},
		Prompt:      agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "continue"}}},
		Overrides:   agentproto.PromptOverrides{Model: "new-model", ReasoningEffort: "high"},
		CodexResume: &agentproto.CodexResumePolicy{Mode: agentproto.CodexResumePreserveThreadSettings, ModelProviderID: "provider"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := tr.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"thread-1","turn":{"id":"turn-1"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("unexpected started events: %#v", result)
	}
	return result.Events[0]
}
