package workservice

import (
	"context"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type TaskListRequest struct {
	Limit       int            `json:"limit,omitempty"`
	After       string         `json:"after,omitempty"`
	State       work.TaskState `json:"state,omitempty"`
	OwnerRoleID string         `json:"owner_role_id,omitempty"`
}
type TaskInspectRequest struct {
	TaskID string `json:"task_id"`
}
type TaskView struct {
	Task         work.Task                  `json:"task"`
	Dependencies []work.Dependency          `json:"dependencies"`
	Evidence     []work.RequirementEvidence `json:"evidence"`
	Gates        []work.PauseGate           `json:"gates"`
}
type TaskTransitionRequest struct {
	Key              string `json:"key"`
	TaskID           string `json:"task_id"`
	ExpectedRevision int64  `json:"expected_revision"`
}

// Lifecycle controls can reopen only their own gate. They require the current
// running carrier even while a gate is closed; receipt-only recovery is excluded.
func (s *Service) withLifecycleWorker(ctx context.Context, token, run, kind, key string, control bool, fn func(*workstore.Tx, Caller) error) error {
	return s.withWorkerReceipt(ctx, token, run, func(tx *workstore.Tx, c Caller) error {
		wc, err := tx.GetContext(c.Worker.ContextID)
		if err != nil {
			return err
		}
		if wc.Purpose == work.PurposeControl {
			if kind != "" && kind != "project.pause" && kind != "project.resume" && kind != "project.archive" && kind != "project.reopen" {
				return workauth.ErrUnauthorized
			}
			if err = s.authorizeControlCaller(tx, c); err != nil {
				return err
			}
			return fn(tx, c)
		}
		if c.Worker.State != work.WorkerBusy || c.Attempt.State != work.AttemptRunning {
			return workauth.ErrUnauthorized
		}
		scope, err := tx.GetScope(c.Worker.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind == work.ScopeBootstrap {
			return workauth.ErrUnauthorized
		}
		if err = ensureMember(tx, c.Worker.ScopeID, c.Worker.RoleID); err != nil {
			return err
		}
		if !control {
			gates, err := tx.ListPauseGates(scope.ID)
			if err != nil {
				return err
			}
			allowed, err := work.GatesAllowRunForContext(scope.ID, c.Attempt.TaskID, c.Worker.ContextID, gates)
			if err != nil {
				return err
			}
			if !allowed {
				return workauth.ErrUnauthorized
			}
		}
		if c.Attempt.TaskID != "" {
			task, err := tx.GetTask(c.Attempt.TaskID)
			if err != nil {
				return err
			}
			if task.OwnerRoleID != c.Worker.RoleID || task.RequirementsRevision != c.Attempt.FrozenRequirementsRevision || task.AssignmentGeneration != c.Attempt.FrozenAssignmentGeneration {
				return workauth.ErrUnauthorized
			}
			if task.State == work.TaskDone || task.State == work.TaskCancelled {
				op, err := tx.GetOperation(stableID("op", c.Worker.ScopeID, string(work.ActorRole), c.Worker.RoleID, kind, key))
				if err != nil || op.State != work.OperationConfirmed {
					return workauth.ErrUnauthorized
				}
			}
		}
		return fn(tx, c)
	})
}
func (s *Service) TaskList(ctx context.Context, token, run string, req TaskListRequest) (KnowledgePage[work.Task], error) {
	var page KnowledgePage[work.Task]
	err := s.withLifecycleWorker(ctx, token, run, "", "", true, func(tx *workstore.Tx, c Caller) error {
		all, err := tx.ListTasks(c.Worker.ScopeID)
		if err != nil {
			return err
		}
		items := []work.Task{}
		for _, task := range all {
			if (req.State == "" || task.State == req.State) && (req.OwnerRoleID == "" || task.OwnerRoleID == req.OwnerRoleID) {
				items = append(items, task)
			}
		}
		page, err = knowledgePage(items, KnowledgeListRequest{Limit: req.Limit, After: req.After}, func(v work.Task) string { return v.ID })
		return err
	})
	return page, err
}
func (s *Service) TaskInspect(ctx context.Context, token, run string, req TaskInspectRequest) (TaskView, error) {
	var view TaskView
	err := s.withLifecycleWorker(ctx, token, run, "", "", true, func(tx *workstore.Tx, c Caller) error {
		task, err := tx.GetTask(req.TaskID)
		if err != nil {
			return err
		}
		if task.ScopeID != c.Worker.ScopeID {
			return workauth.ErrUnauthorized
		}
		view.Task = task
		if view.Dependencies, err = tx.ListDependencies(task.ID); err != nil {
			return err
		}
		if view.Evidence, err = tx.ListEvidence(task.ID); err != nil {
			return err
		}
		gates, err := tx.ListPauseGates(task.ScopeID)
		if err != nil {
			return err
		}
		view.Gates = []work.PauseGate{}
		for _, gate := range gates {
			if gate.TaskID == "" || gate.TaskID == task.ID {
				view.Gates = append(view.Gates, gate)
			}
		}
		return nil
	})
	return view, err
}
func (s *Service) taskTransition(ctx context.Context, token, run, kind string, req TaskTransitionRequest, to work.TaskState) (Result, error) {
	var result Result
	err := s.withLifecycleWorker(ctx, token, run, kind, req.Key, false, func(tx *workstore.Tx, c Caller) error {
		var err error
		result, err = s.mutate(tx, c, kind, req.Key, req.TaskID, req, func() (string, int64, error) {
			task, err := tx.GetTask(req.TaskID)
			if err != nil {
				return "", 0, err
			}
			if err = mayManageTask(tx, c, task); err != nil {
				return "", 0, err
			}
			if task.Revision != req.ExpectedRevision {
				return "", 0, workstore.ErrConflict
			}
			if task.State == work.TaskDone || task.State == work.TaskCancelled {
				return "", 0, fmt.Errorf("terminal task requires a new follow-up task")
			}
			task.State = to
			task.Revision++
			task.UpdatedAt = s.now()
			if err = tx.PutTask(task, req.ExpectedRevision); err != nil {
				return "", 0, err
			}
			return task.ID, task.Revision, nil
		})
		return err
	})
	return result, err
}
func (s *Service) TaskStart(ctx context.Context, t, r string, q TaskTransitionRequest) (Result, error) {
	return s.taskTransition(ctx, t, r, "task.start", q, work.TaskActive)
}
func (s *Service) TaskWait(ctx context.Context, t, r string, q TaskTransitionRequest) (Result, error) {
	return s.taskTransition(ctx, t, r, "task.wait", q, work.TaskWaiting)
}
func (s *Service) TaskCheck(ctx context.Context, t, r string, q TaskTransitionRequest) (Result, error) {
	return s.taskTransition(ctx, t, r, "task.check", q, work.TaskChecking)
}
func (s *Service) TaskDone(ctx context.Context, t, r string, q TaskTransitionRequest) (Result, error) {
	return s.taskTransition(ctx, t, r, "task.done", q, work.TaskDone)
}
func (s *Service) TaskCancel(ctx context.Context, t, r string, q TaskTransitionRequest) (Result, error) {
	return s.taskTransition(ctx, t, r, "task.cancel", q, work.TaskCancelled)
}

func (s *Service) taskGate(ctx context.Context, token, run string, req TaskTransitionRequest, closed bool) (Result, error) {
	kind := "task.resume"
	if closed {
		kind = "task.pause"
	}
	var result Result
	err := s.withLifecycleWorker(ctx, token, run, kind, req.Key, true, func(tx *workstore.Tx, c Caller) error {
		var err error
		result, err = s.mutate(tx, c, kind, req.Key, req.TaskID, req, func() (string, int64, error) {
			task, err := tx.GetTask(req.TaskID)
			if err != nil {
				return "", 0, err
			}
			if err = mayManageTask(tx, c, task); err != nil {
				return "", 0, err
			}
			if task.Revision != req.ExpectedRevision {
				return "", 0, workstore.ErrConflict
			}
			id := stableID("gate", task.ScopeID, "task", task.ID)
			gates, err := tx.ListPauseGates(task.ScopeID)
			if err != nil {
				return "", 0, err
			}
			gate := work.PauseGate{ID: id, ScopeID: task.ScopeID, TaskID: task.ID, Source: work.GateTask, SourceID: task.ID, Revision: 1}
			expected := int64(0)
			for _, existing := range gates {
				if existing.ID == id {
					gate = existing
					expected = gate.Revision
					gate.Revision++
				}
			}
			gate.State = work.GateOpen
			if closed {
				gate.State = work.GateClosed
			}
			if err = tx.PutPauseGate(gate, expected); err != nil {
				return "", 0, err
			}
			task.Revision++
			task.UpdatedAt = s.now()
			if err = tx.PutTask(task, req.ExpectedRevision); err != nil {
				return "", 0, err
			}
			return task.ID, task.Revision, nil
		})
		return err
	})
	return result, err
}
func (s *Service) TaskPause(ctx context.Context, t, r string, q TaskTransitionRequest) (Result, error) {
	return s.taskGate(ctx, t, r, q, true)
}
func (s *Service) TaskResume(ctx context.Context, t, r string, q TaskTransitionRequest) (Result, error) {
	return s.taskGate(ctx, t, r, q, false)
}
