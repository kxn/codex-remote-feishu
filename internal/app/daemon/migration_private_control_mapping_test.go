package daemon

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestControlIsolationExportsExactOperationAndCarrier(t *testing.T) {
	for _, registered := range []bool{false, true} {
		v := workstore.PersonalControlIsolation{Operation: work.Operation{ID: "stop", RequestRef: "request"}, Request: agentproto.Command{CommandID: "stop"}, Attempt: work.ExecutionAttempt{ID: "attempt"}, Carrier: workstore.CarrierRegistration{Incarnation: "old-carrier"}, Process: agentproto.RuntimeProcessIdentity{PID: 123, Platform: "linux", StartIdentity: "start"}}
		if registered {
			v.Native = &workstore.NativeCommand{CommandID: "stop", Incarnation: "old-carrier"}
		}
		evidence, _ := json.Marshal(v)
		op, _ := json.Marshal(v.Operation)
		attempt, _ := json.Marshal(v.Attempt)
		request, _ := json.Marshal(v.Request)
		native, _ := json.Marshal(v.Native)
		carrier, _ := json.Marshal(map[string]any{"registration": v.Carrier})
		process, _ := json.Marshal(v.Process)
		proof := json.RawMessage(`{"migration_id":"m","digest":"p"}`)
		frozen, _ := json.Marshal([]json.RawMessage{evidence, proof})
		hold, _ := json.Marshal(map[string]any{"operation_id": "stop", "operation_payload": json.RawMessage(op), "evidence": json.RawMessage(evidence), "intake_proof_id": "proof", "evidence_hash": migrationBytesHash(frozen)})
		rows := map[string][]workstore.PersonalExportRecord{"personal_migration_control_holds": {{Key: "stop", Payload: hold}}, "operations": {{Key: "stop", Payload: op}}, "attempts": {{Key: "attempt", Payload: attempt}}, "carrier_registrations": {{Key: "old-carrier", Payload: carrier}}, "runtime_carrier_processes": {{Key: "old-carrier", Payload: process}}, "content": {{Key: "proof", Data: proof}, {Key: "request", Data: request}}}
		if registered {
			rows["native_commands"] = []workstore.PersonalExportRecord{{Key: "stop", Payload: native}}
		}
		ops, commands, err := verifiedPersonalControlHolds(rows)
		if err != nil || !ops["stop"] || commands["stop"] != registered {
			t.Fatalf("registered=%v ops=%v commands=%v err=%v", registered, ops, commands, err)
		}
		rows["runtime_carrier_processes"][0].Payload = json.RawMessage(`{"pid":999}`)
		if _, _, err = verifiedPersonalControlHolds(rows); err == nil {
			t.Fatal("changed old carrier accepted")
		}
	}
}
