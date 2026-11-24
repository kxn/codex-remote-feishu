package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
)

// The target retains these uncertain commands as exact imported source
// components. They are never replayed or marked accepted.
func verifiedPersonalControlHolds(rows map[string][]workstore.PersonalExportRecord) (map[string]bool, map[string]bool, error) {
	records := map[string]map[string]workstore.PersonalExportRecord{}
	for table, items := range rows {
		records[table] = map[string]workstore.PersonalExportRecord{}
		for _, r := range items {
			records[table][r.Key] = r
		}
	}
	compact := func(raw []byte) []byte {
		var b bytes.Buffer
		if json.Compact(&b, raw) != nil {
			return nil
		}
		return b.Bytes()
	}
	equal := func(a, b []byte) bool { return len(a) > 0 && len(b) > 0 && bytes.Equal(compact(a), compact(b)) }
	heldOps, heldCommands := map[string]bool{}, map[string]bool{}
	for _, row := range rows["personal_migration_control_holds"] {
		var h struct {
			OperationID string          `json:"operation_id"`
			Operation   json.RawMessage `json:"operation_payload"`
			Evidence    json.RawMessage `json:"evidence"`
			ProofID     string          `json:"intake_proof_id"`
			Hash        string          `json:"evidence_hash"`
		}
		if json.Unmarshal(row.Payload, &h) != nil || h.OperationID != row.Key {
			return nil, nil, fmt.Errorf("invalid control component")
		}
		proof := records["content"][h.ProofID].Data
		frozen, e := json.Marshal([]json.RawMessage{h.Evidence, json.RawMessage(proof)})
		if e != nil || len(proof) == 0 || migrationBytesHash(frozen) != h.Hash {
			return nil, nil, fmt.Errorf("control component digest mismatch")
		}
		var v workstore.PersonalControlIsolation
		if json.Unmarshal(h.Evidence, &v) != nil || v.Operation.ID != row.Key {
			return nil, nil, fmt.Errorf("invalid control evidence")
		}
		opRaw, _ := json.Marshal(v.Operation)
		attemptRaw, _ := json.Marshal(v.Attempt)
		carrierRaw, _ := json.Marshal(v.Carrier)
		processRaw, _ := json.Marshal(v.Process)
		requestRaw, _ := json.Marshal(v.Request)
		var registration struct {
			Registration json.RawMessage `json:"registration"`
		}
		if json.Unmarshal(records["carrier_registrations"][v.Carrier.Incarnation].Payload, &registration) != nil {
			return nil, nil, fmt.Errorf("invalid control carrier")
		}
		if !equal(h.Operation, opRaw) || !equal(opRaw, records["operations"][v.Operation.ID].Payload) || !equal(attemptRaw, records["attempts"][v.Attempt.ID].Payload) || !equal(carrierRaw, registration.Registration) || !equal(processRaw, records["runtime_carrier_processes"][v.Carrier.Incarnation].Payload) || !equal(requestRaw, records["content"][v.Operation.RequestRef].Data) {
			return nil, nil, fmt.Errorf("control component source mismatch")
		}
		if v.Native != nil {
			nativeRaw, _ := json.Marshal(v.Native)
			if !equal(nativeRaw, records["native_commands"][v.Operation.ID].Payload) {
				return nil, nil, fmt.Errorf("control command source mismatch")
			}
			heldCommands[v.Operation.ID] = true
		}
		heldOps[v.Operation.ID] = true
	}
	return heldOps, heldCommands, nil
}
