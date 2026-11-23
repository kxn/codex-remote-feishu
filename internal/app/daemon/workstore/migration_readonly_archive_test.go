package workstore

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPersonalReadArchiveOnlyExcludesExactReadEvidence(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "team.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err = s.Within(ctx, func(tx *Tx) error {
		if e := tx.PutRole(work.Role{ID: "role", Name: "role", Description: "role", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutPrincipal(work.Principal{ID: "principal", DisplayName: "person", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutScope(work.WorkScope{ID: "scope", Kind: work.ScopePersonal, PrincipalID: "principal", RoleID: "role", Revision: 1}, 0); e != nil {
			return e
		}
		for _, kind := range []string{"model.list", "turn.interrupt"} {
			op := work.Operation{ID: kind, ScopeID: "scope", Actor: work.Actor{Kind: work.ActorSystem, ID: "system"}, Kind: kind, TargetID: "worker", TargetGeneration: 1, IdempotencyKey: kind, RequestHash: "hash", State: work.OperationUnknown, Revision: 1}
			if _, _, e := tx.PutOperation(op, 0); e != nil {
				return e
			}
		}
		f, e := tx.BeginPersonalMigrationFence("scope", "migration", now)
		if e != nil {
			return e
		}
		_, e = tx.StartPersonalMigrationDrain("scope", "migration", f.Revision, now)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if err = s.Within(ctx, func(tx *Tx) error {
		before, e := tx.PersonalMigrationStatus("scope")
		if e != nil {
			return e
		}
		if before.UnsettledOperations != 2 {
			t.Fatal("missing unsettled operation")
		}
		reads, e := tx.PersonalReadArchiveCandidates("scope")
		if e != nil {
			return e
		}
		if len(reads) != 1 || reads[0].Operation.Kind != "model.list" || reads[0].Command != nil {
			t.Fatal("wrong read candidates")
		}
		if e = tx.RecordPersonalReadArchive("scope", reads[0], now); e != nil {
			return e
		}
		control := reads[0]
		control.Operation.Kind = "turn.interrupt"
		if !errors.Is(tx.RecordPersonalReadArchive("scope", control, now), ErrConflict) {
			t.Fatal("control write archived as read")
		}
		after, e := tx.PersonalMigrationStatus("scope")
		if e != nil {
			return e
		}
		if after.UnsettledOperations != 1 {
			t.Fatal("read archive masked control or was ignored")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Within(ctx, func(tx *Tx) error {
		st, e := tx.PersonalMigrationStatus("scope")
		if e != nil {
			return e
		}
		if st.UnsettledOperations != 1 {
			t.Fatal("archive lost after restart")
		}
		_, e = tx.tx.Exec(`UPDATE personal_migration_read_archives SET evidence_hash='forged' WHERE operation_id='model.list'`)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if err = s.Within(ctx, func(tx *Tx) error { _, e := tx.PersonalMigrationStatus("scope"); return e }); err == nil {
		t.Fatal("tampered archive accepted")
	}
}
