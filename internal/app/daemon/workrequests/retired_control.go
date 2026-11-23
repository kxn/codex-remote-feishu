package workrequests

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const retiredControlRequestEvidence = "application/vnd.codex-remote.retired-control-request+json"

type retiredControlCandidate struct {
	Request   workstore.NativeRequest
	Operation work.Operation
	Worker    work.WorkerSession
	Process   agentproto.RuntimeProcessIdentity
}

// retiredControlCandidateFor requires the saved cancellation and exact local
// identity. Offline alone never establishes that the original process exited.
func retiredControlCandidateFor(tx *workstore.Tx, id, hostID string) (retiredControlCandidate, bool, error) {
	var c retiredControlCandidate
	if hostID == "" {
		return c, false, nil
	}
	r, err := tx.GetNativeRequest(id)
	if err != nil {
		return c, false, err
	}
	if r.State != "response_pending" || r.ResponseOperationID == "" {
		return c, false, nil
	}
	scope, err := tx.GetScope(r.ScopeID)
	if err != nil {
		return c, false, err
	}
	wc, err := tx.GetContext(r.Binding.ContextID)
	if err != nil {
		return c, false, err
	}
	if scope.Kind != work.ScopePersonal || wc.ScopeID != scope.ID || wc.Purpose != work.PurposeControl {
		return c, false, nil
	}
	worker, err := tx.GetWorker(r.Binding.WorkerSessionID)
	if err != nil {
		return c, false, err
	}
	carrier, err := tx.GetCarrier(r.CarrierIncarnation)
	if err != nil {
		return c, false, err
	}
	if worker.State != work.WorkerRecovering || !carrier.Matches(worker) || !carrier.Owner.SameOwner(r.Binding) {
		return c, false, nil
	}
	connected, err := tx.CarrierConnected(carrier.Incarnation)
	if err != nil {
		return c, false, err
	}
	if connected {
		return c, false, nil
	}
	gate, err := tx.GetRuntimeGate(worker.ID)
	if errors.Is(err, workstore.ErrNotFound) {
		return c, false, nil
	}
	if err != nil {
		return c, false, err
	}
	if gate.ScopeID != scope.ID || gate.AttemptID != r.Binding.AttemptID {
		return c, false, nil
	}
	attempt, err := tx.ValidateNativeBinding(r.Binding)
	if err != nil {
		if errors.Is(err, workstore.ErrNotFound) || errors.Is(err, workstore.ErrConflict) || errors.Is(err, workstore.ErrScopeDenied) {
			return c, false, nil
		}
		return c, false, err
	}
	if attempt.State != work.AttemptRunning {
		return c, false, nil
	}
	turn, err := tx.GetNativeTurn(r.CarrierIncarnation, r.NativeSessionID, r.NativeTurnID)
	if err != nil {
		if errors.Is(err, workstore.ErrNotFound) {
			return c, false, nil
		}
		return c, false, err
	}
	if turn.Binding != r.Binding || turn.TerminalStatus != "" {
		return c, false, nil
	}
	op, err := tx.GetOperation(r.ResponseOperationID)
	if err != nil {
		return c, false, err
	}
	if op.Kind != "native.request_response" || op.State != work.OperationRetryable || op.ScopeID != scope.ID || op.ContextID != wc.ID || op.TargetID != worker.ID {
		return c, false, nil
	}
	_, err = tx.GetNativeCommand(op.ID)
	if err == nil {
		return c, false, nil
	}
	if !errors.Is(err, workstore.ErrNotFound) {
		return c, false, err
	}
	unsent := false
	for _, step := range op.Steps {
		if (step.ID == "native_response_unsent" || step.ID == "local_registration_target_repair") && step.State == work.OperationConfirmed {
			if _, err = tx.GetContent(op.ScopeID, step.Receipt); err != nil {
				return c, false, err
			}
			unsent = true
		}
	}
	if !unsent {
		return c, false, nil
	}
	body, err := tx.GetContent(op.ScopeID, op.RequestRef)
	if err != nil {
		return c, false, err
	}
	var cmd agentproto.Command
	if json.Unmarshal(body.Data, &cmd) != nil || body.SHA256 != op.RequestHash || cmd.Kind != agentproto.CommandRequestRespond || cmd.CommandID != op.ID || cmd.Execution == nil || *cmd.Execution != r.Binding || cmd.Request.RequestID != r.NativeRequestID || cmd.Target.ThreadID != r.NativeSessionID || cmd.Target.TurnID != r.NativeTurnID {
		return c, false, nil
	}
	if cmd.Request.Response["decision"] != "cancel" {
		return c, false, nil
	}
	snapshot, err := tx.GetNativeLaunchSnapshot(worker.ID, worker.Generation)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, workstore.ErrNotFound) {
			return c, false, nil
		}
		return c, false, err
	}
	process, err := tx.GetCarrierProcess(carrier.Incarnation)
	if err != nil {
		if errors.Is(err, workstore.ErrNotFound) {
			return c, false, nil
		}
		return c, false, err
	}
	if snapshot.HostID != hostID || process.HostID != hostID || snapshot.CarrierIncarnation != carrier.Incarnation || snapshot.InstanceGeneration != worker.InstanceGeneration || snapshot.ProcessGeneration != worker.ProcessGeneration || snapshot.LocationGeneration != worker.LocationGeneration {
		return c, false, nil
	}
	return retiredControlCandidate{Request: r, Operation: op, Worker: worker, Process: process}, true, nil
}

// closeRetiredControlRequest does not resolve the attempt or assert a native
// turn completed; it only closes an unsent cancellation for an exited process.
func (s *Service) closeRetiredControlRequest(ctx context.Context, id string) (bool, error) {
	var c retiredControlCandidate
	var ok bool
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		c, ok, e = retiredControlCandidateFor(tx, id, s.localHostID)
		return e
	})
	if err != nil || !ok {
		return false, err
	}
	wait, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if hostinventory.WaitProcessExit(wait, hostinventory.ProcessIdentity{Platform: c.Process.Platform, PID: c.Process.PID, StartIdentity: c.Process.StartIdentity}) != nil {
		return false, nil
	}
	closed := false
	err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		current, eligible, e := retiredControlCandidateFor(tx, id, s.localHostID)
		if e != nil || !eligible {
			return e
		}
		if current.Request.Revision != c.Request.Revision || current.Operation.Revision != c.Operation.Revision || current.Worker.Revision != c.Worker.Revision || current.Process != c.Process {
			return nil
		}
		now := time.Now().UTC()
		evidence, e := json.Marshal(struct {
			RequestID, Carrier string
			Process            agentproto.RuntimeProcessIdentity
			VerifiedAt         time.Time
		}{id, c.Request.CarrierIncarnation, c.Process, now})
		if e != nil {
			return e
		}
		proof := work.NewContent(id+":retired-control-cancel", c.Request.ScopeID, retiredControlRequestEvidence, evidence, now)
		if e = tx.PutContent(proof); e != nil {
			return e
		}
		op := current.Operation
		old := op.Revision
		op.Revision++
		op.State = work.OperationTerminal
		op.LastError = "executor exited before cancellation was sent"
		op.NextAttemptAt = nil
		op.UpdatedAt = now
		op.Steps = append(op.Steps, work.OperationStep{ID: "retired_control_cancel_unsent", State: work.OperationConfirmed, Receipt: proof.ID})
		if _, _, e = tx.PutOperation(op, old); e != nil {
			return e
		}
		r := current.Request
		old = r.Revision
		r.Revision++
		r.State = "stale"
		r.ResolvedEvidenceRef = proof.ID
		r.UpdatedAt = now
		if e = tx.PutNativeRequest(r, old); e != nil {
			return e
		}
		closed = true
		return nil
	})
	return closed, err
}
