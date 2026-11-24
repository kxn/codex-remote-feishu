package daemon

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPersonalControlIsolationRequiresExactAttemptAndPreservesUnknown(t *testing.T) {
	_, store, attempt, now := activePersonalSourceFixture(t)
	ctx := t.Context()
	var isolation workstore.PersonalExecutionIsolation
	if err := store.Within(ctx, func(tx *workstore.Tx) error {
		worker, e := tx.GetWorker(attempt.RunRef.WorkerID)
		if e != nil {
			return e
		}
		carrier, e := tx.GetCarrier(worker.CarrierIncarnation)
		if e != nil {
			return e
		}
		process, e := tx.GetCarrierProcess(worker.CarrierIncarnation)
		if e != nil {
			return e
		}
		for _, registered := range []bool{false, true} {
			id := "unregistered"
			if registered {
				id = "registered"
			}
			binding := carrier.Owner
			binding.AttemptID = attempt.ID
			binding.RunRef = attempt.RunRef.RunID
			command := agentproto.Command{CommandID: id, Kind: agentproto.CommandThreadGoalClear, Execution: &binding, Target: agentproto.Target{ThreadID: "native-thread"}}
			raw, e := json.Marshal(command)
			if e != nil {
				return e
			}
			content := work.NewContent(id+":request", worker.ScopeID, "application/json", raw, now)
			if e = tx.PutContent(content); e != nil {
				return e
			}
			op := work.Operation{ID: id, ScopeID: worker.ScopeID, ContextID: worker.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "admission-gate"}, Kind: "native.gate_stop", TargetID: worker.ID, TargetGeneration: worker.Generation, IdempotencyKey: id, RequestHash: content.SHA256, RequestRef: content.ID, State: work.OperationUnknown, Revision: 1, CreatedAt: now, UpdatedAt: now}
			if _, _, e = tx.PutOperation(op, 0); e != nil {
				return e
			}
			if registered {
				if _, e = tx.RegisterNativeCommand(worker.CarrierIncarnation, command); e != nil {
					return e
				}
			}
		}
		fence, e := tx.BeginPersonalMigrationFence(worker.ScopeID, "migration", now)
		if e != nil {
			return e
		}
		if _, e = tx.StartPersonalMigrationDrain(worker.ScopeID, "migration", fence.Revision, now); e != nil {
			return e
		}
		proof := work.NewContent("migration-intake-proof:digest", worker.ScopeID, "application/json", []byte("{\"migration_id\":\"migration\",\"digest\":\"digest\"}"), now)
		if e = tx.PutContent(proof); e != nil {
			return e
		}
		isolation = workstore.PersonalExecutionIsolation{Worker: worker, Process: process, LaunchHash: "hash", JournalLaunchHash: "hash", InventoryHash: "inventory", ProcessScopesStopped: true}
		return tx.HoldPersonalExecutionIsolation(worker.ScopeID, proof.ID, isolation, now)
	}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"registered", "unregistered"} {
		if err := store.Within(ctx, func(tx *workstore.Tx) error {
			candidate, e := tx.PersonalControlIsolationCandidate("scope", id)
			if id == "unregistered" {
				if !errors.Is(e, workstore.ErrConflict) {
					t.Fatalf("unproven session accepted: %v", e)
				}
				return nil
			}
			if e != nil {
				return e
			}
			if (candidate.Native != nil) != (id == "registered") {
				t.Fatal("native registration changed")
			}
			bad := candidate
			bad.Request.Execution = agentproto.CloneExecutionBinding(candidate.Request.Execution)
			bad.Request.Execution.SessionGeneration++
			if e = tx.RecordPersonalControlIsolation("scope", "migration-intake-proof:digest", bad, now); !errors.Is(e, workstore.ErrConflict) {
				t.Fatalf("changed generation: %v", e)
			}
			if e = tx.RecordPersonalControlIsolation("scope", "migration-intake-proof:digest", candidate, now); e != nil {
				return e
			}
			current, e := tx.GetOperation(id)
			if e != nil {
				return e
			}
			if current.State != work.OperationUnknown {
				t.Fatal("unknown changed")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Within(ctx, func(tx *workstore.Tx) error {
		holds, e := tx.ListPersonalControlIsolations("scope")
		if e != nil {
			return e
		}
		if len(holds) != 1 {
			t.Fatalf("holds=%d", len(holds))
		}
		status, e := tx.PersonalMigrationStatus("scope")
		if e != nil {
			return e
		}
		if status.UnsettledOperations != 2 { // prepared start and unproven control remain
			t.Fatalf("control holds altered unrelated pending operations: %d", status.UnsettledOperations)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
