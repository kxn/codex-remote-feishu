package workengine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type dispatchEnvelope struct {
	InstanceID         string             `json:"instance_id"`
	InstanceGeneration int64              `json:"instance_generation"`
	ProcessGeneration  int64              `json:"process_generation"`
	CarrierIncarnation string             `json:"carrier_incarnation"`
	Command            agentproto.Command `json:"command"`
}
type PendingDispatch struct {
	Issue     string
	Operation work.Operation
	Attempt   work.ExecutionAttempt
	Worker    work.WorkerSession
	Command   agentproto.Command
}

func (e *Engine) admit(ctx context.Context, candidate Candidate, plan DispatchPlan) (PendingDispatch, error) {
	var pending PendingDispatch
	if err := validatePlan(candidate, plan); err != nil {
		return pending, err
	}
	now := e.config.Now().UTC()
	command := plan.Command
	command.InstructionRevision = plan.InstructionRevision
	if command.IssuedAt.IsZero() {
		command.IssuedAt = now
	}
	raw, err := json.Marshal(command)
	if err != nil {
		return pending, err
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	requestRef := "dispatch:" + command.CommandID
	envelope := dispatchEnvelope{InstanceID: candidate.Worker.InstanceID, InstanceGeneration: candidate.Worker.InstanceGeneration, ProcessGeneration: candidate.Worker.ProcessGeneration, CarrierIncarnation: candidate.Worker.CarrierIncarnation, Command: command}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return pending, err
	}
	content := work.NewContent(requestRef, candidate.Scope.ID, "application/json", payload, now)
	attempt := work.ExecutionAttempt{ID: command.Execution.AttemptID, ContextID: candidate.Context.ID, ScopeID: candidate.Scope.ID, RunRef: work.RunRef{WorkerID: candidate.Worker.ID, Generation: candidate.Worker.Generation, RunID: command.Execution.RunRef}, State: work.AttemptDispatchPending, FrozenRefs: append([]work.SourceRef{}, plan.FrozenRefs...), CommandID: command.CommandID, Revision: 1, CreatedAt: now, UpdatedAt: now}
	attempt.FrozenRefs = append(attempt.FrozenRefs, work.SourceRef{Kind: "instruction_bundle", ID: command.CommandID, Version: plan.InstructionRevision, EvidenceRef: requestRef})
	if candidate.Task != nil {
		attempt.TaskID = candidate.Task.ID
		attempt.FrozenRequirementsRevision = candidate.Task.RequirementsRevision
		attempt.FrozenAssignmentGeneration = candidate.Task.AssignmentGeneration
	}
	revisions := map[string]int64{}
	for _, input := range candidate.Inputs {
		attempt.InputIDs = append(attempt.InputIDs, input.ID)
		revisions[input.ID] = input.Revision
	}
	operation := work.Operation{ID: command.CommandID, ScopeID: candidate.Scope.ID, Actor: work.Actor{Kind: work.ActorRole, ID: candidate.Worker.RoleID}, ContextID: candidate.Context.ID, Kind: "native.dispatch", TargetID: candidate.Worker.ID, TargetGeneration: candidate.Worker.Generation, IdempotencyKey: command.CommandID, RequestHash: hash, RequestRef: requestRef, State: work.OperationPrepared, Revision: 1, CreatedAt: now, UpdatedAt: now}
	claim := func(tx *workstore.Tx) error {
		worker, err := tx.GetWorker(candidate.Worker.ID)
		if err != nil {
			return err
		}
		role, err := tx.GetRole(candidate.Role.ID)
		if err != nil {
			return err
		}
		binding, err := tx.GetRoleBotBinding(candidate.Worker.RoleID)
		if err != nil {
			return err
		}
		context, err := tx.GetContext(candidate.Context.ID)
		if err != nil {
			return err
		}
		if worker.Revision != candidate.Worker.Revision || role.Revision != candidate.Role.Revision || binding != candidate.BotBinding || context.Revision != candidate.Context.Revision {
			return workstore.ErrConflict
		}
		if err := tx.CheckContextPreferencesRevision(context.ID, plan.PreferencesRevision); err != nil {
			return err
		}
		if candidate.Task != nil {
			task, err := tx.GetTask(candidate.Task.ID)
			if err != nil {
				return err
			}
			if task.Revision != candidate.Task.Revision {
				return workstore.ErrConflict
			}
		}
		if err = tx.PutContent(content); err != nil {
			return err
		}
		if err = tx.ClaimAttempt(workstore.ClaimRequest{Attempt: attempt, MaxActiveSlots: e.config.MaxConcurrent, ControlReservedSlots: e.config.ControlReservedSlots, ExpectedInputRevisions: revisions, ExpectedWorkerRevision: plan.WorkerRevision, ControllerGeneration: worker.ControllerGeneration, InstanceID: worker.InstanceID, InstanceGeneration: worker.InstanceGeneration, ProcessGeneration: worker.ProcessGeneration, LocationGeneration: worker.LocationGeneration, StartOperation: operation, Now: now}); err != nil {
			return err
		}
		cursor, err := tx.GetSchedulingCursor(e.config.SchedulerID)
		if err != nil && err != workstore.ErrNotFound {
			return err
		}
		expected := cursor.Revision
		cursor.ID = e.config.SchedulerID
		cursor.Revision++
		cursor.LastScopeID = candidate.Scope.ID
		if cursor.RoleByScope == nil {
			cursor.RoleByScope = map[string]string{}
		}
		cursor.RoleByScope[candidate.Scope.ID] = candidate.Worker.RoleID
		return tx.PutSchedulingCursor(cursor, expected)
	}
	err = e.config.SettingsAdmission.Within(ctx, candidate.Role.ID, func() error { return e.store.Within(ctx, claim) })
	if err != nil {
		return pending, err
	}
	return PendingDispatch{Operation: operation, Attempt: attempt, Worker: candidate.Worker, Command: command}, nil
}
func validatePlan(c Candidate, p DispatchPlan) error {
	command := p.Command
	if len(c.Inputs) == 0 || p.WorkerRevision != c.Worker.Revision || p.InstructionRevision == "" || command.CommandID == "" || command.Execution == nil {
		return fmt.Errorf("plan requires exact worker, inputs, instruction revision and execution command")
	}
	if err := command.Execution.ValidateExecution(); err != nil {
		return err
	}
	binding := command.Execution
	if binding.ScopeID != c.Scope.ID || binding.ContextID != c.Context.ID || binding.RoleID != c.Worker.RoleID || binding.WorkerSessionID != c.Worker.ID || binding.SessionGeneration != c.Worker.Generation || binding.ControllerGeneration != c.Worker.ControllerGeneration || binding.BotGeneration != c.BotBinding.IdentityGeneration {
		return fmt.Errorf("plan execution binding mismatch")
	}
	if command.InstructionRevision != "" && command.InstructionRevision != p.InstructionRevision {
		return fmt.Errorf("plan instruction revision mismatch")
	}
	if command.Kind != agentproto.CommandPromptSend && command.Kind != agentproto.CommandReviewStart {
		return fmt.Errorf("scheduler requires an execution-start command")
	}
	return nil
}
