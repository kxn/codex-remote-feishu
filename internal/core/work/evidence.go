package work

import "fmt"

// RequirementEvidence associates declared evidence with the precise task and
// adopted requirement versions. It records a responsible actor's conclusion;
// the domain never guesses semantic quality from a native turn finishing.
type RequirementEvidence struct {
	AssignmentGeneration int64     `json:"assignment_generation"`
	ID                   string    `json:"id"`
	ScopeID              string    `json:"scope_id"`
	TaskID               string    `json:"task_id"`
	RequirementsRevision int64     `json:"requirements_revision"`
	RequirementID        string    `json:"requirement_id"`
	RequirementVersion   string    `json:"requirement_version"`
	Kind                 string    `json:"kind"`
	Source               SourceRef `json:"source"`
	AcceptedBy           Actor     `json:"accepted_by"`
	Obsolete             bool      `json:"obsolete"`
}

func (v RequirementEvidence) Validate() error {
	if err := required(v.ID, v.ScopeID, v.TaskID, v.RequirementID, v.RequirementVersion, v.Kind); err != nil {
		return err
	}
	if err := positive("evidence requirements revision", v.RequirementsRevision); err != nil {
		return err
	}
	if err := v.Source.Validate(); err != nil {
		return err
	}
	if err := positive("evidence assignment generation", v.AssignmentGeneration); err != nil {
		return err
	}
	return v.AcceptedBy.Validate()
}

// ValidateTaskCompletion checks only explicitly adopted requirements. It does
// not impose human approval, review, or a fixed workflow on every task.
func ValidateTaskCompletion(task Task, evidence []RequirementEvidence) error {
	if err := task.Validate(); err != nil {
		return err
	}
	if task.State == TaskCancelled {
		return fmt.Errorf("cancelled task cannot complete")
	}
	declared := map[string]TaskRequirementRef{}
	for _, requirement := range task.Requirements {
		if _, ok := declared[requirement.ID]; ok {
			return fmt.Errorf("duplicate requirement")
		}
		declared[requirement.ID] = requirement
	}
	available := map[string]map[string]bool{}
	ids := map[string]bool{}
	for _, item := range evidence {
		if err := item.Validate(); err != nil {
			return err
		}
		if ids[item.ID] {
			return fmt.Errorf("duplicate evidence")
		}
		ids[item.ID] = true
		if item.ScopeID != task.ScopeID || item.TaskID != task.ID {
			return fmt.Errorf("evidence belongs to another task or scope")
		}
		if item.Obsolete || item.RequirementsRevision != task.RequirementsRevision || item.AssignmentGeneration != task.AssignmentGeneration {
			continue
		}
		requirement, ok := declared[item.RequirementID]
		if !ok {
			return fmt.Errorf("evidence references unknown requirement")
		}
		if item.Obsolete || item.RequirementsRevision != task.RequirementsRevision || item.AssignmentGeneration != task.AssignmentGeneration || item.RequirementVersion != requirement.Source.Version {
			continue
		}
		if actor, constrained := requirement.EvidenceActors[item.Kind]; constrained && actor != item.AcceptedBy.Kind {
			continue
		}
		if available[item.RequirementID] == nil {
			available[item.RequirementID] = map[string]bool{}
		}
		available[item.RequirementID][item.Kind] = true
	}
	for _, requirement := range task.Requirements {
		for _, kind := range requirement.RequiredEvidence {
			if !available[requirement.ID][kind] {
				return fmt.Errorf("requirement %q lacks current %q evidence", requirement.ID, kind)
			}
		}
	}
	return nil
}

// ValidateRevision is suitable for an optimistic conditional write. Callers
// still must enforce the expected value atomically in their store transaction.
func ValidateRevision(current, expected, next int64) error {
	if current < 1 || expected != current || next != current+1 {
		return fmt.Errorf("revision conflict: current=%d expected=%d next=%d", current, expected, next)
	}
	return nil
}
