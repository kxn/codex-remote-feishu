package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// verifiedPersonalPreserved recognises only the exact, unmodified source
// records whose drain transaction proved they were never sent.
func verifiedPersonalPreserved(rows map[string][]workstore.PersonalExportRecord) (map[string]bool, map[string]bool, error) {
	ops := map[string]workstore.PersonalExportRecord{}
	outbox := map[string]workstore.PersonalExportRecord{}
	commands := map[string]bool{}
	for _, r := range rows["operations"] {
		ops[r.Key] = r
	}
	for _, r := range rows["outbox"] {
		outbox[r.Key] = r
	}
	for _, r := range rows["native_commands"] {
		commands[r.Key] = true
	}
	compact := func(v []byte) ([]byte, error) { var b bytes.Buffer; e := json.Compact(&b, v); return b.Bytes(), e }
	preservedOps := map[string]bool{}
	preservedOutbox := map[string]bool{}
	for _, r := range rows["personal_migration_preserved"] {
		var v struct {
			Kind     string          `json:"kind"`
			ID       string          `json:"record_id"`
			Revision int64           `json:"record_revision"`
			Hash     string          `json:"payload_hash"`
			Payload  json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(r.Payload, &v) != nil {
			return nil, nil, fmt.Errorf("invalid preserved source")
		}
		if v.Kind != "operation" && v.Kind != "outbox" {
			continue
		}
		source, ok := ops[v.ID]
		if v.Kind == "outbox" {
			source, ok = outbox[v.ID]
		}
		a, e := compact(v.Payload)
		if e != nil || !ok {
			return nil, nil, fmt.Errorf("preserved source missing")
		}
		b, e := compact(source.Payload)
		if e != nil || !bytes.Equal(a, b) || source.Revision != v.Revision || migrationBytesHash(a) != v.Hash {
			return nil, nil, fmt.Errorf("preserved source changed")
		}
		var state struct {
			State       string `json:"state"`
			OperationID string `json:"operation_id"`
		}
		if json.Unmarshal(a, &state) != nil {
			return nil, nil, fmt.Errorf("invalid preserved state")
		}
		if v.Kind == "operation" {
			var op work.Operation
			if json.Unmarshal(a, &op) != nil || op.ID != v.ID || op.Revision != v.Revision || op.ExternalReceipt != "" || len(op.Steps) != 0 {
				return nil, nil, fmt.Errorf("invalid preserved operation evidence")
			}
			for _, out := range outbox {
				var item work.Outbox
				if json.Unmarshal(out.Payload, &item) != nil {
					return nil, nil, fmt.Errorf("invalid outbox")
				}
				if item.OperationID == v.ID && item.State != work.OutboxPending {
					return nil, nil, fmt.Errorf("operation has sent outbox")
				}
			}
			if state.State != "prepared" || commands[v.ID] {
				return nil, nil, fmt.Errorf("preserved operation may have been sent")
			}
			preservedOps[v.ID] = true
		} else {
			if state.State != "pending" || state.OperationID == "" || commands[state.OperationID] {
				return nil, nil, fmt.Errorf("preserved outbox may have been sent")
			}
			preservedOutbox[v.ID] = true
		}
	}
	for id := range preservedOutbox {
		var v struct {
			OperationID string `json:"operation_id"`
		}
		_ = json.Unmarshal(outbox[id].Payload, &v)
		if !preservedOps[v.OperationID] {
			return nil, nil, fmt.Errorf("preserved outbox operation missing")
		}
	}
	return preservedOps, preservedOutbox, nil
}
