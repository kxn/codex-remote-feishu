package work

import (
	"fmt"
	"time"
)

type AutomationKind string

const (
	AutomationWhip     AutomationKind = "autowhip"
	AutomationContinue AutomationKind = "autocontinue"
)

type AutomationPolicy struct {
	ContextID string         `json:"context_id"`
	ScopeID   string         `json:"scope_id"`
	Kind      AutomationKind `json:"kind"`
	Enabled   bool           `json:"enabled"`
	Source    SourceRef      `json:"source"`
	Revision  int64          `json:"revision"`
	UpdatedAt time.Time      `json:"updated_at"`
}

func (v AutomationPolicy) Validate() error {
	if err := required(v.ContextID, v.ScopeID); err != nil {
		return err
	}
	if !oneOf(v.Kind, AutomationWhip, AutomationContinue) || v.Revision < 1 || v.UpdatedAt.IsZero() {
		return fmt.Errorf("invalid automation policy")
	}
	return v.Source.Validate()
}

type AutomationEpisode struct {
	ID               string         `json:"id"`
	ScopeID          string         `json:"scope_id"`
	ContextID        string         `json:"context_id"`
	Kind             AutomationKind `json:"kind"`
	PolicyRevision   int64          `json:"policy_revision"`
	TriggerAttemptID string         `json:"trigger_attempt_id"`
	RootAttemptID    string         `json:"root_attempt_id"`
	Count            int            `json:"count"`
	InputID          string         `json:"input_id"`
	DueAt            time.Time      `json:"due_at"`
	Source           SourceRef      `json:"source"`
}
