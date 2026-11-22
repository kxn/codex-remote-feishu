package workstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func LegacyPrivateStopGateID(scopeID, contextID string) string {
	data, _ := json.Marshal([]string{scopeID, contextID})
	hash := sha256.Sum256(data)
	return "private_context_gate_" + hex.EncodeToString(hash[:])
}

// OpenLegacyPrivateStopForInput upgrades the gate written by the old private
// Stop when a new authenticated message reaches the same private context.
func (t *Tx) OpenLegacyPrivateStopForInput(scope work.WorkScope, input work.Inbox, message work.Message) error {
	if scope.Kind != work.ScopePersonal || input.RecipientContextID == "" || message.PlatformKey == nil ||
		message.Source != (work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}) ||
		message.ScopeID != scope.ID || input.ScopeID != scope.ID || input.MessageID != message.ID {
		return nil
	}
	gate, stop, err := t.closedLegacyPrivateStop(scope, input.RecipientContextID)
	if err != nil || stop.ID == "" || !message.CreatedAt.After(stop.CreatedAt) {
		return err
	}

	attempts, err := t.ListOccupiedAttempts(scope.ID)
	if err != nil {
		return err
	}
	source := work.SourceRef{Kind: "legacy_private_stop", ID: stop.ID, Version: strconv.FormatInt(gate.Revision, 10), EvidenceRef: stop.RequestRef}
	for _, attempt := range attempts {
		if attempt.ContextID == gate.ContextID {
			if err = t.RecordPrivateAttemptStop(attempt, scope, source, stop.CreatedAt, stop.CreatedAt); err != nil {
				return err
			}
		}
	}
	if err = t.CancelOlderPrivateInputs(scope, gate.ContextID, stop.CreatedAt); err != nil {
		return err
	}
	if err = t.clearSettledLegacyStopGate(scope, gate, stop, source); err != nil {
		return err
	}
	before := gate.Revision
	gate.Revision++
	gate.State = work.GateOpen
	return t.PutPauseGate(gate, before)
}

func (t *Tx) HasLegacyPrivateStop(scopeID, contextID string) (bool, error) {
	scope, err := t.GetScope(scopeID)
	if err != nil {
		return false, err
	}
	_, stop, err := t.closedLegacyPrivateStop(scope, contextID)
	return stop.ID != "", err
}

func (t *Tx) closedLegacyPrivateStop(scope work.WorkScope, contextID string) (work.PauseGate, work.Operation, error) {
	var stop work.Operation
	if scope.Kind != work.ScopePersonal || contextID == "" {
		return work.PauseGate{}, stop, nil
	}
	gate, err := t.GetPauseGate(LegacyPrivateStopGateID(scope.ID, contextID))
	if errors.Is(err, ErrNotFound) {
		return gate, stop, nil
	}
	if err != nil {
		return gate, stop, err
	}
	if gate.State != work.GateClosed || gate.ScopeID != scope.ID || gate.ContextID != contextID ||
		gate.TaskID != "" || gate.Source != work.GateContext || gate.SourceID != scope.PrincipalID {
		return gate, stop, nil
	}
	stop, err = t.legacyPrivateStopReceipt(scope, gate)
	return gate, stop, err
}

func (t *Tx) legacyPrivateStopReceipt(scope work.WorkScope, gate work.PauseGate) (work.Operation, error) {
	ops, err := t.ListOperations(scope.ID)
	if err != nil {
		return work.Operation{}, err
	}
	var latest work.Operation
	for _, op := range ops {
		if op.Kind != "private.stop" || op.State != work.OperationConfirmed || op.TargetID != scope.ID || op.Actor != (work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}) {
			continue
		}
		var receipt struct {
			ObjectID string `json:"object_id"`
			Revision int64  `json:"revision"`
		}
		if err = json.Unmarshal([]byte(op.ExternalReceipt), &receipt); err != nil {
			return work.Operation{}, err
		}
		if receipt.ObjectID == gate.ContextID && receipt.Revision == gate.Revision && op.CreatedAt.After(latest.CreatedAt) {
			latest = op
		}
	}
	return latest, nil
}

// The old stop could leave the exact interrupted worker fenced for a missing
// model report even after independent native quiescence was recorded.
func (t *Tx) clearSettledLegacyStopGate(scope work.WorkScope, gate work.PauseGate, stop work.Operation, source work.SourceRef) error {
	workers, err := t.ListWorkers(scope.ID)
	if err != nil {
		return err
	}
	for _, worker := range workers {
		if worker.ContextID != gate.ContextID || worker.State != work.WorkerUnavailable || worker.Controller != work.ControllerAutomatic {
			continue
		}
		runtimeGate, e := t.GetRuntimeGate(worker.ID)
		if errors.Is(e, ErrNotFound) {
			continue
		}
		if e != nil {
			return e
		}
		if runtimeGate.AttemptID == "" || runtimeGate.Reason != "missing_processing_report" || runtimeGate.RecoveryReason != "" && runtimeGate.RecoveryReason != runtimeGate.Reason {
			continue
		}
		attempt, e := t.GetAttempt(runtimeGate.AttemptID)
		if e != nil {
			return e
		}
		if attempt.State != work.AttemptInterrupted || attempt.CreatedAt.After(stop.CreatedAt) || work.ValidateAttemptBinding(attempt, worker) != nil {
			continue
		}
		proof, e := t.GetQuiescenceReceipt(attempt.ID)
		if errors.Is(e, ErrNotFound) {
			continue
		}
		if e != nil {
			return e
		}
		occupied, e := t.WorkerHasOccupancy(worker.ID)
		if e != nil {
			return e
		}
		if occupied || proof.CarrierIncarnation != worker.CarrierIncarnation || proof.RunRef != attempt.RunRef || proof.ObservedAt.Before(stop.CreatedAt) {
			continue
		}
		if e = t.RecordPrivateAttemptStop(attempt, scope, source, stop.CreatedAt, stop.CreatedAt); e != nil {
			return e
		}
		if e = t.ClearRuntimeGate(worker.ID, runtimeGate.Reason); e != nil {
			return e
		}
		worker.Revision++
		worker.State = work.WorkerRecovering
		worker.UpdatedAt = proof.ObservedAt
		if e = t.PutWorker(worker, worker.Revision-1); e != nil {
			return e
		}
		worker.Revision++
		worker.State = work.WorkerReady
		if e = t.PutWorker(worker, worker.Revision-1); e != nil {
			return e
		}
	}
	return nil
}
