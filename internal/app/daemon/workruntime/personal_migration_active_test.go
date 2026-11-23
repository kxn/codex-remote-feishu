package workruntime

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workcontrol"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestActivePersonalMigrationStopRestartAndQuiescence(t *testing.T) {
	f := newFixture(t, work.BackendClaude, false, true)
	f.start()
	var interrupts atomic.Int32
	f.driver.sendCommand = func(_ context.Context, _ string, c agentproto.Command) error {
		if c.Kind == agentproto.CommandTurnInterrupt {
			interrupts.Add(1)
		}
		return nil
	}
	f.tx(func(tx *workstore.Tx) error {
		fence, e := tx.BeginPersonalMigrationFence("scope", "migration", time.Now())
		if e != nil {
			return e
		}
		if _, e = tx.StartPersonalMigrationDrain("scope", "migration", fence.Revision, time.Now()); e != nil {
			return e
		}
		_, e = tx.RequestPersonalMigrationStops("scope", "migration", time.Now())
		return e
	})
	if err := workcontrol.NewStops(f.store, f.driver).RunScope(context.Background(), "scope"); err != nil {
		t.Fatal(err)
	}
	if interrupts.Load() != 1 {
		t.Fatalf("interrupts=%d", interrupts.Load())
	}
	// Reconstruct the stop coordinator after the original dispatch committed.
	if err := workcontrol.NewStops(f.store, f.driver).RunScope(context.Background(), "scope"); err != nil {
		t.Fatal(err)
	}
	if interrupts.Load() != 1 {
		t.Fatal("interrupt was repeated")
	}
	if err := f.handle(agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "session", TurnID: "turn-1", Status: "interrupted"}); err != nil {
		t.Fatal(err)
	}
	if err := f.coordinator.Reconcile(context.Background(), "attempt"); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error { _, e := tx.GetQuiescenceReceipt("attempt"); return e })
}
