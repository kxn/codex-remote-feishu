package daemon

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type teamPrivateCommands struct{ app *App }
type teamHumanSource struct {
	BootstrapSourceMessageID                                     string
	NavigationRevision                                           int64
	Key, ScopeID, PrincipalID, RoleID, Tenant, ChatID, MessageID string
	Gateway                                                      work.IngressGateway
	SenderIDs                                                    work.SenderIDs
	Authority                                                    workstore.PrivateActionSource
}

func (h teamPrivateCommands) Handle(ctx context.Context, command work.TypedCommandEnvelope) (workingress.PrivateCommandReceipt, error) {
	a := h.app
	r := a.teamRuntime
	if r == nil {
		return workingress.PrivateCommandReceipt{}, fmt.Errorf("team runtime is unavailable")
	}
	var source teamHumanSource
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		actual, e := tx.GetPrivateCommand(command.ID)
		if e != nil {
			return e
		}
		if actual.ScopeID != command.ScopeID || actual.PrincipalID != command.PrincipalID || actual.Text != command.Text {
			return workstore.ErrConflict
		}
		raw, e := tx.GetIngress(actual.IngressID)
		if e != nil {
			return e
		}
		source = teamHumanSource{NavigationRevision: actual.NavigationRevision, Key: actual.ID, ScopeID: actual.ScopeID, PrincipalID: actual.PrincipalID, RoleID: actual.RoleID, Tenant: raw.Envelope.Tenant, ChatID: raw.Envelope.ChatID, MessageID: raw.Envelope.MessageID, Gateway: raw.Gateway, SenderIDs: raw.Envelope.SenderIDs, Authority: workstore.PrivateActionSource{PersonalScopeID: actual.ScopeID, CommandID: actual.ID}}
		_, _, e = tx.ValidatePrivateAction(source.Authority)
		return e
	})
	if err != nil {
		return workingress.PrivateCommandReceipt{}, err
	}
	action, known := control.ParseFeishuTextActionWithoutCatalog(command.Text)
	var receipt string
	if !known {
		receipt, err = a.queuePrivatePanel(ctx, source, "help", "没有识别这个命令，可以使用下面的入口。")
	} else {
		switch action.Kind {
		case control.ActionShowCommandMenu, control.ActionShowCommandHelp:
			receipt, err = a.queuePrivatePanel(ctx, source, "menu", "")
		case control.ActionStatus:
			receipt, err = a.queuePrivatePanel(ctx, source, "status", "")
		case control.ActionShowHistory, control.ActionShowThreads, control.ActionShowAllThreads:
			receipt, err = a.queuePrivatePanel(ctx, source, "contexts", "")
		case control.ActionListInstances:
			receipt, err = a.queuePrivatePanel(ctx, source, "workers", "")
		case control.ActionNewThread:
			err = a.navigateFromSource(ctx, source, "new", "", "", 0)
			if err == nil {
				receipt, err = a.queuePrivatePanel(ctx, source, "menu", "已新建工作。长期记忆会保留，新工作的讨论从这里开始。")
			}
		case control.ActionCronCommand:
			receipt, err = a.queuePrivatePanel(ctx, source, "menu", "旧的定时任务功能已停用，旧任务不会自动执行。")
		case control.ActionPrimaryCommand, control.ActionCoworkersCommand:
			receipt, err = a.queuePrivatePanel(ctx, source, "menu", "项目团队由负责人协调，可以直接和负责人讨论成员及分工。")
		default:
			receipt, err = a.handleTeamPrivateOperation(ctx, source, action)
		}
	}
	if err != nil {
		problem := err
		notice, publishErr := a.queuePrivatePanel(context.WithoutCancel(ctx), source, "command-error", "这次操作没有完成："+problem.Error())
		receipt = notice
		err = errors.Join(problem, publishErr)
	}
	return workingress.PrivateCommandReceipt{Receipt: receipt, Rejected: err != nil && receipt != "", ErrorDetail: teamCommandError(err)}, err
}

func (a *App) navigateFromSource(ctx context.Context, source teamHumanSource, action, projectScopeID, contextID string, expected int64) error {
	if expected == 0 {
		expected = source.NavigationRevision
	}
	if expected < 1 {
		return fmt.Errorf("这条操作缺少当时的工作位置，请重新打开菜单再操作。")
	}

	if action == "select_context" && contextID != "" {
		if err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
			wc, e := tx.GetContext(contextID)
			if e != nil {
				return e
			}
			if wc.ScopeID != source.ScopeID {
				return workstore.ErrScopeDenied
			}
			projectScopeID = wc.ProjectViewScopeID
			return nil
		}); err != nil {
			return err
		}
	}
	if projectScopeID != "" {
		if err := a.verifyWorkMembership(ctx, source.PrincipalID, projectScopeID, source.Tenant, source.SenderIDs, source.Gateway); err != nil {
			return err
		}
	}
	_, err := a.teamRuntime.service.TrustedPrivateNavigate(ctx, workservice.TrustedPrivateNavigateRequest{Key: source.Key, Source: source.Authority, Action: action, ProjectScopeID: projectScopeID, ContextID: contextID, ExpectedRevision: expected})
	return err
}

func (a *App) queuePrivatePanel(ctx context.Context, source teamHumanSource, kind, note string) (string, error) {
	if kind == "contexts" {
		return a.queueTeamContexts(ctx, source, 0)
	}
	id := workui.ID(source.Key, kind)
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if _, e := tx.GetUIView(id); e == nil {
			return nil
		} else if e != workstore.ErrNotFound {
			return e
		}
		nav, e := tx.GetPrivateNavigation(source.ScopeID)
		if e != nil {
			return e
		}
		binding, e := tx.GetRoleBotBinding(source.RoleID)
		if e != nil {
			return e
		}
		panel := workui.Panel{Title: "和助手一起工作"}
		if note != "" {
			panel.Paragraphs = append(panel.Paragraphs, note)
		}
		if nav.Mode == work.PrivateProject {
			if e = tx.RequireProjectPrincipalAccess(nav.ProjectScopeID, source.PrincipalID, time.Now().UTC()); e == nil {
				scope, e := tx.GetScope(nav.ProjectScopeID)
				if e != nil {
					return e
				}
				project, e := tx.GetProject(scope.ProjectID)
				if e != nil {
					return e
				}
				panel.Paragraphs = append(panel.Paragraphs, "当前查看项目："+project.Name+"。这里的私人讨论不会自动发到项目群。")
			} else {
				panel.Paragraphs = append(panel.Paragraphs, "当前项目的访问范围需要重新核对；私人讨论仍保留在这里。")
			}
		} else {
			panel.Paragraphs = append(panel.Paragraphs, "当前是私人工作。可以进入项目查看进展，也可以继续在这里处理自己的事情。")
		}
		switch kind {
		case "workers":
			panel.Title = "当前工作的助手"
			workers, e := tx.ListWorkers(source.ScopeID)
			if e != nil {
				return e
			}
			for _, worker := range workers {
				if worker.ContextID == nav.FrontContextID {
					panel.Paragraphs = append(panel.Paragraphs, fmt.Sprintf("执行状态：%s；使用 %s。", teamWorkerStateLabel(worker.State), worker.Backend))
				}
			}
		case "status":
			panel.Title = "当前工作状态"
			inputs, e := tx.ListInbox(source.ScopeID)
			if e != nil {
				return e
			}
			pending, running := 0, 0
			for _, input := range inputs {
				if input.RecipientContextID == nav.FrontContextID {
					if input.State == work.InboxPending {
						pending++
					}
					if input.State == work.InboxClaimed {
						running++
					}
				}
			}
			panel.Paragraphs = append(panel.Paragraphs, fmt.Sprintf("还有 %d 条待处理输入，%d 条正在处理。", pending, running))
			diagnostics, e := teamPrivateDiagnostics(tx, source.ScopeID, nav.FrontContextID)
			if e != nil {
				return e
			}
			panel.Paragraphs = append(panel.Paragraphs, diagnostics...)
		}
		panel.Buttons = append(panel.Buttons, workui.Button{Label: "项目", Action: "private.projects"}, workui.Button{Label: "新建工作", Action: "private.new"}, workui.Button{Label: "已有工作", Action: "private.contexts"}, workui.Button{Label: "模型和设置", Action: "private.settings"}, workui.Button{Label: "工作目录", Action: "private.resources"}, workui.Button{Label: "暂停", Action: "private.stop"}, workui.Button{Label: "继续", Action: "private.resume"})
		if nav.Mode == work.PrivateProject {
			panel.Buttons = append(panel.Buttons, workui.Button{Label: "返回私人工作", Action: "private.return_personal"})
		}
		view := workstore.UIView{ID: id, ScopeID: source.ScopeID, ContextID: nav.FrontContextID, SurfaceKey: "private-menu:" + source.ScopeID, Kind: "private.navigation", TargetID: source.ScopeID, TargetRevision: nav.Revision, Binding: binding, Tenant: source.Tenant, ChatID: source.ChatID, PrincipalID: source.PrincipalID, ReplyToMessageID: source.MessageID}
		if e = workui.ReplaceFromInteraction(tx, &view, source.Authority.InteractionID); e != nil {
			return e
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

func (a *App) handleTeamViewAction(ctx context.Context, interaction workstore.HumanInteraction, view workstore.UIView, action string) error {
	if view.Kind != "private.context-picker" && view.Kind != "host.upgrade-confirm" && view.Kind != "private.navigation" && view.Kind != "bot.settings" && view.Kind != "private.project-picker" && view.Kind != "execution.location-picker" {
		return fmt.Errorf("这个入口不支持此操作。")
	}
	source := teamHumanSource{NavigationRevision: view.TargetRevision, Key: interaction.ID, ScopeID: view.ScopeID, PrincipalID: interaction.PrincipalID, RoleID: interaction.Gateway.RoleID, Tenant: interaction.Envelope.Tenant, ChatID: interaction.Envelope.ChatID, MessageID: interaction.Envelope.MessageID, Gateway: interaction.Gateway, SenderIDs: interaction.Envelope.SenderIDs, Authority: workstore.PrivateActionSource{PersonalScopeID: view.ScopeID, InteractionID: interaction.ID}}
	var err error
	result := ""
	if view.Kind == "private.context-picker" {
		result, err = a.handleTeamHistoryViewAction(ctx, interaction, view, action)
	} else if view.Kind == "host.upgrade-confirm" {
		result, err = a.handleTeamHostViewAction(ctx, interaction, view, action)
	} else if view.Kind == "private.project-picker" || view.Kind == "execution.location-picker" {
		result, err = a.handleTeamPickerAction(ctx, interaction, view, action)
	} else if view.Kind == "bot.settings" {
		result, err = a.handleTeamSettingsView(ctx, source, view, action)
	} else {
		switch action {
		case "private.new", "private.return_personal", "private.enter_project", "private.select_context":
			project, _ := interaction.Envelope.Values["project_scope_id"].(string)
			workContext, _ := interaction.Envelope.Values["context_id"].(string)
			err = a.navigateFromSource(ctx, source, strings.TrimPrefix(action, "private."), project, workContext, view.TargetRevision)
			if err == nil {
				result, err = a.queuePrivatePanel(ctx, source, "menu", "当前工作已切换。")
			}
		case "private.contexts":
			result, err = a.queuePrivatePanel(ctx, source, "contexts", "")
		default:
			result, err = a.handleTeamPrivateViewOperation(ctx, source, view, action)
		}
	}
	if err != nil {
		return err
	}
	if result == "bootstrap_directory_selected" {
		return nil
	}
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, result)
	})
}

func workRevision(value int64) string { return strconv.FormatInt(value, 10) }

func teamCommandError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
