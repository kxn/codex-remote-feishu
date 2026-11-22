package daemon

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

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
	action, known := parseTeamPrivateCommand(command.Text)
	var receipt string
	if !known {
		receipt, err = a.queuePrivatePanel(ctx, source, "help", "没有识别这个命令，可以使用下面的入口。")
	} else {
		switch action.Kind {
		case control.ActionShowCommandMenu:
			receipt, err = a.queuePrivatePanel(ctx, source, "menu", "")
		case control.ActionShowCommandHelp:
			receipt, err = a.queuePrivatePanel(ctx, source, "help", "")
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
	a.mu.Lock()
	backend := a.workBotSettingsLocked(source.Gateway.GatewayRef).Backend
	a.mu.Unlock()
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
		if e = a.validateTeamPickerSource(tx, source); e != nil {
			return e
		}
		panel, e := teamPrivateMenuPanel(tx, source, nav, backend, kind, note)
		if e != nil {
			return e
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
		view := workstore.UIView{ID: id, ScopeID: source.ScopeID, ContextID: nav.FrontContextID, Kind: "private.navigation", TargetID: source.ScopeID, TargetRevision: nav.Revision, Binding: binding, Tenant: source.Tenant, ChatID: source.ChatID, PrincipalID: source.PrincipalID, ReplyToMessageID: source.MessageID}
		if e = workui.PrivateNavigationFromInteraction(tx, &view, source.Authority.InteractionID); e != nil {
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
	if view.Kind != "private.context-picker" && view.Kind != "host.upgrade-confirm" && view.Kind != "private.navigation" && view.Kind != "bot.settings" && view.Kind != "private.project-picker" && view.Kind != "execution.location-picker" && view.Kind != "host.result" && view.Kind != "host.upgrade-status" && view.Kind != "file.result" {
		return fmt.Errorf("这个入口不支持此操作。")
	}
	source := teamHumanSource{NavigationRevision: view.TargetRevision, Key: interaction.ID, ScopeID: view.ScopeID, PrincipalID: interaction.PrincipalID, RoleID: interaction.Gateway.RoleID, Tenant: interaction.Envelope.Tenant, ChatID: interaction.Envelope.ChatID, MessageID: interaction.Envelope.MessageID, Gateway: interaction.Gateway, SenderIDs: interaction.Envelope.SenderIDs, Authority: workstore.PrivateActionSource{PersonalScopeID: view.ScopeID, InteractionID: interaction.ID}}
	var err error
	result := ""
	if action == "private.menu" || action == "private.basic" || action == "private.workspace" || action == "private.tools" || action == "private.admin" || action == "private.settings" && view.Kind != "private.navigation" {
		if _, err = a.teamFrozenPickerValues(ctx, interaction, view, action); err != nil {
			return err
		}
		if action == "private.settings" {
			result, err = a.queueTeamSettings(ctx, source, "overview", "")
		} else {
			result, err = a.queuePrivatePanel(ctx, source, strings.TrimPrefix(action, "private."), "")
		}
	} else if view.Kind == "private.context-picker" {
		result, err = a.handleTeamHistoryViewAction(ctx, interaction, view, action)
	} else if view.Kind == "host.upgrade-confirm" {
		result, err = a.handleTeamHostViewAction(ctx, interaction, view, action)
	} else if view.Kind == "private.project-picker" || view.Kind == "execution.location-picker" {
		result, err = a.handleTeamPickerAction(ctx, interaction, view, action)
	} else if view.Kind == "bot.settings" {
		result, err = a.handleTeamSettingsView(ctx, source, view, action)
	} else {
		if _, err = a.teamFrozenPickerValues(ctx, interaction, view, action); err != nil {
			return err
		}
		result, err = a.handleTeamPrivateViewOperation(ctx, source, view, action, interaction.Envelope.Form)
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
