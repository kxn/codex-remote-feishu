package workruntime

import (
	"context"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestHistoricalHumanControllerRemainsIsolatedOnHelloAndCannotRecoverAsAutomatic(t *testing.T) {
	f := newFixture(t, work.BackendCodex)
	f.start()
	f.tx(func(tx *workstore.Tx) error {
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		// Seed an already-persisted historical handoff, never a new product action.
		if err = tx.ReserveControllerLocation(worker, worker.ControllerGeneration+1); err != nil {
			return err
		}
		before := worker.Revision
		worker.Controller = work.ControllerHuman
		worker.ControllerGeneration++
		worker.State = work.WorkerRecovering
		worker.Revision++
		return tx.PutWorker(worker, before)
	})
	if err := f.coordinator.HandleHello(context.Background(), f.hello()); err != nil {
		t.Fatal(err)
	}
	command := f.command
	command.Kind = agentproto.CommandThreadGoalSet
	if err := f.coordinator.RegisterCommand(context.Background(), f.carrier.Incarnation, command); err == nil {
		t.Fatal("historical human native owner accepted new automatic input")
	}
	f.tx(func(tx *workstore.Tx) error {
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		gate, err := tx.GetRuntimeGate("worker")
		if err != nil {
			return err
		}
		occupied, err := tx.HasCarrierOccupancy(f.carrier, "attempt")
		if err != nil {
			return err
		}
		leases, err := tx.CountControllerOccupancy(worker.ID)
		if err != nil {
			return err
		}
		if worker.Controller != work.ControllerHuman || worker.State != work.WorkerRecovering || gate.Reason != "historical_human_controller_requires_reconciliation" || !occupied || leases == 0 {
			t.Fatal("historical human record was promoted, released or forgotten")
		}
		return nil
	})
}
