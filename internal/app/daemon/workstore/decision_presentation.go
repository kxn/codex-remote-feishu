package workstore

import (
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (t *Tx) validateDecisionPresentation(request work.DecisionRequest, current bool) error {
	p := request.Presentation
	if p == nil {
		return nil
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if err := t.requireContent(request.ScopeID, p.PromptRef); err != nil {
		return err
	}
	for _, ref := range p.References {
		version, err := t.GetResourceVersion(request.ScopeID, ref.VersionID)
		if err != nil {
			return err
		}
		if version.ResourceID != ref.ResourceID {
			return ErrScopeDenied
		}
	}
	if p.TaskRequirementsRevision != 0 || p.TaskAssignmentGeneration != 0 || p.RequirementID != "" || p.SubmissionID != "" {
		if request.TaskID == "" {
			return fmt.Errorf("review presentation requires an explicit task")
		}
		task, err := t.GetTask(request.TaskID)
		if err != nil {
			return err
		}
		if task.ScopeID != request.ScopeID {
			return ErrScopeDenied
		}
		if current && (task.RequirementsRevision != p.TaskRequirementsRevision || task.AssignmentGeneration != p.TaskAssignmentGeneration) {
			return fmt.Errorf("decision review was prepared for obsolete requirements or assignment")
		}
		if current && p.RequirementID != "" {
			found := false
			for _, requirement := range task.Requirements {
				if requirement.ID == p.RequirementID {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("review requirement no longer exists")
			}
		}
	}
	if p.SubmissionID != "" {
		op, err := t.GetOperation(p.SubmissionID)
		if err != nil {
			return err
		}
		if op.ScopeID != request.ScopeID || op.TargetID != request.TaskID || op.Kind != "task.evidence.submit" || op.State != work.OperationConfirmed {
			return ErrScopeDenied
		}
		body, err := t.GetContent(request.ScopeID, op.RequestRef)
		if err != nil {
			return err
		}
		if op.RequestHash != body.SHA256 {
			return ErrIdempotencyConflict
		}
		var submitted struct {
			TaskID               string `json:"task_id"`
			RequirementID        string `json:"requirement_id"`
			RequirementsRevision int64  `json:"requirements_revision"`
			AssignmentGeneration int64  `json:"assignment_generation"`
		}
		if err = json.Unmarshal(body.Data, &submitted); err != nil {
			return err
		}
		if submitted.TaskID != request.TaskID || submitted.RequirementID != p.RequirementID || submitted.RequirementsRevision != p.TaskRequirementsRevision || submitted.AssignmentGeneration != p.TaskAssignmentGeneration {
			return ErrScopeDenied
		}
	}
	return nil
}
