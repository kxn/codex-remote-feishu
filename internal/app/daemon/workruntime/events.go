package workruntime

import (
	"context"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workdisplay"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// CommittedEvent can only be produced by CommitEvent after the local store
// transaction commits. Its fields are private to prevent fabricated receipts.
type CommittedEvent struct {
	coordinator   *Coordinator
	instance      string
	receipt       agentproto.EventReceipt
	attemptID     string
	durable       bool
	nativeSession string
}

// NativeSession returns only a session whose ownership was established by the
// same durable team event transaction, never an unvalidated event field.
func (e *CommittedEvent) NativeSession() string {
	if e == nil {
		return ""
	}
	return e.nativeSession
}

// CommitEvent performs only local durable work; it does not send a receipt or
// perform native reconciliation. Call PublishCommittedEvent after releasing any
// outer ownership lock, even when the receipt transport can fail.
func (c *Coordinator) CommitEvent(ctx context.Context, currentTransportInstance string, event agentproto.Event) (*CommittedEvent, error) {
	if event.Journal == nil {
		if event.Kind != agentproto.EventItemDelta && event.Kind != agentproto.EventThreadTokenUsageUpdated {
			return nil, fmt.Errorf("critical worker event requires durable journal identity")
		}
		if err := c.applyLivePrivateOutput(ctx, currentTransportInstance, event); err != nil {
			return nil, err
		}
		return &CommittedEvent{coordinator: c, instance: currentTransportInstance}, nil
	}
	var receipt agentproto.EventReceipt
	var attemptID string
	var nativeSession string
	err := c.store.Within(ctx, func(tx *workstore.Tx) error {
		carrier, err := tx.GetCarrier(event.Journal.CarrierIncarnation)
		if err != nil {
			return err
		}
		if err := c.validateTransport(tx, carrier, currentTransportInstance); err != nil {
			return err
		}
		receipt, _, err = tx.AcceptRuntimeEvent(carrier.OriginInstanceID, event, c.options.Clock(), func(tx *workstore.Tx) error {
			var e error
			attemptID, e = c.applyEvent(tx, carrier, event, true)
			return e
		})
		if err == nil && attemptID == "" && nativeLifecycleKind(event.Kind) {
			if binding, e := c.resolveBinding(tx, carrier, event); e == nil {
				if worker, e := tx.GetWorker(binding.WorkerSessionID); e == nil && carrier.Matches(worker) {
					attemptID = binding.AttemptID
				}
			}
		}
		if err == nil && attemptID != "" && event.ThreadID != "" {
			owned, e := tx.HasNativeSessionOwner(carrier.Incarnation, event.ThreadID, attemptID)
			if e != nil {
				return e
			}
			if owned {
				nativeSession = event.ThreadID
			}
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return &CommittedEvent{coordinator: c, instance: currentTransportInstance, receipt: receipt, attemptID: attemptID, durable: true, nativeSession: nativeSession}, nil
}

// PublishCommittedEvent performs post-commit effects, outside ownership locks.
func (c *Coordinator) PublishCommittedEvent(ctx context.Context, committed *CommittedEvent) error {
	return c.PublishCommittedEventTo(ctx, committed, c.options.SendReceipt)
}

// PublishCommittedEventTo preserves the committed token while allowing a
// caller to deliver the receipt only to the connection that supplied the event.
func (c *Coordinator) PublishCommittedEventTo(ctx context.Context, committed *CommittedEvent, send func(string, agentproto.EventReceipt) error) error {
	if committed == nil || committed.coordinator != c {
		return fmt.Errorf("event commit token required")
	}
	if !committed.durable {
		return nil
	}
	var receiptErr error
	if send != nil {
		receipt := committed.receipt
		receipt.InstanceID = committed.instance
		receiptErr = send(committed.instance, receipt)
	}
	var err error
	if committed.attemptID != "" {
		err = c.scheduleReconcile(ctx, committed.attemptID)
	}
	if receiptErr != nil {
		return receiptErr
	}
	return err
}

func (c *Coordinator) HandleEvent(ctx context.Context, instance string, event agentproto.Event) error {
	committed, err := c.CommitEvent(ctx, instance, event)
	if err != nil {
		return err
	}
	return c.PublishCommittedEvent(ctx, committed)
}

func (c *Coordinator) resolveBinding(tx *workstore.Tx, carrier workstore.CarrierRegistration, event agentproto.Event) (agentproto.ExecutionBinding, error) {
	if event.Execution != nil {
		if !carrier.Owner.SameOwner(*event.Execution) {
			return agentproto.ExecutionBinding{}, fmt.Errorf("event carrier owner mismatch")
		}
		if _, err := tx.ValidateNativeBinding(*event.Execution); err != nil {
			return agentproto.ExecutionBinding{}, err
		}
		if event.CommandID != "" {
			command, err := tx.ResolveNativeCommand(carrier.Incarnation, event.CommandID)
			if err != nil {
				return agentproto.ExecutionBinding{}, err
			}
			if command.Incarnation != carrier.Incarnation || command.Binding != *event.Execution {
				return agentproto.ExecutionBinding{}, fmt.Errorf("event command binding mismatch")
			}
		}
		return *event.Execution, nil
	}
	if event.CommandID != "" {
		command, err := tx.ResolveNativeCommand(carrier.Incarnation, event.CommandID)
		if err != nil {
			return agentproto.ExecutionBinding{}, err
		}
		if command.Incarnation != carrier.Incarnation {
			return agentproto.ExecutionBinding{}, fmt.Errorf("event command belongs to another incarnation")
		}
		return command.Binding, nil
	}
	if event.ThreadID != "" && event.TurnID != "" {
		turn, err := tx.GetNativeTurn(carrier.Incarnation, event.ThreadID, event.TurnID)
		if err == nil {
			return turn.Binding, nil
		}
		if !errors.Is(err, workstore.ErrNotFound) {
			return agentproto.ExecutionBinding{}, err
		}
	}
	if event.Kind == agentproto.EventTurnStarted || event.Kind == agentproto.EventThreadGoalUpdated || event.Kind == agentproto.EventThreadGoalCommandResult {
		grant, err := tx.GetContinuationGrant(carrier.Incarnation, event.ThreadID)
		if err == nil && grant.Active && grant.Reconciled {
			worker, e := tx.GetWorker(grant.Binding.WorkerSessionID)
			if e != nil {
				return agentproto.ExecutionBinding{}, e
			}
			occupied, e := tx.HasCarrierOccupancy(carrier, grant.Binding.AttemptID)
			if e != nil {
				return agentproto.ExecutionBinding{}, e
			}
			if carrier.Matches(worker) && worker.Controller == work.ControllerAutomatic && occupied {
				return grant.Binding, nil
			}
		}
	}
	return agentproto.ExecutionBinding{}, workstore.ErrNotFound
}

func (c *Coordinator) applyEvent(tx *workstore.Tx, carrier workstore.CarrierRegistration, event agentproto.Event, drain bool) (string, error) {
	if handled, attempt, e := c.applyPrivateShellBoundary(tx, carrier, event); handled || e != nil {
		return attempt, e
	}
	if event.Kind == agentproto.EventNativeShellRPCResult || event.Kind == agentproto.EventNativeShellItemObserved {
		operation, err := tx.ApplyPrivateShellEvent(event, c.options.Clock())
		if err != nil {
			return "", err
		}
		if e := workdisplay.ProjectPrivateShellResult(tx, operation, event, c.options.Clock()); e != nil {
			return "", fmt.Errorf("project private shell: %w", e)
		}
		return c.resolvePrivateShellBoundaries(tx, carrier, operation)
	}

	if event.Kind == agentproto.EventRuntimeMCPResult || event.Kind == agentproto.EventMCPOAuthLoginURLReady || event.Kind == agentproto.EventMCPOAuthLoginCompleted {
		return "", c.applyMCPReceipt(tx, carrier, event)
	}
	if workdisplay.IsOutput(event) {
		return "", c.applyNativeOutput(tx, carrier, event)
	}
	if event.Kind == agentproto.EventThreadGoalUpdated {
		if handled, err := c.applyIdleGoalObservation(tx, carrier, event); handled || err != nil {
			return "", err
		}
	}
	if event.Kind == agentproto.EventThreadGoalCommandResult && event.CommandID != "" {
		command, e := tx.ResolveNativeCommand(carrier.Incarnation, event.CommandID)
		if e != nil {
			return "", e
		}
		if command.Kind == agentproto.CommandThreadGoalGet || command.Kind == agentproto.CommandThreadGoalClear || command.Binding.AttemptID == "" && command.Binding.RunRef == "" {
			return "", c.applyOwnerGoalReceipt(tx, carrier, event)
		}
	}
	if event.Kind == agentproto.EventNativeMaintenanceStarted || event.Kind == agentproto.EventNativeMaintenanceTerminal {
		return "", c.applyMaintenanceEvent(tx, carrier, event)
	}
	if event.Kind == agentproto.EventModelCatalogUpdated {
		if event.CommandID == "" {
			return "", nil
		}
		return "", tx.RecordRuntimeModelCatalog(event)
	}
	switch event.Kind {
	case agentproto.EventNativeTurnBindingObserved, agentproto.EventTurnStarted, agentproto.EventTurnCompleted, agentproto.EventThreadGoalCommandResult, agentproto.EventThreadGoalUpdated, agentproto.EventRequestStarted, agentproto.EventRequestResolved, agentproto.EventNativeTurnInputAccepted, agentproto.EventNativeTurnInputRejected:
	default:
		return "", nil
	}
	binding, err := c.resolveBinding(tx, carrier, event)
	if err != nil {
		if e := tx.QuarantineRuntimeEvent(event, "native_ownership_unproven"); e != nil {
			return "", e
		}
		return "", c.gate(tx, carrier, "", "native_ownership_unproven")
	}
	worker, err := tx.GetWorker(binding.WorkerSessionID)
	if err != nil {
		return "", err
	}
	current := carrier.Matches(worker)
	if event.Kind == agentproto.EventNativeTurnInputAccepted || event.Kind == agentproto.EventNativeTurnInputRejected {
		return binding.AttemptID, c.applyInputAdmissionReceipt(tx, binding, event)
	}
	if event.Kind == agentproto.EventRequestStarted || event.Kind == agentproto.EventRequestResolved {
		if c.options.ApplyNativeRequest == nil {
			return "", c.gate(tx, carrier, binding.AttemptID, "native_request_handler_unavailable")
		}
		if event.Kind == agentproto.EventRequestStarted {
			if err := c.applyPrivateDisplay(tx, carrier, binding, event); err != nil {
				return "", err
			}
		}
		return "", c.options.ApplyNativeRequest(tx, carrier, binding, event)
	}
	if event.Kind == agentproto.EventThreadGoalCommandResult || event.Kind == agentproto.EventThreadGoalUpdated {
		if event.Kind == agentproto.EventThreadGoalCommandResult && event.RequestID == "" {
			if err := tx.QuarantineRuntimeEvent(event, "native_goal_rpc_unproven"); err != nil {
				return "", err
			}
			return "", c.gate(tx, carrier, binding.AttemptID, "native_goal_rpc_unproven")
		}
		if event.ErrorMessage != "" {
			if event.Kind == agentproto.EventThreadGoalCommandResult {
				if err := c.failGoalReceipt(tx, event); err != nil {
					return "", err
				}
			}
			if current {
				return "", c.gate(tx, carrier, binding.AttemptID, "native_continuation_result_unknown")
			}
			return "", nil
		}
		if err := c.confirmGoalReceipt(tx, event); err != nil {
			return "", err
		}
		active := false
		if event.ThreadGoal != nil {
			switch event.ThreadGoal.Status {
			case "active":
				active = true
			case "paused", "completed", "budget_exhausted":
			default:
				return "", c.gate(tx, carrier, binding.AttemptID, "native_continuation_state_unknown")
			}
		}
		if !event.GoalCleared && event.ThreadGoal == nil && !event.GoalMissing {
			return "", c.gate(tx, carrier, binding.AttemptID, "native_continuation_state_unknown")
		}
		if active {
			allowed, e := nativeGoalAdmission(tx, worker)
			if e != nil {
				return "", e
			}
			if !allowed {
				return "", c.gate(tx, carrier, binding.AttemptID, "native_goal_paused")
			}
			context, e := tx.GetContext(worker.ContextID)
			if e != nil {
				return "", e
			}
			if context.Purpose == work.PurposeControl {
				return "", c.gate(tx, carrier, binding.AttemptID, "management_continuation_forbidden")
			}
		}
		if active && current && worker.Controller != work.ControllerAutomatic {
			return "", c.gate(tx, carrier, binding.AttemptID, "continuation_requires_automatic_control")
		}
		version := int64(0)
		if event.ThreadGoal != nil {
			version = event.ThreadGoal.UpdatedAt
		}
		commandID := event.CommandID
		budget := int64(0)
		objective := ""
		if event.ThreadGoal != nil {
			objective = event.ThreadGoal.Objective
			if event.ThreadGoal.TokenBudget != nil {
				budget = *event.ThreadGoal.TokenBudget
			}
		}
		source, e := c.resolveGoalSource(tx, carrier, binding, event, active, version, budget, objective)
		if e != nil {
			if errors.Is(e, workstore.ErrNotFound) || errors.Is(e, workstore.ErrScopeDenied) {
				return "", c.gate(tx, carrier, binding.AttemptID, "continuation_state_conflict")
			}
			return "", e
		}
		if source.Pending {
			return "", nil
		}
		commandID, budget, objective, version = source.CommandID, source.Budget, source.Objective, source.Version

		if active {
			owned, e := tx.HasNativeSessionOwner(carrier.Incarnation, event.ThreadID, binding.AttemptID)
			if e != nil {
				return "", e
			}
			occupied, e := tx.HasCarrierOccupancy(carrier, binding.AttemptID)
			if e != nil {
				return "", e
			}
			if !owned || !occupied || version <= 0 || budget <= 0 {
				return "", c.gate(tx, carrier, binding.AttemptID, "continuation_authorization_incomplete")
			}
		}
		status := ""
		if event.ThreadGoal != nil {
			status = event.ThreadGoal.Status
		}
		if err := tx.PutContinuationGrant(workstore.ContinuationGrant{Status: status, AuthorizationKind: source.Kind, SourceTurnID: source.TurnID, BudgetSource: source.BudgetSource, Source: source.Source, Incarnation: carrier.Incarnation, SessionID: event.ThreadID, Binding: binding, CommandID: commandID, GoalVersion: version, Active: active, ConfirmedSequence: event.Journal.Sequence, Reconciled: true, TokenBudget: budget, Objective: objective}); err != nil {
			return "", err
		}
		if current && event.ThreadGoal != nil && event.ThreadGoal.Status == "budget_exhausted" {
			if err := c.noteNativeGoalBudgetStop(tx, carrier, binding, event); err != nil {
				return "", err
			}
		}
		if active && current {
			if err := c.replayGrantedSession(tx, carrier, binding, event.ThreadID); err != nil {
				return "", err
			}
		}

		if !active && current {
			return binding.AttemptID, nil
		}
		return "", nil
	}
	if event.Kind == agentproto.EventTurnCompleted && event.TurnID == "" && event.CommandID != "" && event.RequestID != "" && event.Status == "failed" {
		switch event.TurnCompletionOrigin {
		case agentproto.TurnCompletionOriginThreadStartRejected, agentproto.TurnCompletionOriginThreadResumeRejected, agentproto.TurnCompletionOriginTurnStartRejected:
			if err := tx.RecordNativeRejection(event.CommandID, event); err != nil {
				return "", err
			}
			if err := c.applyPrivateDisplay(tx, carrier, binding, event); err != nil {
				return "", err
			}
			if current {
				return binding.AttemptID, nil
			}
			return "", nil
		}
	}
	if event.Kind == agentproto.EventTurnCompleted && event.Status != "completed" && event.Status != "failed" && event.Status != "interrupted" {
		if err := tx.QuarantineRuntimeEvent(event, "native_terminal_status_unknown"); err != nil {
			return "", err
		}
		return "", c.gate(tx, carrier, binding.AttemptID, "native_terminal_status_unknown")
	}
	if event.ThreadID == "" || event.TurnID == "" {
		if err := tx.QuarantineRuntimeEvent(event, "native_ids_missing"); err != nil {
			return "", err
		}
		return "", c.gate(tx, carrier, binding.AttemptID, "native_ids_missing")
	}
	turn := workstore.NativeTurn{CarrierIncarnation: carrier.Incarnation, SessionID: event.ThreadID, TurnID: event.TurnID, Binding: binding, Started: event.Kind == agentproto.EventTurnStarted, LastSequence: event.Journal.Sequence, UpdatedAt: c.options.Clock()}
	if event.Kind == agentproto.EventTurnStarted && event.CommandID == "" && event.Execution == nil {
		if grant, e := tx.GetContinuationGrant(carrier.Incarnation, event.ThreadID); e == nil && grant.Active && grant.Binding == binding {
			turn.ContinuationAuthorization = grant.CommandID
			turn.AuthorizationSequence = grant.ConfirmedSequence
		}
	}
	if event.Kind == agentproto.EventTurnCompleted {
		turn.TerminalStatus = event.Status
		turn.TerminalSequence = event.Journal.Sequence
	}
	if err := tx.PutNativeTurn(turn); err != nil {
		if e := tx.QuarantineRuntimeEvent(event, "native_turn_binding_conflict"); e != nil {
			return "", e
		}
		return "", c.gate(tx, carrier, binding.AttemptID, "native_turn_binding_conflict")
	}
	if current {
		if err := tx.BindObservedNativeSession(carrier.Incarnation, event.ThreadID, event.TurnID, event.CommandID); err != nil {
			if e := tx.QuarantineRuntimeEvent(event, "native_session_binding_conflict"); e != nil {
				return "", e
			}
			return "", c.gate(tx, carrier, binding.AttemptID, "native_session_binding_conflict")
		}
	}
	if drain && event.Kind == agentproto.EventNativeTurnBindingObserved {
		if err := c.replayPrivateShellBoundariesForModel(tx, carrier, event); err != nil {
			return "", err
		}
	}
	if current && event.Kind == agentproto.EventTurnStarted {
		attempt, err := tx.GetAttempt(binding.AttemptID)
		if err != nil {
			return "", err
		}
		if attempt.State == work.AttemptDispatchPending || attempt.State == work.AttemptReconciling {
			expected := attempt.Revision
			attempt.Revision++
			attempt.State = work.AttemptRunning
			attempt.UpdatedAt = c.options.Clock()
			if err := tx.PutAttempt(attempt, expected); err != nil {
				return "", err
			}
		}
	}
	if current && event.Kind == agentproto.EventTurnStarted {
		if err := c.prepareSessionGoalIntent(tx, binding.AttemptID, event.ThreadID); err != nil {
			return "", err
		}
	}
	if event.Kind == agentproto.EventTurnStarted {
		if err := c.applyPrivateDisplay(tx, carrier, binding, event); err != nil {
			return "", err
		}
	}
	if drain {
		pending, err := tx.QuarantinedNativeEvents(carrier.Incarnation, event.ThreadID, event.TurnID)
		if err != nil {
			return "", err
		}
		for _, old := range pending {
			resolved, e := c.resolveBinding(tx, carrier, old)
			if e != nil || resolved != binding {
				continue
			}
			if _, err := c.applyEvent(tx, carrier, old, false); err != nil {
				return "", err
			}
			if err := tx.ResolveQuarantine(carrier.Incarnation, old.Journal.Sequence); err != nil {
				return "", err
			}
		}
	}
	if event.Kind == agentproto.EventTurnCompleted {
		if err := c.applyPrivateDisplay(tx, carrier, binding, event); err != nil {
			return "", err
		}
	}
	if event.Kind == agentproto.EventTurnCompleted && c.options.ApplyReviewEvent != nil {
		context, e := tx.GetContext(binding.ContextID)
		if e != nil {
			return "", e
		}
		if context.Purpose == work.PurposeReview {
			if e = c.options.ApplyReviewEvent(tx, carrier, binding, event); e != nil {
				return "", e
			}
		}
	}
	if current && drain {
		count, e := tx.CountUnresolvedNativeEvents(carrier.Incarnation)
		if e != nil {
			return "", e
		}
		if count == 0 {
			gate, e := tx.GetRuntimeGate(worker.ID)
			if e == nil && gate.Reason == "native_ownership_unproven" && gate.RecoveryReason == "" {
				if e = tx.ClearRuntimeGate(worker.ID, gate.Reason); e != nil {
					return "", e
				}
			} else if e != nil && !errors.Is(e, workstore.ErrNotFound) {
				return "", e
			}
		}
	}
	if current {
		stored, err := tx.GetNativeTurn(carrier.Incarnation, event.ThreadID, event.TurnID)
		if err != nil {
			return "", err
		}
		if stored.TerminalStatus != "" {
			return binding.AttemptID, nil
		}
	}
	return "", nil
}

func nativeLifecycleKind(kind agentproto.EventKind) bool {
	switch kind {
	case agentproto.EventNativeTurnBindingObserved, agentproto.EventTurnStarted, agentproto.EventTurnCompleted, agentproto.EventThreadGoalCommandResult, agentproto.EventThreadGoalUpdated:
		return true
	}
	return false
}
