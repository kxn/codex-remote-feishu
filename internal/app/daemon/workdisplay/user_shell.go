package workdisplay

import (
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// APPLAUSE is the original queued-input injection gesture, not a shell console.
// Successful payload reads only update the source reaction; never echo the
// private structured payload, attachment paths, or a fabricated model final.
func ProjectPrivateShellResult(tx *workstore.Tx, operation workstore.PrivateShellOperation, event agentproto.Event, now time.Time) error {
	title, body := "", ""
	switch event.Kind {
	case agentproto.EventNativeShellRPCResult:
		if operation.State == "completed" || operation.State == "running" {
			return nil
		}
		return ProjectPrivateShellStatus(tx, operation, now)
	case agentproto.EventNativeShellItemObserved:
		if event.Action != "completed" {
			return nil
		}
		if event.Status == "completed" && shellExitCodeZero(event.Metadata["exitCode"]) {
			return settleShellNotices(tx, operation, now)
		}
		title = "鼓掌追加未完成"
		code := "未提供"
		if value, ok := event.Metadata["exitCode"]; ok {
			code = fmt.Sprint(value)
		}
		body = fmt.Sprintf("原生退出状态：%s（退出码：%s）。\n未能确认这组待办完整送达。原消息已保留待核对，不会重复发送；其他消息仍可继续。", event.Status, code)
	default:
		return nil
	}
	return queueShellNotice(tx, operation, event.Status, title, body, "")
}

// ProjectPrivateShellStatus projects saved ACK outcomes without inventing native events.
func ProjectPrivateShellStatus(tx *workstore.Tx, operation workstore.PrivateShellOperation, now time.Time) error {
	switch operation.State {
	case "rejected":
		return queueShellNotice(tx, operation, "rejected", "鼓掌追加未被接收", "这次追加没有送达原生。原消息仍在队列中，可改用点赞追加或等待下一轮。", "")
	case "unknown":
		// Native receipt is already known; waiting for the fixed read to finish
		// is an ordinary intermediate state, not a reason to publish a notice.
		if operation.Acknowledged {
			return nil
		}
		return queueShellNotice(tx, operation, "unknown", "鼓掌追加结果待核对", "尚未确认原生是否收到这组待办。原消息保留待核对，不会重复发送。", "")
	}
	return nil
}

func queueShellNotice(tx *workstore.Tx, operation workstore.PrivateShellOperation, status, title, body, replaces string) error {
	binding, err := tx.GetRoleBotBinding(operation.Binding.RoleID)
	if err != nil {
		return err
	}
	if binding != operation.Binding {
		return nil
	}
	input, err := tx.GetInbox(operation.InputID)
	if err != nil {
		return err
	}
	message, err := tx.GetMessage(input.MessageID)
	if err != nil {
		return err
	}
	if message.PlatformKey == nil || message.ScopeID != operation.ScopeID {
		return workstore.ErrScopeDenied
	}
	for index, card := range feishu.PrivateExecutionReply(title, body, nil, nil) {
		key := stableID("shell-notice", operation.ID, status, fmt.Sprint(index))
		if _, err = workui.Queue(tx, workstore.UIView{ID: key, ReplacesViewID: replaces, SurfaceKey: "private-shell-notice:" + operation.ID + ":" + fmt.Sprint(index), ScopeID: operation.ScopeID, ContextID: operation.ContextID, Kind: "private.shell-notice", TargetID: operation.ID, TargetRevision: operation.Revision, Binding: operation.Binding, PrincipalID: operation.PrincipalID, Tenant: message.PlatformKey.Tenant, ChatID: message.PlatformKey.ChatID, ReplyToMessageID: message.PlatformKey.MessageID}, card); err != nil {
			return err
		}
	}
	return nil
}
func shellExitCodeZero(value any) bool {
	switch code := value.(type) {
	case int:
		return code == 0
	case int64:
		return code == 0
	case float64:
		return code == 0
	}
	return false
}
func settleShellNotices(tx *workstore.Tx, operation workstore.PrivateShellOperation, now time.Time) error {
	views, err := tx.ListUIViews(operation.ScopeID)
	if err != nil {
		return err
	}
	for _, view := range views {
		if view.Kind != "private.shell-notice" || view.TargetID != operation.ID || view.Invalidated || view.ReplacesViewID != "" {
			continue
		}
		op, e := tx.GetOperation(view.OperationID)
		if e != nil {
			return e
		}
		if view.ExternalMessageID != "" {
			if op.State == work.OperationConfirmed && view.ReplacesViewID == "" {
				if e = queueShellNotice(tx, operation, "confirmed", "已确认追加", "这组待办已确认追加到原生。", view.ID); e != nil {
					return e
				}
			}
			continue
		}
		// An unknown create/PATCH may already have reached Feishu. Freeze it;
		// never manufacture a replacement or repeat that uncertain write.
		if op.State != work.OperationPrepared {
			continue
		}
		before := view.Revision
		view.Revision++
		view.Invalidated = true
		if err = tx.PutUIView(view, before); err != nil {
			return err
		}
	}
	return nil
}
