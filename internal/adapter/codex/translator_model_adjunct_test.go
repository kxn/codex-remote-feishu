package codex

import (
	"reflect"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestObserveServerModelVerificationProducesStateOnlyEvent(t *testing.T) {
	tr := NewTranslator("inst-1")

	result, err := tr.ObserveServer([]byte(`{"method":"model/verification","params":{"threadId":"thread-1","turnId":"turn-1","verifications":[{"id":"verify-1","type":"account","message":"Sign in again"},{"id":"verify-2","type":"policy","reason":"workspace policy"}]}}`))
	if err != nil {
		t.Fatalf("observe model verification: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one verification event, got %#v", result.Events)
	}
	event := result.Events[0]
	if event.Kind != agentproto.EventTurnModelVerification {
		t.Fatalf("unexpected event kind: %#v", event)
	}
	if event.ThreadID != "thread-1" || event.TurnID != "turn-1" {
		t.Fatalf("unexpected verification identity: %#v", event)
	}
	if event.ModelVerification == nil {
		t.Fatalf("expected verification payload, got %#v", event)
	}
	if len(event.ModelVerification.Verifications) != 2 {
		t.Fatalf("expected two verification records, got %#v", event.ModelVerification)
	}
	if event.ModelVerification.Verifications[0].ID != "verify-1" || event.ModelVerification.Verifications[0].Type != "account" || event.ModelVerification.Verifications[0].Message != "Sign in again" {
		t.Fatalf("unexpected first verification: %#v", event.ModelVerification.Verifications[0])
	}
	if event.ModelVerification.Verifications[1].Reason != "workspace policy" {
		t.Fatalf("unexpected second verification: %#v", event.ModelVerification.Verifications[1])
	}
}

func TestObserveServerModelSafetyBufferingUpdatedProducesStateOnlyEvent(t *testing.T) {
	tr := NewTranslator("inst-1")

	result, err := tr.ObserveServer([]byte(`{"method":"model/safetyBuffering/updated","params":{"threadId":"thread-1","turnId":"turn-1","model":"gpt-5.4","useCases":["coding","analysis"],"reasons":["policy","capacity"],"showBufferingUi":true,"fasterModel":"gpt-5.4-mini"}}`))
	if err != nil {
		t.Fatalf("observe model safety buffering updated: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one safety buffering event, got %#v", result.Events)
	}
	event := result.Events[0]
	if event.Kind != agentproto.EventTurnModelSafetyBufferingUpdated {
		t.Fatalf("unexpected event kind: %#v", event)
	}
	if event.ThreadID != "thread-1" || event.TurnID != "turn-1" {
		t.Fatalf("unexpected safety buffering identity: %#v", event)
	}
	if event.ModelSafetyBuffering == nil {
		t.Fatalf("expected safety buffering payload, got %#v", event)
	}
	if event.ModelSafetyBuffering.Model != "gpt-5.4" || event.ModelSafetyBuffering.FasterModel != "gpt-5.4-mini" || !event.ModelSafetyBuffering.ShowBufferingUI {
		t.Fatalf("unexpected safety buffering payload: %#v", event.ModelSafetyBuffering)
	}
	if !reflect.DeepEqual(event.ModelSafetyBuffering.UseCases, []string{"coding", "analysis"}) {
		t.Fatalf("unexpected use cases: %#v", event.ModelSafetyBuffering.UseCases)
	}
	if !reflect.DeepEqual(event.ModelSafetyBuffering.Reasons, []string{"policy", "capacity"}) {
		t.Fatalf("unexpected reasons: %#v", event.ModelSafetyBuffering.Reasons)
	}
}
