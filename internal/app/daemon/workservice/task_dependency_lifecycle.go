package workservice

import (
	"context"
	"fmt"
	"strconv"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type TaskDependencyRequest struct {
	Key               string                   `json:"key"`
	TaskID            string                   `json:"task_id"`
	ExpectedRevision  int64                    `json:"expected_revision"`
	DependsOnTaskID   string                   `json:"depends_on_task_id"`
	DependsOnRevision int64                    `json:"depends_on_revision"`
	Condition         work.DependencyCondition `json:"condition"`
	Source            *work.SourceRef          `json:"source,omitempty"`
}

func (s *Service) taskDependency(ctx context.Context, token, run string, req TaskDependencyRequest, satisfy bool) (Result, error) {
	kind := "task.dependency.add"
	if satisfy {
		kind = "task.dependency.satisfy"
	}
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
				return "", 0, fmt.Errorf("terminal task dependencies cannot change")
			}
			child, err := tx.GetTask(req.DependsOnTaskID)
			if err != nil {
				return "", 0, err
			}
			if child.ScopeID != task.ScopeID {
				return "", 0, workauth.ErrUnauthorized
			}
			if child.Revision != req.DependsOnRevision {
				return "", 0, workstore.ErrConflict
			}
			dep := work.Dependency{TaskID: task.ID, DependsOnTaskID: child.ID, Condition: req.Condition}
			if satisfy {
				if req.Source == nil {
					return "", 0, fmt.Errorf("dependency satisfaction needs an explicit evidence source")
				}
				if req.Condition == work.DependencyResultAvailable && child.State != work.TaskChecking && child.State != work.TaskDone {
					return "", 0, fmt.Errorf("dependency result is not declared available")
				}
				if req.Condition == work.DependencyReviewAccepted || req.Condition == work.DependencyIntegrated || req.Condition == work.DependencyResultAvailable {
					evidence, err := tx.ListEvidence(child.ID)
					if err != nil {
						return "", 0, err
					}
					found := req.Condition == work.DependencyResultAvailable && req.Source.Kind == "task" && req.Source.ID == child.ID && req.Source.Version == strconv.FormatInt(child.Revision, 10)
					for _, v := range evidence {
						if !v.Obsolete && v.RequirementsRevision == child.RequirementsRevision && v.AssignmentGeneration == child.AssignmentGeneration && v.Source == *req.Source && (req.Condition != work.DependencyIntegrated || v.Kind == "integrated") {
							found = true
						}
					}
					if !found {
						return "", 0, fmt.Errorf("dependency must cite the exact current child result or adopted evidence for the declared condition")
					}
				}
				dep.SatisfiedBy = req.Source
				if err = tx.SatisfyDependency(dep); err != nil {
					return "", 0, err
				}
			} else {
				if req.Source != nil {
					return "", 0, fmt.Errorf("new dependency cannot start already satisfied")
				}
				if err = tx.AddDependency(dep); err != nil {
					return "", 0, err
				}
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
func (s *Service) TaskDependencyAdd(ctx context.Context, t, r string, q TaskDependencyRequest) (Result, error) {
	return s.taskDependency(ctx, t, r, q, false)
}
func (s *Service) TaskDependencySatisfy(ctx context.Context, t, r string, q TaskDependencyRequest) (Result, error) {
	return s.taskDependency(ctx, t, r, q, true)
}
