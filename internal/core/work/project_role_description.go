package work

import (
	"fmt"
	"time"
)

// ProjectRoleDescription overrides responsibilities for one project. It never
// changes the shared role's model profile or descriptions in other projects.
type ProjectRoleDescription struct {
	ProjectID          string    `json:"project_id"`
	RoleID             string    `json:"role_id"`
	Description        string    `json:"description"`
	DescriptionVersion int64     `json:"description_version"`
	Source             SourceRef `json:"source"`
	UpdatedBy          Actor     `json:"updated_by"`
	Revision           int64     `json:"revision"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func (v ProjectRoleDescription) Validate() error {
	if err := required(v.ProjectID, v.RoleID, v.Description); err != nil {
		return err
	}
	if err := positive("description version", v.DescriptionVersion); err != nil {
		return err
	}
	if err := positive("description revision", v.Revision); err != nil {
		return err
	}
	if v.UpdatedBy.Kind != ActorPrincipal {
		return fmt.Errorf("responsibility update requires actual human source")
	}
	if err := v.UpdatedBy.Validate(); err != nil {
		return err
	}
	return v.Source.Validate()
}
