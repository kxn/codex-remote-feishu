package workstore

import (
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (t *Tx) checkTaskParticipation(old, task work.Task) error {
	requests, err := listRecords[work.DecisionRequest](t, "decision_requests", " WHERE scope_id=? AND json_extract(payload,'$.task_id')=? AND json_extract(payload,'$.participation_gate_id') IS NOT NULL AND state='open'", task.ScopeID, task.ID)
	if err != nil {
		return err
	}
	for _, request := range requests {
		if task.State != work.TaskCancelled {
			if task.State == old.State || task.State == work.TaskWaiting || task.State == work.TaskTodo {
				continue
			}
			return fmt.Errorf("task is waiting for participation decision %s", request.ID)
		}
		request.State = work.DecisionAborted
		request.Revision++
		if err = t.PutDecisionRequest(request, request.Revision-1); err != nil {
			return err
		}
	}
	return nil
}

// Preserve the hold while replacing obsolete review requests in the same task
// transaction. New human instructions never require approving superseded work.
func (t *Tx) refreshTaskParticipation(task work.Task) error {
	requests, err := t.ListDecisionRequests(task.ScopeID)
	if err != nil {
		return err
	}
	for _, old := range requests {
		if old.TaskID != task.ID || old.ParticipationGateID == "" || old.State != work.DecisionOpen || old.Presentation == nil {
			continue
		}
		if old.Presentation.TaskRequirementsRevision == task.RequirementsRevision && old.Presentation.TaskAssignmentGeneration == task.AssignmentGeneration {
			continue
		}
		next := old
		next.ID = fmt.Sprintf("%s:task:%d:%d", old.ParticipationGateID, task.RequirementsRevision, task.AssignmentGeneration)
		next.Revision = 1
		next.RequestGeneration++
		next.CreatedAt = task.UpdatedAt
		next.ResponseOperationID, next.SupersededBy = "", ""
		presentation := *old.Presentation
		presentation.TaskRequirementsRevision, presentation.TaskAssignmentGeneration = task.RequirementsRevision, task.AssignmentGeneration
		presentation.RequirementID, presentation.SubmissionID = "", ""
		presentation.References = nil
		snapshot, err := json.MarshalIndent(task, "", "  ")
		if err != nil {
			return err
		}
		body := work.NewContent(next.ID+":prompt", task.ScopeID, "text/plain; charset=utf-8", []byte("任务要求或责任已更新，原决定已失效。请按以下当前任务重新决定。参与点："+presentation.Title+"\n"+string(snapshot)), task.UpdatedAt)
		if err = t.PutContent(body); err != nil {
			return err
		}
		presentation.PromptRef = body.ID
		next.Presentation = &presentation
		if err = t.PutDecisionRequest(next, 0); err != nil {
			return err
		}
		old.State, old.SupersededBy = work.DecisionSuperseded, next.ID
		old.Revision++
		if err = t.PutDecisionRequest(old, old.Revision-1); err != nil {
			return err
		}
		if next.Owner.Kind == work.ActorRole {
			if err = t.PutInbox(work.Inbox{ID: next.ID + ":inbox", ScopeID: task.ScopeID, TaskID: task.ID, RecipientRoleID: next.Owner.ID, EventID: "decision:" + next.ID, State: work.InboxPending, AvailableAt: task.UpdatedAt, Revision: 1}, 0); err != nil {
				return err
			}
		}
	}
	return nil
}
