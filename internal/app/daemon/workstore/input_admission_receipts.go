package workstore

import (
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"reflect"
	"time"
)

func (t *Tx) ValidateAttemptInputAdmissionCommand(command agentproto.Command) error {
	v, err := t.GetAttemptInputAdmissionByCommand(command.CommandID)
	if err != nil {
		return err
	}
	if v.State != work.InputAdmissionSending && v.State != work.InputAdmissionUnknown {
		return ErrConflict
	}
	metadata := command.NativeInputAdmission
	if metadata == nil || metadata.Version != 1 || metadata.ID != v.ID || !reflect.DeepEqual(metadata.InputIDs, v.InputIDs) || command.Kind != agentproto.CommandTurnSteer || command.Execution == nil || command.Execution.AttemptID != v.AttemptID || command.Execution.RunRef != v.RunRef.RunID || command.Target.ThreadID != v.NativeSessionID || command.Target.TurnID != v.NativeTurnID {
		return ErrScopeDenied
	}
	op, err := t.GetOperation(v.CommandID)
	if err != nil {
		return err
	}
	if op.State != work.OperationSending && op.State != work.OperationUnknown {
		return ErrConflict
	}
	worker, err := t.GetWorker(v.RunRef.WorkerID)
	if err != nil {
		return err
	}
	canonical, err := t.NativeCommandPayload(v.CommandID, worker.CarrierIncarnation)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(canonical, command) {
		return ErrIdempotencyConflict
	}
	for _, step := range op.Steps {
		if step.ID != "project_message_source" {
			continue
		}
		var proof struct {
			Source    work.SourceRef     `json:"source"`
			Selection ProjectInputSource `json:"selection"`
		}
		if err := json.Unmarshal([]byte(step.Receipt), &proof); err != nil {
			return err
		}
		input, err := t.GetInbox(proof.Selection.InputID)
		if err != nil {
			return err
		}
		if input.Revision != proof.Selection.InputRevision+1 || input.MessageID != proof.Source.ID {
			return ErrConflict
		}
		msg, err := t.GetMessage(input.MessageID)
		if err != nil {
			return err
		}
		if msg.Revision != proof.Selection.MessageRevision || msg.RecalledEvidenceRef != "" || msg.BodyRef != proof.Source.EvidenceRef {
			return ErrConflict
		}
		scope, err := t.GetScope(input.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind != work.ScopeProject {
			return ErrScopeDenied
		}
		if _, err = t.authorizeProjectMessageSource(scope, input, msg, time.Now().UTC()); err != nil {
			return err
		}
		if err = t.requireProjectReady(scope.ProjectID, input.RecipientRoleID); err != nil {
			return err
		}
		if err = t.requireRoleInScope(scope.ID, input.RecipientRoleID); err != nil {
			return err
		}
		if input.TaskID != "" {
			task, e := t.GetTask(input.TaskID)
			if e != nil {
				return e
			}
			attempt, e := t.GetAttempt(v.AttemptID)
			if e != nil {
				return e
			}
			if task.Revision != proof.Selection.TaskRevision || task.OwnerRoleID != input.RecipientRoleID || task.RequirementsRevision != attempt.FrozenRequirementsRevision || task.AssignmentGeneration != attempt.FrozenAssignmentGeneration || task.State == work.TaskDone || task.State == work.TaskCancelled {
				return ErrConflict
			}
		}
	}
	for _, id := range v.InputIDs {
		input, e := t.GetInbox(id)
		if e != nil {
			return e
		}
		if input.ScopeID != v.ScopeID || input.RecipientContextID != v.ContextID || input.State != work.InboxClaimed || input.ClaimedAttemptID != v.AttemptID {
			return ErrScopeDenied
		}
	}
	return nil
}
func (t *Tx) AcceptAttemptInputAdmission(commandID string, source work.SourceRef) error {
	return t.finishAttemptInputAdmission(commandID, source, true)
}
func (t *Tx) RejectAttemptInputAdmission(commandID string, source work.SourceRef) error {
	return t.finishAttemptInputAdmission(commandID, source, false)
}
func (t *Tx) finishAttemptInputAdmission(commandID string, source work.SourceRef, accepted bool) error {
	return t.atomic(func() error {
		v, err := t.GetAttemptInputAdmissionByCommand(commandID)
		if err != nil {
			return err
		}
		attempt, err := t.GetAttempt(v.AttemptID)
		if err != nil {
			return err
		}
		body, e := t.GetContent(attempt.ScopeID, source.EvidenceRef)
		if e != nil {
			return e
		}
		var event agentproto.Event
		if e = json.Unmarshal(body.Data, &event); e != nil {
			return e
		}
		if event.Journal == nil || source.Kind != string(event.Kind) || source.Version != event.Journal.PayloadHash || event.Execution == nil {
			return ErrScopeDenied
		}
		saved, e := t.GetRuntimeEvent(event.Journal.CarrierIncarnation, event.Journal.Sequence)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(saved, event) {
			return ErrScopeDenied
		}
		native, e := t.GetNativeCommand(commandID)
		if e != nil {
			return e
		}
		if native.Incarnation != event.Journal.CarrierIncarnation || native.Binding != *event.Execution {
			return ErrScopeDenied
		}
		kind := agentproto.EventNativeTurnInputRejected
		if accepted {
			kind = agentproto.EventNativeTurnInputAccepted
		}
		if event.Kind != kind || source.Kind != string(kind) || event.CommandID != v.CommandID || event.RequestID == "" || (accepted && (event.ThreadID != v.NativeSessionID || event.TurnID != v.NativeTurnID)) || event.Execution == nil || event.Execution.AttemptID != v.AttemptID || event.Execution.RunRef != v.RunRef.RunID {
			return ErrScopeDenied
		}
		if v.State == work.InputAdmissionQuiescedUnknown {
			for _, old := range v.LateNativeSources {
				if reflect.DeepEqual(old, source) {
					return nil
				}
			}
			if len(v.LateNativeSources) < 8 {
				v.LateNativeSources = append(v.LateNativeSources, source)
				v.Revision++
				v.UpdatedAt = time.Now().UTC()
				_, e := t.tx.ExecContext(t.ctx, "UPDATE attempt_input_admissions SET payload=? WHERE id=?", mustJSON(v), v.ID)
				return e
			}
			return nil
		}
		if v.State == work.InputAdmissionConfirmed || v.State == work.InputAdmissionRejected {
			if v.NativeSource != nil && reflect.DeepEqual(*v.NativeSource, source) {
				return nil
			}
			return ErrIdempotencyConflict
		}
		if v.State != work.InputAdmissionSending && v.State != work.InputAdmissionUnknown {
			return ErrConflict
		}
		op, err := t.GetOperation(commandID)
		if err != nil {
			return err
		}
		before := op.Revision
		op.Revision++
		op.UpdatedAt = time.Now().UTC()
		op.ExternalReceipt = source.EvidenceRef
		if accepted {
			if event.Status != "accepted" {
				return ErrScopeDenied
			}
			v.State = work.InputAdmissionConfirmed
			op.State = work.OperationConfirmed
		} else if event.Status == "unknown" {
			v.State = work.InputAdmissionUnknown
			op.State = work.OperationUnknown
			op.LastError = "native steer acceptance is unknown"
		} else {
			if event.Status != "failed" && event.Status != "rejected" {
				return ErrScopeDenied
			}
			v.State = work.InputAdmissionRejected
			op.State = work.OperationTerminal
			op.LastError = event.ErrorMessage
			for _, id := range v.InputIDs {
				input, e := t.GetInbox(id)
				if e != nil {
					return e
				}
				if input.State != work.InboxClaimed || input.ClaimedAttemptID != v.AttemptID {
					return ErrConflict
				}
				revision := input.Revision
				input.Revision++
				input.State = work.InboxPending
				input.ClaimedAttemptID = ""
				if e = t.PutInbox(input, revision); e != nil {
					return e
				}
			}
		}
		if _, _, err = t.PutOperation(op, before); err != nil {
			return err
		}
		old := v.Revision
		v.Revision++
		v.NativeSource = &source
		v.UpdatedAt = time.Now().UTC()
		result, err := t.tx.ExecContext(t.ctx, "UPDATE attempt_input_admissions SET payload=? WHERE id=? AND json_extract(payload,'$.revision')=?", mustJSON(v), v.ID, old)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrConflict
		}
		return nil
	})
}
func (t *Tx) MarkAttemptInputAdmissionUnknown(commandID, reason string) error {
	return t.atomic(func() error {
		v, err := t.GetAttemptInputAdmissionByCommand(commandID)
		if err != nil {
			return err
		}
		if v.State == work.InputAdmissionConfirmed || v.State == work.InputAdmissionRejected || v.State == work.InputAdmissionNotSent || v.State == work.InputAdmissionQuiescedUnknown {
			return nil
		}
		v.State = work.InputAdmissionUnknown
		v.Revision++
		v.UpdatedAt = time.Now().UTC()
		if _, err = t.tx.ExecContext(t.ctx, "UPDATE attempt_input_admissions SET payload=? WHERE id=?", mustJSON(v), v.ID); err != nil {
			return err
		}
		op, err := t.GetOperation(commandID)
		if err != nil {
			return err
		}
		if op.State == work.OperationConfirmed || op.State == work.OperationTerminal {
			return fmt.Errorf("admission and canonical operation disagree")
		}
		before := op.Revision
		op.Revision++
		op.State = work.OperationUnknown
		op.LastError = reason
		_, _, err = t.PutOperation(op, before)
		return err
	})
}
