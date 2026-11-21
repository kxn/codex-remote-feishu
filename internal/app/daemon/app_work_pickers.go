package daemon

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const teamPickerPageSize = 10

func (a *App) queueTeamProjectPicker(ctx context.Context, source teamHumanSource) (string, error) {
	return a.queueTeamProjectPickerPage(ctx, source, 0)
}
func (a *App) queueTeamProjectPickerPage(ctx context.Context, source teamHumanSource, page int) (string, error) {
	if page < 0 {
		return "", fmt.Errorf("无效页码")
	}
	var projects []work.Project
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		scope, _, e := tx.ValidatePrivateAction(source.Authority)
		if e != nil {
			return e
		}
		if scope.ID != source.ScopeID || scope.PrincipalID != source.PrincipalID || scope.RoleID != source.RoleID {
			return workstore.ErrScopeDenied
		}
		nav, e := tx.GetPrivateNavigation(scope.ID)
		if e != nil {
			return e
		}
		if nav.Revision != source.NavigationRevision {
			return workstore.ErrConflict
		}
		all, e := tx.ListProjects()
		if e != nil {
			return e
		}
		for _, project := range all {
			member, e := tx.GetProjectMember(project.ID, source.RoleID)
			if e == workstore.ErrNotFound {
				continue
			}
			if e != nil {
				return e
			}
			if member.State != work.ProjectMemberReady {
				continue
			}
			channels, e := tx.ListProjectChannelBindings(project.ID)
			if e != nil {
				return e
			}
			for _, channel := range channels {
				if channel.Active && channel.Purpose == "main" && channel.Tenant == source.Tenant {
					projects = append(projects, project)
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	panel := workui.Panel{Title: "选择要协助的项目", Paragraphs: []string{"这里可以选择你当前可访问的项目。进入项目后，私人讨论仍保留在私聊。"}}
	start := page * teamPickerPageSize
	if start > len(projects) {
		return "", workstore.ErrConflict
	}
	end := min(start+teamPickerPageSize, len(projects))
	unavailable := false
	for _, project := range projects[start:end] {
		if err = a.verifyWorkMembership(ctx, source.PrincipalID, project.ScopeID, source.Tenant, source.SenderIDs, source.Gateway); err != nil {
			unavailable = true
			continue
		}
		panel.Buttons = append(panel.Buttons, workui.Button{Label: project.Name, Action: "picker.project.select", Values: map[string]any{"project_scope_id": project.ScopeID}})
	}
	if len(panel.Buttons) == 0 {
		panel.Paragraphs = append(panel.Paragraphs, "当前页没有可进入的项目。可以先在项目群里发言，或稍后重新核对。")
	}
	if unavailable {
		panel.Paragraphs = append(panel.Paragraphs, "部分访问范围暂时未能确认，未显示这些项目。")
	}
	if page > 0 {
		panel.Buttons = append(panel.Buttons, workui.Button{Label: "上一页", Action: "picker.project.page", Values: map[string]any{"page": strconv.Itoa(page - 1)}})
	}
	if end < len(projects) {
		panel.Buttons = append(panel.Buttons, workui.Button{Label: "下一页", Action: "picker.project.page", Values: map[string]any{"page": strconv.Itoa(page + 1)}})
	}
	return a.queueTeamPicker(ctx, source, "private.project-picker", source.ScopeID, source.NavigationRevision, panel)
}
func (a *App) queueTeamPicker(ctx context.Context, source teamHumanSource, kind, target string, revision int64, panel workui.Panel) (string, error) {
	id := workui.ID(source.Key, kind)
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if prior, e := tx.GetUIView(id); e == nil {
			if prior.ScopeID != source.ScopeID || prior.PrincipalID != source.PrincipalID || prior.TargetID != target {
				return workstore.ErrConflict
			}
			return nil
		} else if e != workstore.ErrNotFound {
			return e
		}
		if err := a.validateTeamPickerSource(tx, source); err != nil {
			return err
		}
		binding, e := tx.GetRoleBotBinding(source.RoleID)
		if e != nil {
			return e
		}
		scope, e := tx.GetScope(source.ScopeID)
		if e != nil {
			return e
		}
		generation := int64(0)
		if scope.Kind == work.ScopeBootstrap {
			generation = scope.BindingGeneration
		}
		if scope.Kind == work.ScopeProject {
			channel, e := tx.GetProjectChannelBinding(source.Tenant, source.ChatID)
			if e != nil {
				return e
			}
			if !channel.Active || channel.ProjectID != scope.ProjectID {
				return workstore.ErrScopeDenied
			}
			generation = channel.BindingGeneration
		}
		view := workstore.UIView{ID: id, ScopeID: source.ScopeID, SurfaceKey: source.ScopeID + ":" + kind, Kind: kind, TargetID: target, TargetRevision: revision, Binding: binding, Tenant: source.Tenant, ChatID: source.ChatID, PrincipalID: source.PrincipalID, ChannelGeneration: generation, ReplyToMessageID: source.MessageID, CreatedAt: time.Now().UTC()}

		if scope.Kind == work.ScopePersonal {
			front, _, e := worksettings.FrozenContext(tx, source.Authority)
			if e != nil {
				return e
			}
			view.ContextID = front.ID
			if kind == "private.context-picker" && target != scope.ID {
				described, e := tx.GetContext(target)
				if e != nil {
					return e
				}
				if described.ScopeID != scope.ID || described.RoleID != scope.RoleID {
					return workstore.ErrScopeDenied
				}
				view.ContextID = described.ID
			}
		}
		if source.Authority.InteractionID != "" {
			interaction, e := tx.GetHumanInteraction(source.Authority.InteractionID)
			if e != nil {
				return e
			}
			action, _ := interaction.Envelope.Values["action"].(string)
			if strings.HasSuffix(action, ".page") || action == "history.contexts-page" || action == "history.read" {
				if e = workui.ReplaceFromInteraction(tx, &view, interaction.ID); e != nil {
					return e
				}
			}
		}
		card, e := workui.Card(id, panel)
		if e != nil {
			return e
		}
		_, e = workui.Queue(tx, view, card)
		return e
	})
	return id, err
}
func (a *App) validateTeamPickerSource(tx *workstore.Tx, source teamHumanSource) error {
	scope, err := tx.GetScope(source.ScopeID)
	if err != nil {
		return err
	}
	if scope.Kind == work.ScopePersonal {
		actual, _, e := tx.ValidatePrivateAction(source.Authority)
		if e != nil {
			return e
		}
		if actual.ID != scope.ID || actual.PrincipalID != source.PrincipalID || actual.RoleID != source.RoleID {
			return workstore.ErrScopeDenied
		}
		return nil
	}
	interaction, e := tx.GetHumanInteraction(source.Authority.InteractionID)
	if e != nil {
		return e
	}
	if viewID, _ := interaction.Envelope.Values["team_view"].(string); viewID != "" {
		view, e := tx.GetUIView(viewID)
		if e != nil {
			return e
		}
		if e = tx.ValidateViewInteraction(view, interaction); e != nil {
			return e
		}
	}
	if interaction.PrincipalID != source.PrincipalID || interaction.Gateway != source.Gateway || interaction.Envelope.Tenant != source.Tenant || interaction.Envelope.ChatID != source.ChatID {
		return workstore.ErrScopeDenied
	}
	if scope.Kind == work.ScopeBootstrap {
		if scope.PrincipalID != source.PrincipalID || scope.RoleID != source.RoleID || scope.Tenant != source.Tenant || scope.ChatID != source.ChatID {
			return workstore.ErrScopeDenied
		}
		return tx.RequireOpenBootstrapScope(scope)
	}
	return tx.RequireProjectPrincipalAccess(scope.ID, source.PrincipalID, time.Now().UTC())
}
