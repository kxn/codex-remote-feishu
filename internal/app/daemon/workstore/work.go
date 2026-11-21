package workstore

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"reflect"
)

func (t *Tx) PutContext(v work.WorkContext, expected int64) error {
	scope, err := t.GetScope(v.ScopeID)
	if err != nil {
		return err
	}
	if scope.Kind == work.ScopeLibrary {
		return ErrScopeDenied
	}
	if (scope.Kind == work.ScopePersonal || scope.Kind == work.ScopeBootstrap) && scope.RoleID != v.RoleID {
		return fmt.Errorf("context role outside personal scope")
	}
	if v.Purpose == work.PurposeReview {
		if err = t.validateReviewContext(v, expected == 0); err != nil {
			return err
		}
	}
	if v.Purpose == work.PurposeControl {
		if err = t.ValidateControlContext(v); err != nil {
			return err
		}
	}
	if v.ProjectViewScopeID != "" {
		if scope.Kind != work.ScopePersonal {
			return ErrScopeDenied
		}
		target, e := t.GetContext(v.ProjectContextID)
		if e != nil {
			return e
		}
		if target.ScopeID != v.ProjectViewScopeID || target.RoleID != v.RoleID || target.Purpose == work.PurposeControl {
			return ErrScopeDenied
		}
		if expected == 0 {
			if e = t.RequireProjectPrincipalAccess(v.ProjectViewScopeID, scope.PrincipalID, v.CreatedAt); e != nil {
				return e
			}
		}
	}
	if v.TaskID != "" {
		task, e := t.GetTask(v.TaskID)
		if e != nil {
			return e
		}
		if task.ScopeID != v.ScopeID {
			return ErrScopeDenied
		}
		if expected == 0 && task.OwnerRoleID != v.RoleID {
			return ErrScopeDenied
		}
	}
	if expected > 0 {
		old, err := t.GetContext(v.ID)
		if err != nil {
			return err
		}
		if err = immutable(!reflect.DeepEqual(old.Review, v.Review) || old.ControlTargetContextID != v.ControlTargetContextID || old.Purpose != v.Purpose || old.ScopeID != v.ScopeID || old.RoleID != v.RoleID || old.Workstream != v.Workstream || old.TaskID != v.TaskID || old.ProjectViewScopeID != v.ProjectViewScopeID || old.ProjectContextID != v.ProjectContextID); err != nil {
			return err
		}
	}
	return t.record("contexts", v.ID, v.Revision, expected, v)
}
func (t *Tx) GetContext(id string) (work.WorkContext, error) {
	var v work.WorkContext
	err := t.read("contexts", id, &v)
	return v, err
}
func (t *Tx) ListContexts(scopeID string) ([]work.WorkContext, error) {
	return listRecords[work.WorkContext](t, "contexts", " WHERE scope_id=?", scopeID)
}
func (t *Tx) PutWorker(v work.WorkerSession, expected int64) error {
	if v.LocationID != "" {
		location, e := t.GetLocation(v.LocationID)
		if e != nil {
			return e
		}
		if location.Generation != v.LocationGeneration {
			return fmt.Errorf("worker location generation does not match registered location")
		}
	}
	context, err := t.GetContext(v.ContextID)
	if err != nil {
		return err
	}
	if err = work.ValidateBinding(context, v); err != nil {
		return err
	}
	if expected > 0 {
		old, e := t.GetWorker(v.ID)
		if e != nil {
			return e
		}
		if e = immutable(old.ContextID != v.ContextID || old.ScopeID != v.ScopeID || old.RoleID != v.RoleID || old.Backend != v.Backend); e != nil {
			return e
		}
		if e = work.ValidateWorkerTransition(old.State, v.State); e != nil {
			return e
		}
		if v.Generation < old.Generation || v.Generation > old.Generation+1 || v.ControllerGeneration < old.ControllerGeneration || v.InstanceGeneration < old.InstanceGeneration || v.ProcessGeneration < old.ProcessGeneration || (v.LocationID == old.LocationID && v.LocationGeneration < old.LocationGeneration) {
			return fmt.Errorf("worker generation regressed or skipped")
		}
		bindingChanged := old.CarrierIncarnation != v.CarrierIncarnation || old.NativeSessionID != v.NativeSessionID || old.NativeNamespace != v.NativeNamespace || old.InstanceID != v.InstanceID || old.LocationID != v.LocationID || old.ProcessGeneration != v.ProcessGeneration || old.InstanceGeneration != v.InstanceGeneration || old.LocationGeneration != v.LocationGeneration
		if bindingChanged || v.Generation != old.Generation || v.State == work.WorkerRetired {
			if err = t.AssertMaintenanceResourceAvailable("worker:" + old.ID); err != nil {
				return err
			}
			count, e := t.CountControllerOccupancy(old.ID)
			if e != nil {
				return e
			}
			if count > 0 {
				return fmt.Errorf("worker retains human/handoff location occupancy")
			}
		}
		if bindingChanged && v.Generation != old.Generation+1 {
			return fmt.Errorf("binding change requires new worker generation")
		}
		if bindingChanged || v.Generation != old.Generation || v.State == work.WorkerReady || v.State == work.WorkerRetired {
			var occupied int
			if e = t.tx.QueryRowContext(t.ctx, "SELECT count(*) FROM carrier_occupancy o JOIN attempts a ON a.id=o.attempt_id WHERE a.worker_id=?", old.ID).Scan(&occupied); e != nil {
				return e
			}
			if occupied > 0 {
				return fmt.Errorf("worker retains unreconciled execution occupancy")
			}
		}
		if old.State == work.WorkerBusy && (bindingChanged || v.Generation != old.Generation) {
			return fmt.Errorf("busy worker cannot rebind")
		}
		if old.Controller != v.Controller && v.ControllerGeneration != old.ControllerGeneration+1 {
			return fmt.Errorf("controller change requires generation")
		}
	}
	return t.record("workers", v.ID, v.Revision, expected, v)
}
func (t *Tx) GetWorker(id string) (work.WorkerSession, error) {
	var v work.WorkerSession
	err := t.read("workers", id, &v)
	return v, err
}

func (t *Tx) CurrentContextWorker(contextID string) (work.WorkerSession, error) {
	workers, err := listRecords[work.WorkerSession](t, "workers", " WHERE context_id=? AND state<>'retired'", contextID)
	if err != nil {
		return work.WorkerSession{}, err
	}
	if len(workers) == 0 {
		return work.WorkerSession{}, ErrNotFound
	}
	if len(workers) != 1 {
		return work.WorkerSession{}, fmt.Errorf("work context has multiple current workers")
	}
	return workers[0], nil
}
func (t *Tx) ListWorkers(scopeID string) ([]work.WorkerSession, error) {
	return listRecords[work.WorkerSession](t, "workers", " WHERE scope_id=?", scopeID)
}
func (t *Tx) PutTask(v work.Task, expected int64) error {
	return t.atomic(func() error { return t.putTask(v, expected) })
}
func (t *Tx) putTask(v work.Task, expected int64) error {
	scope, scopeErr := t.GetScope(v.ScopeID)
	if scopeErr != nil {
		return scopeErr
	}
	if scope.Kind == work.ScopeBootstrap || scope.Kind == work.ScopeLibrary {
		return fmt.Errorf("this scope cannot contain tasks")
	}
	if err := t.requireRoleInScope(v.ScopeID, v.OwnerRoleID); err != nil {
		return err
	}
	if expected == 0 && v.RequirementsRevision != 1 {
		return fmt.Errorf("initial requirements revision must be 1")
	}
	v.AvailableResultRevision = 0
	if v.HasAvailableResult() {
		v.AvailableResultRevision = v.Revision
	}
	if expected > 0 {
		old, err := t.GetTask(v.ID)
		if err != nil {
			return err
		}
		if err = immutable(old.ScopeID != v.ScopeID); err != nil {
			return err
		}
		if err = work.ValidateTaskTransition(old.State, v.State); err != nil {
			return err
		}
		if old.HasAvailableResult() && v.HasAvailableResult() {
			v.AvailableResultRevision = old.AvailableResultRevision
		}
		if v.AssignmentGeneration < old.AssignmentGeneration || v.AssignmentGeneration > old.AssignmentGeneration+1 {
			return fmt.Errorf("invalid assignment generation")
		}
		if old.OwnerRoleID != v.OwnerRoleID && v.AssignmentGeneration != old.AssignmentGeneration+1 {
			return fmt.Errorf("owner change requires assignment generation")
		}
		changed := old.Title != v.Title || !reflect.DeepEqual(old.Requirements, v.Requirements) || !reflect.DeepEqual(old.BusinessSource, v.BusinessSource)
		want := old.RequirementsRevision
		if changed {
			want++
		}
		if v.RequirementsRevision != want {
			return fmt.Errorf("requirements revision must reflect semantic changes")
		}
		if changed || v.AssignmentGeneration != old.AssignmentGeneration || (old.State != v.State && v.State != work.TaskWaiting) {
			if err = t.checkTaskParticipation(old, v); err != nil {
				return err
			}
		}
		if changed || v.AssignmentGeneration != old.AssignmentGeneration {
			if err = t.invalidateDependentConclusions(v.ID, old.RequirementsRevision, old.AssignmentGeneration, v.UpdatedAt); err != nil {
				return err
			}
		}
	}
	if v.State == work.TaskDone {
		var blocked int
		if err := t.tx.QueryRowContext(t.ctx, "SELECT count(*) FROM dependencies WHERE parent_id=? AND satisfied_by IS NULL", v.ID).Scan(&blocked); err != nil {
			return err
		}
		if blocked > 0 {
			return fmt.Errorf("task has unsatisfied dependencies")
		}
		evidence, err := t.ListEvidence(v.ID)
		if err != nil {
			return err
		}
		if err = work.ValidateTaskCompletion(v, evidence); err != nil {
			return err
		}
	}
	if err := t.record("tasks", v.ID, v.Revision, expected, v); err != nil {
		return err
	}
	if expected > 0 {
		if err := t.refreshTaskParticipation(v); err != nil {
			return err
		}
	}
	_, err := t.tx.ExecContext(t.ctx, "INSERT INTO task_requirements(task_id,requirements_revision,payload) VALUES(?,?,?) ON CONFLICT(task_id,requirements_revision) DO NOTHING", v.ID, v.RequirementsRevision, mustJSON(v))
	return err
}
func (t *Tx) GetTaskRequirements(taskID string, revision int64) (work.Task, error) {
	var task work.Task
	var payload string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM task_requirements WHERE task_id=? AND requirements_revision=?", taskID, revision).Scan(&payload)
	if err == sql.ErrNoRows {
		return task, ErrNotFound
	}
	if err != nil {
		return task, err
	}
	err = json.Unmarshal([]byte(payload), &task)
	return task, err
}
func (t *Tx) GetTask(id string) (work.Task, error) {
	var v work.Task
	err := t.read("tasks", id, &v)
	return v, err
}
func (t *Tx) ListTasks(scopeID string) ([]work.Task, error) {
	return listRecords[work.Task](t, "tasks", " WHERE scope_id=?", scopeID)
}
func (t *Tx) PutPauseGate(v work.PauseGate, expected int64) error {
	if v.ContextID != "" {
		context, e := t.GetContext(v.ContextID)
		if e != nil {
			return e
		}
		if context.ScopeID != v.ScopeID {
			return ErrScopeDenied
		}
	}
	if v.TaskID != "" {
		task, err := t.GetTask(v.TaskID)
		if err != nil {
			return err
		}
		if task.ScopeID != v.ScopeID {
			return fmt.Errorf("pause gate outside task scope")
		}
	}
	if expected > 0 {
		old, err := t.GetPauseGate(v.ID)
		if err != nil {
			return err
		}
		if err = immutable(old.ScopeID != v.ScopeID || old.TaskID != v.TaskID || old.Source != v.Source || old.SourceID != v.SourceID || old.ContextID != v.ContextID); err != nil {
			return err
		}
	}
	return t.record("pause_gates", v.ID, v.Revision, expected, v)
}
func (t *Tx) GetPauseGate(id string) (work.PauseGate, error) {
	var v work.PauseGate
	err := t.read("pause_gates", id, &v)
	return v, err
}
func (t *Tx) ListPauseGates(scopeID string) ([]work.PauseGate, error) {
	return listRecords[work.PauseGate](t, "pause_gates", " WHERE scope_id=?", scopeID)
}
func (t *Tx) AddDependency(v work.Dependency) error {
	if err := v.Validate(); err != nil {
		return err
	}
	parent, err := t.GetTask(v.TaskID)
	if err != nil {
		return err
	}
	child, err := t.GetTask(v.DependsOnTaskID)
	if err != nil {
		return err
	}
	if parent.ScopeID != child.ScopeID {
		return fmt.Errorf("cross-scope dependency")
	}
	var cycle int
	err = t.tx.QueryRowContext(t.ctx, `WITH RECURSIVE descendants(id) AS (SELECT child_id FROM dependencies WHERE parent_id=? UNION SELECT d.child_id FROM dependencies d JOIN descendants ON d.parent_id=descendants.id) SELECT count(*) FROM descendants WHERE id=?`, v.DependsOnTaskID, v.TaskID).Scan(&cycle)
	if err != nil {
		return err
	}
	if cycle > 0 {
		return fmt.Errorf("dependency cycle")
	}
	_, err = t.tx.ExecContext(t.ctx, "INSERT INTO dependencies(parent_id,child_id,condition,satisfied_by) VALUES(?,?,?,?)", v.TaskID, v.DependsOnTaskID, v.Condition, dependencyEvidence(v.SatisfiedBy))
	return err
}
func (t *Tx) ListDependencies(taskID string) ([]work.Dependency, error) {
	rows, err := t.tx.QueryContext(t.ctx, "SELECT parent_id,child_id,condition,satisfied_by FROM dependencies WHERE parent_id=? ORDER BY child_id,condition", taskID)
	return readDependencies(rows, err)
}

func (t *Tx) ListPendingDependentTasks(childID string, condition work.DependencyCondition) ([]work.Dependency, error) {
	rows, err := t.tx.QueryContext(t.ctx, "SELECT parent_id,child_id,condition,satisfied_by FROM dependencies WHERE child_id=? AND condition=? AND satisfied_by IS NULL ORDER BY parent_id", childID, condition)
	return readDependencies(rows, err)
}

func readDependencies(rows *sql.Rows, err error) ([]work.Dependency, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []work.Dependency{}
	for rows.Next() {
		var v work.Dependency
		var evidence sql.NullString
		if err = rows.Scan(&v.TaskID, &v.DependsOnTaskID, &v.Condition, &evidence); err != nil {
			return nil, err
		}
		if evidence.Valid {
			var ref work.SourceRef
			if err = json.Unmarshal([]byte(evidence.String), &ref); err != nil {
				return nil, err
			}
			v.SatisfiedBy = &ref
		}
		values = append(values, v)
	}
	return values, rows.Err()
}

func dependencyEvidence(value *work.SourceRef) any {
	if value == nil {
		return nil
	}
	return mustJSON(value)
}
