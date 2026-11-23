package workruntime

import (
	"context"
	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// A disconnected idle carrier has no active attempt to trigger reconciliation.
// Its last exact quiescence proof remains usable only with both write fences
// and independently verified process exit, never merely because it is offline.
func VerifyRetiredIdleCarrier(ctx context.Context, store *workstore.Store, stateDir, hostID string, worker work.WorkerSession) (NativeObservation, error) {
	var observation NativeObservation
	var gateRevision int64
	check := func(tx *workstore.Tx) error {
		current, err := tx.GetWorker(worker.ID)
		if err != nil {
			return err
		}
		if current.Revision != worker.Revision || current.State != work.WorkerRecovering || current.Controller != work.ControllerAutomatic {
			return workstore.ErrConflict
		}
		gate, err := tx.GetRuntimeGate(worker.ID)
		if err != nil {
			return err
		}
		if gate.AttemptID != "" || gate.RecoveryReason != "" || gate.Reason != "carrier_disconnected_reconcile" {
			return workstore.ErrConflict
		}
		if gateRevision != 0 && gateRevision != gate.Revision {
			return workstore.ErrConflict
		}
		gateRevision = gate.Revision
		connected, err := tx.CarrierConnected(worker.CarrierIncarnation)
		if err != nil {
			return err
		}
		occupied, err := tx.WorkerHasOccupancy(worker.ID)
		if err != nil {
			return err
		}
		unresolved, err := tx.CountUnresolvedNativeEvents(worker.CarrierIncarnation)
		if err != nil {
			return err
		}
		if connected || occupied || unresolved != 0 {
			return workstore.ErrConflict
		}
		attemptID, err := tx.CarrierQuiescenceAttempt(worker)
		if err != nil {
			return err
		}
		proof, err := tx.GetQuiescenceReceipt(attemptID)
		if err != nil {
			return err
		}
		body, err := tx.GetContent(worker.ScopeID, proof.Source.EvidenceRef)
		if err != nil {
			return err
		}
		return json.Unmarshal(body.Data, &observation)
	}
	if err := store.Within(ctx, check); err != nil {
		return observation, err
	}
	evidence, err := verifyRetiredCarrierFence(ctx, store, stateDir, hostID, worker, observation)
	if err != nil {
		return observation, err
	}
	if err = store.Within(ctx, check); err != nil {
		return observation, err
	}
	observation.RetiredCarrierVerified, observation.RetirementEvidence = true, evidence
	observation.ObservedAt = evidence.VerifiedAt
	return observation, nil
}
