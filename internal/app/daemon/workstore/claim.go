package workstore

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"reflect"
	"time"
)

type ClaimRequest struct {
	ControlReservedSlots   int
	MaxActiveSlots         int
	ExpectedInputRevisions map[string]int64
	Attempt                work.ExecutionAttempt
	ExpectedWorkerRevision int64
	ControllerGeneration   int64
	InstanceID             string
	InstanceGeneration     int64
	ProcessGeneration      int64
	LocationGeneration     int64
	StartOperation         work.Operation
	Now                    time.Time
}

func (t *Tx) ClaimAttempt(request ClaimRequest) error {
	return t.atomic(func() error { return t.claimAttempt(request) })
}
func (t *Tx) claimAttempt(request ClaimRequest) error {
	attempt := request.Attempt
	if request.ControlReservedSlots < 0 || (request.ControlReservedSlots > 0 && (request.MaxActiveSlots < 1 || request.ControlReservedSlots >= request.MaxActiveSlots)) {
		return fmt.Errorf("invalid control reserve")
	}
	if request.MaxActiveSlots > 0 {
		count, err := t.ActiveSlotCount()
		if err != nil {
			return err
		}
		if count >= request.MaxActiveSlots {
			return fmt.Errorf("all execution slots occupied")
		}
	}
	if attempt.State != work.AttemptDispatchPending || attempt.Revision != 1 {
		return fmt.Errorf("new claim must be dispatch_pending revision 1")
	}
	if err := attempt.Validate(); err != nil {
		return err
	}
	worker, err := t.GetWorker(attempt.RunRef.WorkerID)
	if err != nil {
		return err
	}
	if err = t.ValidateSessionGoalInputs(attempt, worker); err != nil {
		return err
	}
	if err = t.AssertHostCompactionAvailable(worker.ID, worker.Generation); err != nil {
		return err
	}
	if err = t.AssertMaintenanceResourceAvailable("worker:" + worker.ID); err != nil {
		return err
	}
	if err = t.AssertMaintenanceResourceAvailable(fmt.Sprintf("carrier:%s:%d:%d", worker.InstanceID, worker.InstanceGeneration, worker.ProcessGeneration)); err != nil {
		return err
	}
	if gate, e := t.GetRuntimeGate(worker.ID); e == nil {
		return fmt.Errorf("worker runtime is gated: %s", gate.Reason)
	} else if !errors.Is(e, ErrNotFound) {
		return e
	}
	if err = work.ValidateAttemptBinding(attempt, worker); err != nil {
		return err
	}
	if worker.Revision != request.ExpectedWorkerRevision {
		return ErrConflict
	}
	if worker.State != work.WorkerReady || worker.Controller != work.ControllerAutomatic {
		return fmt.Errorf("worker not available for automatic admission")
	}
	if request.InstanceID == "" || request.InstanceGeneration < 1 || request.ProcessGeneration < 1 || worker.InstanceID != request.InstanceID || worker.InstanceGeneration != request.InstanceGeneration || worker.ProcessGeneration != request.ProcessGeneration || worker.ControllerGeneration != request.ControllerGeneration || worker.LocationGeneration != request.LocationGeneration {
		return fmt.Errorf("stale execution carrier/controller/location")
	}
	context, err := t.GetContext(attempt.ContextID)
	if err != nil {
		return err
	}
	if err = t.CheckAdmissionCapacity(context.Purpose, AdmissionLimits{MaxActiveSlots: request.MaxActiveSlots, ControlReservedSlots: request.ControlReservedSlots}); err != nil {
		return err
	}
	if context.TaskID != "" && context.TaskID != attempt.TaskID {
		return fmt.Errorf("attempt differs from context task binding")
	}
	scope, err := t.GetScope(attempt.ScopeID)
	if err != nil {
		return err
	}
	if scope.Kind == work.ScopeBootstrap && (attempt.TaskID != "" || worker.RoleID != scope.RoleID) {
		return fmt.Errorf("bootstrap accepts only its leader input workstream")
	}
	if context.Purpose == work.PurposeControl {
		if err = t.ValidateControlContext(context); err != nil {
			return err
		}
		if attempt.TaskID != "" || len(attempt.InputIDs) == 0 {
			return fmt.Errorf("control requires actual human input and no task")
		}
		location, e := t.GetLocation(worker.LocationID)
		if e != nil {
			return e
		}
		if location.IsolatedContextID != context.ID {
			return fmt.Errorf("control requires isolated scratch location")
		}
	}
	if scope.Kind == work.ScopeProject && context.Purpose != work.PurposeControl {
		if err = t.requireProjectReady(scope.ProjectID, worker.RoleID); err != nil {
			return err
		}
	}
	gates, err := t.ListPauseGates(attempt.ScopeID)
	if err != nil {
		return err
	}
	allowed, err := work.GatesAllowRunForPurpose(attempt.ScopeID, attempt.TaskID, attempt.ContextID, context.Purpose, gates)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("work is paused")
	}
	if attempt.TaskID != "" {
		task, e := t.GetTask(attempt.TaskID)
		if e != nil {
			return e
		}
		if task.RequirementsRevision != attempt.FrozenRequirementsRevision || task.AssignmentGeneration != attempt.FrozenAssignmentGeneration || task.ScopeID != attempt.ScopeID || task.OwnerRoleID != worker.RoleID || task.State == work.TaskDone || task.State == work.TaskCancelled {
			return fmt.Errorf("attempt task ownership/state mismatch")
		}
		var blocked int
		if e = t.tx.QueryRowContext(t.ctx, "SELECT count(*) FROM dependencies WHERE parent_id=? AND satisfied_by IS NULL", task.ID).Scan(&blocked); e != nil {
			return e
		}
		if blocked > 0 {
			return fmt.Errorf("task dependencies unsatisfied")
		}
	}
	if attempt.TaskID != "" {
		if _, err = t.tx.ExecContext(t.ctx, "INSERT INTO task_occupancy(task_id,attempt_id) VALUES(?,?)", attempt.TaskID, attempt.ID); err != nil {
			return fmt.Errorf("task occupied: %w", err)
		}
	}
	if worker.LocationID != "" {
		location, e := t.GetLocation(worker.LocationID)
		if e != nil {
			return e
		}
		if e = t.AssertNativeUpgradeAvailable(worker.Backend, location.HostID); e != nil {
			return e
		}
		if location.Generation != worker.LocationGeneration {
			return fmt.Errorf("stale location generation")
		}
		keys := append([]string{"cwd:" + location.HostID + ":" + location.Cwd}, location.ResourceKeys...)
		for _, key := range keys {
			if e = t.AssertMaintenanceResourceAvailable(key); e != nil {
				return e
			}
			if e = t.AssertRuntimeResourceAvailable(key); e != nil {
				return e
			}
			if _, e = t.tx.ExecContext(t.ctx, "INSERT INTO resource_occupancy(resource_key,attempt_id,generation) VALUES(?,?,?)", key, attempt.ID, attempt.RunRef.Generation); e != nil {
				return fmt.Errorf("resource occupied: %w", e)
			}
		}
	}
	operation := request.StartOperation
	if operation.ID != attempt.CommandID || operation.ContextID != attempt.ContextID || operation.ScopeID != attempt.ScopeID || operation.TargetID != worker.ID || operation.TargetGeneration != worker.Generation || operation.State != work.OperationPrepared {
		return fmt.Errorf("start operation binding mismatch")
	}
	canonical, created, err := t.PutOperation(operation, 0)
	if err != nil {
		return err
	}
	if !created || canonical.ID != attempt.CommandID {
		return fmt.Errorf("claim command key already belongs to another operation: %w", ErrIdempotencyConflict)
	}
	if _, err = t.tx.ExecContext(t.ctx, "INSERT INTO carrier_occupancy(instance_id,instance_generation,process_generation,attempt_id) VALUES(?,?,?,?)", worker.InstanceID, worker.InstanceGeneration, worker.ProcessGeneration, attempt.ID); err != nil {
		return fmt.Errorf("carrier occupied: %w", err)
	}
	// Occupancy and attempt inserts share a deferred FK, so reservations can be
	// prepared first while every failure still rolls back the complete claim.
	if err = t.record("attempts", attempt.ID, attempt.Revision, 0, attempt); err != nil {
		return err
	}
	for _, id := range attempt.InputIDs {
		input, e := t.GetInbox(id)
		if e != nil {
			return e
		}
		if input.ContentState == work.InputContentPending || input.TaskID != attempt.TaskID || input.State != work.InboxPending || input.ScopeID != attempt.ScopeID || input.RecipientRoleID != worker.RoleID || input.RecipientContextID != "" && input.RecipientContextID != worker.ContextID {
			return fmt.Errorf("input is not claimable by worker")
		}
		if revision, ok := request.ExpectedInputRevisions[id]; ok && revision != input.Revision {
			return ErrConflict
		}
		if !request.Now.IsZero() && input.AvailableAt.After(request.Now) {
			return fmt.Errorf("input not available yet")
		}
		input.RecipientContextID = worker.ContextID
		input.State = work.InboxClaimed
		input.ClaimedAttemptID = attempt.ID
		expected := input.Revision
		input.Revision++
		if e = t.PutInbox(input, expected); e != nil {
			return e
		}
		if _, e = t.tx.ExecContext(t.ctx, "INSERT INTO attempt_inputs(attempt_id,inbox_id) VALUES(?,?)", attempt.ID, id); e != nil {
			return e
		}
	}
	worker.State = work.WorkerBusy
	worker.Revision++
	return t.PutWorker(worker, request.ExpectedWorkerRevision)
}
func (t *Tx) GetAttempt(id string) (work.ExecutionAttempt, error) {
	var v work.ExecutionAttempt
	err := t.read("attempts", id, &v)
	return v, err
}
func (t *Tx) ListAttempts(scopeID string) ([]work.ExecutionAttempt, error) {
	return listRecords[work.ExecutionAttempt](t, "attempts", " WHERE scope_id=?", scopeID)
}
func (t *Tx) PutAttempt(v work.ExecutionAttempt, expected int64) error {
	if expected == 0 {
		return fmt.Errorf("create attempts through ClaimAttempt")
	}
	old, err := t.GetAttempt(v.ID)
	if err != nil {
		return err
	}
	if err = immutable(old.RunRef != v.RunRef || old.ContextID != v.ContextID || old.ScopeID != v.ScopeID || old.CommandID != v.CommandID || old.TaskID != v.TaskID || old.FrozenRequirementsRevision != v.FrozenRequirementsRevision || old.FrozenAssignmentGeneration != v.FrozenAssignmentGeneration || !reflect.DeepEqual(old.InputIDs, v.InputIDs) || !reflect.DeepEqual(old.FrozenRefs, v.FrozenRefs)); err != nil {
		return err
	}
	if err = work.ValidateAttemptTransition(old.State, v.State); err != nil {
		return err
	}
	return t.record("attempts", v.ID, v.Revision, expected, v)
}

func (t *Tx) GetAttemptByRunRef(runRef string) (work.ExecutionAttempt, error) {
	var id string
	err := t.tx.QueryRowContext(t.ctx, "SELECT id FROM attempts WHERE run_id=?", runRef).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return work.ExecutionAttempt{}, ErrNotFound
	}
	if err != nil {
		return work.ExecutionAttempt{}, err
	}
	return t.GetAttempt(id)
}
