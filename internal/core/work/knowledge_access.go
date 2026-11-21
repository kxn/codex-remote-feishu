package work

import "fmt"

// ReadScope is an explicit execution identity. The store checks it against the
// current scope, active team roster, and optional task/context ownership.
type ReadScope struct {
	ScopeID   string `json:"scope_id"`
	RoleID    string `json:"role_id"`
	ContextID string `json:"context_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
}

func (v ReadScope) Validate() error { return required(v.ScopeID, v.RoleID) }

type KnowledgeOwnerKind string

const (
	KnowledgeGlobal KnowledgeOwnerKind = "global"
	KnowledgeTeam   KnowledgeOwnerKind = "team"
	KnowledgeRole   KnowledgeOwnerKind = "role"
)

type KnowledgeOwner struct {
	Kind   KnowledgeOwnerKind `json:"kind"`
	TeamID string             `json:"team_id,omitempty"`
	RoleID string             `json:"role_id,omitempty"`
}

func (v KnowledgeOwner) Validate() error {
	switch v.Kind {
	case KnowledgeGlobal:
		if v.TeamID != "" || v.RoleID != "" {
			return fmt.Errorf("global knowledge cannot carry narrower ownership")
		}
	case KnowledgeTeam:
		if err := required(v.TeamID); err != nil {
			return err
		}
		if v.RoleID != "" {
			return fmt.Errorf("team knowledge cannot carry role ownership")
		}
	case KnowledgeRole:
		if err := required(v.RoleID); err != nil {
			return err
		}
		if v.TeamID != "" {
			return fmt.Errorf("role knowledge cannot carry team ownership")
		}
	default:
		return fmt.Errorf("invalid knowledge owner")
	}
	return nil
}
