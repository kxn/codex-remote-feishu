package workingress

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// The business input stays queued. The linked copy grants only the leader's
// restricted control context a chance to understand a real human while paused.
func (i *Ingress) controlCopy(tx *workstore.Tx, scope work.WorkScope, original work.Inbox) error {
	if original.AwaitingText {
		return nil
	}
	if scope.Kind != work.ScopeProject {
		return nil
	}
	project, err := tx.GetProject(scope.ProjectID)
	if err != nil {
		return err
	}
	blocked := project.State == work.ProjectPaused || project.State == work.ProjectPausing
	if !blocked {
		var e error
		blocked, e = controlInputBlocked(tx, scope, original)
		if e != nil {
			return e
		}
	}
	if !blocked {
		return nil
	}
	context := work.WorkContext{ID: opaque("project-control-context", project.ID, project.LeaderRoleID), ScopeID: scope.ID, RoleID: project.LeaderRoleID, Purpose: work.PurposeControl, Workstream: opaque("project-control-stream", project.ID, project.LeaderRoleID), Revision: 1, CreatedAt: i.config.Now(), UpdatedAt: i.config.Now()}
	if err = tx.ValidateControlContext(context); err == workstore.ErrScopeDenied {
		return nil
	} else if err != nil {
		return err
	}
	if _, err = tx.GetContext(context.ID); err == workstore.ErrNotFound {
		if err = tx.PutContext(context, 0); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	input := work.Inbox{ID: opaque("project-control-input", original.ID), ScopeID: scope.ID, RecipientRoleID: project.LeaderRoleID, RecipientContextID: context.ID, ControlOfInputID: original.ID, RelatedTaskID: original.TaskID, MessageID: original.MessageID, EventID: opaque("control-event", original.ID), State: work.InboxPending, ContentState: original.ContentState, AvailableAt: original.AvailableAt, Revision: 1}
	return tx.PutInbox(input, 0)
}

func controlInputBlocked(tx *workstore.Tx, scope work.WorkScope, input work.Inbox) (bool, error) {
	gates, err := tx.ListPauseGates(scope.ID)
	if err != nil {
		return false, err
	}
	allowed, err := work.GatesAllowRunForContext(scope.ID, input.TaskID, input.RecipientContextID, gates)
	if err != nil {
		return false, err
	}
	if !allowed {
		return true, nil
	}
	workers, err := tx.ListWorkers(scope.ID)
	if err != nil {
		return false, err
	}
	for _, worker := range workers {
		if worker.RoleID != input.RecipientRoleID || worker.State == work.WorkerRetired || (input.RecipientContextID != "" && worker.ContextID != input.RecipientContextID) {
			continue
		}
		context, err := tx.GetContext(worker.ContextID)
		if err != nil {
			return false, err
		}
		if context.Purpose == work.PurposeControl {
			continue
		}
		if _, err = tx.GetRuntimeGate(worker.ID); err == nil {
			return true, nil
		} else if err != workstore.ErrNotFound {
			return false, err
		}
		if worker.Controller != work.ControllerAutomatic || worker.State == work.WorkerHandoff || worker.State == work.WorkerRecovering || worker.State == work.WorkerUnavailable {
			return true, nil
		}
		allowed, err := work.GatesAllowRunForContext(scope.ID, input.TaskID, context.ID, gates)
		if err != nil {
			return false, err
		}
		if !allowed {
			return true, nil
		}
	}
	return false, nil
}
