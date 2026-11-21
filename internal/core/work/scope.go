// Package work defines durable business facts and their pure invariants. It does
// not own persistence, native runtimes, or external service calls.
package work

import (
	"fmt"
	"strings"
	"time"
)

func required(fields ...string) error {
	for _, field := range fields {
		if strings.TrimSpace(field) == "" {
			return fmt.Errorf("required identifier or value is empty")
		}
	}
	return nil
}
func oneOf[T ~string](value T, allowed ...T) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
func positive(name string, value int64) error {
	if value < 1 {
		return fmt.Errorf("%s must be positive", name)
	}
	return nil
}

// IdentityScope preserves the application and ID namespace. An external ID
// alone is never sufficient evidence that two identities refer to one person.
type IdentityScope struct {
	Tenant            string `json:"tenant"`
	AppIdentityDomain string `json:"app_identity_domain"`
	IDType            string `json:"id_type"`
	ExternalID        string `json:"external_id"`
	Generation        int64  `json:"generation"`
}

func (v IdentityScope) Validate() error {
	if err := required(v.Tenant, v.AppIdentityDomain, v.IDType, v.ExternalID); err != nil {
		return err
	}
	return positive("identity generation", v.Generation)
}

type ExternalIdentity struct {
	PrincipalID string `json:"principal_id"`
	IdentityScope
}

func (v ExternalIdentity) Validate() error {
	if err := required(v.PrincipalID); err != nil {
		return err
	}
	return v.IdentityScope.Validate()
}

type Role struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Description        string    `json:"description"`
	DescriptionVersion int64     `json:"description_version"`
	SettingsRevision   int64     `json:"settings_revision"`
	Revision           int64     `json:"revision"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func (v Role) Validate() error {
	if err := required(v.ID, v.Name, v.Description); err != nil {
		return err
	}
	for _, n := range []int64{v.DescriptionVersion, v.SettingsRevision, v.Revision} {
		if err := positive("role revision", n); err != nil {
			return err
		}
	}
	return nil
}

type RoleBotBinding struct {
	RoleID             string `json:"role_id"`
	GatewayRef         string `json:"gateway_ref"`
	AppID              string `json:"app_id"`
	IdentityGeneration int64  `json:"identity_generation"`
}

func (v RoleBotBinding) Validate() error {
	if err := required(v.RoleID, v.GatewayRef, v.AppID); err != nil {
		return err
	}
	return positive("identity generation", v.IdentityGeneration)
}

type ScopeKind string

const (
	ScopePersonal  ScopeKind = "personal"
	ScopeBootstrap ScopeKind = "bootstrap"
	ScopeProject   ScopeKind = "project"
	ScopeLibrary   ScopeKind = "library"
)

type WorkScope struct {
	Tenant               string    `json:"tenant,omitempty"`
	ChatID               string    `json:"chat_id,omitempty"`
	BootstrapOperationID string    `json:"bootstrap_operation_id,omitempty"`
	BindingGeneration    int64     `json:"binding_generation"`
	RoleID               string    `json:"role_id,omitempty"`
	ID                   string    `json:"id"`
	Kind                 ScopeKind `json:"kind"`
	PrincipalID          string    `json:"principal_id,omitempty"`
	TeamID               string    `json:"team_id,omitempty"`
	ProjectID            string    `json:"project_id,omitempty"`
	Revision             int64     `json:"revision"`
	CreatedAt            time.Time `json:"created_at"`
}

func (v WorkScope) Validate() error {
	if err := required(v.ID); err != nil {
		return err
	}
	if err := positive("scope revision", v.Revision); err != nil {
		return err
	}
	switch v.Kind {
	case ScopeLibrary:
		if v.PrincipalID != "" || v.RoleID != "" || v.TeamID != "" || v.ProjectID != "" {
			return fmt.Errorf("shipped library cannot claim user or team ownership")
		}
	case ScopeBootstrap:
		if err := required(v.PrincipalID, v.RoleID, v.Tenant, v.ChatID, v.BootstrapOperationID); err != nil {
			return err
		}
		if v.TeamID != "" || v.ProjectID != "" || v.BindingGeneration < 0 {
			return fmt.Errorf("bootstrap scope ownership is invalid")
		}
	case ScopePersonal:
		if err := required(v.PrincipalID, v.RoleID); err != nil {
			return err
		}
		if v.ProjectID != "" || v.TeamID != "" {
			return fmt.Errorf("personal scope cannot carry project or team ownership")
		}
	case ScopeProject:
		if err := required(v.TeamID, v.ProjectID); err != nil {
			return err
		}
		if v.PrincipalID != "" || v.RoleID != "" {
			return fmt.Errorf("project scope cannot carry personal ownership")
		}
	default:
		return fmt.Errorf("invalid scope kind %q", v.Kind)
	}
	if v.Kind != ScopeBootstrap && (v.Tenant != "" || v.ChatID != "" || v.BootstrapOperationID != "" || v.BindingGeneration != 0) {
		return fmt.Errorf("non-bootstrap scope cannot carry bootstrap identity")
	}
	return nil
}

type ProjectState string

const (
	ProjectProvisioning ProjectState = "provisioning"
	ProjectActive       ProjectState = "active"
	ProjectDegraded     ProjectState = "degraded"
	ProjectPausing      ProjectState = "pausing"
	ProjectPaused       ProjectState = "paused"
	ProjectArchiving    ProjectState = "archiving"
	ProjectArchived     ProjectState = "archived"
)

type Project struct {
	Name         string       `json:"name"`
	ID           string       `json:"id"`
	ScopeID      string       `json:"scope_id"`
	TeamID       string       `json:"team_id"`
	LeaderRoleID string       `json:"leader_role_id"`
	State        ProjectState `json:"state"`
	Revision     int64        `json:"revision"`
}

func (v Project) Validate() error {
	if err := required(v.ID, v.Name, v.ScopeID, v.TeamID, v.LeaderRoleID); err != nil {
		return err
	}
	if err := ValidateProjectTransition(v.State, v.State); err != nil {
		return err
	}
	return positive("project revision", v.Revision)
}

type ProjectChannelBinding struct {
	ProjectID         string `json:"project_id"`
	Tenant            string `json:"tenant"`
	ChatID            string `json:"chat_id"`
	Purpose           string `json:"purpose"`
	BindingGeneration int64  `json:"binding_generation"`
	Active            bool   `json:"active"`
}

func (v ProjectChannelBinding) Validate() error {
	if err := required(v.ProjectID, v.Tenant, v.ChatID, v.Purpose); err != nil {
		return err
	}
	return positive("channel binding generation", v.BindingGeneration)
}
