package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
)

func verifiedPersonalPublicationHolds(rows map[string][]workstore.PersonalExportRecord) (map[string]bool, map[string]bool, error) {
	ops := map[string]json.RawMessage{}
	boxes := map[string]json.RawMessage{}
	content := map[string][]byte{}
	for _, r := range rows["operations"] {
		ops[r.Key] = r.Payload
	}
	for _, r := range rows["outbox"] {
		boxes[r.Key] = r.Payload
	}
	for _, r := range rows["content"] {
		content[r.Key] = r.Data
	}
	compact := func(raw json.RawMessage) ([]byte, error) {
		var b bytes.Buffer
		err := json.Compact(&b, raw)
		return b.Bytes(), err
	}
	heldOps := map[string]bool{}
	heldBoxes := map[string]bool{}
	for _, r := range rows["personal_migration_publication_holds"] {
		var h struct {
			OutboxID    string          `json:"outbox_id"`
			OperationID string          `json:"operation_id"`
			Outbox      json.RawMessage `json:"outbox_payload"`
			Operation   json.RawMessage `json:"operation_payload"`
			ProofID     string          `json:"intake_proof_id"`
			Hash        string          `json:"evidence_hash"`
		}
		if json.Unmarshal(r.Payload, &h) != nil || h.OutboxID != r.Key || h.OperationID == "" || len(content[h.ProofID]) == 0 {
			return nil, nil, fmt.Errorf("invalid publication hold")
		}
		frozen, err := json.Marshal([]json.RawMessage{h.Outbox, h.Operation, json.RawMessage(content[h.ProofID])})
		if err != nil || migrationBytesHash(frozen) != h.Hash {
			return nil, nil, fmt.Errorf("publication hold digest mismatch")
		}
		a, err := compact(h.Outbox)
		if err != nil {
			return nil, nil, err
		}
		b, err := compact(boxes[h.OutboxID])
		if err != nil || !bytes.Equal(a, b) {
			return nil, nil, fmt.Errorf("publication hold outbox mismatch")
		}
		a, err = compact(h.Operation)
		if err != nil {
			return nil, nil, err
		}
		b, err = compact(ops[h.OperationID])
		if err != nil || !bytes.Equal(a, b) {
			return nil, nil, fmt.Errorf("publication hold operation mismatch")
		}
		var op struct{ Kind, State string }
		var out struct {
			State             string `json:"state"`
			OperationID       string `json:"operation_id"`
			ExternalMessageID string `json:"external_message_id"`
		}
		if json.Unmarshal(h.Operation, &op) != nil || op.Kind != "feishu.publish" || (op.State != "sending" && op.State != "unknown") {
			return nil, nil, fmt.Errorf("invalid held publication")
		}
		if json.Unmarshal(h.Outbox, &out) != nil || out.OperationID != h.OperationID || (out.State != "sending" && out.State != "unknown") || out.ExternalMessageID != "" {
			return nil, nil, fmt.Errorf("invalid held outbox")
		}
		heldOps[h.OperationID] = true
		heldBoxes[h.OutboxID] = true
	}
	return heldOps, heldBoxes, nil
}
