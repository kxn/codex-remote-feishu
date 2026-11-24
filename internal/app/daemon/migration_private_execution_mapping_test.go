package daemon

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestExecutionIsolationKeepsExactComponentsAndRejectsTamper(t *testing.T) {
	worker := work.WorkerSession{ID: "w", ScopeID: "personal", CarrierIncarnation: "carrier", Generation: 2}
	workerRaw, _ := json.Marshal(worker)
	process := json.RawMessage(`{"platform":"linux","pid":123,"start_identity":"start"}`)
	evidence, _ := json.Marshal(workstore.PersonalExecutionIsolation{Worker: worker, ProcessScopesStopped: true})
	proof := json.RawMessage(`{"migration_id":"m","digest":"p"}`)
	frozen, _ := json.Marshal([]json.RawMessage{workerRaw, process, evidence, proof})
	hold, _ := json.Marshal(map[string]any{"worker_id": "w", "worker_payload": json.RawMessage(workerRaw), "process_payload": process, "intake_proof_id": "proof", "evidence": json.RawMessage(evidence), "evidence_hash": migrationBytesHash(frozen)})
	rows := map[string][]workstore.PersonalExportRecord{
		"workers": {{Key: "w", Payload: workerRaw}}, "runtime_carrier_processes": {{Key: "carrier", Payload: process}},
		"content": {{Key: "proof", Data: proof}}, "personal_migration_execution_holds": {{Key: "w", Payload: hold}},
		"attempts":        {{Key: "a", Payload: json.RawMessage(`{"run_ref":{"worker_id":"w","generation":2}}`)}, {Key: "older", Payload: json.RawMessage(`{"run_ref":{"worker_id":"w","generation":1}}`)}, {Key: "other", Payload: json.RawMessage(`{"run_ref":{"worker_id":"other"}}`)}},
		"native_commands": {{Key: "cmd", Payload: json.RawMessage(`{"command_id":"cmd","incarnation":"carrier"}`)}},
		"operations":      {{Key: "cmd", Payload: json.RawMessage(`{"id":"cmd"}`)}, {Key: "unrelated", Payload: json.RawMessage(`{"id":"unrelated","target_id":"w"}`)}},
	}
	attempts, ops, commands, err := verifiedPersonalExecutionHolds(rows)
	if err != nil || !attempts["a"] || attempts["older"] || attempts["other"] || !ops["cmd"] || ops["unrelated"] || !commands["cmd"] {
		t.Fatalf("unexpected classification: attempts=%v ops=%v commands=%v err=%v", attempts, ops, commands, err)
	}
	rows["runtime_carrier_processes"][0].Payload = json.RawMessage(`{"platform":"linux","pid":124,"start_identity":"start"}`)
	if _, _, _, err = verifiedPersonalExecutionHolds(rows); err == nil {
		t.Fatal("changed process accepted")
	}
}
