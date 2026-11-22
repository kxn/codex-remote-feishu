package workruntime

import (
	"context"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type RetirementEvidence struct {
	Process                   agentproto.RuntimeProcessIdentity `json:"process"`
	VerifiedHostID            string                            `json:"verified_host_id"`
	VerifiedAt                time.Time                         `json:"verified_at"`
	OriginalObservedAt        time.Time                         `json:"original_observed_at"`
	InspectionSource          work.SourceRef                    `json:"inspection_source"`
	NativeWriteWatermark      int64                             `json:"native_write_watermark"`
	CanonicalCommandWatermark int64                             `json:"canonical_command_watermark"`
}

// Reinterpret a committed inspection only after exact retirement proves that
// its native/continuation/background facts cannot have gained unseen work.
// No prior QuiescenceReceipt is required and no frame is replayed.
func VerifyRetiredInspection(ctx context.Context, store *workstore.Store, stateDir, localHostID string, request InspectionRequest) (NativeObservation, error) {
	var observation NativeObservation
	var worker work.WorkerSession
	var gateRevision int64
	err := store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		worker, err = tx.GetWorker(request.Binding.WorkerSessionID)
		if err != nil {
			return err
		}
		gateRevision, err = retiredInspectionState(tx, worker, request)
		if err != nil {
			return err
		}
		committed, err := tx.LatestCommittedInspection(request.Carrier.Incarnation, request.Binding, request.SessionID, request.TurnID)
		if err != nil {
			return err
		}
		canonical := committed.Command.RuntimeInspection
		if canonical.SubjectCommandID != request.SubjectCommandID {
			return fmt.Errorf("retired inspection subject differs from original prompt")
		}
		observation, err = ObserveRuntimeInspection(request, *committed.Event.RuntimeInspection)
		if err != nil {
			return err
		}
		if err = observation.ValidateStopped(request); err != nil {
			return err
		}
		observation.Source = committed.Source
		observation.InspectionCommandID = committed.Command.CommandID
		observation.CanonicalCommandWatermark = committed.CanonicalWatermark
		observation.NativeWriteWatermark = committed.Event.RuntimeInspection.NativeWriteWatermark
		return tx.AssertNoEffectfulCommandsAfter(worker.CarrierIncarnation, observation.CanonicalCommandWatermark)
	})
	if err != nil {
		return observation, err
	}
	evidence, err := verifyRetiredCarrierFence(ctx, store, stateDir, localHostID, worker, observation)
	if err != nil {
		return observation, err
	}
	err = store.Within(ctx, func(tx *workstore.Tx) error {
		current, err := tx.GetWorker(worker.ID)
		if err != nil {
			return err
		}
		revision, err := retiredInspectionState(tx, current, request)
		if err != nil {
			return err
		}
		if current.Revision != worker.Revision || revision != gateRevision {
			return workstore.ErrConflict
		}
		return tx.AssertNoEffectfulCommandsAfter(worker.CarrierIncarnation, observation.CanonicalCommandWatermark)
	})
	if err != nil {
		return observation, err
	}
	observation.RetiredCarrierVerified = true
	observation.RetirementEvidence = evidence
	observation.ObservedAt = evidence.VerifiedAt
	return observation, nil
}

func retiredInspectionState(tx *workstore.Tx, worker work.WorkerSession, request InspectionRequest) (int64, error) {
	if worker.State != work.WorkerRecovering || worker.Controller != work.ControllerAutomatic || !request.Carrier.Matches(worker) || request.Binding.AttemptID == "" || request.Binding.WorkerSessionID != worker.ID {
		return 0, workstore.ErrConflict
	}
	if request.SessionID != worker.NativeSessionID || request.LocationID != worker.LocationID || request.LocationGeneration != worker.LocationGeneration {
		return 0, workstore.ErrConflict
	}
	unresolved, err := tx.CountUnresolvedNativeEvents(worker.CarrierIncarnation)
	if err != nil {
		return 0, err
	}
	if unresolved != 0 {
		return 0, fmt.Errorf("retired carrier has unresolved native evidence")
	}
	connected, err := tx.CarrierConnected(worker.CarrierIncarnation)
	if err != nil {
		return 0, err
	}
	if connected {
		return 0, fmt.Errorf("live carrier requires a fresh native inspection")
	}
	attempt, err := tx.GetAttempt(request.Binding.AttemptID)
	if err != nil {
		return 0, err
	}
	if attempt.RunRef.WorkerID != worker.ID || attempt.RunRef.Generation != worker.Generation || attempt.RunRef.RunID != request.Binding.RunRef || attempt.ScopeID != worker.ScopeID || attempt.ContextID != worker.ContextID || attempt.CommandID != request.SubjectCommandID {
		return 0, workstore.ErrConflict
	}
	gate, err := tx.GetRuntimeGate(worker.ID)
	if err != nil {
		return 0, err
	}
	if gate.AttemptID != attempt.ID || gate.ScopeID != worker.ScopeID {
		return 0, workstore.ErrConflict
	}
	return gate.Revision, nil
}

// Shared by both retirement entry points: host identity, exact OS exit, frozen
// launch contract, canonical fence and native journal integrity are independent
// requirements. Process disappearance alone never proves work quiescent.
func verifyRetiredCarrierFence(ctx context.Context, store *workstore.Store, stateDir, localHostID string, worker work.WorkerSession, observation NativeObservation) (*RetirementEvidence, error) {
	var snapshot agentproto.RuntimeLaunchSnapshot
	var process agentproto.RuntimeProcessIdentity
	err := store.Within(ctx, func(tx *workstore.Tx) error {
		if observation.CanonicalCommandWatermark <= 0 || observation.NativeWriteWatermark <= 0 || observation.InspectionCommandID == "" {
			return fmt.Errorf("retirement requires both durable watermarks")
		}
		index, err := tx.NativeCommandWatermark(observation.InspectionCommandID)
		if err != nil {
			return err
		}
		if index != observation.CanonicalCommandWatermark {
			return fmt.Errorf("canonical inspection watermark mismatch")
		}
		if err = tx.AssertNoEffectfulCommandsAfter(worker.CarrierIncarnation, index); err != nil {
			return err
		}
		snapshot, err = tx.GetNativeLaunchSnapshot(worker.ID, worker.Generation)
		if err != nil {
			return err
		}
		process, err = tx.GetCarrierProcess(worker.CarrierIncarnation)
		if err != nil {
			return err
		}
		if localHostID == "" || snapshot.HostID != localHostID || process.HostID != snapshot.HostID {
			return fmt.Errorf("retired process has no matching explicit host identity")
		}
		if snapshot.CarrierIncarnation != worker.CarrierIncarnation || snapshot.InstanceGeneration != worker.InstanceGeneration || snapshot.ProcessGeneration != worker.ProcessGeneration || snapshot.LocationGeneration != worker.LocationGeneration {
			return workstore.ErrConflict
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if err = hostinventory.WaitProcessExit(waitCtx, hostinventory.ProcessIdentity{Platform: process.Platform, PID: process.PID, StartIdentity: process.StartIdentity}); err != nil {
		return nil, fmt.Errorf("exact carrier exit is unproven: %w", err)
	}
	if err = runtimejournal.VerifyRetiredWriteFence(ctx, snapshot, stateDir, observation.NativeWriteWatermark); err != nil {
		return nil, err
	}
	originalObservedAt := observation.ObservedAt
	if observation.RetirementEvidence != nil && !observation.RetirementEvidence.OriginalObservedAt.IsZero() {
		originalObservedAt = observation.RetirementEvidence.OriginalObservedAt
	}
	return &RetirementEvidence{Process: process, VerifiedHostID: localHostID, VerifiedAt: time.Now().UTC(), OriginalObservedAt: originalObservedAt, InspectionSource: observation.Source, NativeWriteWatermark: observation.NativeWriteWatermark, CanonicalCommandWatermark: observation.CanonicalCommandWatermark}, nil
}
