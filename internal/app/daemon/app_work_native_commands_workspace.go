package daemon

import (
	"context"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"time"
)

// Workspace commands become real human work in the selected private context.
// The Agent uses its normal constrained tools and reports the actual result;
// the daemon never fabricates a legacy surface or performs hidden Git writes.
func (a *App) queueTeamWorkspaceWork(ctx context.Context, source teamHumanSource, action control.Action) (string, error) {
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		scope, proof, e := tx.ValidatePrivateAction(source.Authority)
		if e != nil {
			return e
		}
		switch {
		case source.Authority.CommandID != "":
			command, e := tx.GetPrivateCommand(source.Authority.CommandID)
			if e != nil {
				return e
			}
			if command.Text != action.Text || command.Text == "" {
				return workstore.ErrScopeDenied
			}
		case source.Authority.InteractionID != "":
			click, e := tx.GetHumanInteraction(source.Authority.InteractionID)
			if e != nil {
				return e
			}
			viewID, _ := click.Envelope.Values["team_view"].(string)
			view, e := tx.GetUIView(viewID)
			if e != nil {
				return e
			}
			elements, e := teamFrozenCardElements(tx, view, click)
			if e != nil {
				return e
			}
			callback, _ := click.Envelope.Values["action"].(string)
			if teamFrozenCallback(elements, click.Envelope.Values, callback) == nil {
				return workstore.ErrScopeDenied
			}
			fromForm, known, e := teamMenuToolCommand(callback, click.Envelope.Form)
			if e != nil || !known || fromForm.Kind != action.Kind || fromForm.Text != action.Text {
				return workstore.ErrScopeDenied
			}
		default:
			return workstore.ErrScopeDenied
		}
		wc, _, e := worksettings.FrozenContext(tx, source.Authority)
		if e != nil {
			return e
		}
		if wc.Purpose == work.PurposeReview || wc.Purpose == work.PurposeControl {
			return workstore.ErrScopeDenied
		}
		resourceScope := wc.ScopeID
		if wc.ProjectViewScopeID != "" {
			resourceScope = wc.ProjectViewScopeID
			if e = tx.RequireProjectPrincipalAccess(resourceScope, scope.PrincipalID, time.Now().UTC()); e != nil {
				return e
			}
		}
		preference, e := tx.GetExecutionPreference(resourceScope)
		if e != nil {
			return fmt.Errorf("请先用 /workspace 选择允许操作的目录：%w", e)
		}
		if len(preference.SelectedLocationIDs) == 0 {
			return fmt.Errorf("请先用 /workspace 选择允许操作的目录")
		}
		id := teamID("human-workspace-work", source.Key)
		now := time.Now().UTC()
		if _, e = tx.GetMessage(id); e == nil {
			return nil
		} else if e != workstore.ErrNotFound {
			return e
		}
		body := work.NewContent(id+":body", scope.ID, "text/plain", []byte(action.Text), now)
		if e = tx.PutContent(body); e != nil {
			return e
		}
		message := work.Message{ID: id, ScopeID: scope.ID, ConversationID: wc.ID, Source: work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}, DerivedFrom: &proof, BodyRef: body.ID, Revision: 1, CreatedAt: now}
		input := work.Inbox{ID: id + ":input", ScopeID: scope.ID, RecipientRoleID: scope.RoleID, RecipientContextID: wc.ID, EventID: id, MessageID: message.ID, State: work.InboxPending, Revision: 1, AvailableAt: now}
		_, _, e = tx.AcceptMessage(message, []work.Inbox{input})
		return e
	})
	if err != nil {
		return "", err
	}
	return a.queuePrivatePanel(ctx, source, "status", "已将工作目录操作加入当前会话，等待助手处理。")
}
