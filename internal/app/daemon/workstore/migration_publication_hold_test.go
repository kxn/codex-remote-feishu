package workstore

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestUncertainPersonalPublicationHeldWithoutInventingReceipt(t *testing.T) {
	ctx := t.Context()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "team.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	if err = s.Within(ctx, func(tx *Tx) error {
		if e := tx.PutRole(work.Role{ID: "role", Name: "role", Description: "role", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutPrincipal(work.Principal{ID: "person", DisplayName: "person", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutScope(work.WorkScope{ID: "scope", Kind: work.ScopePersonal, PrincipalID: "person", RoleID: "role", Revision: 1}, 0); e != nil {
			return e
		}
		var e error
		body := work.NewContent("body", "scope", "application/json", []byte(`{}`), now)
		if e = tx.PutContent(body); e != nil {
			return e
		}
		op := work.Operation{ID: "send", ScopeID: "scope", Actor: work.Actor{Kind: work.ActorSystem, ID: "display"}, Kind: "feishu.publish", TargetID: "chat", TargetGeneration: 1, IdempotencyKey: "send", RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationSending, Steps: []work.OperationStep{{ID: "first_send", State: work.OperationSending, Receipt: now.Format(time.RFC3339Nano)}}, Revision: 1}
		if _, _, e = tx.PutOperation(op, 0); e != nil {
			return e
		}
		out := work.Outbox{ID: "out", ScopeID: "scope", OperationID: op.ID, Target: "chat", EventID: "event", PayloadRef: body.ID, State: work.OutboxSending, Revision: 1}
		if e = tx.PutOutbox(out, 0); e != nil {
			return e
		}
		f, e := tx.BeginPersonalMigrationFence("scope", "migration", now)
		if e != nil {
			return e
		}
		if _, e = tx.StartPersonalMigrationDrain("scope", "migration", f.Revision, now); e != nil {
			return e
		}
		proof, _ := json.Marshal(map[string]string{"migration_id": "migration", "digest": "fixture"})
		return tx.PutContent(work.NewContent("migration-intake-proof:fixture", "scope", "application/json", proof, now))
	}); err != nil {
		t.Fatal(err)
	}
	if err = s.Within(ctx, func(tx *Tx) error {
		before, e := tx.PersonalMigrationStatus("scope")
		if e != nil {
			return e
		}
		if before.UnsettledOperations != 1 || before.UnsettledOutbox != 1 {
			t.Fatal("expected uncertain send")
		}
		if e = tx.HoldPersonalUncertainPublication("scope", "out", "migration-intake-proof:fixture", now); e != nil {
			return e
		}
		if e = tx.HoldPersonalUncertainPublication("scope", "out", "migration-intake-proof:fixture", now); e != nil {
			return e
		}
		after, e := tx.PersonalMigrationStatus("scope")
		if e != nil {
			return e
		}
		if after.UnsettledOperations != 0 || after.UnsettledOutbox != 0 {
			t.Fatal("component still globally blocks drain")
		}
		op, e := tx.GetOperation("send")
		if e != nil {
			return e
		}
		out, e := tx.GetOutbox("out")
		if e != nil {
			return e
		}
		if op.State != work.OperationSending || out.State != work.OutboxSending || op.ExternalReceipt != "" || out.ExternalMessageID != "" {
			t.Fatal("uncertain send fabricated receipt")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err = s.Within(ctx, func(tx *Tx) error {
		_, e := tx.tx.Exec(`UPDATE personal_migration_publication_holds SET evidence_hash='forged'`)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if err = s.Within(ctx, func(tx *Tx) error { _, e := tx.PersonalMigrationStatus("scope"); return e }); err == nil {
		t.Fatal("tampered publication hold accepted")
	}
}
