package daemon

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
)

func TestPreservedSourceRequiresExactUnsentEvidence(t *testing.T) {
	op := json.RawMessage(`{"id":"operation","revision":1,"state":"prepared"}`)
	preserve := func(raw json.RawMessage) []byte {
		v, _ := json.Marshal(map[string]any{"kind": "operation", "record_id": "operation", "record_revision": 1, "payload_hash": migrationBytesHash(raw), "payload": raw})
		return v
	}
	rows := map[string][]workstore.PersonalExportRecord{"operations": {{Key: "operation", Revision: 1, Payload: op}}, "personal_migration_preserved": {{Key: "preserved", Payload: preserve(op)}}}
	ops, _, err := verifiedPersonalPreserved(rows)
	if err != nil || !ops["operation"] {
		t.Fatalf("valid source: %v", err)
	}
	rows["native_commands"] = []workstore.PersonalExportRecord{{Key: "operation"}}
	if _, _, err = verifiedPersonalPreserved(rows); err == nil {
		t.Fatal("registered native command accepted as unsent")
	}
	delete(rows, "native_commands")
	changed := json.RawMessage(`{"id":"operation","revision":1,"state":"prepared","external_receipt":"sent"}`)
	rows["operations"][0].Payload = changed
	rows["personal_migration_preserved"][0].Payload = preserve(changed)
	if _, _, err = verifiedPersonalPreserved(rows); err == nil {
		t.Fatal("external receipt accepted as unsent")
	}
	rows["operations"][0].Payload = op
	rows["personal_migration_preserved"][0].Payload = preserve(op)
	rows["outbox"] = []workstore.PersonalExportRecord{{Key: "outbox", Payload: json.RawMessage(`{"operation_id":"operation","state":"sent"}`)}}
	if _, _, err = verifiedPersonalPreserved(rows); err == nil {
		t.Fatal("sent outbox accepted as unsent")
	}
}
