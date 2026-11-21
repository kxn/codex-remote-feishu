package workprovision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type membership struct {
	RoleID string `json:"role_id"`
	AppID  string `json:"app_id"`
	InChat bool   `json:"in_chat"`
}

func (s *Service) members(ctx context.Context, state *snapshot) error {
	inviter := state.bindings[state.project.LeaderRoleID]
	leader, leaderErr := s.platform(ctx, inviter)
	var issues []error
	for _, roleID := range roleIDs(*state) {
		binding := state.bindings[roleID]
		if _, err := s.step(ctx, *state, "members.identity."+roleID, binding, binding, false, func(context.Context, json.RawMessage) (effect, error) {
			return effect{ID: binding.AppID, Data: binding}, nil
		}); err != nil {
			issues = append(issues, err)
			continue
		}
		var memberRevision int64
		if err := s.store.Within(ctx, func(tx *workstore.Tx) error {
			member, err := tx.GetProjectMember(state.project.ID, roleID)
			memberRevision = member.Revision
			return err
		}); err != nil {
			issues = append(issues, err)
			continue
		}
		observation := roleID + "." + strconv.FormatInt(memberRevision, 10)
		platform, err := s.platform(ctx, binding)
		if err != nil {
			issues = append(issues, fmt.Errorf("%s: %w", roleID, err))
			continue
		}
		check, err := s.step(ctx, *state, "members.check."+observation, binding, map[string]string{"chat_id": state.request.ChatID}, false, func(ctx context.Context, _ json.RawMessage) (effect, error) {
			inChat, err := platform.IsCallerInChat(ctx, state.request.ChatID)
			value := membership{RoleID: roleID, AppID: binding.AppID, InChat: inChat}
			return effect{ID: binding.AppID, Data: value}, err
		})
		observed, decodeErr := decodeStep[membership](check)
		if decodeErr != nil && err == nil {
			err = decodeErr
		}
		if err != nil {
			issues = append(issues, err)
			continue
		}
		memberState := work.ProjectMemberReady
		if !observed.InChat {
			memberState = work.ProjectMemberInvited
			if roleID == state.project.LeaderRoleID || leaderErr != nil {
				issues = append(issues, fmt.Errorf("%s: 负责人机器人当前不可邀请成员", roleID))
				memberState = work.ProjectMemberUnavailable
			} else {
				invitation, inviteErr := s.step(ctx, *state, "members.invite."+roleID, inviter, map[string]string{"chat_id": state.request.ChatID, "app_id": binding.AppID}, true, func(ctx context.Context, intent json.RawMessage) (effect, error) {
					var request map[string]string
					if err := json.Unmarshal(intent, &request); err != nil {
						return effect{}, err
					}
					result, err := leader.InviteBots(ctx, request["chat_id"], []string{request["app_id"]})
					return effect{ID: binding.AppID, Data: result}, err
				})
				// Only this role's own credential can prove that its bot actually joined.
				confirmation, confirmErr := s.step(ctx, *state, "members.confirm."+observation, binding, map[string]string{"chat_id": state.request.ChatID}, false, func(ctx context.Context, _ json.RawMessage) (effect, error) {
					inChat, err := platform.IsCallerInChat(ctx, state.request.ChatID)
					return effect{ID: binding.AppID, Data: membership{RoleID: roleID, AppID: binding.AppID, InChat: inChat}}, err
				})
				own, parseErr := decodeStep[membership](confirmation)
				if confirmErr == nil {
					confirmErr = parseErr
				}
				if confirmErr == nil && own.InChat {
					observed = own
					memberState = work.ProjectMemberReady
					if inviteErr != nil && invitation.State == work.OperationUnknown {
						_, inviteErr = s.confirmKnown(ctx, *state, invitation, effect{ID: binding.AppID, Data: map[string]any{"joined": true, "confirmed_by": binding}})
					}
				} else if inviteErr != nil || confirmErr != nil {
					memberState = work.ProjectMemberUnavailable
					issues = append(issues, errors.Join(inviteErr, confirmErr))
				} else {
					result, _ := decodeStep[feishu.ProjectChatMembersResult](invitation)
					for _, id := range append(result.InvalidIDs, result.NotExistedIDs...) {
						if id == binding.AppID {
							memberState = work.ProjectMemberUnavailable
						}
					}
				}
			}
		}
		if err := s.store.Within(ctx, func(tx *workstore.Tx) error {
			if err := s.current(tx, *state); err != nil {
				return err
			}
			fresh, err := tx.GetRoleBotBinding(roleID)
			if err != nil {
				return err
			}
			if fresh != binding {
				return ErrStale
			}
			member, err := tx.GetProjectMember(state.project.ID, roleID)
			if err != nil {
				return err
			}
			if member.State == work.ProjectMemberRemoved {
				return ErrStale
			}
			previous := member.Revision
			member.Revision++
			member.State = memberState
			member.BotGeneration = binding.IdentityGeneration
			receipt, _ := json.Marshal(observed)
			member.MemberReceipt = string(receipt)
			return tx.PutProjectMember(member, previous)
		}); err != nil {
			issues = append(issues, err)
		}
	}
	return errors.Join(issues...)
}

// RefreshMembers is for verified membership events or explicit reconciliation.
// Each bot observes itself; this never infers bots from the human member list.
func (s *Service) RefreshMembers(ctx context.Context, projectID string) error {
	var operationID string
	if err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		home, err := tx.GetProjectHome(projectID)
		operationID = home.OperationID
		return err
	}); err != nil {
		return err
	}
	state, err := s.load(ctx, operationID)
	if err != nil {
		return err
	}
	memberErr := s.members(ctx, &state)
	err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := s.current(tx, state); err != nil {
			return err
		}
		project, err := tx.GetProject(projectID)
		if err != nil {
			return err
		}
		operation, err := tx.GetOperation(operationID)
		if err != nil {
			return err
		}
		members, err := tx.ListProjectMembers(projectID)
		if err != nil {
			return err
		}
		ready := operation.State == work.OperationConfirmed && state.home.SyncState == "synced" && memberErr == nil
		for _, member := range members {
			if member.State != work.ProjectMemberRemoved {
				ready = ready && member.State == work.ProjectMemberReady
			}
		}
		if project.State != work.ProjectProvisioning && project.State != work.ProjectActive && project.State != work.ProjectDegraded {
			return nil
		}
		previous := project.Revision
		project.Revision++
		if ready {
			project.State = work.ProjectActive
		} else {
			project.State = work.ProjectDegraded
		}
		return tx.PutProject(project, previous)
	})
	if err != nil {
		return err
	}
	return memberErr
}
