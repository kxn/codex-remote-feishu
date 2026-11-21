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
	if page < 0 {
		return "", fmt.Errorf("无效页码")
	}
	panel := workui.Panel{Title: "选择已有工作", Paragraphs: []string{"查看记录不会接管或恢复旧的原生执行。点击继续会选择这项工作，后台的其他工作仍按各自状态进行。"}}
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
				panel.Buttons = append(panel.Buttons, workui.Button{Label: title, Action: "history.select-context", Values: map[string]any{"context_id": wc.ID}})
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
	source.NavigationRevision = navigation.Revision
	return a.queueTeamPicker(ctx, source, "private.context-picker", source.ScopeID, navigation.Revision, panel)
}
func (a *App) queueTeamContextHistory(ctx context.Context, source teamHumanSource, contextID string, page int) (string, error) {
	if handled, note, err := a.queueTeamReviewHistory(ctx, source, contextID, page); handled || err != nil {
		return note, err
	}
	if page < 0 {
		return "", fmt.Errorf("无效页码")
	}
	panel := workui.Panel{Title: "工作讨论记录", Paragraphs: []string{"这里展示这项工作的输入和回复。查看不会重新启动旧会话。"}}
	var nav work.PrivateNavigation
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if e := a.validateTeamPickerSource(tx, source); e != nil {
			return e
		}
		wc, e := tx.GetContext(contextID)
		if e != nil {
			return e
		}
		if wc.ScopeID != source.ScopeID || wc.RoleID != source.RoleID {
			return workstore.ErrScopeDenied
		}
		nav, e = tx.GetPrivateNavigation(source.ScopeID)
		if e != nil {
			return e
		}
		inputs, e := tx.ListInbox(source.ScopeID)
		if e != nil {
			return e
		}
		type item struct {
			Message work.Message
			State   work.InboxState
		}
		items := []item{}
		seen := map[string]bool{}
		for _, input := range inputs {
			if input.RecipientContextID != contextID || input.MessageID == "" || seen[input.MessageID] {
				continue
			}
			message, e := tx.GetMessage(input.MessageID)
			if e != nil {
				return e
			}
			if message.ScopeID != source.ScopeID {
				return workstore.ErrScopeDenied
			}
			seen[message.ID] = true
			items = append(items, item{message, input.State})
		}
		sort.SliceStable(items, func(i, j int) bool { return items[i].Message.CreatedAt.Before(items[j].Message.CreatedAt) })
		start := page * teamPickerPageSize
		if start > len(items) {
			return workstore.ErrConflict
		}
		end := min(start+teamPickerPageSize, len(items))
		for _, entry := range items[start:end] {
			body, e := tx.GetContent(source.ScopeID, entry.Message.BodyRef)
			if e != nil {
				return e
			}
			who := "系统"
			switch entry.Message.Source.Kind {
			case work.ActorPrincipal:
				who = "你"
			case work.ActorRole:
				who = "助手"
			}
			text := string(body.Data)
			if runes := []rune(text); len(runes) > 1400 {
				text = string(runes[:1400]) + "…"
			}
			if entry.Message.RecalledEvidenceRef != "" {
				text = "[已撤回，保留审计记录]\n" + text
			}
			panel.Paragraphs = append(panel.Paragraphs, who+"：\n"+text)
			for _, attachment := range entry.Message.Attachments {
				panel.Paragraphs = append(panel.Paragraphs, "附件："+attachment.FileName)
			}
		}
		if len(items) == 0 {
			panel.Paragraphs = append(panel.Paragraphs, "这项工作还没有新的讨论记录。")
		}
		if page > 0 {
			panel.Buttons = append(panel.Buttons, workui.Button{Label: "上一页", Action: "history.read", Values: map[string]any{"context_id": contextID, "page": strconv.Itoa(page - 1)}})
		}
		if end < len(items) {
			panel.Buttons = append(panel.Buttons, workui.Button{Label: "下一页", Action: "history.read", Values: map[string]any{"context_id": contextID, "page": strconv.Itoa(page + 1)}})
		}
		if wc.Purpose != work.PurposeReview && wc.Purpose != work.PurposeControl {
			panel.Buttons = append(panel.Buttons, workui.Button{Label: "选择这项工作继续", Action: "history.select-context", Values: map[string]any{"context_id": contextID}})
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if page == 0 {
		legacy, e := a.teamClaimedLegacyHistory(ctx, source, contextID)
		if e != nil {
			return "", e
		}
		panel.Paragraphs = append(panel.Paragraphs, legacy...)
	}
	source.NavigationRevision = nav.Revision
	return a.queueTeamPicker(ctx, source, "private.context-picker", source.ScopeID, nav.Revision, panel)
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
		return a.queuePrivatePanel(ctx, source, "menu", "已选择这项工作；没有擅自恢复任何旧的原生执行。")
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
