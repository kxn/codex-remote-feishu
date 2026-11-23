package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
)

func verifiedPersonalReadArchives(rows map[string][]workstore.PersonalExportRecord) (map[string]bool, error) {
	ops := map[string]json.RawMessage{}
	commands := map[string]json.RawMessage{}
	for _, r := range rows["operations"] {
		ops[r.Key] = r.Payload
	}
	for _, r := range rows["native_commands"] {
		commands[r.Key] = r.Payload
	}
	compact := func(raw json.RawMessage) ([]byte, error) {
		var b bytes.Buffer
		err := json.Compact(&b, raw)
		return b.Bytes(), err
	}
	result := map[string]bool{}
	for _, r := range rows["personal_migration_read_archives"] {
		var a struct {
			OperationID string          `json:"operation_id"`
			Operation   json.RawMessage `json:"operation_payload"`
			Command     json.RawMessage `json:"command_payload"`
			Process     json.RawMessage `json:"process_payload"`
			Hash        string          `json:"evidence_hash"`
		}
		if json.Unmarshal(r.Payload, &a) != nil || a.OperationID != r.Key || a.Hash == "" {
			return nil, fmt.Errorf("invalid read archive")
		}
		frozen, err := json.Marshal([]json.RawMessage{a.Operation, a.Command, a.Process})
		if err != nil || migrationBytesHash(frozen) != a.Hash {
			return nil, fmt.Errorf("read archive digest mismatch")
		}
		x, err := compact(a.Operation)
		if err != nil {
			return nil, err
		}
		y, err := compact(ops[a.OperationID])
		if err != nil || !bytes.Equal(x, y) {
			return nil, fmt.Errorf("read archive operation mismatch")
		}
		var op struct{ Kind, State string }
		if json.Unmarshal(a.Operation, &op) != nil || op.State != "unknown" || (op.Kind != "runtime.inspect" && op.Kind != "model.list") {
			return nil, fmt.Errorf("read archive is not an unknown read")
		}
		if string(a.Command) == "null" {
			if len(commands[a.OperationID]) != 0 || string(a.Process) != "null" {
				return nil, fmt.Errorf("read archive registration mismatch")
			}
		} else {
			x, err = compact(a.Command)
			if err != nil {
				return nil, err
			}
			y, err = compact(commands[a.OperationID])
			if err != nil || !bytes.Equal(x, y) || string(a.Process) == "null" {
				return nil, fmt.Errorf("read archive command mismatch")
			}
		}
		result[a.OperationID] = true
	}
	return result, nil
}
