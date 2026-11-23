package daemon

import (
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

var errTeamPrivateShellUnsent = errors.New("发送前执行状态发生变化，这次命令没有发送。原消息仍在队列中，请确认当前执行后重新鼓掌。")

var errTeamReactionUnsupported = errors.New("当前执行后端暂不支持这种轮内追加方式，这条消息仍保留在队列中。")

// This readonly, append-only notice has its own exact UI publication receipt;
// it does not join the private navigation surface or borrow execution authority.
func queueTeamReactionUnsupported(tx *workstore.Tx, action workstore.PrivateReactionAction, source teamHumanSource, message string) error {
	if _, _, err := tx.ValidatePrivateReactionAction(action.ID); err != nil {
		return nil
	}
	input, err := tx.GetInbox(action.InputID)
	if err != nil {
		return err
	}
	if input.State != work.InboxPending {
		return nil
	}
	id := teamID("reaction-notice", action.ID)
	card, err := workui.Card(id, workui.Panel{Title: "未追加到当前执行", Paragraphs: []string{message}})
	if err != nil {
		return err
	}
	_, err = workui.Queue(tx, workstore.UIView{ID: id, SurfaceKey: "reaction-notice:" + action.ID, ScopeID: action.ScopeID, ContextID: action.ContextID, Kind: "private.reaction-notice", TargetID: action.Binding.RoleID, TargetRevision: action.NavigationRevision, Binding: action.Binding, PrincipalID: action.PrincipalID, Tenant: source.Tenant, ChatID: source.ChatID, ReplyToMessageID: source.MessageID}, card)
	return err
}
