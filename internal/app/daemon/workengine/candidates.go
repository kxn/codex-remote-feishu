package workengine

import (
	"context"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"sort"
)

type inputGroup struct{ taskID, contextID string }

func (e *Engine) snapshot(ctx context.Context) (candidates []Candidate, deferred []Deferral, needs []WorkerNeed, err error) {
	err = e.store.Within(ctx, func(tx *workstore.Tx) error {
		targets, err := tx.ListSchedulingTargets()
		if err != nil {
			return err
		}
		cursor, err := tx.GetSchedulingCursor(e.config.SchedulerID)
		if err != nil && !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		targets = fairTargets(targets, cursor)
		byTarget := make([][]Candidate, len(targets))
		used := map[string]bool{}
		for index, target := range targets {
			scope, err := tx.GetScope(target.ScopeID)
			if err != nil {
				return err
			}
			inputs, err := tx.ListPendingInputs(target.ScopeID, target.RoleID)
			if err != nil {
				return err
			}
			sort.SliceStable(inputs, func(i, j int) bool {
				if inputs[i].AvailableAt.Equal(inputs[j].AvailableAt) {
					return inputs[i].ID < inputs[j].ID
				}
				return inputs[i].AvailableAt.Before(inputs[j].AvailableAt)
			})
			groups := map[inputGroup][]work.Inbox{}
			order := []inputGroup{}
			now := e.config.Now()
			for _, input := range inputs {
				if input.ContentState == work.InputContentPending || input.AvailableAt.After(now) {
					continue
				}
				key := inputGroup{input.TaskID, input.RecipientContextID}
				if len(groups[key]) == 0 {
					order = append(order, key)
				}
				if len(groups[key]) < e.config.MaxInputsPerAttempt {
					groups[key] = append(groups[key], input)
				}
			}
			contexts, err := tx.ListContexts(scope.ID)
			if err != nil {
				return err
			}
			workers, err := tx.ListWorkers(scope.ID)
			if err != nil {
				return err
			}
			contextByID := map[string]work.WorkContext{}
			for _, context := range contexts {
				contextByID[context.ID] = context
			}
			for _, group := range order {
				need := WorkerNeed{ScopeID: scope.ID, RoleID: target.RoleID, TaskID: group.taskID, ContextID: group.contextID}
				if err := readyScopeForContext(tx, scope, target.RoleID, group.taskID, contextByID[group.contextID]); err != nil {
					deferred = append(deferred, Deferral{Need: need, Reason: err.Error()})
					continue
				}
				var chosen *work.WorkerSession
				var context work.WorkContext
				exists := false
				for i := range workers {
					worker := workers[i]
					if worker.RoleID != target.RoleID || worker.State == work.WorkerRetired {
						continue
					}
					owner, ok := contextByID[worker.ContextID]
					if !ok {
						continue
					}
					if owner.Purpose == work.PurposeControl && group.contextID == "" {
						continue
					}
					if group.contextID != "" {
						if owner.ID != group.contextID {
							continue
						}
						if owner.TaskID != "" && owner.TaskID != group.taskID {
							continue
						}
					} else if owner.TaskID != group.taskID {
						continue
					}
					exists = true
					if worker.Controller == work.ControllerAutomatic && (worker.State == work.WorkerPreparing || worker.State == work.WorkerRecovering) {
						occupied, e := tx.WorkerHasOccupancy(worker.ID)
						if e != nil {
							return e
						}
						gate, gerr := tx.GetRuntimeGate(worker.ID)
						if gerr != nil && !errors.Is(gerr, workstore.ErrNotFound) {
							return gerr
						}
						eligible := worker.State == work.WorkerPreparing && (errors.Is(gerr, workstore.ErrNotFound) || gate.Reason == "snapshot_rebuild_required")
						if worker.State == work.WorkerRecovering && gerr == nil && gate.Reason == "snapshot_rebuild_required" {
							eligible = true
						}
						if !occupied && eligible {
							prepare := need
							prepare.ContextID = owner.ID
							needs = append(needs, prepare)
						}
					}
					if used[worker.ID] || worker.State != work.WorkerReady || worker.Controller != work.ControllerAutomatic || worker.InstanceID == "" || worker.CarrierIncarnation == "" {
						continue
					}
					contextGates, e := tx.ListPauseGates(scope.ID)
					if e != nil {
						return e
					}
					allowed, e := work.GatesAllowRunForPurpose(scope.ID, group.taskID, owner.ID, owner.Purpose, contextGates)
					if e != nil {
						return e
					}
					if !allowed {
						continue
					}
					chosen = &worker
					context = owner
					break
				}
				if chosen == nil {
					reason := "worker requires preparation"
					if exists {
						reason = "workstream worker is busy or unavailable"
					} else {
						needs = append(needs, need)
					}
					deferred = append(deferred, Deferral{Need: need, Reason: reason})
					continue
				}
				role, err := tx.GetRole(target.RoleID)
				if err != nil {
					return err
				}
				binding, err := tx.GetRoleBotBinding(target.RoleID)
				if err != nil {
					deferred = append(deferred, Deferral{Need: need, Reason: "role bot binding unavailable"})
					continue
				}
				candidate := Candidate{Scope: scope, Context: context, Worker: *chosen, Role: role, BotBinding: binding, Inputs: groups[group]}
				if group.taskID != "" {
					task, err := tx.GetTask(group.taskID)
					if err != nil {
						return err
					}
					candidate.Task = &task
				}
				byTarget[index] = append(byTarget[index], candidate)
				used[chosen.ID] = true
			}
		}
		for round := 0; ; round++ {
			more := false
			for _, values := range byTarget {
				if round < len(values) {
					candidates = append(candidates, values[round])
					more = true
				}
			}
			if !more {
				break
			}
		}
		return nil
	})
	return
}
func readyScope(tx *workstore.Tx, scope work.WorkScope, roleID, taskID string) error {
	if scope.Kind == work.ScopeBootstrap && (roleID != scope.RoleID || taskID != "") {
		return fmt.Errorf("bootstrap only admits its leader input workstream")
	}
	if scope.Kind == work.ScopeProject {
		project, err := tx.GetProject(scope.ProjectID)
		if err != nil {
			return err
		}
		if project.State != work.ProjectActive && project.State != work.ProjectDegraded {
			return fmt.Errorf("project is not active")
		}
		member, err := tx.GetProjectMember(scope.ProjectID, roleID)
		if err != nil || member.State != work.ProjectMemberReady {
			return fmt.Errorf("project member is not ready")
		}
		team, err := tx.GetTeamMember(scope.TeamID, roleID)
		if err != nil || !team.Active {
			return fmt.Errorf("team member is not active")
		}
	}
	gates, err := tx.ListPauseGates(scope.ID)
	if err != nil {
		return err
	}
	allowed, err := work.GatesAllowRun(scope.ID, taskID, gates)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("work is paused")
	}
	if taskID != "" {
		task, err := tx.GetTask(taskID)
		if err != nil {
			return err
		}
		if task.OwnerRoleID != roleID || task.State == work.TaskDone || task.State == work.TaskCancelled {
			return fmt.Errorf("task is no longer assigned or executable")
		}
		dependencies, err := tx.ListDependencies(taskID)
		if err != nil {
			return err
		}
		for _, dependency := range dependencies {
			if dependency.SatisfiedBy == nil {
				return fmt.Errorf("task dependencies are not satisfied")
			}
		}
	}
	return nil
}

func readyScopeForContext(tx *workstore.Tx, scope work.WorkScope, roleID, taskID string, context work.WorkContext) error {
	if context.Purpose != work.PurposeControl {
		return readyScope(tx, scope, roleID, taskID)
	}
	if context.RoleID != roleID || taskID != "" {
		return workstore.ErrScopeDenied
	}
	if err := tx.ValidateControlContext(context); err != nil {
		return err
	}
	gates, err := tx.ListPauseGates(scope.ID)
	if err != nil {
		return err
	}
	allowed, err := work.GatesAllowRunForPurpose(scope.ID, "", context.ID, context.Purpose, gates)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("control context is paused")
	}
	return nil
}
