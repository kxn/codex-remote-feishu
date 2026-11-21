package work

import (
	"fmt"
	"time"
)

type PrivateNavigationMode string

const (
	PrivatePersonal PrivateNavigationMode = "personal"
	PrivateProject  PrivateNavigationMode = "project"
)

type PrivateNavigation struct {
	PersonalScopeID  string                `json:"personal_scope_id"`
	PrincipalID      string                `json:"principal_id"`
	RoleID           string                `json:"role_id"`
	Mode             PrivateNavigationMode `json:"mode"`
	FrontContextID   string                `json:"front_context_id"`
	ProjectScopeID   string                `json:"project_scope_id,omitempty"`
	ProjectContextID string                `json:"project_context_id,omitempty"`
	Revision         int64                 `json:"revision"`
	Source           SourceRef             `json:"source"`
	UpdatedAt        time.Time             `json:"updated_at"`
}

func (v PrivateNavigation) Validate() error {
	if err := required(v.PersonalScopeID, v.PrincipalID, v.RoleID, v.FrontContextID); err != nil {
		return err
	}
	if err := v.Source.Validate(); err != nil {
		return err
	}
	switch v.Mode {
	case PrivatePersonal:
		if v.ProjectScopeID != "" || v.ProjectContextID != "" {
			return fmt.Errorf("personal view cannot carry a selected project")
		}
	case PrivateProject:
		if err := required(v.ProjectScopeID, v.ProjectContextID); err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid private navigation mode")
	}
	return positive("navigation revision", v.Revision)
}

type ProjectAccessState string

const (
	ProjectAccessActive  ProjectAccessState = "active"
	ProjectAccessRevoked ProjectAccessState = "revoked"
)

type ProjectPrincipalAccess struct {
	Tenant            string             `json:"tenant"`
	ChatID            string             `json:"chat_id"`
	ScopeID           string             `json:"scope_id"`
	PrincipalID       string             `json:"principal_id"`
	State             ProjectAccessState `json:"state"`
	BindingGeneration int64              `json:"binding_generation"`
	Source            SourceRef          `json:"source"`
	VerifiedAt        time.Time          `json:"verified_at"`
	ExpiresAt         time.Time          `json:"expires_at"`
	Revision          int64              `json:"revision"`
}

func (v ProjectPrincipalAccess) Validate() error {
	if err := required(v.ScopeID, v.PrincipalID, v.Tenant, v.ChatID); err != nil {
		return err
	}
	if err := v.Source.Validate(); err != nil {
		return err
	}
	if !oneOf(v.State, ProjectAccessActive, ProjectAccessRevoked) || v.BindingGeneration < 1 || v.VerifiedAt.IsZero() || !v.ExpiresAt.After(v.VerifiedAt) {
		return fmt.Errorf("invalid project membership validity")
	}
	return positive("project access revision", v.Revision)
}

type MembershipStatus string

const (
	MembershipPresent MembershipStatus = "present"
	MembershipAbsent  MembershipStatus = "absent"
)

// MembershipCheck is a trusted SDK query receipt. Model/UI request bodies must
// never be decoded into this type; the system query operation owns its proof.
type MembershipCheck struct {
	ID                string           `json:"id"`
	ScopeID           string           `json:"scope_id"`
	PrincipalID       string           `json:"principal_id"`
	Gateway           IngressGateway   `json:"gateway"`
	Tenant            string           `json:"tenant"`
	ChatID            string           `json:"chat_id"`
	BindingGeneration int64            `json:"binding_generation"`
	QueriedIdentity   IdentityScope    `json:"queried_identity"`
	Status            MembershipStatus `json:"status"`
	OperationID       string           `json:"operation_id"`
	ResponseRef       string           `json:"response_ref"`
	ObservedAt        time.Time        `json:"observed_at"`
	ExpiresAt         time.Time        `json:"expires_at"`
}

func (v MembershipCheck) Validate() error {
	if err := required(v.ID, v.ScopeID, v.PrincipalID, v.Tenant, v.ChatID, v.OperationID, v.ResponseRef); err != nil {
		return err
	}
	if err := v.Gateway.Validate(); err != nil {
		return err
	}
	if err := v.QueriedIdentity.Validate(); err != nil {
		return err
	}
	if !oneOf(v.Status, MembershipPresent, MembershipAbsent) || v.BindingGeneration < 1 || v.ObservedAt.IsZero() || !v.ExpiresAt.After(v.ObservedAt) {
		return fmt.Errorf("invalid membership query receipt")
	}
	return nil
}
