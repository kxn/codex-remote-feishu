package daemon

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func teamWorkerStateLabel(state work.WorkerState) string {
	switch state {
	case work.WorkerPreparing:
		return "正在准备"
	case work.WorkerReady:
		return "已就绪"
	case work.WorkerBusy:
		return "正在处理"
	case work.WorkerMaintaining:
		return "正在维护会话"
	case work.WorkerRecovering:
		return "正在核对恢复状态"
	case work.WorkerHandoff:
		return "正在交接"
	case work.WorkerUnavailable:
		return "暂不可用"
	case work.WorkerRetired:
		return "已退役"
	}
	return "状态待核对"
}
func teamPrivateDiagnostics(tx *workstore.Tx, scopeID, contextID string) ([]string, error) {
	problems, err := tx.ListDispatchProblems(scopeID)
	if err != nil {
		return nil, err
	}
	lines := []string{}
	for _, problem := range problems {
		if contextID != "" && problem.ContextID != "" && problem.ContextID != contextID {
			continue
		}
		lines = append(lines, problem.Reason)
		if len(lines) == 5 {
			break
		}
	}
	return lines, nil
}
func (a *App) queueTeamContexts(ctx context.Context, source teamHumanSource, page int) (string, error) {
	return a.queueTeamContextsNotice(ctx, source, page, "")
}

func (a *App) queueTeamContextsNotice(ctx context.Context, source teamHumanSource, page int, note string) (string, error) {
	if page < 0 {
		return "", fmt.Errorf("无效页码")
	}
	panel := workui.Panel{Title: "已有会话", Paragraphs: []string{"「查看」只打开历史。「切换」决定下一条私聊进入的会话。"}}
	if note != "" {
		panel.Paragraphs = append([]string{note}, panel.Paragraphs...)
	}
	var navigation work.PrivateNavigation
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if e := a.validateTeamPickerSource(tx, source); e != nil {
			return e
		}
		var e error
		navigation, e = tx.GetPrivateNavigation(source.ScopeID)
		if e != nil {
			return e
		}
		contexts, e := tx.ListContexts(source.ScopeID)
		if e != nil {
			return e
		}
		visible := contexts[:0]
		for _, wc := range contexts {
			if wc.Purpose != work.PurposeControl {
				visible = append(visible, wc)
			}
		}
		contexts = visible
		sort.SliceStable(contexts, func(i, j int) bool {
			if contexts[i].CreatedAt.Equal(contexts[j].CreatedAt) {
				return contexts[i].ID < contexts[j].ID
			}
			return contexts[i].CreatedAt.After(contexts[j].CreatedAt)
		})
		start := page * teamPickerPageSize
		if start > len(contexts) {
			return workstore.ErrConflict
		}
		end := min(start+teamPickerPageSize, len(contexts))
		for _, wc := range contexts[start:end] {
			title := "私人工作"
			if wc.Purpose == work.PurposeReview {
				title = "只读审查"
			}
			if strings.HasPrefix(wc.Workstream, "legacy:") {
				title = "已认领的既有工作"
			}
			if wc.ProjectViewScopeID != "" {
				title = "项目相关工作"
				if tx.RequireProjectPrincipalAccess(wc.ProjectViewScopeID, source.PrincipalID, time.Now().UTC()) != nil {
					title += "（访问需重新核对）"
				} else {
					scope, e := tx.GetScope(wc.ProjectViewScopeID)
					if e != nil {
						return e
					}
					project, e := tx.GetProject(scope.ProjectID)
					if e != nil {
						return e
					}
					title = project.Name
				}
			}
			if wc.ID == navigation.FrontContextID {
				title = "当前：" + title
			}
			if !wc.CreatedAt.IsZero() {
				title += " · " + wc.CreatedAt.Local().Format("01-02 15:04")
			}
			if wc.Purpose != work.PurposeReview {
				panel.Buttons = append(panel.Buttons, workui.Button{Label: "切换到 " + title, Action: "history.select-context", Values: map[string]any{"context_id": wc.ID}})
			}
			panel.Buttons = append(panel.Buttons, workui.Button{Label: "查看：" + title, Action: "history.read", Values: map[string]any{"context_id": wc.ID, "page": "0"}})
		}
		if len(contexts) == 0 {
			panel.Paragraphs = append(panel.Paragraphs, "还没有已有工作。发送一句需求即可开始。")
		}
		if page > 0 {
			panel.Buttons = append(panel.Buttons, workui.Button{Label: "上一页", Action: "history.contexts-page", Values: map[string]any{"page": strconv.Itoa(page - 1)}})
		}
		if end < len(contexts) {
			panel.Buttons = append(panel.Buttons, workui.Button{Label: "下一页", Action: "history.contexts-page", Values: map[string]any{"page": strconv.Itoa(page + 1)}})
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	panel.Buttons = append(panel.Buttons, workui.Button{Label: "返回工作区与会话", Action: "private.workspace"})
	source.NavigationRevision = navigation.Revision
	return a.queueTeamPicker(ctx, source, "private.context-picker", source.ScopeID, navigation.Revision, panel)
}
func (a *App) handleTeamHistoryViewAction(ctx context.Context, interaction workstore.HumanInteraction, view workstore.UIView, action string) (string, error) {
	if view.Kind != "private.context-picker" {
		return "", workstore.ErrScopeDenied
	}
	values, err := a.teamFrozenPickerValues(ctx, interaction, view, action)
	if err != nil {
		return "", err
	}
	source := teamHumanSource{NavigationRevision: view.TargetRevision, Key: interaction.ID, ScopeID: view.ScopeID, PrincipalID: interaction.PrincipalID, RoleID: interaction.Gateway.RoleID, Tenant: view.Tenant, ChatID: view.ChatID, MessageID: interaction.Envelope.MessageID, Gateway: interaction.Gateway, SenderIDs: interaction.Envelope.SenderIDs, Authority: workstore.PrivateActionSource{PersonalScopeID: view.ScopeID, InteractionID: interaction.ID}}
	contextID, _ := values["context_id"].(string)
	if action == "history.select-context" {
		if err = a.navigateFromSource(ctx, source, "select_context", "", contextID, view.TargetRevision); err != nil {
			return "", err
		}
		return a.queueTeamContextsNotice(ctx, source, 0, "已切换会话。下一条消息会进入所选会话。")
	}
	text, _ := values["page"].(string)
	page, err := strconv.Atoi(text)
	if err != nil {
		return "", err
	}
	if action == "history.contexts-page" {
		return a.queueTeamContexts(ctx, source, page)
	}
	if action == "history.read" {
		return a.queueTeamContextHistory(ctx, source, contextID, page)
	}
	return "", workstore.ErrScopeDenied
}
