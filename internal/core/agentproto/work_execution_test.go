package agentproto

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExecutionBindingWireRoundTripAndLegacyVersion(t *testing.T) {
	binding := &ExecutionBinding{Version: 1, ScopeID: "scope", ContextID: "context", RoleID: "role", WorkerSessionID: "worker", SessionGeneration: 2, ControllerGeneration: 3, BotGeneration: 4, AttemptID: "attempt", RunRef: "run"}
	if err := binding.ValidateExecution(); err != nil {
		t.Fatal(err)
	}
	for _, envelope := range []Envelope{
		{Type: EnvelopeCommand, Command: &Command{Kind: CommandPromptSend, Execution: binding, InstructionRevision: "instructions-v1"}},
		{Type: EnvelopeEventBatch, EventBatch: &EventBatch{Events: []Event{{Kind: EventTurnCompleted, Execution: binding}}}},
	} {
		raw, err := MarshalEnvelope(envelope)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := UnmarshalEnvelope(raw)
		if err != nil {
			t.Fatal(err)
		}
		var got *ExecutionBinding
		if decoded.Command != nil {
			got = decoded.Command.Execution
			if decoded.Command.InstructionRevision != "instructions-v1" {
				t.Fatal("instruction revision lost on wire")
			}
		} else {
			got = decoded.EventBatch.Events[0].Execution
		}
		if got == nil || *got != *binding {
			t.Fatalf("execution binding lost on wire: %s", raw)
		}
	}
	var legacy Hello
	if err := json.Unmarshal([]byte(`{"protocol":"relay.agent.v1","instance":{"instanceId":"legacy"}}`), &legacy); err != nil || legacy.WorkerProtocolVersion != 0 {
		t.Fatalf("legacy hello advertised worker protocol: %#v, %v", legacy, err)
	}
	raw, err := json.Marshal(legacy)
	if err != nil || strings.Contains(string(raw), "workerProtocolVersion") {
		t.Fatalf("legacy hello must omit worker protocol: %s, %v", raw, err)
	}
}

func TestRuntimeLaunchSnapshotOwnerExcludesAttempt(t *testing.T) {
	snapshot := RuntimeLaunchSnapshot{Version: 1, Owner: ExecutionBinding{Version: 1, ScopeID: "s", ContextID: "c", RoleID: "r", WorkerSessionID: "w", SessionGeneration: 1, ControllerGeneration: 1, BotGeneration: 1}, CWD: "/workspace", InstructionBundle: InstructionBundle{Revision: "v1", Text: "profile and role"}}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	snapshot.Owner.RunRef = "must-not-freeze-current-run"
	if err := snapshot.Validate(); err == nil {
		t.Fatal("snapshot accepted an attempt-specific owner")
	}
}
