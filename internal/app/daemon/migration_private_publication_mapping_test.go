package daemon

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
)

func TestPublicationHoldRemainsComponentNotExecutionHold(t *testing.T) {
	op := json.RawMessage(`{"id":"operation","kind":"feishu.publish","state":"sending"}`)
	out := json.RawMessage(`{"id":"outbox","operation_id":"operation","state":"sending"}`)
	proof := json.RawMessage(`{"migration_id":"migration","digest":"proof"}`)
	raw, _ := json.Marshal([]json.RawMessage{out, op, proof})
	hold, _ := json.Marshal(map[string]any{"outbox_id": "outbox", "operation_id": "operation", "outbox_payload": out, "operation_payload": op, "intake_proof_id": "proof", "evidence_hash": migrationBytesHash(raw)})
	rows := map[string][]workstore.PersonalExportRecord{"operations": {{Key: "operation", Payload: op}}, "outbox": {{Key: "outbox", Payload: out}}, "content": {{Key: "proof", Data: proof}}, "personal_migration_publication_holds": {{Key: "outbox", Payload: hold}}}
	ops, boxes, err := verifiedPersonalPublicationHolds(rows)
	if err != nil || !ops["operation"] || !boxes["outbox"] {
		t.Fatalf("valid component: %v", err)
	}
	rows["outbox"][0].Payload = json.RawMessage(`{"id":"outbox","operation_id":"operation","state":"sent"}`)
	if _, _, err = verifiedPersonalPublicationHolds(rows); err == nil {
		t.Fatal("changed source outbox accepted")
	}
}
