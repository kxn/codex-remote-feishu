package executionowner

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionReadDoesNotClaimUnmanagedAndRejectsForeign(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "registry.db"), verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	reg := Registration{Carrier: Carrier{InstanceID: "one", Incarnation: "inc-one", Backend: "codex", Namespace: "account", Generation: 1}, Owner: Owner{Application: Private, ID: "one", Epoch: 1}}
	foreign := Registration{Carrier: Carrier{InstanceID: "two", Incarnation: "inc-two", Backend: "codex", Namespace: "account", Generation: 1}, Owner: Owner{Application: Team, ID: "two", Epoch: 1}}
	for _, v := range []Registration{reg, foreign} {
		if err = r.Register(t.Context(), v); err != nil {
			t.Fatal(err)
		}
	}
	if err = r.WithScopedLease(t.Context(), reg, func(l *Lease) error { return l.CheckSessionRead(reg, "local") }); err != nil {
		t.Fatal(err)
	}
	if err = r.BindSession(t.Context(), foreign, "local"); err != nil {
		t.Fatalf("read reserved unknown session: %v", err)
	}
	if err = r.WithScopedLease(t.Context(), reg, func(l *Lease) error { return l.CheckSessionRead(reg, "local") }); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign read: %v", err)
	}
	if err = r.WithScopedLease(t.Context(), reg, func(l *Lease) error { return l.ReserveSession(reg, "local") }); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign write: %v", err)
	}
	if err = r.WithScopedLease(t.Context(), reg, func(l *Lease) error { return l.CheckSessionControl(reg, "local") }); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign control: %v", err)
	}
	if err = r.WithScopedLease(t.Context(), foreign, func(l *Lease) error { return l.CheckSessionControl(foreign, "local") }); err != nil {
		t.Fatal(err)
	}
}

func TestReadIntentSchemaMigratesNonemptyV1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.db")
	r, err := Open(path, verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	a := Registration{Carrier: Carrier{InstanceID: "a", Incarnation: "a-inc", Backend: "codex", Namespace: "account", Generation: 1}, Owner: Owner{Application: Private, ID: "a", Epoch: 1}}
	b := Registration{Carrier: Carrier{InstanceID: "b", Incarnation: "b-inc", Backend: "codex", Namespace: "account", Generation: 1}, Owner: Owner{Application: Team, ID: "b", Epoch: 1}}
	for _, v := range []Registration{a, b} {
		if err = r.Register(t.Context(), v); err != nil {
			t.Fatal(err)
		}
	}
	if err = r.BindSession(t.Context(), a, "history"); err != nil {
		t.Fatal(err)
	}
	if err = r.ReserveSession(t.Context(), b, "reserved"); err != nil {
		t.Fatal(err)
	}
	if err = r.Transfer(t.Context(), a, Owner{Private, "next", 2}, "proof"); err != nil {
		t.Fatal(err)
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`DROP TABLE session_read_intents`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`PRAGMA user_version=1`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	r, err = Open(path, verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, q := range []string{`SELECT count(*) FROM carriers WHERE incarnation IN ('a-inc','b-inc')`, `SELECT count(*) FROM sessions WHERE incarnation='a-inc' AND session_id='history' AND active=0`, `SELECT count(*) FROM session_reservations WHERE incarnation='b-inc' AND session_id='reserved'`, `SELECT count(*) FROM transfers WHERE incarnation='a-inc' AND new_owner='next'`, `SELECT count(*) FROM stop_receipts WHERE incarnation='a-inc' AND evidence_ref='proof'`} {
		var count int
		if err = r.db.QueryRow(q).Scan(&count); err != nil || count == 0 {
			t.Fatalf("migration lost %s: count=%d err=%v", q, count, err)
		}
	}
	var version int
	if err = r.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 2 {
		t.Fatalf("version=%d err=%v", version, err)
	}
}

func TestReadIntentFencesForeignClaimUntilExactCompletion(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "registry.db"), verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	a := Registration{Carrier: Carrier{InstanceID: "a", Incarnation: "a-inc", Backend: "codex", Namespace: "account", Generation: 1}, Owner: Owner{Application: Private, ID: "a", Epoch: 1}}
	b := Registration{Carrier: Carrier{InstanceID: "b", Incarnation: "b-inc", Backend: "codex", Namespace: "account", Generation: 1}, Owner: Owner{Application: Team, ID: "b", Epoch: 1}}
	for _, v := range []Registration{a, b} {
		if err = r.Register(t.Context(), v); err != nil {
			t.Fatal(err)
		}
	}
	if err = r.WithScopedLease(t.Context(), a, func(l *Lease) error { return l.BeginSessionRead(a, "local", "op-1", time.Now().Add(time.Minute)) }); err != nil {
		t.Fatal(err)
	}
	if err = r.WithScopedLease(t.Context(), a, func(l *Lease) error { return l.BeginSessionRead(a, "local", "op-2", time.Now().Add(time.Minute)) }); err != nil {
		t.Fatal(err)
	}
	if err = r.BindSession(t.Context(), b, "local"); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign claim crossed pending read: %v", err)
	}
	if err = r.WithScopedLease(t.Context(), a, func(l *Lease) error { return l.FinishSessionRead(a, "local", "op-1") }); err != nil {
		t.Fatal(err)
	}
	if err = r.BindSession(t.Context(), b, "local"); !errors.Is(err, ErrConflict) {
		t.Fatalf("second pending read lost: %v", err)
	}
	if err = r.WithScopedLease(t.Context(), a, func(l *Lease) error { return l.FinishSessionRead(a, "local", "op-2") }); err != nil {
		t.Fatal(err)
	}
	if err = r.BindSession(t.Context(), b, "local"); err != nil {
		t.Fatal(err)
	}
}

func TestReadIntentExpirySurvivesRestartAndDoesNotReleaseWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.db")
	r, err := Open(path, verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	a := Registration{Carrier: Carrier{InstanceID: "a", Incarnation: "a-inc", Backend: "codex", Namespace: "account", Generation: 1}, Owner: Owner{Application: Private, ID: "a", Epoch: 1}}
	b := Registration{Carrier: Carrier{InstanceID: "b", Incarnation: "b-inc", Backend: "codex", Namespace: "account", Generation: 1}, Owner: Owner{Application: Team, ID: "b", Epoch: 1}}
	for _, v := range []Registration{a, b} {
		if err = r.Register(t.Context(), v); err != nil {
			t.Fatal(err)
		}
	}
	if err = r.WithScopedLease(t.Context(), a, func(l *Lease) error {
		if e := l.ReserveSession(a, "write"); e != nil {
			return e
		}
		return l.BeginSessionRead(a, "write", "read-write", time.Now().Add(time.Minute))
	}); err != nil {
		t.Fatal(err)
	}
	if err = r.WithScopedLease(t.Context(), a, func(l *Lease) error { return l.FinishSessionRead(a, "write", "read-write") }); err != nil {
		t.Fatal(err)
	}
	if err = r.BindSession(t.Context(), b, "write"); !errors.Is(err, ErrConflict) {
		t.Fatalf("read finish released write reservation: %v", err)
	}
	if err = r.WithScopedLease(t.Context(), a, func(l *Lease) error { return l.BeginSessionRead(a, "local", "expired", time.Now().Add(time.Minute)) }); err != nil {
		t.Fatal(err)
	}
	if _, err = r.db.Exec(`UPDATE session_read_intents SET expires_ns=? WHERE operation_id=?`, time.Now().Add(-time.Second).UnixNano(), "expired"); err != nil {
		t.Fatal(err)
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	r, err = Open(path, verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err = r.BindSession(t.Context(), b, "local"); err != nil {
		t.Fatal(err)
	}
	if err = r.WithScopedLease(t.Context(), a, func(l *Lease) error { return l.BeginSessionRead(a, "local", "expired", time.Now().Add(time.Minute)) }); !errors.Is(err, ErrReadIntentExpired) {
		t.Fatalf("expired in-flight operation was retried: %v", err)
	}
	if err = r.WithScopedLease(t.Context(), a, func(l *Lease) error { return l.CheckSessionRead(a, "local") }); !errors.Is(err, ErrConflict) {
		t.Fatalf("late result accepted after foreign claim: %v", err)
	}
}

func TestReadIntentRetirementReleasesOnlyRetiredCarrier(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.db")
	r, err := Open(path, verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	a := Registration{Carrier: Carrier{InstanceID: "a", Incarnation: "a-inc", Backend: "codex", Namespace: "account", Generation: 1}, Owner: Owner{Application: Private, ID: "a", Epoch: 1}}
	b := Registration{Carrier: Carrier{InstanceID: "b", Incarnation: "b-inc", Backend: "codex", Namespace: "account", Generation: 1}, Owner: Owner{Application: Team, ID: "b", Epoch: 1}}
	for _, v := range []Registration{a, b} {
		if err = r.Register(t.Context(), v); err != nil {
			t.Fatal(err)
		}
	}
	if err = r.WithScopedLease(t.Context(), a, func(l *Lease) error { return l.BeginSessionRead(a, "history", "read", time.Now().Add(time.Minute)) }); err != nil {
		t.Fatal(err)
	}
	if err = r.Retire(t.Context(), a, "proof"); err != nil {
		t.Fatal(err)
	}
	if err = r.BindSession(t.Context(), b, "history"); err != nil {
		t.Fatal(err)
	}
	var allowed bool
	if err = r.WithRetiredSessionRead(t.Context(), a, "proof", "history", "read", func(ok bool) error { allowed = ok; return nil }); err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("retired late read observed a foreign claimed session")
	}
	if err = r.WithRetiredSessionRead(t.Context(), a, "wrong", "history", "read", func(bool) error { t.Fatal("invalid stop proof entered callback"); return nil }); !errors.Is(err, ErrConflict) {
		t.Fatalf("invalid proof: %v", err)
	}
}

func TestRetiredReadMissingIntentStillExpiresUnderExactStopFence(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "registry.db"), verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	a := Registration{Carrier: Carrier{InstanceID: "a", Incarnation: "a-inc", Backend: "codex", Namespace: "account", Generation: 1}, Owner: Owner{Application: Private, ID: "a", Epoch: 1}}
	if err = r.Register(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if err = r.Retire(t.Context(), a, "proof"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(-time.Second).UnixNano()
	called := 0
	if err = r.WithRetiredReadExpiration(t.Context(), a, "proof", "thread", "op", deadline, func(got int64) error {
		called++
		if got != deadline {
			t.Fatalf("deadline changed: %d", got)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatal("missing registry row prevented proven expired observation")
	}
	if err = r.WithRetiredReadExpiration(t.Context(), a, "wrong", "thread", "op", deadline, func(int64) error { t.Fatal("invalid stop entered callback"); return nil }); !errors.Is(err, ErrConflict) {
		t.Fatalf("invalid proof: %v", err)
	}
}
