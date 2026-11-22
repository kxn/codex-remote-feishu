package work

import "fmt"

type PublicationMemberAuthority struct {
	RoleID          string `json:"role_id"`
	TeamRevision    int64  `json:"team_revision"`
	ProjectRevision int64  `json:"project_revision"`
	BotGeneration   int64  `json:"bot_generation"`
}

type PublicationTaskAuthority struct {
	TaskID               string `json:"task_id,omitempty"`
	OwnerRoleID          string `json:"owner_role_id,omitempty"`
	RequirementsRevision int64  `json:"requirements_revision,omitempty"`
	AssignmentGeneration int64  `json:"assignment_generation,omitempty"`
}

type PublicationGrant struct {
	ID                   string                     `json:"id"`
	ScopeID              string                     `json:"scope_id"`
	LeadershipGeneration int64                      `json:"leadership_generation"`
	Grantor              PublicationMemberAuthority `json:"grantor"`
	Grantee              PublicationMemberAuthority `json:"grantee"`
	Task                 PublicationTaskAuthority   `json:"task"`
	Tenant               string                     `json:"tenant"`
	ChatID               string                     `json:"chat_id"`
	ChannelGeneration    int64                      `json:"channel_generation"`
	RootMessageID        string                     `json:"root_message_id,omitempty"`
	ContentKind          string                     `json:"content_kind"`
	References           []DeliveryRef              `json:"references,omitempty"`
	Revoked              bool                       `json:"revoked"`
	Revision             int64                      `json:"revision"`
}

func (v PublicationGrant) Validate() error {
	if err := required(v.ID, v.ScopeID, v.Grantor.RoleID, v.Grantee.RoleID, v.Task.TaskID, v.Task.OwnerRoleID, v.Tenant, v.ChatID); err != nil {
		return err
	}
	if v.Grantor.RoleID == v.Grantee.RoleID || v.Task.OwnerRoleID != v.Grantee.RoleID ||
		!oneOf(v.ContentKind, "message", "delivery") || (v.ContentKind == "delivery") != (len(v.References) != 0) {
		return fmt.Errorf("delegation needs a distinct task owner and an explicit publication type and versions")
	}
	for _, n := range []int64{v.Revision, v.LeadershipGeneration, v.ChannelGeneration, v.Grantor.TeamRevision, v.Grantor.ProjectRevision, v.Grantor.BotGeneration, v.Grantee.TeamRevision, v.Grantee.ProjectRevision, v.Grantee.BotGeneration, v.Task.RequirementsRevision, v.Task.AssignmentGeneration} {
		if err := positive("publication delegation generation", n); err != nil {
			return err
		}
	}
	seen := map[DeliveryRef]bool{}
	for _, ref := range v.References {
		if ref.ResourceID == "" || ref.VersionID == "" || ref.Current || seen[ref] {
			return fmt.Errorf("delegation requires unique fixed resource versions")
		}
		seen[ref] = true
	}
	return nil
}
