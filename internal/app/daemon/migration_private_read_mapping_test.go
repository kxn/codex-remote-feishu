package daemon

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
)

func TestArchivedUnknownReadDoesNotBecomeExecutionHold(t *testing.T) {
	op := json.RawMessage(`{"id":"op","kind":"model.list","state":"unknown"}`)
	nilJSON := json.RawMessage(`null`)
	frozen, _ := json.Marshal([]json.RawMessage{op, nilJSON, nilJSON})
	archived, _ := json.Marshal(map[string]any{"operation_id": "op", "operation_payload": op, "command_payload": nil, "process_payload": nil, "evidence_hash": migrationBytesHash(frozen)})
	rows := map[string][]workstore.PersonalExportRecord{"operations": {{Key: "op", Payload: op}}, "personal_migration_read_archives": {{Key: "op", Payload: archived}}}
	found, err := verifiedPersonalReadArchives(rows)
	if err != nil || !found["op"] {
		t.Fatalf("valid archived read: %v", err)
	}
	rows["operations"][0].Payload = json.RawMessage(`{"id":"op","kind":"turn.interrupt","state":"unknown"}`)
	if _, err = verifiedPersonalReadArchives(rows); err == nil {
		t.Fatal("different control operation accepted")
	}
}
