package workstore

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"time"
)

// ResolveUncertainAttemptInputAdmissions is the trusted runtime's stop-only
// exit. It never says that the native process rejected or handled the input.
// Proven carrier quiescence permits preserving inputs for explicit recovery;
// the sticky gate prevents an automatic replay of uncertain side effects.
func (t *Tx) ResolveUncertainAttemptInputAdmissions(attemptID string, proof work.QuiescenceEvidence) error {
	return t.atomic(func() error {
		if err := proof.Validate(); err != nil {
			return err
		}
		attempt, err := t.GetAttempt(attemptID)
		if err != nil {
			return err
		}
		if attempt.RunRef != proof.RunRef {
			return ErrScopeDenied
		}
		worker, err := t.GetWorker(attempt.RunRef.WorkerID)
		if err != nil {
			return err
		}
		if worker.Generation != proof.RunRef.Generation || worker.CarrierIncarnation != proof.CarrierIncarnation || worker.InstanceID != proof.InstanceID || worker.InstanceGeneration != proof.InstanceGeneration || worker.ProcessGeneration != proof.ProcessGeneration {
			return ErrScopeDenied
		}
		if err = t.requireContent(attempt.ScopeID, proof.Source.EvidenceRef); err != nil {
			return err
		}
		admissions, err := t.ListAttemptInputAdmissions(attempt.ID)
		if err != nil {
			return err
		}
		changed := false
		for _, v := range admissions {
			if v.State != work.InputAdmissionSending && v.State != work.InputAdmissionUnknown {
				continue
			}
			for _, id := range v.InputIDs {
				input, e := t.GetInbox(id)
				if e != nil {
					return e
				}
				if input.ClaimedAttemptID != attempt.ID || input.State != work.InboxClaimed {
					return ErrConflict
				}
				before := input.Revision
				input.Revision++
				input.State = work.InboxPending
				input.ClaimedAttemptID = ""
				if e = t.PutInbox(input, before); e != nil {
					return e
				}
			}
			op, e := t.GetOperation(v.CommandID)
			if e != nil {
				return e
			}
			if op.State == work.OperationConfirmed || op.State == work.OperationTerminal {
				return fmt.Errorf("uncertain admission disagrees with canonical native outcome")
			}
			before := op.Revision
			op.Revision++
			op.State = work.OperationUnknown
			op.LastError = "native input delivery remained unknown after verified stop; explicit recovery required"
			op.Steps = append(op.Steps, work.OperationStep{ID: "quiesced_delivery_unknown", State: work.OperationConfirmed, Receipt: mustJSON(proof)})
			if _, _, e = t.PutOperation(op, before); e != nil {
				return e
			}
			v.State = work.InputAdmissionQuiescedUnknown
			v.Quiescence = &proof
			v.Revision++
			v.UpdatedAt = time.Now().UTC()
			if _, e = t.tx.ExecContext(t.ctx, "UPDATE attempt_input_admissions SET payload=? WHERE id=?", mustJSON(v), v.ID); e != nil {
				return e
			}
			changed = true
		}
		if !changed {
			return nil
		}
		return t.PutRuntimeGate(RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, AttemptID: attempt.ID, Reason: "native_input_delivery_unknown", RecoveryReason: "native_input_delivery_unknown", ResponsibleRoleID: worker.RoleID, CreatedAt: proof.ObservedAt})
	})
}
