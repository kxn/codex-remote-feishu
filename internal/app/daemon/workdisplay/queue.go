package workdisplay

import (
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func QueueIfBusy(tx *workstore.Tx, scope work.WorkScope, input work.Inbox, source work.IngressRecord, now time.Time) error {
	if scope.Kind != work.ScopePersonal || source.Envelope.ChatType != "p2p" || input.RecipientContextID == "" || source.Route.ScopeID != scope.ID {
		return nil
	}
	prefs, err := tx.GetContextPreferences(input.RecipientContextID)
	if err != nil && !errors.Is(err, workstore.ErrNotFound) {
		return err
	}
	if prefs.Verbose != nil && state.SurfaceVerbosity(*prefs.Verbose) == state.SurfaceVerbosityQuiet {
		return nil
	}
	workers, err := tx.ListWorkers(scope.ID)
	if err != nil {
		return err
	}
	for _, worker := range workers {
		if worker.ContextID != input.RecipientContextID {
			continue
		}
		_, err := tx.GetOccupiedAttemptForWorker(worker.ID)
		if errors.Is(err, workstore.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		_, err = queue(tx, scope.ID, input.RecipientContextID, stableID("private-queue", input.ID), workservice.MessageProjection{
			PrivateDisplay: &workservice.PrivateDisplayPublication{SourceIngressID: source.ID},
			RoleID:         source.Gateway.RoleID, AppID: source.Gateway.AppID, GatewayRef: source.Gateway.GatewayRef, BotGeneration: source.Gateway.BotGeneration,
			ChatID: source.Envelope.ChatID, Tenant: source.Envelope.Tenant, ReplyToMessageID: source.Envelope.MessageID,
			Card: feishu.PrivateExecutionNotice("已加入队列", "已收到这条消息，当前会话仍在处理上一条请求。"),
		}, now)
		return err
	}
	return nil
}
