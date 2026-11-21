package workservice

import (
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Applying a business choice records the local conclusion and wakes its
// responsible role. It never claims an external deployment or a task is done.
func (s *Service) applyBusinessDecision(tx *workstore.Tx, request work.DecisionRequest, decision work.Decision, op work.Operation, messageID string) error {
	if request.Kind != work.DecisionBusiness {
		return fmt.Errorf("native requests require their exact native response path")
	}
	if request.AgreementProposalRef != "" {
		if err := s.applyAgreementConflictDecision(tx, request, decision); err != nil {
			return err
		}
	}
	var task work.Task
	if request.TaskID != "" {
		var err error
		task, err = tx.GetTask(request.TaskID)
		if err != nil {
			return err
		}
		if request.Presentation != nil && (task.RequirementsRevision != request.Presentation.TaskRequirementsRevision || task.AssignmentGeneration != request.Presentation.TaskAssignmentGeneration) {
			return workstore.ErrConflict
		}
	}
	if request.Presentation != nil && request.Presentation.SubmissionID != "" && decision.Choice == "approve" {
		original, err := readReviewSubmission(tx, task, request.Presentation.SubmissionID)
		if err != nil {
			return err
		}
		if err = tx.PutEvidence(work.RequirementEvidence{ID: stableID("decision-evidence", decision.ID), ScopeID: task.ScopeID, TaskID: task.ID, RequirementsRevision: task.RequirementsRevision, AssignmentGeneration: task.AssignmentGeneration, RequirementID: original.RequirementID, RequirementVersion: original.RequirementVersion, Kind: original.Kind, Source: decision.Source, AcceptedBy: decision.Actor}); err != nil {
			return err
		}
	}
	recipient := ""
	scope, err := tx.GetScope(request.ScopeID)
	if err != nil {
		return err
	}
	if task.ID != "" {
		recipient = task.OwnerRoleID
	} else if scope.Kind == work.ScopeProject {
		project, err := tx.GetProject(scope.ProjectID)
		if err != nil {
			return err
		}
		recipient = project.LeaderRoleID
	} else {
		recipient = scope.RoleID
	}
	if messageID == "" {
		title := "工作决定"
		if request.Presentation != nil {
			title = request.Presentation.Title
		}
		body := work.NewContent(stableID("decision-notice-body", decision.ID), request.ScopeID, "text/plain", []byte(title+"："+decision.Choice), s.now())
		if err = tx.PutContent(body); err != nil {
			return err
		}
		messageID = stableID("decision-notice-message", decision.ID)
		if _, _, err = tx.AcceptMessage(work.Message{ID: messageID, ScopeID: request.ScopeID, ConversationID: stableID("decision-conversation", request.ID), Source: decision.Actor, BodyRef: body.ID, DerivedFrom: &decision.Source, Revision: 1, CreatedAt: s.now()}, nil); err != nil {
			return err
		}
	}
	taskID := task.ID
	if task.State == work.TaskDone || task.State == work.TaskCancelled {
		taskID = ""
	}
	recipientContext := ""
	if taskID == "" && scope.Kind == work.ScopePersonal && request.AttemptID != "" {
		origin, err := tx.GetAttempt(request.AttemptID)
		if err != nil {
			return err
		}
		recipientContext = origin.ContextID
	}
	if err = tx.PutInbox(work.Inbox{ID: stableID("decision-followup", decision.ID), ScopeID: request.ScopeID, TaskID: taskID, RecipientContextID: recipientContext, RecipientRoleID: recipient, EventID: "decision:" + decision.ID, MessageID: messageID, State: work.InboxPending, ContentState: work.InputContentReady, AvailableAt: s.now(), Revision: 1}, 0); err != nil {
		return err
	}
	op.State = work.OperationSending
	op.Revision++
	if _, _, err = tx.PutOperation(op, 1); err != nil {
		return err
	}
	receipt, _ := json.Marshal(map[string]any{"decision_id": decision.ID, "choice": decision.Choice, "local_conclusion_recorded": true})
	op.State = work.OperationConfirmed
	op.Revision++
	op.ExternalReceipt = string(receipt)
	if _, _, err = tx.PutOperation(op, 2); err != nil {
		return err
	}
	current, err := tx.GetDecisionRequest(request.ScopeID, request.ID)
	if err != nil {
		return err
	}
	expected := current.Revision
	current.State = work.DecisionApplying
	current.Revision++
	if err = tx.PutDecisionRequest(current, expected); err != nil {
		return err
	}
	expected = current.Revision
	current.State = work.DecisionResolved
	current.Revision++
	return tx.PutDecisionRequest(current, expected)
}
