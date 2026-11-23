package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workcontrol"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPersonalDrainRetiresRealCarrierAndResumesWithoutAnotherExit(t *testing.T) {
	a, store, starts := shutdownFixture(t, true)
	r := a.teamRuntime
	r.runCancel()
	r.stops = workcontrol.NewStops(store, r.driver)
	first, err := r.advancePersonalSourceDrain(context.Background(), "scope", "migration", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if first.Fence.State != "draining" || *starts != 1 {
		t.Fatal("wrong source state")
	}
	var exits int
	if err = store.Within(context.Background(), func(tx *workstore.Tx) error {
		ops, e := tx.ListOperations("scope")
		if e != nil {
			return e
		}
		for _, op := range ops {
			if op.Kind == "process.exit" {
				exits++
				if op.State != work.OperationConfirmed {
					t.Fatal("process exit lacks proof")
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if exits != 1 {
		t.Fatalf("exit operations %d", exits)
	}
	if err = store.Within(context.Background(), func(tx *workstore.Tx) error {
		proofs, e := collectPersonalSourceStopProof(tx, "scope")
		if e != nil {
			return e
		}
		if len(proofs) != 1 || proofs[0].ExitEvidenceID == "" || proofs[0].RebuildEvidenceID == "" {
			t.Fatal("missing actual worker stop proofs")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A fresh coordinator object loses only in-memory activity counts; the
	// persisted fence and operation receipts still determine recovery.
	resumed := &teamRuntime{store: store, host: r.host, coordinator: r.coordinator, stops: r.stops}
	if _, err = resumed.advancePersonalSourceDrain(context.Background(), "scope", "migration", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = store.Within(context.Background(), func(tx *workstore.Tx) error {
		ops, e := tx.ListOperations("scope")
		if e != nil {
			return e
		}
		count := 0
		for _, op := range ops {
			if op.Kind == "process.exit" {
				count++
			}
		}
		if count != 1 {
			t.Fatal("replayed process exit")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
