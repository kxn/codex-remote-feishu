package codex

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestObserveServerModelReroutedProducesCanonicalEvent(t *testing.T) {
	tr := NewTranslator("inst-1")

	if _, err := tr.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-1","cwd":"/tmp/project"}}`)); err != nil {
		t.Fatalf("observe client thread resume: %v", err)
	}
	if _, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandPromptSend,
		Origin: agentproto.Origin{ChatID: "surface-1"},
		Target: agentproto.Target{ThreadID: "thread-1"},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
	}); err != nil {
		t.Fatalf("translate prompt send: %v", err)
	}
	if _, err := tr.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"thread-1","turn":{"id":"turn-1"}}}`)); err != nil {
		t.Fatalf("observe turn started: %v", err)
	}

	result, err := tr.ObserveServer([]byte(`{"method":"model/rerouted","params":{"threadId":"thread-1","turnId":"turn-1","fromModel":"gpt-5.4","toModel":"gpt-5.4-mini","reason":"safety_downgrade"}}`))
	if err != nil {
		t.Fatalf("observe model rerouted: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one reroute event, got %#v", result.Events)
	}
	event := result.Events[0]
	if event.Kind != agentproto.EventTurnModelRerouted {
		t.Fatalf("unexpected event kind: %#v", event)
	}
	if event.ThreadID != "thread-1" || event.TurnID != "turn-1" {
		t.Fatalf("unexpected reroute identity: %#v", event)
	}
	if event.Initiator.Kind != agentproto.InitiatorRemoteSurface {
		t.Fatalf("expected remote initiator, got %#v", event.Initiator)
	}
	if event.TrafficClass != agentproto.TrafficClassPrimary {
		t.Fatalf("expected primary traffic class, got %#v", event)
	}
	if event.ModelReroute == nil {
		t.Fatalf("expected reroute payload, got %#v", event)
	}
	if event.ModelReroute.FromModel != "gpt-5.4" || event.ModelReroute.ToModel != "gpt-5.4-mini" || event.ModelReroute.Reason != "safety_downgrade" {
		t.Fatalf("unexpected reroute payload: %#v", event.ModelReroute)
	}
}
