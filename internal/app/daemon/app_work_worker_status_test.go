package daemon

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateWorkerStatusDistinguishesLazyPreparationFromRecovery(t *testing.T) {
	for _, scenario := range []string{"lazy", "no-gate", "reconciling", "unresolved-recovery", "occupied", "ready"} {
		t.Run(scenario, func(t *testing.T) {
			a, worker := runtimeRecoveryFixture(t)
			pickerTx(t, a, func(tx *workstore.Tx) error {
				worker.State = work.WorkerRecovering
				if scenario == "ready" {
					worker.State = work.WorkerReady
				}
				worker.Revision++
				if scenario == "occupied" {
					worker.Generation++
					worker.LocationID = "status-location"
					worker.LocationGeneration = 1
					if err := tx.PutLocation(work.ExecutionLocation{ID: worker.LocationID, HostID: "local", Cwd: t.TempDir(), Generation: 1, Revision: 1}, 0); err != nil {
						return err
					}
				}
				if err := tx.PutWorker(worker, 1); err != nil {
					return err
				}
				if scenario != "no-gate" {
					gate := workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, Reason: "snapshot_rebuild_required", CreatedAt: time.Now().UTC()}
					if scenario == "reconciling" {
						gate.Reason = "native_reconciliation"
					}
					if scenario == "unresolved-recovery" {
						gate.RecoveryReason = "missing_processing_report"
					}
					if err := tx.PutRuntimeGate(gate); err != nil {
						return err
					}
				}
				if scenario == "occupied" {
					if err := tx.ReserveControllerLocation(worker, 2); err != nil {
						return err
					}
				}
				label, err := teamWorkerStatusLabel(tx, worker)
				if err != nil {
					return err
				}
				want := teamWorkerStateLabel(worker.State)
				if scenario == "lazy" {
					want = "可继续，执行前自动准备"
				}
				if label != want {
					t.Fatalf("label=%q want=%q", label, want)
				}
				return nil
			})
		})
	}
}
