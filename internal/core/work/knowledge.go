package work

import (
	"fmt"
	"time"
)

type MemoryScopeKind string

const (
	MemoryRole        MemoryScopeKind = "role"
	MemoryTeam        MemoryScopeKind = "team"
	MemoryProject     MemoryScopeKind = "project"
	MemoryRoleProject MemoryScopeKind = "role_project"
	MemoryPersonal    MemoryScopeKind = "personal"
	MemoryTask        MemoryScopeKind = "task"
	MemoryContext     MemoryScopeKind = "context"
)

type MemoryScope struct {
	Kind        MemoryScopeKind `json:"kind"`
	RoleID      string          `json:"role_id,omitempty"`
	TeamID      string          `json:"team_id,omitempty"`
	ProjectID   string          `json:"project_id,omitempty"`
	PrincipalID string          `json:"principal_id,omitempty"`
	TaskID      string          `json:"task_id,omitempty"`
	ContextID   string          `json:"context_id,omitempty"`
}

func (v MemoryScope) Validate() error {
	var want []string
	switch v.Kind {
	case MemoryRole:
		want = []string{v.RoleID}
	case MemoryTeam:
		want = []string{v.TeamID}
	case MemoryProject:
		want = []string{v.ProjectID}
	case MemoryRoleProject:
		want = []string{v.RoleID, v.ProjectID}
	case MemoryPersonal:
		want = []string{v.PrincipalID, v.RoleID}
	case MemoryTask:
		want = []string{v.TaskID}
	case MemoryContext:
		want = []string{v.ContextID}
	default:
		return fmt.Errorf("invalid memory scope")
	}
	if err := required(want...); err != nil {
		return err
	}
	count := 0
	for _, id := range []string{v.RoleID, v.TeamID, v.ProjectID, v.PrincipalID, v.TaskID, v.ContextID} {
		if id != "" {
			count++
		}
	}
	if count != len(want) {
		return fmt.Errorf("memory scope contains unrelated ownership")
	}
	return nil
}

type MemoryState string

const (
	MemoryValid       MemoryState = "valid"
	MemoryProposed    MemoryState = "proposed"
	MemoryInvalidated MemoryState = "invalidated"
	MemoryRevoked     MemoryState = "revoked"
)

type MemoryEntry struct {
	SharingGrantID string      `json:"sharing_grant_id,omitempty"`
	StorageScopeID string      `json:"storage_scope_id"`
	ID             string      `json:"id"`
	Scope          MemoryScope `json:"scope"`
	Author         Actor       `json:"author"`
	Sources        []SourceRef `json:"sources"`
	ContentRef     string      `json:"content_ref"`
	Applicability  string      `json:"applicability"`
	State          MemoryState `json:"state"`
	Revision       int64       `json:"revision"`
	UpdatedAt      time.Time   `json:"updated_at"`
}

func (v MemoryEntry) Validate() error {
	if err := required(v.ID, v.StorageScopeID, v.ContentRef, v.Applicability); err != nil {
		return err
	}
	if err := v.Scope.Validate(); err != nil {
		return err
	}
	if err := v.Author.Validate(); err != nil {
		return err
	}
	if len(v.Sources) == 0 {
		return fmt.Errorf("memory requires source provenance")
	}
	for _, ref := range v.Sources {
		if err := ref.Validate(); err != nil {
			return err
		}
	}
	if !oneOf(v.State, MemoryValid, MemoryProposed, MemoryInvalidated, MemoryRevoked) {
		return fmt.Errorf("invalid memory state")
	}
	return positive("memory revision", v.Revision)
}

type PracticeRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

func (v PracticeRef) Validate() error { return required(v.ID, v.Version) }

type PracticeDefinition struct {
	SharingGrantID string         `json:"sharing_grant_id,omitempty"`
	StorageScopeID string         `json:"storage_scope_id"`
	Owner          KnowledgeOwner `json:"owner"`
	Revision       int64          `json:"revision"`
	PracticeRef
	Purpose              string    `json:"purpose"`
	Applicability        string    `json:"applicability"`
	Exclusions           string    `json:"exclusions"`
	Inputs               []string  `json:"inputs"`
	Steps                []string  `json:"steps"`
	Outputs              []string  `json:"outputs"`
	Escalation           string    `json:"escalation"`
	RequiredCapabilities []string  `json:"required_capabilities"`
	ContentRef           string    `json:"content_ref"`
	Source               SourceRef `json:"source"`
}

func (v PracticeDefinition) Validate() error {
	if err := required(v.StorageScopeID); err != nil {
		return err
	}
	if err := v.Owner.Validate(); err != nil {
		return err
	}
	if err := positive("practice revision", v.Revision); err != nil {
		return err
	}
	if err := v.PracticeRef.Validate(); err != nil {
		return err
	}
	if err := required(v.Purpose, v.Applicability, v.Exclusions, v.Escalation, v.ContentRef); err != nil {
		return err
	}
	return v.Source.Validate()
}

type PracticeAdoption struct {
	Owner      KnowledgeOwner `json:"owner"`
	ID         string         `json:"id"`
	ScopeID    string         `json:"scope_id"`
	TaskID     string         `json:"task_id,omitempty"`
	Practice   PracticeRef    `json:"practice"`
	Exceptions []string       `json:"exceptions"`
	AdoptedBy  Actor          `json:"adopted_by"`
	Source     SourceRef      `json:"source"`
	Revision   int64          `json:"revision"`
	AdoptedAt  time.Time      `json:"adopted_at"`
}

func (v PracticeAdoption) Validate() error {
	if err := v.Owner.Validate(); err != nil {
		return err
	}
	if err := required(v.ID, v.ScopeID); err != nil {
		return err
	}
	if err := v.Practice.Validate(); err != nil {
		return err
	}
	if err := v.AdoptedBy.Validate(); err != nil {
		return err
	}
	if err := v.Source.Validate(); err != nil {
		return err
	}
	return positive("adoption revision", v.Revision)
}
