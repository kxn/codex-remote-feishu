package workruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type InspectionRequest struct {
	StopBackground     bool
	ResumeRejected     bool
	SubjectCommandID   string
	Carrier            workstore.CarrierRegistration
	Binding            agentproto.ExecutionBinding
	SessionID          string
	TurnID             string
	LocationID         string
	LocationGeneration int64
}

// Only the trusted host driver constructs observations. Native/continuation
// frames and background process/task inspection are separate evidence domains.
// A model report, socket close, timeout, or PID disappearance is insufficient.
type NativeObservation struct {
	RetirementEvidence        *RetirementEvidence `json:"retirement_evidence,omitempty"`
	RetiredCarrierVerified    bool                `json:"retired_carrier_verified,omitempty"`
	NativeWriteWatermark      int64               `json:"native_write_watermark"`
	CanonicalCommandWatermark int64               `json:"canonical_command_watermark"`
	InspectionCommandID       string              `json:"inspection_command_id,omitempty"`
	Source                    work.SourceRef      `json:"source,omitempty"`
	CarrierIncarnation        string              `json:"carrier_incarnation"`
	InstanceID                string              `json:"instance_id"`
	InstanceGeneration        int64               `json:"instance_generation"`
	ProcessGeneration         int64               `json:"process_generation"`
	LocationGeneration        int64               `json:"location_generation"`
	SessionID                 string              `json:"session_id"`
	TurnID                    string              `json:"turn_id"`
	NativeStopped             bool                `json:"native_stopped"`
	ContinuationStopped       bool                `json:"continuation_stopped"`
	BackgroundWorkStopped     bool                `json:"background_work_stopped"`
	NativeEvidence            json.RawMessage     `json:"native_evidence"`
	ContinuationEvidence      json.RawMessage     `json:"continuation_evidence"`
	BackgroundEvidence        json.RawMessage     `json:"background_evidence"`
	GoalActive                bool                `json:"goal_active"`
	GoalVersion               int64               `json:"goal_version"`
	ObservedAt                time.Time           `json:"observed_at"`
}

func (o NativeObservation) validatesIdentity(request InspectionRequest) bool {
	return o.CarrierIncarnation == request.Carrier.Incarnation && o.InstanceID == request.Carrier.OriginInstanceID && o.InstanceGeneration == request.Carrier.InstanceGeneration && o.ProcessGeneration == request.Carrier.ProcessGeneration && o.LocationGeneration == request.LocationGeneration && o.SessionID == request.SessionID && o.TurnID == request.TurnID && !o.ObservedAt.IsZero()
}
func (o NativeObservation) ValidateStopped(request InspectionRequest) error {
	if !o.provesStopped(request) {
		return fmt.Errorf("native, continuation and background quiescence proof is incomplete or stale")
	}
	return nil
}

func (o NativeObservation) provesStopped(request InspectionRequest) bool {
	return o.validatesIdentity(request) && o.NativeStopped && o.ContinuationStopped && o.BackgroundWorkStopped && !o.GoalActive && validInspectionEvidence(o.NativeEvidence) && validInspectionEvidence(o.ContinuationEvidence) && validInspectionEvidence(o.BackgroundEvidence)
}

func (c *Coordinator) inspectionRequest(tx *workstore.Tx, attempt work.ExecutionAttempt) (InspectionRequest, error) {
	turns, err := tx.ListNativeTurns(attempt.ID)
	if err != nil {
		return InspectionRequest{}, err
	}
	if len(turns) == 0 {
		command, e := tx.GetNativeCommand(attempt.CommandID)
		if e != nil {
			return InspectionRequest{}, e
		}
		if command.TerminalRejection == nil && (command.Ack == nil || command.Ack.Accepted || command.Ack.RuntimeReceipt == nil || command.Ack.RuntimeReceipt.Stage != "prepared" || command.Ack.RuntimeReceipt.PreparedFrames != 0) {
			return InspectionRequest{}, fmt.Errorf("native turn or pre-write rejection evidence missing")
		}
		carrier, e := tx.GetCarrier(command.Incarnation)
		if e != nil {
			return InspectionRequest{}, e
		}
		worker, e := tx.GetWorker(attempt.RunRef.WorkerID)
		if e != nil {
			return InspectionRequest{}, e
		}
		if !carrier.Matches(worker) {
			return InspectionRequest{}, fmt.Errorf("rejected command carrier is historical")
		}
		resumeRejected, e := tx.IsNativeResumeRejection(command)
		if e != nil {
			return InspectionRequest{}, e
		}
		return InspectionRequest{SubjectCommandID: attempt.CommandID, Carrier: carrier, Binding: command.Binding, SessionID: command.SessionID, LocationID: worker.LocationID, LocationGeneration: worker.LocationGeneration, ResumeRejected: resumeRejected}, nil
	}
	turn := turns[0]
	for _, candidate := range turns {
		if candidate.LastSequence > turn.LastSequence {
			turn = candidate
		}
	}
	carrier, err := tx.GetCarrier(turn.CarrierIncarnation)
	if err != nil {
		return InspectionRequest{}, err
	}
	worker, err := tx.GetWorker(attempt.RunRef.WorkerID)
	if err != nil {
		return InspectionRequest{}, err
	}
	if !carrier.Matches(worker) {
		return InspectionRequest{}, fmt.Errorf("attempt carrier is historical")
	}
	return InspectionRequest{SubjectCommandID: attempt.CommandID, Carrier: carrier, Binding: turn.Binding, SessionID: turn.SessionID, TurnID: turn.TurnID, LocationID: worker.LocationID, LocationGeneration: worker.LocationGeneration}, nil
}

func (c *Coordinator) ReconcileContinuation(ctx context.Context, incarnation, sessionID string) error {
	if c.options.Driver == nil {
		return fmt.Errorf("native driver unavailable")
	}
	var request InspectionRequest
	var grant workstore.ContinuationGrant
	err := c.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		grant, err = tx.GetContinuationGrant(incarnation, sessionID)
		if err != nil {
			return err
		}
		attempt, err := tx.GetAttempt(grant.Binding.AttemptID)
		if err != nil {
			return err
		}
		request, err = c.inspectionRequest(tx, attempt)
		return err
	})
	if err != nil {
		return err
	}
	observation, err := c.options.Driver.Inspect(ctx, request)
	if err != nil {
		return err
	}
	if !observation.validatesIdentity(request) || !json.Valid(observation.ContinuationEvidence) || string(observation.ContinuationEvidence) == "null" || !observation.GoalActive || observation.GoalVersion != grant.GoalVersion {
		return fmt.Errorf("continuation could not be reconciled with prior authorization")
	}
	grant.Reconciled = true
	return c.store.Within(ctx, func(tx *workstore.Tx) error {
		old, err := tx.GetContinuationGrant(incarnation, sessionID)
		if err != nil {
			return err
		}
		if old.CommandID != grant.CommandID || !old.Active {
			return workstore.ErrConflict
		}
		if err := tx.PutContinuationGrant(grant); err != nil {
			return err
		}
		carrier, err := tx.GetCarrier(incarnation)
		if err != nil {
			return err
		}
		pending, err := tx.QuarantinedSessionEvents(incarnation, sessionID)
		if err != nil {
			return err
		}
		for _, event := range pending {
			if event.Kind != agentproto.EventTurnStarted && event.Kind != agentproto.EventTurnCompleted {
				continue
			}
			binding, e := c.resolveBinding(tx, carrier, event)
			if e != nil || binding != grant.Binding {
				continue
			}
			if _, e := c.applyEvent(tx, carrier, event, false); e != nil {
				return e
			}
			if e := tx.ResolveQuarantine(incarnation, event.Journal.Sequence); e != nil {
				return e
			}
		}
		return nil
	})
}

func (c *Coordinator) Reconcile(ctx context.Context, attemptID string) error {
	var request InspectionRequest
	var ready bool
	var unresolvedOutcome string
	err := c.store.Within(ctx, func(tx *workstore.Tx) error {
		if _, err := tx.GetQuiescenceReceipt(attemptID); err == nil {
			return nil
		} else if !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		attempt, err := tx.GetAttempt(attemptID)
		if err != nil {
			return err
		}
		worker, e := tx.GetWorker(attempt.RunRef.WorkerID)
		if e != nil {
			return e
		}
		if worker.Controller != work.ControllerAutomatic {
			return fmt.Errorf("historical human controller requires isolated resource reconciliation")
		}
		request, err = c.inspectionRequest(tx, attempt)
		if err != nil {
			return err
		}
		request.StopBackground, err = tx.PrivateAttemptStopped(attempt)
		if err != nil {
			return err
		}
		if pending, e := tx.HasPendingPrivateShellTurns(request.Carrier.Incarnation); e != nil {
			return e
		} else if pending {
			return nil
		}
		count, err := tx.CountOpenNativeTurns(attemptID)
		if err != nil {
			return err
		}
		if count != 0 {
			return nil
		}
		active, e := tx.HasActiveContinuation(request.Carrier.Incarnation)
		if e == nil && active {
			return nil
		}
		if e != nil && !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		quarantined, e := tx.CountUnresolvedNativeEvents(request.Carrier.Incarnation)
		if e != nil {
			return e
		}
		if quarantined > 0 {
			return nil
		}
		if pending, e := pendingSessionGoalIntent(tx, attemptID); e != nil {
			return e
		} else if pending {
			return nil
		}
		if err = c.gate(tx, request.Carrier, attemptID, "native_reconciliation"); err != nil {
			return err
		}
		ready = true
		return nil
	})
	if err != nil || !ready {
		return err
	}
	if c.options.Driver == nil {
		return fmt.Errorf("native quiescence requires a host inspection driver")
	}
	observation, err := c.observeAttemptQuiescence(ctx, request)
	if err != nil {
		return err
	}
	if !observation.validatesIdentity(request) || !observation.NativeStopped || !observation.ContinuationStopped || observation.GoalActive || !validInspectionEvidence(observation.NativeEvidence) || !validInspectionEvidence(observation.ContinuationEvidence) {
		return fmt.Errorf("native execution or continuation is not proven stopped")
	}
	err = c.store.Within(ctx, func(tx *workstore.Tx) error {
		if e := validateRetirementCommit(tx, request, observation); e != nil {
			return e
		}
		attempt, e := tx.GetAttempt(attemptID)
		if e != nil {
			return e
		}
		count, e := tx.CountOpenNativeTurns(attemptID)
		if e != nil {
			return e
		}
		if count != 0 {
			return workstore.ErrConflict
		}
		active, e := tx.HasActiveContinuation(request.Carrier.Incarnation)
		if e == nil && active {
			return workstore.ErrConflict
		}
		if e != nil && !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		if e := c.resolveStoppedInputAdmissions(tx, request, observation); e != nil {
			return e
		}
		if e := c.preparePrivateSuccessReport(tx, attempt, request, observation); e != nil {
			return e
		}
		if e := c.preparePrivateRejectedTurnReport(tx, attempt, request, observation); e != nil {
			return e
		}
		if e := c.preparePrivateStartRejectionReport(tx, &attempt, request, observation); e != nil {
			return e
		}
		if e := c.prepareAutomaticFailureReport(tx, attempt, request, observation); e != nil {
			return e
		}
		if e := c.commitNativeOutcome(tx, attempt, request); e != nil {
			return e
		}
		gate, e := tx.GetRuntimeGate(attempt.RunRef.WorkerID)
		if e != nil && !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		if e == nil {
			for _, reason := range []string{gate.Reason, gate.RecoveryReason} {
				if reason == "missing_processing_report" || reason == "native_resume_rejected" {
					unresolvedOutcome = reason
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !observation.provesStopped(request) {
		return fmt.Errorf("native ended; background work remains uncertain and retains occupancy")
	}
	return c.store.Within(ctx, func(tx *workstore.Tx) error {
		if _, e := tx.GetQuiescenceReceipt(attemptID); e == nil {
			return nil
		} else if !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		count, err := tx.CountOpenNativeTurns(attemptID)
		if err != nil {
			return err
		}
		if count != 0 {
			return workstore.ErrConflict
		}
		active, e := tx.HasActiveContinuation(request.Carrier.Incarnation)
		if e == nil && active {
			return workstore.ErrConflict
		}
		if e != nil && !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		worker, err := tx.GetWorker(request.Binding.WorkerSessionID)
		if err != nil {
			return err
		}
		if !request.Carrier.Matches(worker) {
			return workstore.ErrConflict
		}
		if err := validateRetirementCommit(tx, request, observation); err != nil {
			return err
		}
		proof, err := c.persistQuiescenceEvidence(tx, request, observation)
		if err != nil {
			return err
		}
		if err := tx.ReconcileQuiescence(workstore.ReconcileQuiescenceRequest{Evidence: proof, ExpectedWorkerRevision: worker.Revision}); err != nil {
			return err
		}
		if unresolvedOutcome == "native_resume_rejected" && request.ResumeRejected {
			recovering, err := c.queueMissingResumeRecovery(tx, worker, request, proof)
			if err != nil || recovering {
				return err
			}
		}
		if unresolvedOutcome == "" {
			wc, e := tx.GetContext(worker.ContextID)
			if e != nil {
				return e
			}
			if wc.Purpose == work.PurposeReview {
				if e = tx.CompleteReview(attemptID); e != nil {
					return e
				}
			}
		}
		if unresolvedOutcome != "" {
			if err := c.completeFailedReview(tx, attemptID, request); err != nil {
				return err
			}
			if err := tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, AttemptID: attemptID, Reason: unresolvedOutcome, CreatedAt: c.options.Clock()}); err != nil {
				return err
			}
			worker, err = tx.GetWorker(worker.ID)
			if err != nil {
				return err
			}
			expected := worker.Revision
			worker.Revision++
			worker.State = work.WorkerUnavailable
			worker.UpdatedAt = observation.ObservedAt
			return tx.PutWorker(worker, expected)
		}
		if gate, e := tx.GetRuntimeGate(worker.ID); e == nil && gate.RecoveryReason != "" {
			gate.Reason = gate.RecoveryReason
			if e = tx.PutRuntimeGate(gate); e != nil {
				return e
			}
			fresh, e := tx.GetWorker(worker.ID)
			if e != nil {
				return e
			}
			before := fresh.Revision
			fresh.State = work.WorkerUnavailable
			fresh.Revision++
			fresh.UpdatedAt = c.options.Clock()
			return tx.PutWorker(fresh, before)
		} else if e != nil && !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		for _, reason := range []string{"native_reconciliation", "carrier_disconnected_reconcile", "native_ownership_unproven", "native_command_rejected", "native_command_unknown"} {
			if err := tx.ClearRuntimeGate(worker.ID, reason); err != nil {
				return err
			}
		}
		if observation.RetiredCarrierVerified {
			if err := c.requireRetiredCarrierRebuild(tx, request); err != nil {
				return err
			}
		}
		wc, e := tx.GetContext(worker.ContextID)
		if e != nil {
			return e
		}
		if wc.Purpose != work.PurposeReview && wc.Purpose != work.PurposeControl {
			return c.queueAutomaticFollowup(tx, attemptID, request)
		}
		return nil
	})
}
