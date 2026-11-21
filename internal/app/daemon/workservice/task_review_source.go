package workservice

import (
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

// A model may interpret natural-language feedback, but it cannot move a human's
// approval from the actually replied-to frozen review to another submission.
// Unanchored feedback remains input; ask a precise review before counting it as
// human acceptance. Role reviews never enter this principal-only boundary.
func requireHumanSubmissionReview(tx *workstore.Tx, message work.Message, task work.Task, submissionID string, review *work.DecisionRequest) error {
	reject := func() error {
		return &workcmd.Error{Code: "blocked", Message: "human acceptance requires a reply to the exact published review decision for this submission; preserve this feedback and show the precise current review instead of applying it to another version"}
	}
	if message.Source.Kind != work.ActorPrincipal || message.PlatformKey == nil {
		return reject()
	}
	key := message.PlatformKey
	ingress, err := tx.GetIngressByPlatformMessage(key.Tenant, key.ChatID, key.MessageID)
	if err != nil {
		if err == workstore.ErrNotFound {
			return reject()
		}
		return err
	}
	if ingress.MessageID != message.ID || (ingress.Route.ScopeID != task.ScopeID && ingress.Route.ProjectViewScopeID != task.ScopeID) || ingress.Envelope.SenderType != "user" || ingress.Envelope.ParentID == "" {
		return reject()
	}
	mapping, err := tx.GetPlatformMessageMapping(key.Tenant, key.ChatID, ingress.Envelope.ParentID)
	if err != nil {
		if err == workstore.ErrNotFound {
			return reject()
		}
		return err
	}
	if mapping.ScopeID != task.ScopeID || mapping.TaskID != task.ID || mapping.OperationID == "" {
		return reject()
	}
	operation, err := tx.GetOperation(mapping.OperationID)
	if err != nil {
		return err
	}
	if operation.State != work.OperationConfirmed {
		return reject()
	}
	ref := operation.RequestRef
	for _, step := range operation.Steps {
		if step.ID == "send_projection" {
			ref = step.Receipt
			break
		}
	}
	body, err := tx.GetContent(task.ScopeID, ref)
	if err != nil {
		return err
	}
	var projection MessageProjection
	if err = json.Unmarshal(body.Data, &projection); err != nil {
		return err
	}
	if projection.UIViewID == "" {
		return reject()
	}
	view, err := tx.GetUIView(projection.UIViewID)
	if err != nil {
		return err
	}
	if view.Kind != "business.decision" || view.Invalidated || view.ScopeID != task.ScopeID || view.ExternalMessageID != ingress.Envelope.ParentID || view.OperationID != operation.ID || view.Tenant != key.Tenant || view.ChatID != key.ChatID {
		return reject()
	}
	decision, err := tx.GetDecisionRequest(task.ScopeID, view.TargetID)
	if err != nil {
		return err
	}
	p := decision.Presentation
	if decision.Kind != work.DecisionBusiness || decision.State != work.DecisionOpen || decision.TaskID != task.ID || decision.Revision != view.TargetRevision || p == nil || p.SubmissionID != submissionID || p.TaskRequirementsRevision != task.RequirementsRevision || p.TaskAssignmentGeneration != task.AssignmentGeneration {
		return reject()
	}
	allowed := false
	for _, actor := range decision.AllowedActors {
		allowed = allowed || actor == message.Source
	}
	if !allowed {
		return reject()
	}
	// Verify the version-bound submission again; a renamed requirement or reassigned
	// task cannot reuse the old review even when a model repeats its business ID.
	if _, err = readReviewSubmission(tx, task, submissionID); err != nil {
		return fmt.Errorf("review source no longer applies: %w", err)
	}
	*review = decision
	return nil
}
