package workprovision

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type MemberExecutionPreparer interface {
	PrepareProjectMember(context.Context, work.Project, string, []string) error
}

func (s *Service) loadMember(ctx context.Context, id string) (snapshot, workservice.ProjectMemberProvisionIntent, error) {
	var state snapshot
	var intent workservice.ProjectMemberProvisionIntent
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(id)
		if err != nil {
			return err
		}
		if op.Kind != "project.member.provision" {
			return fmt.Errorf("not member provisioning")
		}
		body, err := tx.GetContent(op.ScopeID, op.RequestRef)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(body.Data, &intent); err != nil {
			return err
		}
		state.operation = op
		state.project, err = tx.GetProject(intent.ProjectID)
		if err != nil {
			return err
		}
		if op.TargetID != state.project.ID || op.ScopeID != state.project.ScopeID || op.TargetGeneration != intent.ChannelGeneration || op.Actor != (work.Actor{Kind: work.ActorRole, ID: intent.Inviter.RoleID}) {
			return workstore.ErrScopeDenied
		}
		state.request = workservice.CreateProjectRequest{Tenant: intent.Tenant, ChatID: intent.ChatID, Name: state.project.Name}
		state.bindings = map[string]work.RoleBotBinding{intent.Role.RoleID: intent.Role}
		state.home = workstore.ProjectHome{Identity: intent.Inviter}
		if err = s.current(tx, state); err != nil {
			return err
		}
		member, err := tx.GetProjectMember(state.project.ID, intent.Role.RoleID)
		if err != nil {
			return err
		}
		if member.State == work.ProjectMemberRemoved || member.BotGeneration != intent.Role.IdentityGeneration {
			return ErrStale
		}
		expected := intent.MemberRevision
		applied, e := tx.GetProvisionStep(id, "members.apply")
		if e == nil && applied.State == work.OperationConfirmed {
			expected, err = strconv.ParseInt(applied.Version, 10, 64)
			if err != nil {
				return err
			}
		} else if e != nil && e != workstore.ErrNotFound {
			return e
		}
		if member.Revision != expected {
			return ErrStale
		}
		pref, err := tx.GetExecutionPreference(state.project.ScopeID)
		if err == nil {
			state.request.LocationIDs = pref.SelectedLocationIDs
			state.request.DefaultLocationID = pref.DefaultLocationID
		} else if err != workstore.ErrNotFound {
			return err
		}
		return nil
	})
	return state, intent, err
}
func (s *Service) runMember(ctx context.Context, id string) (Report, error) {
	state, intent, err := s.loadMember(ctx, id)
	if err != nil {
		return Report{OperationID: id}, err
	}
	if state.operation.State == work.OperationConfirmed {
		return s.report(ctx, state, nil)
	}
	if state.project.State != work.ProjectActive && state.project.State != work.ProjectDegraded {
		return s.reportWithError(ctx, state, []string{"项目当前未允许准备新成员"}, fmt.Errorf("project is not active"))
	}
	if err = s.addMember(ctx, &state, intent); err != nil {
		if markErr := s.markStage(ctx, &state, "members", err); markErr != nil {
			return Report{}, markErr
		}
		return s.finishMember(ctx, state, []string{err.Error()})
	}
	if err = s.markStage(ctx, &state, "members", nil); err != nil {
		return Report{}, err
	}
	_, err = s.step(ctx, state, "execution.prepare_member", intent.Role, map[string]any{"role_id": intent.Role.RoleID, "locations": state.request.LocationIDs}, false, func(ctx context.Context, _ json.RawMessage) (effect, error) {
		var e error
		if one, ok := s.execution.(MemberExecutionPreparer); ok {
			e = one.PrepareProjectMember(ctx, state.project, intent.Role.RoleID, state.request.LocationIDs)
		} else {
			e = s.execution.PrepareProject(ctx, state.project, state.request.LocationIDs)
		}
		return effect{ID: intent.Role.RoleID, Data: map[string]any{"role_id": intent.Role.RoleID, "prepared": e == nil}}, e
	})
	if markErr := s.markStage(ctx, &state, "execution", err); markErr != nil {
		return Report{}, markErr
	}
	issues := []string{}
	if err != nil {
		issues = append(issues, err.Error())
	}
	return s.finishMember(ctx, state, issues)
}
func (s *Service) addMember(ctx context.Context, state *snapshot, intent workservice.ProjectMemberProvisionIntent) error {
	// A durable apply receipt proves this operation's own membership revision.
	if step, err := s.getStep(ctx, state.operation.ID, "members.apply"); err == nil && step.State == work.OperationConfirmed {
		return nil
	}
	platform, err := s.platform(ctx, intent.Role)
	if err != nil {
		return err
	}
	own, err := platform.IsCallerInChat(ctx, intent.ChatID)
	if err != nil {
		return err
	}
	if !own {
		inviter, err := s.platform(ctx, intent.Inviter)
		if err != nil {
			return err
		}
		invitation, inviteErr := s.step(ctx, *state, "members.invite."+intent.Role.RoleID, intent.Inviter, map[string]string{"chat_id": intent.ChatID, "app_id": intent.Role.AppID}, true, func(ctx context.Context, _ json.RawMessage) (effect, error) {
			result, err := inviter.InviteBots(ctx, intent.ChatID, []string{intent.Role.AppID})
			return effect{ID: intent.Role.AppID, Data: result}, err
		})
		own, err = platform.IsCallerInChat(ctx, intent.ChatID)
		if err != nil {
			return err
		}
		if !own {
			if inviteErr != nil {
				return inviteErr
			}
			return fmt.Errorf("成员邀请已登记，等待该Bot自己的入群证据")
		}
		if inviteErr != nil && invitation.State == work.OperationUnknown {
			if _, err = s.confirmKnown(ctx, *state, invitation, effect{ID: intent.Role.AppID, Data: map[string]any{"joined": true, "confirmed_by": intent.Role}}); err != nil {
				return err
			}
		}
	}
	return s.applyMember(ctx, *state, intent)
}
func (s *Service) applyMember(ctx context.Context, state snapshot, intent workservice.ProjectMemberProvisionIntent) error {
	return s.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := s.current(tx, state); err != nil {
			return err
		}
		fresh, err := tx.GetRoleBotBinding(intent.Role.RoleID)
		if err != nil {
			return err
		}
		if fresh != intent.Role {
			return ErrStale
		}
		if existing, err := tx.GetProvisionStep(state.operation.ID, "members.apply"); err == nil && existing.State == work.OperationConfirmed {
			return nil
		} else if err != nil && err != workstore.ErrNotFound {
			return err
		}
		member, err := tx.GetProjectMember(state.project.ID, intent.Role.RoleID)
		if err != nil {
			return err
		}
		if member.Revision != intent.MemberRevision || member.State == work.ProjectMemberRemoved {
			return ErrStale
		}
		member.State = work.ProjectMemberReady
		member.Revision++
		receipt, err := json.Marshal(membership{RoleID: intent.Role.RoleID, AppID: intent.Role.AppID, InChat: true})
		if err != nil {
			return err
		}
		member.MemberReceipt = string(receipt)
		if err = tx.PutProjectMember(member, intent.MemberRevision); err != nil {
			return err
		}
		raw, err := json.Marshal(intent)
		if err != nil {
			return err
		}
		body, err := json.Marshal(member)
		if err != nil {
			return err
		}
		step := workstore.ProvisionStep{ID: workstore.ProvisionStepID(state.operation.ID, "members.apply"), ScopeID: state.project.ScopeID, OperationID: state.operation.ID, StepID: "members.apply", Identity: intent.Role, State: work.OperationConfirmed, Intent: raw, Receipt: body, ExternalID: intent.Role.AppID, Version: strconv.FormatInt(member.Revision, 10), Revision: 1, UpdatedAt: s.now()}
		return tx.PutProvisionStep(step, 0)
	})
}

func (s *Service) getStep(ctx context.Context, opID, key string) (workstore.ProvisionStep, error) {
	var step workstore.ProvisionStep
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		step, err = tx.GetProvisionStep(opID, key)
		return err
	})
	return step, err
}
func (s *Service) finishMember(ctx context.Context, state snapshot, issues []string) (Report, error) {
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := s.current(tx, state); err != nil {
			return err
		}
		op, err := tx.GetOperation(state.operation.ID)
		if err != nil {
			return err
		}
		old := op.Revision
		op.Revision++
		op.UpdatedAt = s.now()
		op.State = work.OperationConfirmed
		receipt, err := json.Marshal(map[string]any{"project_id": state.project.ID, "steps": "provision_steps", "member_verified": true, "native_preparation": "prepared"})
		if err != nil {
			return err
		}
		op.ExternalReceipt = string(receipt)
		if len(issues) > 0 {
			op.State = work.OperationUnknown
			op.LastError = fmt.Sprint(issues)
		}
		_, _, err = tx.PutOperation(op, old)
		return err
	})
	if err != nil {
		return Report{}, err
	}
	return s.report(ctx, state, issues)
}
