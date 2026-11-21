package workstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"reflect"
	"time"
)

type BeginAttemptInputAdmissionRequest struct {
	ID                     string
	AttemptID              string
	RunRef                 work.RunRef
	ExpectedWorkerRevision int64
	InputIDs               []string
	Command                agentproto.Command
	Source                 PrivateActionSource
	Now                    time.Time
}

func (t *Tx) BeginAttemptInputAdmission(req BeginAttemptInputAdmissionRequest) (v work.AttemptInputAdmission, created bool, err error) {
	err = t.atomic(func() error {
		if req.ID == "" || req.Now.IsZero() || len(req.InputIDs) == 0 {
			return fmt.Errorf("steer requires exact inputs and time")
		}
		attempt, e := t.GetAttempt(req.AttemptID)
		if e != nil {
			return e
		}
		principal, source, e := t.authorizePrivateInputAdmission(req.Source, attempt, req.Now)
		if e != nil {
			return e
		}
		if old, e := t.GetAttemptInputAdmission(req.ID); e == nil {
			if old.AttemptID != attempt.ID || old.RunRef != req.RunRef || old.Source.ID != source.ID || !reflect.DeepEqual(old.InputIDs, req.InputIDs) || old.CommandID != req.Command.CommandID {
				return ErrIdempotencyConflict
			}
			op, e := t.GetOperation(old.CommandID)
			if e != nil {
				return e
			}
			body, e := t.GetContent(old.ScopeID, op.RequestRef)
			if e != nil {
				return e
			}
			var canonical agentproto.Command
			if e = json.Unmarshal(body.Data, &canonical); e != nil {
				return e
			}
			proposed := req.Command
			proposed.IssuedAt = canonical.IssuedAt
			if !reflect.DeepEqual(canonical, proposed) {
				return ErrIdempotencyConflict
			}
			v = old
			return nil
		} else if !errors.Is(e, ErrNotFound) {
			return e
		}
		worker, e := t.GetWorker(attempt.RunRef.WorkerID)
		if e != nil {
			return e
		}
		if worker.Revision != req.ExpectedWorkerRevision || attempt.RunRef != req.RunRef || attempt.State != work.AttemptRunning || worker.State != work.WorkerBusy || worker.Controller != work.ControllerAutomatic || worker.Backend != work.BackendCodex {
			return ErrConflict
		}
		if e = work.ValidateAttemptBinding(attempt, worker); e != nil {
			return e
		}
		wc, e := t.GetContext(worker.ContextID)
		if e != nil {
			return e
		}
		if wc.Purpose == work.PurposeControl || wc.Purpose == work.PurposeReview || wc.RoleID != principal.RoleID {
			return ErrScopeDenied
		}
		scope, e := t.GetScope(worker.ScopeID)
		if e != nil {
			return e
		}
		if scope.Kind == work.ScopeProject {
			if e = t.requireProjectReady(scope.ProjectID, worker.RoleID); e != nil {
				return e
			}
		}
		if attempt.TaskID != "" {
			task, e := t.GetTask(attempt.TaskID)
			if e != nil {
				return e
			}
			if task.OwnerRoleID != worker.RoleID || task.RequirementsRevision != attempt.FrozenRequirementsRevision || task.AssignmentGeneration != attempt.FrozenAssignmentGeneration || task.State == work.TaskDone || task.State == work.TaskCancelled {
				return ErrConflict
			}
		}
		gates, e := t.ListPauseGates(scope.ID)
		if e != nil {
			return e
		}
		allowed, e := work.GatesAllowRunForPurpose(scope.ID, attempt.TaskID, wc.ID, wc.Purpose, gates)
		if e != nil {
			return e
		}
		if !allowed {
			return ErrScopeDenied
		}
		if _, e = t.GetRuntimeGate(worker.ID); e == nil {
			return ErrScopeDenied
		} else if !errors.Is(e, ErrNotFound) {
			return e
		}
		if e = t.AssertMaintenanceResourceAvailable("worker:" + worker.ID); e != nil {
			return e
		}
		if e = t.AssertHostCompactionAvailable(worker.ID, worker.Generation); e != nil {
			return e
		}
		carrier, e := t.GetCarrier(worker.CarrierIncarnation)
		if e != nil {
			return e
		}
		if !carrier.Matches(worker) {
			return ErrConflict
		}
		occupied, e := t.HasCarrierOccupancy(carrier, attempt.ID)
		if e != nil {
			return e
		}
		if !occupied {
			return ErrConflict
		}
		command := req.Command
		if command.Kind != agentproto.CommandTurnSteer || command.CommandID == "" || command.Execution == nil || command.Execution.AttemptID != attempt.ID || command.Execution.RunRef != attempt.RunRef.RunID || !command.Execution.SameOwner(carrier.Owner) || command.Target.ThreadID != worker.NativeSessionID || command.Target.TurnID == "" || len(command.Prompt.Inputs) == 0 {
			return ErrScopeDenied
		}
		binding, e := t.GetRoleBotBinding(worker.RoleID)
		if e != nil {
			return e
		}
		if command.Execution.BotGeneration != binding.IdentityGeneration {
			return ErrConflict
		}
		metadata := command.NativeInputAdmission
		if metadata == nil || metadata.Version != agentproto.NativeInputAdmissionProtocolVersion || metadata.ID != req.ID || !reflect.DeepEqual(metadata.InputIDs, req.InputIDs) {
			return ErrScopeDenied
		}
		original, e := t.NativeCommandPayload(attempt.CommandID, carrier.Incarnation)
		if e != nil {
			return e
		}
		if original.InstructionRevision == "" || command.InstructionRevision != original.InstructionRevision {
			return ErrScopeDenied
		}
		turn, e := t.GetNativeTurn(carrier.Incarnation, command.Target.ThreadID, command.Target.TurnID)
		if e != nil {
			return e
		}
		if !turn.Started || turn.TerminalStatus != "" || turn.Binding != *command.Execution {
			return ErrConflict
		}
		seen := map[string]bool{}
		for _, id := range attempt.InputIDs {
			seen[id] = true
		}
		for _, id := range req.InputIDs {
			if id == "" || seen[id] {
				return ErrScopeDenied
			}
			seen[id] = true
			input, e := t.GetInbox(id)
			if e != nil {
				return e
			}
			if input.State != work.InboxPending || input.ScopeID != scope.ID || input.RecipientRoleID != worker.RoleID || input.RecipientContextID != wc.ID || input.TaskID != attempt.TaskID || input.AvailableAt.After(req.Now) || input.ControlOfInputID != "" || input.SessionGoalIntentID != "" {
				return ErrScopeDenied
			}
		}
		raw, e := json.Marshal(command)
		if e != nil {
			return e
		}
		body := work.NewContent(command.CommandID+":request", scope.ID, "application/json", raw, req.Now)
		if e = t.PutContent(body); e != nil {
			return e
		}
		op := work.Operation{ID: command.CommandID, ScopeID: scope.ID, ContextID: wc.ID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: principal.PrincipalID}, Kind: "native.input.steer", TargetID: worker.ID, TargetGeneration: worker.Generation, IdempotencyKey: req.ID, RequestRef: body.ID, RequestHash: body.SHA256, Steps: []work.OperationStep{{ID: "human_source", State: work.OperationConfirmed, Receipt: mustJSON(source)}}, State: work.OperationSending, Revision: 1, CreatedAt: req.Now, UpdatedAt: req.Now}
		canonical, added, e := t.PutOperation(op, 0)
		if e != nil {
			return e
		}
		if !added || canonical.ID != op.ID {
			return ErrIdempotencyConflict
		}
		v = work.AttemptInputAdmission{ID: req.ID, CommandID: command.CommandID, ScopeID: scope.ID, ContextID: wc.ID, AttemptID: attempt.ID, RunRef: attempt.RunRef, NativeSessionID: command.Target.ThreadID, NativeTurnID: command.Target.TurnID, InputIDs: append([]string(nil), req.InputIDs...), Source: source, State: work.InputAdmissionSending, Revision: 1, CreatedAt: req.Now, UpdatedAt: req.Now}
		if e = v.Validate(); e != nil {
			return e
		}
		if _, e = t.tx.ExecContext(t.ctx, "INSERT INTO attempt_input_admissions(id,command_id,attempt_id,payload) VALUES(?,?,?,?)", v.ID, v.CommandID, v.AttemptID, mustJSON(v)); e != nil {
			return e
		}
		for _, id := range req.InputIDs {
			input, e := t.GetInbox(id)
			if e != nil {
				return e
			}
			before := input.Revision
			input.Revision++
			input.State = work.InboxClaimed
			input.ClaimedAttemptID = attempt.ID
			if e = t.PutInbox(input, before); e != nil {
				return e
			}
		}
		created = true
		return nil
	})
	return
}
