package workdisplay

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func queueMediaInputFeedback(tx *workstore.Tx, scope work.WorkScope, input work.Inbox, source work.IngressRecord, now time.Time) (bool, error) {
	publish := func(kind, title, detail string) (bool, error) {
		_, err := queue(tx, scope.ID, input.RecipientContextID, stableID(kind, input.ID), workservice.MessageProjection{PrivateDisplay: &workservice.PrivateDisplayPublication{SourceIngressID: source.ID}, RoleID: source.Gateway.RoleID, AppID: source.Gateway.AppID, GatewayRef: source.Gateway.GatewayRef, BotGeneration: source.Gateway.BotGeneration, ChatID: source.Envelope.ChatID, Tenant: source.Envelope.Tenant, ReplyToMessageID: source.Envelope.MessageID, Card: feishu.PrivateExecutionNotice(title, detail)}, now)
		return true, err
	}
	cluster, err := tx.StagedMediaCluster(input)
	if err != nil {
		return false, err
	}
	within, err := tx.MediaBatchWithinBudget(cluster)
	if err != nil {
		return false, err
	}
	if !within {
		return publish("private-media-budget", "附件过多，尚未发送", "这批附件超过单次64个或64MiB的处理上限，资料仍已保存。请撤回部分图片或文件，或使用 /stop 清空待处理队列后分批重新发送；每批附件后补充一条文字说明。")
	}
	if input.ContentState == work.InputContentPartial {
		detail := "这条消息有内容或附件未能完整读取；助手只能使用已成功读取的内容。请重新发送缺失的附件。"
		if input.AwaitingText {
			detail = "附件未能完整读取，原始消息已保存，尚未启动助手。请重新发送附件，再补充一条文字说明。"
		}
		return publish("private-input-issue", "部分内容未读取", detail)
	}
	return input.AwaitingText, nil
}
