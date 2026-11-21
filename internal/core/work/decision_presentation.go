package work

import (
	"fmt"
	"strings"
)

type DecisionChoice struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// DecisionPresentation freezes what a human is asked to decide. Updating its
// body, choices, requirements or artifact requires a new request generation.
type DecisionPresentation struct {
	Title                    string           `json:"title"`
	PromptRef                string           `json:"prompt_ref"`
	Choices                  []DecisionChoice `json:"choices"`
	References               []DeliveryRef    `json:"references,omitempty"`
	TaskRequirementsRevision int64            `json:"task_requirements_revision,omitempty"`
	TaskAssignmentGeneration int64            `json:"task_assignment_generation,omitempty"`
	RequirementID            string           `json:"requirement_id,omitempty"`
	SubmissionID             string           `json:"submission_id,omitempty"`
}

func (v DecisionPresentation) Validate() error {
	if strings.TrimSpace(v.Title) == "" || len([]rune(v.Title)) > 200 || v.PromptRef == "" || len(v.Choices) < 2 || len(v.Choices) > 12 {
		return fmt.Errorf("decision requires bounded title, saved body and2..12 explicit options")
	}
	seen := map[string]bool{}
	for _, choice := range v.Choices {
		if choice.Value == "" || len(choice.Value) > 128 || strings.TrimSpace(choice.Label) == "" || len([]rune(choice.Label)) > 100 || seen[choice.Value] {
			return fmt.Errorf("decision choices require unique bounded values and labels")
		}
		seen[choice.Value] = true
	}
	for _, ref := range v.References {
		if err := ref.Validate(); err != nil {
			return err
		}
		if ref.Current || ref.VersionID == "" {
			return fmt.Errorf("decisions require explicitly saved artifact versions")
		}
	}
	if v.SubmissionID != "" && (v.RequirementID == "" || v.TaskRequirementsRevision < 1 || v.TaskAssignmentGeneration < 1) {
		return fmt.Errorf("review requires exact requirement and assignment versions")
	}
	return nil
}
