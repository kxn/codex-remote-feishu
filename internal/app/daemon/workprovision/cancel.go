package workprovision

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type CancelProvisioningRequest struct {
	OperationID      string
	ExpectedRevision int64
	SourceMessageID  string
}

// CancelProvisioningWithHumanProof stops future saga effects; it never cancels
// an already transmitted call or erases an uncertain effect. There is no Agent
// command or caller-controlled confirmation boolean for this trusted ingress.
func (s *Service) CancelProvisioningWithHumanProof(ctx context.Context, principalID string, req CancelProvisioningRequest) (Report, error) {
	var op work.Operation
	var project work.Project
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		op, err = tx.GetOperation(req.OperationID)
		if err != nil {
			return err
		}
		if op.Kind != "project.provision" && op.Kind != "project.member.provision" {
			return fmt.Errorf("operation is not provisioning")
		}
		if op.Revision != req.ExpectedRevision {
			return workstore.ErrConflict
		}
		project, err = tx.GetProject(op.TargetID)
		if err != nil {
			return err
		}
		if project.ScopeID != op.ScopeID {
			return workstore.ErrScopeDenied
		}
		body, err := tx.GetContent(op.ScopeID, op.RequestRef)
		if err != nil {
			return err
		}
		var request struct {
			Tenant          string `json:"tenant"`
			ChatID          string `json:"chat_id"`
			SourceMessageID string `json:"source_message_id"`
		}
		if err = json.Unmarshal(body.Data, &request); err != nil {
			return err
		}
		original, err := tx.GetMessage(request.SourceMessageID)
		if err != nil {
			return err
		}
		message, err := tx.GetMessage(req.SourceMessageID)
		if err != nil {
			return err
		}
		actor := work.Actor{Kind: work.ActorPrincipal, ID: principalID}
		if principalID == "" || message.Source != actor || original.Source != actor || message.ID == original.ID || message.DerivedFrom != nil {
			return workstore.ErrScopeDenied
		}
		if message.PlatformKey == nil || message.PlatformKey.Tenant != request.Tenant || message.PlatformKey.ChatID != request.ChatID || message.CreatedAt.Before(op.UpdatedAt) {
			return workstore.ErrScopeDenied
		}
		if op.State == work.OperationConfirmed {
			return fmt.Errorf("completed establishment cannot be canceled; archive the project explicitly")
		}
		for _, step := range op.Steps {
			if step.ID == "cancellation_requested" {
				return nil
			}
		}
		proof, err := json.Marshal(map[string]any{"principal_id": principalID, "source_message_id": message.ID, "source_revision": message.Revision, "source_body_ref": message.BodyRef})
		if err != nil {
			return err
		}
		evidence := work.NewContent(stableID("cancel_proof", op.ID, message.ID), op.ScopeID, "application/json", proof, s.now())
		if err = tx.PutContent(evidence); err != nil {
			return err
		}
		op.Steps = append(op.Steps, work.OperationStep{ID: "cancellation_requested", State: work.OperationConfirmed, Receipt: evidence.ID})
		old := op.Revision
		op.Revision++
		op.UpdatedAt = s.now()
		op.LastError = "human cancellation requested; known resources retained and uncertain calls remain unresolved"
		_, _, err = tx.PutOperation(op, old)
		return err
	})
	if err != nil {
		return Report{OperationID: req.OperationID}, err
	}
	return s.settleCancellation(ctx, op, project.ID)
}
func cancellationRequested(op work.Operation) bool {
	for _, step := range op.Steps {
		if step.ID == "cancellation_requested" {
			return true
		}
	}
	return false
}

func (s *Service) settleCancellation(ctx context.Context, operation work.Operation, projectID string) (Report, error) {
	report := Report{OperationID: operation.ID, ProjectID: projectID, Blockers: []string{}}
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(operation.ID)
		if err != nil {
			return err
		}
		if op.State == work.OperationConfirmed || op.State == work.OperationTerminal {
			report.State = string(op.State)
			report.Steps, err = tx.ListProvisionSteps(op.ID)
			return err
		}
		newCancellation := !cancellationRequested(op)
		if newCancellation {
			project, err := tx.GetProject(projectID)
			if err != nil {
				return err
			}
			if project.State != work.ProjectArchiving || project.ScopeID != op.ScopeID {
				return fmt.Errorf("provisioning has no proven cancellation request")
			}
			op.Steps = append(op.Steps, work.OperationStep{ID: "cancellation_requested", State: work.OperationConfirmed, Receipt: "project-archiving:" + projectID})
		}
		children, err := tx.ListProvisionSteps(op.ID)
		if err != nil {
			return err
		}
		for _, step := range children {
			if step.State == work.OperationUnknown || step.State == work.OperationSending {
				report.Blockers = append(report.Blockers, step.StepID+": uncertain external effect retained; verify the exact receipt before final cancellation")
			}
			if step.State == work.OperationPrepared || step.State == work.OperationRetryable {
				old := step.Revision
				step.Revision++
				step.State = work.OperationTerminal
				step.Blocker = "canceled before another effect invocation"
				step.UpdatedAt = s.now()
				if err = tx.PutProvisionStep(step, old); err != nil {
					return err
				}
			}
		}
		for i := range op.Steps {
			if op.Steps[i].ID == "cancellation_requested" {
				continue
			}
			if op.Steps[i].State == work.OperationPrepared || op.Steps[i].State == work.OperationRetryable {
				op.Steps[i].State = work.OperationTerminal
				op.Steps[i].Receipt = "canceled before remaining stage invocation"
			}
		}
		if len(report.Blockers) == 0 {
			for i := range op.Steps {
				if op.Steps[i].State != work.OperationConfirmed {
					op.Steps[i].State = work.OperationTerminal
					op.Steps[i].Receipt = "canceled; actual completed child receipts retained"
				}
			}
		}
		if op.State == work.OperationUnknown && !newCancellation && len(report.Blockers) > 0 && op.LastError == fmt.Sprint(report.Blockers) {
			report.State = string(op.State)
			report.Steps = children
			return nil
		}
		if op.State != work.OperationTerminal {
			old := op.Revision
			op.Revision++
			op.UpdatedAt = s.now()
			if len(report.Blockers) == 0 {
				op.State = work.OperationTerminal
				op.LastError = "human canceled provisioning; actual created resources retained"
			} else {
				if op.State == work.OperationPrepared {
					op.State = work.OperationSending
					if _, _, err = tx.PutOperation(op, old); err != nil {
						return err
					}
					old = op.Revision
					op.Revision++
				}
				op.State = work.OperationUnknown
				op.LastError = fmt.Sprint(report.Blockers)
			}
			if _, _, err = tx.PutOperation(op, old); err != nil {
				return err
			}
		}
		report.State = string(op.State)
		report.Steps, err = tx.ListProvisionSteps(op.ID)
		return err
	})
	return report, err
}

// SettleArchivedEstablishment stops unfinished provisioning authorized by the
// project archive transition. Recorded unknown external effects still require
// exact reconciliation before archiving can settle.
func (s *Service) SettleArchivedEstablishment(ctx context.Context, project work.Project) error {
	var operations []work.Operation
	if err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		operations, err = tx.ListOperations(project.ScopeID)
		return err
	}); err != nil {
		return err
	}
	for _, operation := range operations {
		if (operation.Kind == "project.provision" || operation.Kind == "project.member.provision") &&
			operation.State != work.OperationConfirmed && operation.State != work.OperationTerminal {
			if _, err := s.settleCancellation(ctx, operation, project.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
