package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// PersonalSourceDrain is progress, never an assertion of finality. The caller
// must retain the fence and repeat after genuine runtime/transport evidence.
type PersonalSourceDrain struct {
	Fence        workstore.PersonalMigrationFence
	Status       workstore.PersonalMigrationDrainStatus
	StopAttempts []string
	Workers      []work.WorkerSession
}

type BoundPersonalSourceDrain struct {
	Neutral ingressowner.DrainProof
	Source  PersonalSourceDrain
}

// advanceBoundPersonalSourceDrain stops neutral delivery before closing the
// team writer gate. The proof is rechecked by the neutral owner registry; this
// method does not mark the source final or perform target transfer.
func (r *teamRuntime) advanceBoundPersonalSourceDrain(ctx context.Context, router *ingressowner.Router, binding ingressowner.OwnerScopeBinding, migrationID string, now time.Time) (BoundPersonalSourceDrain, error) {
	var out BoundPersonalSourceDrain
	if router == nil || binding.ScopeID == "" || migrationID == "" {
		return out, fmt.Errorf("bound neutral owner required")
	}
	proof, err := router.BeginBoundOwnerMigration(ctx, binding, migrationID)
	if err != nil {
		return out, err
	}
	if err = router.VerifyBoundOwnerMigrationDrain(ctx, binding, migrationID, proof); err != nil {
		return out, err
	}
	out.Neutral = proof
	out.Source, err = r.advancePersonalSourceDrain(ctx, binding.ScopeID, migrationID, now)
	if out.Source.Fence.ScopeID != "" {
		raw, e := json.Marshal(proof)
		if e != nil {
			return out, errors.Join(err, e)
		}
		e = r.store.Within(ctx, func(tx *workstore.Tx) error {
			return tx.PutContent(work.NewContent("migration-intake-proof:"+proof.Digest, binding.ScopeID, "application/json", raw, now))
		})
		if e != nil {
			return out, errors.Join(err, e)
		}
		if e = r.holdUncertainPersonalPublications(ctx, binding.ScopeID, "migration-intake-proof:"+proof.Digest, now); e != nil {
			return out, errors.Join(err, e)
		}
		if e = r.holdUncertainPersonalReactions(ctx, binding.ScopeID, "migration-intake-proof:"+proof.Digest, now); e != nil {
			return out, errors.Join(err, e)
		}
		if e = r.isolateExitedPersonalWorkers(ctx, binding.ScopeID, "migration-intake-proof:"+proof.Digest, now); e != nil {
			return out, errors.Join(err, e)
		}
		if e = r.isolatePersonalControlOperations(ctx, binding.ScopeID, "migration-intake-proof:"+proof.Digest, now); e != nil {
			return out, errors.Join(err, e)
		}
	}
	if e := router.VerifyBoundOwnerMigrationDrain(ctx, binding, migrationID, proof); e != nil {
		return out, errors.Join(err, e)
	}
	return out, err
}

func (r *teamRuntime) advancePersonalSourceDrain(ctx context.Context, scopeID, migrationID string, now time.Time) (PersonalSourceDrain, error) {
	var result PersonalSourceDrain
	if r.store == nil || r.stops == nil || r.coordinator == nil || r.host == nil {
		return result, fmt.Errorf("team source runtime unavailable")
	}
	fence, _, err := r.beginPersonalSourceFence(ctx, scopeID, migrationID, now)
	if err != nil {
		return result, err
	}
	if fence.State == "prepared" {
		err = r.store.Within(ctx, func(tx *workstore.Tx) error {
			var e error
			fence, e = tx.StartPersonalMigrationDrain(scopeID, migrationID, fence.Revision, now)
			return e
		})
		if err != nil {
			return result, err
		}
	}
	if fence.State != "draining" {
		return result, workstore.ErrConflict
	}
	result.Fence = fence
	if err = r.waitPersonalSourceActivities(ctx, scopeID); err != nil {
		return result, err
	}
	if err = r.archiveStoppedPersonalReads(ctx, scopeID, now); err != nil {
		return result, err
	}
	err = r.store.Within(ctx, func(tx *workstore.Tx) error {
		ops, e := tx.ListOperations(scopeID)
		if e != nil {
			return e
		}
		for _, op := range ops {
			if op.State != work.OperationPrepared {
				continue
			}
			if e = tx.PreservePersonalMigrationUnsent(scopeID, "operation", op.ID, op.Revision, now); e != nil && !errors.Is(e, workstore.ErrConflict) {
				return e
			}
		}
		out, e := tx.ListOutbox(scopeID)
		if e != nil {
			return e
		}
		for _, v := range out {
			if v.State != work.OutboxPending {
				continue
			}
			if e = tx.PreservePersonalMigrationUnsent(scopeID, "outbox", v.ID, v.Revision, now); e != nil && !errors.Is(e, workstore.ErrConflict) {
				return e
			}
		}
		previews, e := tx.ListPersonalMigrationPreviews(scopeID)
		if e != nil {
			return e
		}
		for _, v := range previews {
			if v.PreviewState != "queued" {
				continue
			}
			if e = tx.PreservePersonalMigrationUnsent(scopeID, "preview", v.ID, v.Revision, now); e != nil && !errors.Is(e, workstore.ErrConflict) {
				return e
			}
		}
		result.StopAttempts, e = tx.RequestPersonalMigrationStops(scopeID, migrationID, now)
		return e
	})
	if err != nil {
		return result, err
	}
	if err = r.stops.RunScope(ctx, scopeID); err != nil {
		return result, err
	}
	var failures []error
	for _, id := range result.StopAttempts {
		if err = r.coordinator.Reconcile(ctx, id); err != nil {
			failures = append(failures, fmt.Errorf("attempt %s: %w", id, err))
		}
	}
	err = r.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		result.Status, e = tx.PersonalMigrationStatus(scopeID)
		if e != nil {
			return e
		}
		result.Workers, e = tx.ListWorkers(scopeID)
		return e
	})
	if err != nil {
		return result, errors.Join(err, errors.Join(failures...))
	}
	if result.Status.UnsettledAttempts == 0 && result.Status.OccupiedCarriers == 0 && result.Status.OccupiedResources == 0 && result.Status.OccupiedControllers == 0 {
		for _, worker := range result.Workers {
			if worker.State == work.WorkerRetired {
				continue
			}
			var isolated workstore.PersonalExecutionIsolation
			holdErr := r.store.Within(ctx, func(tx *workstore.Tx) error {
				var e error
				isolated, e = tx.GetPersonalExecutionIsolation(scopeID, worker.ID)
				return e
			})
			if holdErr == nil {
				if holdErr = r.verifyPersonalExecutionIsolation(ctx, isolated); holdErr != nil {
					failures = append(failures, fmt.Errorf("worker %s isolation: %w", worker.ID, holdErr))
				}
				continue
			}
			if !errors.Is(holdErr, workstore.ErrNotFound) {
				failures = append(failures, fmt.Errorf("worker %s isolation: %w", worker.ID, holdErr))
				continue
			}
			if worker.State == work.WorkerPreparing {
				var unsent bool
				err = r.store.Within(ctx, func(tx *workstore.Tx) error {
					var e error
					unsent, e = tx.WorkerLaunchUnsent(worker)
					if e != nil || !unsent {
						return e
					}
					_, e = tx.GetCarrierProcess(worker.CarrierIncarnation)
					if errors.Is(e, workstore.ErrNotFound) {
						ops, e := tx.ListOperations(scopeID)
						if e != nil {
							return e
						}
						for _, op := range ops {
							if op.Kind == "worker.launch" && op.TargetID == worker.ID && op.TargetGeneration == worker.Generation && op.State == work.OperationPrepared {
								return tx.PreservePersonalMigrationUnsent(scopeID, "operation", op.ID, op.Revision, now)
							}
						}
						return workstore.ErrConflict
					}
					if e != nil {
						return e
					}
					unsent = false
					return nil
				})
				if err != nil {
					failures = append(failures, fmt.Errorf("worker %s: %w", worker.ID, err))
					continue
				}
				if unsent {
					continue
				}
			}
			if worker.Controller != work.ControllerAutomatic || worker.CarrierIncarnation == "" || (worker.State != work.WorkerReady && worker.State != work.WorkerRecovering) {
				failures = append(failures, fmt.Errorf("worker %s lacks safely stoppable carrier", worker.ID))
				continue
			}
			if err = r.host.Retire(ctx, worker.ID); err != nil {
				failures = append(failures, fmt.Errorf("worker %s: %w", worker.ID, err))
			}
		}
	}
	return result, errors.Join(failures...)
}
