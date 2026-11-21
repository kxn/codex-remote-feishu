package work

import "fmt"

type ProjectMemberState string

const (
	ProjectMemberInvited     ProjectMemberState = "invited"
	ProjectMemberReady       ProjectMemberState = "ready"
	ProjectMemberUnavailable ProjectMemberState = "unavailable"
	ProjectMemberRemoved     ProjectMemberState = "removed"
)

type ProjectMember struct {
	AvailabilityEvidence *SourceRef         `json:"availability_evidence,omitempty"`
	ProjectID            string             `json:"project_id"`
	RoleID               string             `json:"role_id"`
	State                ProjectMemberState `json:"state"`
	BotGeneration        int64              `json:"bot_generation"`
	MemberReceipt        string             `json:"member_receipt,omitempty"`
	Revision             int64              `json:"revision"`
}

func (v ProjectMember) Validate() error {
	if v.AvailabilityEvidence != nil {
		if err := v.AvailabilityEvidence.Validate(); err != nil {
			return err
		}
	}
	if err := required(v.ProjectID, v.RoleID); err != nil {
		return err
	}
	if !oneOf(v.State, ProjectMemberInvited, ProjectMemberReady, ProjectMemberUnavailable, ProjectMemberRemoved) {
		return fmt.Errorf("invalid project member state")
	}
	if v.BotGeneration < 0 {
		return fmt.Errorf("negative bot generation")
	}
	return positive("project member revision", v.Revision)
}
