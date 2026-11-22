package workruntime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// VerifyRetiredQuiescence combines a committed three-domain runtime receipt,
// explicit protocol watermarks and exact OS exit. None alone proves recovery.
func VerifyRetiredQuiescence(ctx context.Context, store *workstore.Store, stateDir, localHostID string, worker work.WorkerSession, attemptID string) (NativeObservation, error) {
	var observation NativeObservation
	var snapshot agentproto.RuntimeLaunchSnapshot
	var process agentproto.RuntimeProcessIdentity
	err := store.Within(ctx, func(tx *workstore.Tx) error {
		gate, e := tx.GetRuntimeGate(worker.ID)
		if e != nil {
			return e
		}
		if gate.AttemptID != attemptID || attemptID == "" {
			return fmt.Errorf("retired recovery requires the exact gated attempt")
		}
		proof, e := tx.GetQuiescenceReceipt(attemptID)
		if e != nil {
			return e
		}
		if e = proof.Validate(); e != nil {
			return e
		}
		if proof.RunRef.WorkerID != worker.ID || proof.RunRef.Generation != worker.Generation || proof.CarrierIncarnation != worker.CarrierIncarnation || proof.InstanceID != worker.InstanceID || proof.InstanceGeneration != worker.InstanceGeneration || proof.ProcessGeneration != worker.ProcessGeneration {
			return fmt.Errorf("retired proof belongs to another carrier")
		}
		body, e := tx.GetContent(worker.ScopeID, proof.Source.EvidenceRef)
		if e != nil {
			return e
		}
		if e = json.Unmarshal(body.Data, &observation); e != nil {
			return e
		}
		if observation.CanonicalCommandWatermark <= 0 || observation.InspectionCommandID == "" || observation.NativeWriteWatermark <= 0 {
			return fmt.Errorf("quiescence predates durable recovery watermarks")
		}
		index, e := tx.NativeCommandWatermark(observation.InspectionCommandID)
		if e != nil {
			return e
		}
		if index != observation.CanonicalCommandWatermark {
			return fmt.Errorf("quiescence canonical command watermark mismatch")
		}
		if e = tx.AssertNoEffectfulCommandsAfter(worker.CarrierIncarnation, index); e != nil {
			return e
		}
		snapshot, e = tx.GetNativeLaunchSnapshot(worker.ID, worker.Generation)
		if e != nil {
			return e
		}
		process, e = tx.GetCarrierProcess(worker.CarrierIncarnation)
		if e == nil && (localHostID == "" || snapshot.HostID != localHostID || process.HostID != snapshot.HostID) {
			return fmt.Errorf("retired process has no matching explicit host identity")
		}
		if e != nil {
			return e
		}
		carrier, e := tx.GetCarrier(worker.CarrierIncarnation)
		if e != nil {
			return e
		}
		request := InspectionRequest{Carrier: carrier, Binding: carrier.Owner, SessionID: observation.SessionID, TurnID: observation.TurnID, LocationID: worker.LocationID, LocationGeneration: worker.LocationGeneration}
		return observation.ValidateStopped(request)
	})
	if err != nil {
		return observation, err
	}
	evidence, err := verifyRetiredCarrierFence(ctx, store, stateDir, localHostID, worker, observation)
	if err != nil {
		return observation, err
	}
	observation.RetiredCarrierVerified = true
	observation.RetirementEvidence = evidence
	return observation, nil
}
