package workservice

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type AssignTaskRequest struct {
	Key            string                    `json:"key"`
	Title          string                    `json:"title"`
	OwnerRoleID    string                    `json:"owner_role_id"`
	ParentTaskID   string                    `json:"parent_task_id,omitempty"`
	ParentRevision int64                     `json:"parent_revision,omitempty"`
	Condition      work.DependencyCondition  `json:"condition,omitempty"`
	Requirements   []work.TaskRequirementRef `json:"requirements"`
	BusinessSource *work.SourceRef           `json:"business_source,omitempty"`
}

func (s *Service) AssignTask(ctx context.Context, token, runRef string, req AssignTaskRequest) (Result, error) {
	var result Result
	err := s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		id := stableID("task", c.Worker.ScopeID, c.Worker.RoleID, req.Key)
		var err error
		result, err = s.mutate(tx, c, "task.assign", req.Key, id, req, func() (string, int64, error) {
			if strings.TrimSpace(req.Title) == "" {
				return "", 0, fmt.Errorf("task title is required")
			}
			if err := ensureMember(tx, c.Worker.ScopeID, req.OwnerRoleID); err != nil {
				return "", 0, err
			}
			task := work.Task{ID: id, ScopeID: c.Worker.ScopeID, Title: req.Title, OwnerRoleID: req.OwnerRoleID, RequirementsRevision: 1, AssignmentGeneration: 1, State: work.TaskTodo, Requirements: req.Requirements, BusinessSource: req.BusinessSource, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
			input := work.Inbox{ID: stableID("inbox", id), ScopeID: task.ScopeID, TaskID: task.ID, RecipientRoleID: task.OwnerRoleID, EventID: "task:" + id, State: work.InboxPending, AvailableAt: s.now(), Revision: 1}
			if req.ParentTaskID != "" {
				parent, err := tx.GetTask(req.ParentTaskID)
				if err != nil {
					return "", 0, err
				}
				if err = mayManageTask(tx, c, parent); err != nil {
					return "", 0, err
				}
				if parent.Revision != req.ParentRevision {
					return "", 0, workstore.ErrConflict
				}
				dep := work.Dependency{TaskID: parent.ID, DependsOnTaskID: id, Condition: req.Condition}
				if err = tx.Delegate(task, input, dep); err != nil {
					return "", 0, err
				}
				if err = tx.InheritTaskConversation(task.ScopeID, parent.ID, task.ID); err != nil && !errors.Is(err, workstore.ErrNotFound) {
					return "", 0, err
				}
			} else {
				if req.ParentRevision != 0 || req.Condition != "" {
					return "", 0, fmt.Errorf("dependency requires parent task")
				}
				if err := tx.PutTask(task, 0); err != nil {
					return "", 0, err
				}
				if err := tx.PutInbox(input, 0); err != nil {
					return "", 0, err
				}
			}
			return id, 1, nil
		})
		return err
	})
	return result, err
}

type ReviseTaskRequest struct {
	Key              string                    `json:"key"`
	TaskID           string                    `json:"task_id"`
	ExpectedRevision int64                     `json:"expected_revision"`
	Title            string                    `json:"title"`
	OwnerRoleID      string                    `json:"owner_role_id"`
	Requirements     []work.TaskRequirementRef `json:"requirements"`
	BusinessSource   *work.SourceRef           `json:"business_source,omitempty"`
}

func (s *Service) ReviseTask(ctx context.Context, token, runRef string, req ReviseTaskRequest) (Result, error) {
	var result Result
	err := s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		var err error
		result, err = s.mutate(tx, c, "task.revise", req.Key, req.TaskID, req, func() (string, int64, error) {
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
				return "", 0, fmt.Errorf("terminal task cannot be revised; create a follow-up task")
			}
			if err = ensureMember(tx, task.ScopeID, req.OwnerRoleID); err != nil {
				return "", 0, err
			}
			if task.OwnerRoleID != req.OwnerRoleID {
				task.AssignmentGeneration++
			}
			if task.Title != req.Title || !reflect.DeepEqual(task.Requirements, req.Requirements) || !reflect.DeepEqual(task.BusinessSource, req.BusinessSource) {
				task.RequirementsRevision++
			}
			task.Title, task.OwnerRoleID, task.Requirements, task.BusinessSource = req.Title, req.OwnerRoleID, req.Requirements, req.BusinessSource
			task.Revision++
			task.UpdatedAt = s.now()
			if err = tx.PutTask(task, req.ExpectedRevision); err != nil {
				return "", 0, err
			}
			// The running attempt retains its frozen assignment. This durable event
			// is admitted only after its carrier is known to be quiescent.
			input := work.Inbox{ID: stableID("inbox", task.ID, fmt.Sprint(task.Revision)), ScopeID: task.ScopeID, TaskID: task.ID, RecipientRoleID: task.OwnerRoleID, EventID: fmt.Sprintf("task:%s:revision:%d", task.ID, task.Revision), State: work.InboxPending, AvailableAt: s.now(), Revision: 1}
			if err = tx.PutInbox(input, 0); err != nil {
				return "", 0, err
			}
			return task.ID, task.Revision, nil
		})
		return err
	})
	return result, err
}
