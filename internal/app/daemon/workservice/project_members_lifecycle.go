package workservice

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type ProjectMemberRequest struct {
	Key                    string `json:"key"`
	ProjectID              string `json:"project_id"`
	ExpectedRevision       int64  `json:"expected_revision"`
	RoleID                 string `json:"role_id"`
	ExpectedMemberRevision int64  `json:"expected_member_revision"`
	ReplacementRoleID      string `json:"replacement_role_id,omitempty"`
	SourceMessageID        string `json:"source_message_id"`
}
type ProjectMemberProvisionIntent struct {
	ProjectID         string              `json:"project_id"`
	Role              work.RoleBotBinding `json:"role"`
	Inviter           work.RoleBotBinding `json:"inviter"`
	MemberRevision    int64               `json:"member_revision"`
	Tenant            string              `json:"tenant"`
	ChatID            string              `json:"chat_id"`
	ChannelGeneration int64               `json:"channel_generation"`
	SourceMessageID   string              `json:"source_message_id"`
}
type ProjectMemberResult struct {
	Result
	ProvisionOperationID string `json:"provision_operation_id,omitempty"`
}

func (s *Service) ProjectMemberAdd(ctx context.Context, token, run string, req ProjectMemberRequest) (ProjectMemberResult, error) {
	var result ProjectMemberResult
	err := s.withWorker(ctx, token, run, func(tx *workstore.Tx, c Caller) error {
		if _, _, err := currentHumanInstruction(tx, c, req.SourceMessageID); err != nil {
			return err
		}
		opID := stableID("member_provision", c.Worker.ScopeID, c.Worker.RoleID, req.Key)
		var err error
		result.Result, err = s.mutate(tx, c, "project.member.add", req.Key, req.ProjectID, req, func() (string, int64, error) {
			project, err := currentProject(tx, c, req.ProjectID, true)
			if err != nil {
				return "", 0, err
			}
			if project.Revision != req.ExpectedRevision {
				return "", 0, workstore.ErrConflict
			}
			if req.ReplacementRoleID != "" {
				return "", 0, fmt.Errorf("member addition cannot replace another role")
			}
			roster, err := tx.GetTeamMember(project.TeamID, req.RoleID)
			if err != nil || !roster.Active {
				return "", 0, fmt.Errorf("role must first join the active team roster")
			}
			role, err := tx.GetRoleBotBinding(req.RoleID)
			if err != nil {
				return "", 0, err
			}
			inviter, err := tx.GetRoleBotBinding(project.LeaderRoleID)
			if err != nil {
				return "", 0, err
			}
			channels, err := tx.ListProjectChannelBindings(project.ID)
			if err != nil {
				return "", 0, err
			}
			var main work.ProjectChannelBinding
			for _, binding := range channels {
				if binding.Active && binding.Purpose == "main" {
					if main.ChatID != "" {
						return "", 0, fmt.Errorf("project has multiple main channels")
					}
					main = binding
				}
			}
			if main.ChatID == "" {
				return "", 0, fmt.Errorf("project has no active main conversation")
			}
			member := work.ProjectMember{ProjectID: project.ID, RoleID: req.RoleID, State: work.ProjectMemberInvited, BotGeneration: role.IdentityGeneration, Revision: req.ExpectedMemberRevision + 1}
			if err = tx.PutProjectMember(member, req.ExpectedMemberRevision); err != nil {
				return "", 0, err
			}
			intent := ProjectMemberProvisionIntent{ProjectID: project.ID, Role: role, Inviter: inviter, MemberRevision: member.Revision, Tenant: main.Tenant, ChatID: main.ChatID, ChannelGeneration: main.BindingGeneration, SourceMessageID: req.SourceMessageID}
			raw, err := json.Marshal(intent)
			if err != nil {
				return "", 0, err
			}
			body := work.NewContent(stableID("member_request", opID), project.ScopeID, "application/json", raw, s.now())
			if err = tx.PutContent(body); err != nil {
				return "", 0, err
			}
			op := work.Operation{ID: opID, ScopeID: project.ScopeID, Actor: work.Actor{Kind: work.ActorRole, ID: c.Worker.RoleID}, Kind: "project.member.provision", TargetID: project.ID, TargetGeneration: main.BindingGeneration, IdempotencyKey: req.Key, RequestHash: body.SHA256, RequestRef: body.ID, State: work.OperationPrepared, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now(), Steps: []work.OperationStep{{ID: "members", State: work.OperationPrepared}, {ID: "execution", State: work.OperationPrepared}}}
			if _, _, err = tx.PutOperation(op, 0); err != nil {
				return "", 0, err
			}
			project.Revision++
			if err = tx.PutProject(project, req.ExpectedRevision); err != nil {
				return "", 0, err
			}
			return project.ID, project.Revision, nil
		})
		if err == nil {
			result.Status = "accepted"
			result.ProvisionOperationID = opID
		}
		return err
	})
	return result, err
}
func (s *Service) ProjectMemberRemove(ctx context.Context, token, run string, req ProjectMemberRequest) (Result, error) {
	var result Result
	err := s.withWorker(ctx, token, run, func(tx *workstore.Tx, c Caller) error {
		message, _, err := currentHumanInstruction(tx, c, req.SourceMessageID)
		if err != nil {
			return err
		}
		result, err = s.mutate(tx, c, "project.member.remove", req.Key, req.ProjectID, req, func() (string, int64, error) {
			project, err := currentProject(tx, c, req.ProjectID, true)
			if err != nil {
				return "", 0, err
			}
			if project.Revision != req.ExpectedRevision {
				return "", 0, workstore.ErrConflict
			}
			if project.LeaderRoleID == req.RoleID {
				return "", 0, fmt.Errorf("change project leader before removing the current leader")
			}
			member, err := tx.GetProjectMember(project.ID, req.RoleID)
			if err != nil {
				return "", 0, err
			}
			if member.Revision != req.ExpectedMemberRevision {
				return "", 0, workstore.ErrConflict
			}
			if req.ReplacementRoleID == req.RoleID {
				return "", 0, fmt.Errorf("replacement must be another ready role")
			}
			if req.ReplacementRoleID != "" {
				if err = ensureMember(tx, project.ScopeID, req.ReplacementRoleID); err != nil {
					return "", 0, err
				}
			}
			tasks, err := tx.ListTasks(project.ScopeID)
			if err != nil {
				return "", 0, err
			}
			for _, task := range tasks {
				if task.OwnerRoleID != req.RoleID || task.State == work.TaskDone || task.State == work.TaskCancelled {
					continue
				}
				if req.ReplacementRoleID == "" {
					return "", 0, fmt.Errorf("unfinished task %s requires an explicit replacement role", task.ID)
				}
				old := task.Revision
				task.Revision++
				task.AssignmentGeneration++
				task.OwnerRoleID = req.ReplacementRoleID
				task.UpdatedAt = s.now()
				if err = tx.PutTask(task, old); err != nil {
					return "", 0, err
				}
				if err = tx.PutInbox(work.Inbox{ID: stableID("member_handoff", task.ID, req.Key), ScopeID: task.ScopeID, TaskID: task.ID, RecipientRoleID: task.OwnerRoleID, EventID: fmt.Sprintf("task:%s:assignment:%d", task.ID, task.AssignmentGeneration), State: work.InboxPending, Revision: 1, AvailableAt: s.now()}, 0); err != nil {
					return "", 0, err
				}
			}
			if err = s.migrateProjectAuthority(tx, c, project, req.RoleID, req.ReplacementRoleID, message, req.Key); err != nil {
				return "", 0, err
			}
			inputs, err := tx.ListInbox(project.ScopeID)
			if err != nil {
				return "", 0, err
			}
			for _, input := range inputs {
				if input.RecipientRoleID != req.RoleID || input.State != work.InboxPending {
					continue
				}
				old := input.Revision
				input.Revision++
				input.State = work.InboxCancelled
				if err = tx.PutInbox(input, old); err != nil {
					return "", 0, err
				}
				if input.MessageID != "" {
					recipient := req.ReplacementRoleID
					if recipient == "" {
						recipient = project.LeaderRoleID
					}
					copy := input
					copy.ID = stableID("member_input_handoff", input.ID, req.Key)
					copy.RecipientRoleID = recipient
					copy.RecipientContextID = ""
					copy.State = work.InboxPending
					copy.Revision = 1
					copy.ClaimedAttemptID = ""
					if err = tx.PutInbox(copy, 0); err != nil {
						return "", 0, err
					}
				}
			}
			old := member.Revision
			member.Revision++
			member.State = work.ProjectMemberRemoved
			if err = tx.PutProjectMember(member, old); err != nil {
				return "", 0, err
			}
			project.Revision++
			if err = tx.PutProject(project, req.ExpectedRevision); err != nil {
				return "", 0, err
			}
			return project.ID, project.Revision, nil
		})
		return err
	})
	return result, err
}
