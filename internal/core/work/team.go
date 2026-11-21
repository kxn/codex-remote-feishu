package work

import "time"

type Principal struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"display_name"`
	Revision    int64     `json:"revision"`
	CreatedAt   time.Time `json:"created_at"`
}

func (v Principal) Validate() error {
	if err := required(v.ID, v.DisplayName); err != nil {
		return err
	}
	return positive("principal revision", v.Revision)
}

type Team struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	LeaderRoleID string `json:"leader_role_id"`
	Revision     int64  `json:"revision"`
}

func (v Team) Validate() error {
	if err := required(v.ID, v.Name, v.LeaderRoleID); err != nil {
		return err
	}
	return positive("team revision", v.Revision)
}

type TeamMember struct {
	TeamID   string `json:"team_id"`
	RoleID   string `json:"role_id"`
	Active   bool   `json:"active"`
	Revision int64  `json:"revision"`
}

func (v TeamMember) Validate() error {
	if err := required(v.TeamID, v.RoleID); err != nil {
		return err
	}
	return positive("team member revision", v.Revision)
}
