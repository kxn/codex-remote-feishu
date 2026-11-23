package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func verifiedPersonalExecutionHolds(rows map[string][]workstore.PersonalExportRecord) (map[string]bool, map[string]bool, map[string]bool, error) {
	workers := map[string]json.RawMessage{}
	processes := map[string]json.RawMessage{}
	content := map[string][]byte{}
	for _, r := range rows["workers"] {
		workers[r.Key] = r.Payload
	}
	for _, r := range rows["runtime_carrier_processes"] {
		processes[r.Key] = r.Payload
	}
	for _, r := range rows["content"] {
		content[r.Key] = r.Data
	}
	compact := func(raw json.RawMessage) ([]byte, error) {
		var b bytes.Buffer
		err := json.Compact(&b, raw)
		return b.Bytes(), err
	}
	heldWorkers := map[string]bool{}
	carriers := map[string]bool{}
	for _, r := range rows["personal_migration_execution_holds"] {
		var h struct {
			WorkerID string          `json:"worker_id"`
			Worker   json.RawMessage `json:"worker_payload"`
			Process  json.RawMessage `json:"process_payload"`
			ProofID  string          `json:"intake_proof_id"`
			Evidence json.RawMessage `json:"evidence"`
			Hash     string          `json:"evidence_hash"`
		}
		if json.Unmarshal(r.Payload, &h) != nil || h.WorkerID != r.Key || len(content[h.ProofID]) == 0 {
			return nil, nil, nil, fmt.Errorf("invalid execution component")
		}
		frozen, e := json.Marshal([]json.RawMessage{h.Worker, h.Process, h.Evidence, json.RawMessage(content[h.ProofID])})
		if e != nil || migrationBytesHash(frozen) != h.Hash {
			return nil, nil, nil, fmt.Errorf("execution component digest mismatch")
		}
		a, e := compact(h.Worker)
		if e != nil {
			return nil, nil, nil, e
		}
		b, e := compact(workers[h.WorkerID])
		if e != nil || !bytes.Equal(a, b) {
			return nil, nil, nil, fmt.Errorf("execution worker changed")
		}
		var worker work.WorkerSession
		var proof workstore.PersonalExecutionIsolation
		if json.Unmarshal(h.Worker, &worker) != nil || json.Unmarshal(h.Evidence, &proof) != nil || !proof.ProcessScopesStopped || proof.Worker.ID != worker.ID || worker.CarrierIncarnation == "" {
			return nil, nil, nil, fmt.Errorf("execution isolation unproven")
		}
		a, e = compact(h.Process)
		if e != nil {
			return nil, nil, nil, e
		}
		b, e = compact(processes[worker.CarrierIncarnation])
		if e != nil || !bytes.Equal(a, b) {
			return nil, nil, nil, fmt.Errorf("execution process changed")
		}
		heldWorkers[worker.ID] = true
		carriers[worker.CarrierIncarnation] = true
	}
	heldAttempts := map[string]bool{}
	for _, r := range rows["attempts"] {
		var a work.ExecutionAttempt
		if json.Unmarshal(r.Payload, &a) != nil {
			return nil, nil, nil, fmt.Errorf("invalid execution attempt")
		}
		if heldWorkers[a.RunRef.WorkerID] {
			heldAttempts[r.Key] = true
		}
	}
	heldCommands := map[string]bool{}
	for _, r := range rows["native_commands"] {
		var n workstore.NativeCommand
		if json.Unmarshal(r.Payload, &n) != nil {
			return nil, nil, nil, fmt.Errorf("invalid native command")
		}
		if carriers[n.Incarnation] {
			heldCommands[r.Key] = true
		}
	}
	heldOps := map[string]bool{}
	for _, r := range rows["operations"] {
		var op work.Operation
		if json.Unmarshal(r.Payload, &op) != nil {
			return nil, nil, nil, fmt.Errorf("invalid operation")
		}
		if heldCommands[op.ID] {
			heldOps[op.ID] = true
		}
	}
	return heldAttempts, heldOps, heldCommands, nil
}
