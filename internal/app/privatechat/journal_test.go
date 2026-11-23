package privatechat

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestJournalIngressDedupAndPagination(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if _, err = OpenJournal(path); err == nil {
		t.Fatal("second writer opened")
	}
	owner := Owner{"g", "s", "c", "u", "tenant", "app"}
	in := Ingress{Owner: owner, Generation: 1, EventID: "first", Lane: "input", Payload: []byte(`{"text":"hello"}`), Sources: []string{"original"}}
	seq, duplicate, err := j.Admit(ctx, in)
	if err != nil || duplicate {
		t.Fatalf("admit %v %v", duplicate, err)
	}
	in.Generation = 2
	again, duplicate, err := j.Admit(ctx, in)
	if err != nil || !duplicate || again != seq {
		t.Fatalf("dedup %d %v %v", again, duplicate, err)
	}
	in.Owner.ChatID = "other"
	if _, _, err = j.Admit(ctx, in); !errors.Is(err, ErrJournalConflict) {
		t.Fatalf("owner conflict: %v", err)
	}
	in.Owner = owner
	in.Generation = 1
	in.EventID = "second"
	if _, _, err = j.Admit(ctx, in); err != nil {
		t.Fatal(err)
	}
	page, err := j.Pending(ctx, owner, 1, "input", 0, 1)
	if err != nil || len(page) != 1 || page[0].Sequence != seq {
		t.Fatalf("page: %+v %v", page, err)
	}
	page, err = j.Pending(ctx, owner, 1, "input", seq, 1)
	if err != nil || len(page) != 1 || page[0].Ingress.EventID != "second" {
		t.Fatalf("second page: %+v %v", page, err)
	}
}
func TestHistoricalDispatchIsQuarantinedAfterRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	owner := Owner{"g", "s", "c", "u", "tenant", "app"}
	seq, _, err := j.Admit(ctx, Ingress{Owner: owner, Generation: 1, EventID: "old", Lane: "input", Payload: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	d := Dispatch{ID: "old-dispatch", Lane: "input", Mode: "resume", Owner: owner, Generation: 1, Session: NativeSession{"codex", "namespace", "session", "carrier", 1}, CommandID: "old-command", InputSequences: []int64{seq}}
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = prepareDispatchTx(ctx, tx, d); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE dispatch SET state='sending' WHERE id=?`, d.ID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	// Recreate the pre-native v2 schema with a historical sending dispatch.
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`DROP TABLE native_query_proofs; DROP TABLE native_command_receipts; DROP TABLE native_events; DROP TABLE native_commands; DROP TABLE native_carriers; DROP TABLE private_applied; PRAGMA user_version=2;`)
	if err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	if err = legacy.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	state, err := j.DispatchState(ctx, d.ID)
	if err != nil || state != "unknown" {
		t.Fatalf("historical state %q %v", state, err)
	}
	pending, err := j.Pending(ctx, owner, 1, "input", 0, 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("uncertain input released: %+v %v", pending, err)
	}
}
