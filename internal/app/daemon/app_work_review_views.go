package daemon

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const teamReviewPageRunes = 2800

type teamReviewPresentation struct {
	AttemptID string
	Context   work.WorkContext
	BodyRef   string
	Failed    bool
}

func loadTeamReviewPresentation(tx *workstore.Tx, attemptID string) (teamReviewPresentation, error) {
	p := teamReviewPresentation{AttemptID: attemptID}
	attempt, err := tx.GetAttempt(attemptID)
	if err != nil {
		return p, err
	}
	p.Context, err = tx.GetContext(attempt.ContextID)
	if err != nil {
		return p, err
	}
	if p.Context.Purpose != work.PurposeReview || p.Context.Review == nil {
		return p, workstore.ErrScopeDenied
	}
	result, err := tx.GetReviewResult(attemptID)
	if err == nil {
		if !result.Delivered {
			return p, workstore.ErrConflict
		}
		p.BodyRef = result.BodyRef
		return p, nil
	}
	if !errors.Is(err, workstore.ErrNotFound) {
		return p, err
	}
	failure, err := tx.GetMessage("review-failure:" + attemptID)
	if err != nil {
		return p, err
	}
	if failure.ScopeID != attempt.ScopeID || failure.Source != (work.Actor{Kind: work.ActorSystem, ID: "review-failure"}) {
		return p, workstore.ErrScopeDenied
	}
	p.BodyRef, p.Failed = failure.BodyRef, true
	return p, nil
}

func reviewPresentationDestination(tx *workstore.Tx, p teamReviewPresentation) (workstore.UIView, error) {
	scope, err := tx.GetScope(p.Context.ScopeID)
	if err != nil {
		return workstore.UIView{}, err
	}
	if scope.Kind != work.ScopePersonal || scope.RoleID != p.Context.RoleID {
		return workstore.UIView{}, workstore.ErrScopeDenied
	}
	binding, err := tx.GetRoleBotBinding(scope.RoleID)
	if err != nil {
		return workstore.UIView{}, err
	}
	view := workstore.UIView{ScopeID: scope.ID, ContextID: p.Context.Review.SourceContextID, Kind: "review.result", TargetID: p.AttemptID, TargetRevision: 1, Binding: binding, PrincipalID: scope.PrincipalID}
	var gateway work.IngressGateway
	switch source := p.Context.Review.Source; source.Kind {
	case "private_command":
		command, e := tx.GetPrivateCommand(source.ID)
		if e != nil {
			return view, e
		}
		if command.ScopeID != scope.ID || command.PrincipalID != scope.PrincipalID || command.ContextID != view.ContextID {
			return view, workstore.ErrScopeDenied
		}
		raw, e := tx.GetIngress(command.IngressID)
		if e != nil {
			return view, e
		}
		gateway = raw.Gateway
		view.Tenant, view.ChatID = raw.Envelope.Tenant, raw.Envelope.ChatID
	case "human_interaction":
		action, e := tx.GetHumanInteraction(source.ID)
		if e != nil {
			return view, e
		}
		if action.PrincipalID != scope.PrincipalID {
			return view, workstore.ErrScopeDenied
		}
		gateway = action.Gateway
		view.Tenant, view.ChatID = action.Envelope.Tenant, action.Envelope.ChatID
	case "sdk_menu":
		menu, e := tx.GetMenuInteraction(source.ID)
		if e != nil {
			return view, e
		}
		if menu.ScopeID != scope.ID || menu.PrincipalID != scope.PrincipalID {
			return view, workstore.ErrScopeDenied
		}
		gateway = menu.Gateway
		view.Tenant, view.ChatID = menu.Envelope.Tenant, menu.ChatID
	default:
		return view, fmt.Errorf("review has no authenticated private return destination")
	}
	if gateway.RoleID != binding.RoleID || gateway.AppID != binding.AppID || gateway.BotGeneration != binding.IdentityGeneration || gateway.GatewayRef != binding.GatewayRef || view.ChatID == "" {
		return view, workstore.ErrScopeDenied
	}
	return view, nil
}

func queueTeamReviewPage(tx *workstore.Tx, p teamReviewPresentation, view workstore.UIView, page int, interactionID string) (workstore.UIView, error) {
	body, err := tx.GetContent(p.Context.ScopeID, p.BodyRef)
	if err != nil {
		return view, err
	}
	text := []rune(string(body.Data))
	pages := max(1, (len(text)+teamReviewPageRunes-1)/teamReviewPageRunes)
	if page < 0 || page >= pages {
		return view, fmt.Errorf("评审页码已失效")
	}
	title := "只读审查结果"
	if p.Failed {
		title = "只读审查未完成"
	}
	scopeLabel := "发起时选定的范围"
	spec := p.Context.Review
	if spec.Target.CommitSHA != "" {
		scopeLabel = "指定提交：" + spec.Target.CommitSHA
	} else if spec.Target.Branch != "" {
		scopeLabel = "相对分支：" + spec.Target.Branch
	} else if spec.Target.Instructions != "" {
		scopeLabel = "您指定的自定义范围"
	} else {
		scopeLabel = "发起时选定目录的未提交改动"
	}
	panel := workui.Panel{Title: title, Paragraphs: []string{scopeLabel, fmt.Sprintf("已保存原文 · 第 %d / %d 页", page+1, pages), string(text[page*teamReviewPageRunes : min(len(text), (page+1)*teamReviewPageRunes)]), "这份结果不代表任务验收。要继续讨论或提出修改要求，可先返回发起这次审查的工作。"}, Buttons: []workui.Button{{Label: "回到发起工作", Action: "review.return"}}}
	if page > 0 {
		panel.Buttons = append(panel.Buttons, workui.Button{Label: "上一页", Action: "review.page", Values: map[string]any{"page": page - 1}})
	}
	if page+1 < pages {
		panel.Buttons = append(panel.Buttons, workui.Button{Label: "下一页", Action: "review.page", Values: map[string]any{"page": page + 1}})
	}
	if view.ID == "" {
		view.ID = "review-presentation:" + p.AttemptID
	}
	if view.SurfaceKey == "" {
		view.SurfaceKey = "review-presentation:" + p.AttemptID
	}
	view.ReplacesViewID = ""
	view.ExternalMessageID = ""
	view.Invalidated = false
	view.Revision = 0
	if interactionID != "" {
		view.ID = workui.ID("review-page", p.AttemptID, interactionID, strconv.Itoa(page))
		if err = workui.ReplaceFromInteraction(tx, &view, interactionID); err != nil {
			return view, err
		}
	}
	card, err := workui.Card(view.ID, panel)
	if err != nil {
		return view, err
	}
	return workui.Queue(tx, view, card)
}

func (a *App) runTeamReviewViews(ctx context.Context) error {
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		ids, err := tx.PendingReviewPresentations(32)
		if err != nil {
			return err
		}
		for _, id := range ids {
			p, e := loadTeamReviewPresentation(tx, id)
			if e != nil {
				return e
			}
			view, e := reviewPresentationDestination(tx, p)
			if errors.Is(e, workstore.ErrScopeDenied) {
				if e = tx.MarkReviewPresentationUnavailable(id, "原 Bot 身份已变化；结果保留，可通过本人工作记录重新查看"); e != nil {
					return e
				}
				continue
			}
			if e != nil {
				return e
			}
			if _, e = queueTeamReviewPage(tx, p, view, 0, ""); e != nil {
				return e
			}
		}
		return nil
	})
}

func (a *App) handleTeamReviewView(ctx context.Context, interaction workstore.HumanInteraction, view workstore.UIView, action string) error {
	var p teamReviewPresentation
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if e := tx.ValidateViewInteraction(view, interaction); e != nil {
			return e
		}
		var e error
		p, e = loadTeamReviewPresentation(tx, view.TargetID)
		if e != nil {
			return e
		}
		if p.Context.ScopeID != view.ScopeID || view.ContextID != p.Context.Review.SourceContextID {
			return workstore.ErrScopeDenied
		}
		if action == "review.page" {
			value, ok := interaction.Envelope.Values["page"]
			if !ok {
				return fmt.Errorf("缺少评审页码")
			}
			page, e := strconv.Atoi(fmt.Sprint(value))
			if e != nil {
				return e
			}
			if _, e = queueTeamReviewPage(tx, p, view, page, interaction.ID); e != nil {
				return e
			}
			return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, "review_page")
		}
		if action != "review.return" {
			return fmt.Errorf("这个评审操作已不可用")
		}
		return nil
	})
	if err != nil || action != "review.return" {
		return err
	}
	var revision int64
	err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		nav, e := tx.GetPrivateNavigation(view.ScopeID)
		revision = nav.Revision
		return e
	})
	if err != nil {
		return err
	}
	source := teamHumanSource{Key: interaction.ID, ScopeID: view.ScopeID, PrincipalID: interaction.PrincipalID, RoleID: interaction.Gateway.RoleID, Tenant: view.Tenant, ChatID: view.ChatID, MessageID: interaction.Envelope.MessageID, Gateway: interaction.Gateway, SenderIDs: interaction.Envelope.SenderIDs, NavigationRevision: revision, Authority: workstore.PrivateActionSource{PersonalScopeID: view.ScopeID, InteractionID: interaction.ID}}
	if err = a.navigateFromSource(ctx, source, "select_context", "", view.ContextID, revision); err != nil {
		return err
	}
	if _, err = a.queuePrivatePanel(ctx, source, "menu", "已回到发起这次审查的工作。审查结果已保存；修改代码仍以您的明确要求为准。"); err != nil {
		return err
	}
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, strings.TrimSpace(action))
	})
}

// Explicit history access uses this new, authenticated destination; it does
// not inherit a previous App generation's private chat address.
func (a *App) queueTeamReviewHistory(ctx context.Context, source teamHumanSource, contextID string, page int) (bool, string, error) {
	handled := false
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
		if wc.Purpose != work.PurposeReview {
			return nil
		}
		id, e := tx.LatestReviewPresentation(contextID)
		if e == workstore.ErrNotFound {
			return nil
		}
		if e != nil {
			return e
		}
		p, e := loadTeamReviewPresentation(tx, id)
		if e != nil {
			return e
		}
		binding, e := tx.GetRoleBotBinding(source.RoleID)
		if e != nil {
			return e
		}
		view := workstore.UIView{ScopeID: source.ScopeID, ContextID: p.Context.Review.SourceContextID, Kind: "review.result", TargetID: id, TargetRevision: 1, Binding: binding, PrincipalID: source.PrincipalID, Tenant: source.Tenant, ChatID: source.ChatID}
		// A fresh opening must be a new visible message, not a hidden overwrite of
		// a result whose proactive publication may belong to an older identity.
		view.ID = workui.ID("review-history", id, source.Key, strconv.Itoa(page))
		view.SurfaceKey = "review-history:" + id
		// queueTeamReviewPage freezes this explicit open as a page identity. Only
		// genuine card interactions can request inline replacement.
		opened, e := queueTeamReviewPage(tx, p, view, page, source.Authority.InteractionID)
		if e == nil {
			_ = opened
			handled = true
		}
		return e
	})
	return handled, "已打开保存的只读审查结果。", err
}
