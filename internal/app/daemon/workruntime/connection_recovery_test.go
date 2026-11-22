package workruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestDaemonStartupReconcilesDeadPersistedConnectedCarrierWithoutOfflineRPC(t *testing.T) {
	f, _ := retiredInspectionFixture(t, "connected", false)
	options := f.coordinator.options
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := workstore.Open(t.Context(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	f.store, f.coordinator = reopened, New(reopened, options)
	calls := 0
	f.driver.inspect = func(InspectionRequest) (NativeObservation, error) {
		calls++
		return NativeObservation{}, errors.New("old daemon socket no longer exists")
	}
	if err = f.coordinator.RecoverConnectionsOnStartup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = f.coordinator.Reconcile(context.Background(), "attempt"); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("startup kept routing an exited old connection to live RPC")
	}
	f.tx(func(tx *workstore.Tx) error {
		input, err := tx.GetInbox("input")
		if err != nil {
			return err
		}
		gate, err := tx.GetRuntimeGate("worker")
		if err != nil {
			return err
		}
		if input.State != work.InboxProcessed || gate.Reason != "snapshot_rebuild_required" || gate.AttemptID != "attempt" {
			t.Fatalf("old connected carrier never completed real retirement: input=%s gate=%+v", input.State, gate)
		}
		return nil
	})
}

func TestDaemonStartupExpiresPersistedConnectionWithoutClaimingQuiescence(t *testing.T) {
	f := newFixture(t, work.BackendCodex)
	f.start()
	options := f.coordinator.options
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := workstore.Open(t.Context(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	f.store = reopened
	f.coordinator = New(reopened, options)
	f.tx(func(tx *workstore.Tx) error {
		connected, err := tx.CarrierConnected(f.carrier.Incarnation)
		if !connected {
			t.Fatal("ordinary Store.Open unexpectedly changed live connection state")
		}
		return err
	})
	if err = f.coordinator.RecoverConnectionsOnStartup(t.Context()); err != nil {
		t.Fatal(err)
	}
	var revision int64
	f.tx(func(tx *workstore.Tx) error {
		connected, err := tx.CarrierConnected(f.carrier.Incarnation)
		if err != nil {
			return err
		}
		if connected {
			t.Fatal("old daemon connection survived actual startup")
		}
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		revision = worker.Revision
		gate, err := tx.GetRuntimeGate(worker.ID)
		if err != nil {
			return err
		}
		if worker.State != work.WorkerRecovering || gate.Reason != "carrier_disconnected_reconcile" || gate.AttemptID != "attempt" {
			t.Fatalf("startup did not preserve exact recovery association: %+v %+v", worker, gate)
		}
		input, err := tx.GetInbox("input")
		if err != nil {
			return err
		}
		if input.State != work.InboxClaimed {
			t.Fatal("startup consumed outstanding business input")
		}
		occupied, err := tx.HasCarrierOccupancy(f.carrier, "attempt")
		if err != nil {
			return err
		}
		if !occupied {
			t.Fatal("socket expiry released native occupancy")
		}
		if _, err = tx.GetQuiescenceReceipt("attempt"); !errors.Is(err, workstore.ErrNotFound) {
			t.Fatal("startup manufactured quiescence evidence")
		}
		return nil
	})
	if f.driver.inspections != 0 {
		t.Fatal("startup tried to infer OS state from a native RPC")
	}
	if err = f.coordinator.RecoverConnectionsOnStartup(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		worker, err := tx.GetWorker("worker")
		if worker.Revision != revision {
			t.Fatal("startup recovery replay changed an already-disconnected worker")
		}
		return err
	})
	if err = f.coordinator.HandleHello(t.Context(), f.hello()); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		connected, err := tx.CarrierConnected(f.carrier.Incarnation)
		if !connected {
			t.Fatal("new current daemon Hello was discarded")
		}
		return err
	})
}

func TestStartupDisconnectPreservesExplicitGateAndAttempt(t *testing.T) {
	for _, tt := range []struct{ reason, recovery string }{
		{"missing_processing_report", ""}, {"native_resume_rejected", ""}, {"native_reconciliation", "native_goal_budget_exhausted"}, {"historical_human_controller_requires_reconciliation", ""},
	} {
		t.Run(tt.reason+tt.recovery, func(t *testing.T) {
			f := newFixture(t, work.BackendCodex)
			f.start()
			var original workstore.RuntimeGate
			f.tx(func(tx *workstore.Tx) error {
				worker, err := tx.GetWorker("worker")
				if err != nil {
					return err
				}
				before := worker.Revision
				worker.Revision++
				worker.State = work.WorkerRecovering
				if err = tx.PutWorker(worker, before); err != nil {
					return err
				}
				if err = tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, AttemptID: "attempt", Reason: tt.reason, RecoveryReason: tt.recovery, CreatedAt: f.now}); err != nil {
					return err
				}
				original, err = tx.GetRuntimeGate(worker.ID)
				return err
			})
			if err := f.coordinator.RecoverConnectionsOnStartup(t.Context()); err != nil {
				t.Fatal(err)
			}
			f.tx(func(tx *workstore.Tx) error {
				gate, err := tx.GetRuntimeGate("worker")
				if err != nil {
					return err
				}
				if gate != original {
					t.Fatalf("transport loss erased explicit decision: before=%+v after=%+v", original, gate)
				}
				occupied, err := tx.HasCarrierOccupancy(f.carrier, "attempt")
				if !occupied {
					t.Fatal("protected gate input was released")
				}
				return err
			})
		})
	}
}

func TestStartupDisconnectHandlesReadyAndAlreadyRebuildRequired(t *testing.T) {
	for _, rebuild := range []bool{false, true} {
		t.Run(map[bool]string{false: "ready", true: "rebuild-required"}[rebuild], func(t *testing.T) {
			f := newFixture(t, work.BackendCodex)
			f.start()
			f.report()
			if err := f.handle(agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "session", TurnID: "turn-1", Status: "completed"}); err != nil {
				t.Fatal(err)
			}
			var previous work.WorkerSession
			f.tx(func(tx *workstore.Tx) error {
				worker, err := tx.GetWorker("worker")
				if err != nil {
					return err
				}
				if worker.State != work.WorkerReady {
					t.Fatalf("fixture not ready: %s", worker.State)
				}
				if rebuild {
					before := worker.Revision
					worker.Revision++
					worker.State = work.WorkerRecovering
					if err = tx.PutWorker(worker, before); err != nil {
						return err
					}
					if err = tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, AttemptID: "attempt", Reason: "snapshot_rebuild_required", CreatedAt: f.now}); err != nil {
						return err
					}
				}
				previous = worker
				return nil
			})
			if err := f.coordinator.RecoverConnectionsOnStartup(t.Context()); err != nil {
				t.Fatal(err)
			}
			f.tx(func(tx *workstore.Tx) error {
				worker, err := tx.GetWorker("worker")
				if err != nil {
					return err
				}
				gate, err := tx.GetRuntimeGate(worker.ID)
				if err != nil {
					return err
				}
				if worker.State != work.WorkerRecovering {
					t.Fatal("old ready socket remained dispatchable after startup")
				}
				if rebuild {
					if worker.Revision != previous.Revision || gate.Reason != "snapshot_rebuild_required" || gate.AttemptID != "attempt" {
						t.Fatal("existing proven rebuild obligation was overwritten")
					}
				} else if gate.Reason != "carrier_disconnected_reconcile" {
					t.Fatalf("wrong idle recovery gate: %+v", gate)
				}
				return nil
			})
		})
	}
}
