package workservice

import (
	"context"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type ControlContextView struct {
	Project          *work.Project               `json:"project,omitempty"`
	Scope            work.WorkScope              `json:"scope"`
	ControlContext   work.WorkContext            `json:"control_context"`
	TargetContext    *work.WorkContext           `json:"target_context,omitempty"`
	Gates            []work.PauseGate            `json:"gates"`
	Workers          []work.WorkerSession        `json:"workers"`
	DispatchProblems []workstore.DispatchProblem `json:"dispatch_problems"`
	RuntimeGates     []workstore.RuntimeGate     `json:"runtime_gates"`
}

func (s *Service) ControlContext(ctx context.Context, token, runRef string) (view ControlContextView, err error) {
	err = s.withManagementWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		var e error
		view.ControlContext, e = tx.GetContext(c.Worker.ContextID)
		if e != nil {
			return e
		}
		if view.ControlContext.Purpose != work.PurposeControl {
			return workauth.ErrUnauthorized
		}
		view.Scope, e = tx.GetScope(c.Worker.ScopeID)
		if e != nil {
			return e
		}
		if view.ControlContext.ControlTargetContextID != "" {
			target, e := tx.GetContext(view.ControlContext.ControlTargetContextID)
			if e != nil {
				return e
			}
			view.TargetContext = &target
			if target.ProjectViewScopeID != "" && tx.RequireProjectPrincipalAccess(target.ProjectViewScopeID, view.Scope.PrincipalID, s.now()) == nil {
				projectScope, e := tx.GetScope(target.ProjectViewScopeID)
				if e != nil {
					return e
				}
				project, e := tx.GetProject(projectScope.ProjectID)
				if e != nil {
					return e
				}
				view.Project = &project
			}

		}
		view.Gates, e = tx.ListPauseGates(view.Scope.ID)
		if e != nil {
			return e
		}
		all, e := tx.ListWorkers(view.Scope.ID)
		if e != nil {
			return e
		}
		for _, worker := range all {
			if view.TargetContext != nil && worker.ContextID != view.TargetContext.ID {
				continue
			}
			view.Workers = append(view.Workers, worker)
			if gate, e := tx.GetRuntimeGate(worker.ID); e == nil {
				view.RuntimeGates = append(view.RuntimeGates, gate)
			} else if e != workstore.ErrNotFound {
				return e
			}
		}
		problems, e := tx.ListDispatchProblems(view.Scope.ID)
		if e != nil {
			return e
		}
		for _, problem := range problems {
			if view.TargetContext == nil || problem.ContextID == "" || problem.ContextID == view.TargetContext.ID {
				view.DispatchProblems = append(view.DispatchProblems, problem)
			}
		}
		return nil
	})
	return
}

type ControlResumeCurrentRequest struct {
	ExpectedProjectRevision int64  `json:"expected_project_revision,omitempty"`
	Key                     string `json:"key"`
	SourceMessageID         string `json:"source_message_id"`
	ExpectedGateRevision    int64  `json:"expected_gate_revision"`
}

func (s *Service) ControlResumeCurrent(ctx context.Context, token, runRef string, req ControlResumeCurrentRequest) (result Result, err error) {
	err = s.withManagementWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		wc, e := tx.GetContext(c.Worker.ContextID)
		if e != nil {
			return e
		}
		scope, e := tx.GetScope(wc.ScopeID)
		if e != nil {
			return e
		}
		if scope.Kind != work.ScopePersonal || wc.Purpose != work.PurposeControl || wc.ControlTargetContextID == "" {
			return workauth.ErrUnauthorized
		}
		message, _, e := currentHumanInstruction(tx, c, req.SourceMessageID)
		if e != nil {
			return e
		}
		if message.Source.ID != scope.PrincipalID {
			return workauth.ErrUnauthorized
		}
		result, e = s.mutate(tx, c, "private.control.resume", req.Key, wc.ControlTargetContextID, req, func() (string, int64, error) {

			targetContext, e := tx.GetContext(wc.ControlTargetContextID)
			if e != nil {
				return "", 0, e
			}
			if targetContext.ProjectViewScopeID != "" {
				if req.ExpectedGateRevision != 0 || req.ExpectedProjectRevision < 1 {
					return "", 0, fmt.Errorf("selected project resume needs its exact project revision")
				}
				if e = tx.RequireProjectPrincipalAccess(targetContext.ProjectViewScopeID, scope.PrincipalID, s.now()); e != nil {
					return "", 0, e
				}
				if e = ensureMember(tx, targetContext.ProjectViewScopeID, scope.RoleID); e != nil {
					return "", 0, e
				}
				projectScope, e := tx.GetScope(targetContext.ProjectViewScopeID)
				if e != nil {
					return "", 0, e
				}
				project, e := tx.GetProject(projectScope.ProjectID)
				if e != nil {
					return "", 0, e
				}
				if project.Revision != req.ExpectedProjectRevision {
					return "", 0, workstore.ErrConflict
				}
				if project.State != work.ProjectPaused {
					return "", 0, fmt.Errorf("项目尚未处于可恢复的已暂停状态；原生执行或其他阻塞仍需单独核对")
				}
				resumed, e := applyProjectTransition(tx, project, "project.resume")
				return resumed.ID, resumed.Revision, e
			}
			if req.ExpectedProjectRevision != 0 {
				return "", 0, fmt.Errorf("personal work resume does not accept another project target")
			}
			gates, e := tx.ListPauseGates(scope.ID)
			if e != nil {
				return "", 0, e
			}
			var target *work.PauseGate
			for _, gate := range gates {
				if gate.ContextID == wc.ControlTargetContextID && gate.Source == work.GateContext && gate.SourceID == scope.PrincipalID {
					if target != nil {
						return "", 0, fmt.Errorf("multiple personal pause gates require explicit reconciliation")
					}
					copy := gate
					target = &copy
				}
			}
			if target == nil {
				return "", 0, fmt.Errorf("当前工作没有可由此入口解除的个人暂停")
			}
			if target.Revision != req.ExpectedGateRevision {
				return "", 0, workstore.ErrConflict
			}
			old := target.Revision
			target.Revision++
			target.State = work.GateOpen
			if e = tx.PutPauseGate(*target, old); e != nil {
				return "", 0, e
			}
			return target.ID, target.Revision, nil
		})
		return e
	})
	return
}
