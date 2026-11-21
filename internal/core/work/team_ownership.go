package work

import "fmt"

// TeamOwnership is explicit management authority, independent of the team leader
// role used for business coordination. Transfer requires application authorization.
type TeamOwnership struct {
	TeamID      string    `json:"team_id"`
	PrincipalID string    `json:"principal_id"`
	Source      SourceRef `json:"source"`
	UpdatedBy   Actor     `json:"updated_by"`
	Revision    int64     `json:"revision"`
}

func (v TeamOwnership) Validate() error {
	if err := required(v.TeamID, v.PrincipalID); err != nil {
		return err
	}
	if err := positive("ownership revision", v.Revision); err != nil {
		return err
	}
	if v.UpdatedBy.Kind != ActorPrincipal {
		return fmt.Errorf("team owner requires actual human authorization")
	}
	if err := v.UpdatedBy.Validate(); err != nil {
		return err
	}
	return v.Source.Validate()
}
