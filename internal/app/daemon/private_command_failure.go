package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

type privateNoInstanceReceipt struct {
	Version                        int
	EffectID, CommandID, SurfaceID string
}
type privateNoInstanceReducer struct{ host *App }

func (r privateNoInstanceReducer) ReduceHostReceipt(app *privatechat.App, e privatechat.Effect, raw json.RawMessage) ([]eventcontract.Event, error) {
	if err := (privateHostReducer{r.host}).check(app); err != nil {
		return nil, err
	}
	event, _, target, err := privatechat.DecodeStoredEffectTarget(e.Body)
	if err != nil {
		return nil, err
	}
	var receipt privateNoInstanceReceipt
	if json.Unmarshal(raw, &receipt) != nil || receipt.Version != 1 || receipt.EffectID != e.ID || event.Command == nil || event.Command.CommandID != receipt.CommandID || event.SurfaceSessionID != receipt.SurfaceID || target == nil || *target != "" {
		return nil, privatechat.ErrJournalConflict
	}
	return app.Service().HandleCommandDispatchFailure(receipt.SurfaceID, receipt.CommandID, agentproto.ErrorInfo{Code: "no_attached_instance", Layer: "daemon", Stage: "dispatch_prepare", Operation: string(event.Command.Kind), Message: "当前飞书会话还没有接管实例。", SurfaceSessionID: receipt.SurfaceID, CommandID: receipt.CommandID}), nil
}
func (r *privateWorkRunner) failNoInstance(ctx context.Context, work privatechat.PendingEffectWork) error {
	if work.Effect.State == "confirmed" {
		_, err := r.processor.ApplyLocalHostReceipt(ctx, work.Owner, work.Generation, work.Effect.Effect.ID, privateNoInstanceReducer{r.host})
		return err
	}
	if work.Effect.State != "prepared" && work.Effect.State != "unknown" {
		return privatechat.ErrDeferred
	}
	event, _, target, err := privatechat.DecodeStoredEffectTarget(work.Effect.Effect.Body)
	if err != nil || event.Command == nil || target == nil || *target != "" {
		return fmt.Errorf("invalid empty native target")
	}
	if work.Effect.State == "prepared" {
		if err = r.journal.MarkEffectSending(ctx, work.Owner, work.Generation, work.Effect.Effect.ID); err != nil {
			return err
		}
	}
	receipt, _ := json.Marshal(privateNoInstanceReceipt{Version: 1, EffectID: work.Effect.Effect.ID, CommandID: event.Command.CommandID, SurfaceID: work.Owner.SurfaceID})
	if err = r.journal.ConfirmLocalEffect(ctx, work.Owner, work.Generation, work.Effect.Effect.ID, receipt, privateLocalVerifier{owner: work.Owner, generation: work.Generation, effect: work.Effect.Effect, receipt: receipt}); err != nil {
		return err
	}
	_, err = r.processor.ApplyLocalHostReceipt(ctx, work.Owner, work.Generation, work.Effect.Effect.ID, privateNoInstanceReducer{r.host})
	return err
}

type privateKnownUnsentReducer struct{ host *App }

func (r privateKnownUnsentReducer) ReduceHostReceipt(app *privatechat.App, e privatechat.Effect, raw json.RawMessage) ([]eventcontract.Event, error) {
	if err := (privateHostReducer{r.host}).check(app); err != nil {
		return nil, err
	}
	event, _, target, err := privatechat.DecodeStoredEffectTarget(e.Body)
	if err != nil {
		return nil, err
	}
	var receipt privatechat.KnownUnsentReceipt
	if json.Unmarshal(raw, &receipt) != nil || receipt.Version != 1 || receipt.EffectID != e.ID || event.Command == nil || event.Command.CommandID != receipt.CommandID || target == nil || *target == "" || *target != receipt.InstanceID || receipt.Reason == "" {
		return nil, privatechat.ErrJournalConflict
	}
	problem := agentproto.ErrorInfo{Code: "native_command_not_sent", Layer: "daemon", Stage: "dispatch_prepare", Operation: string(event.Command.Kind), Message: "命令未发送，输入已恢复，请重试。", SurfaceSessionID: event.SurfaceSessionID, CommandID: receipt.CommandID}
	if receipt.Reason == "legacy_private_delivery" {
		problem.Code = "private_runtime_upgrade_required"
		problem.Message = "当前运行环境版本较旧，无法安全执行此操作。请先发送 /detach，等待运行环境退出完成后再重新选择工作目录。"
	}
	if event.Command.Kind == agentproto.CommandThreadCompactStart || event.Command.Kind == agentproto.CommandReviewStart {
		if err = app.Service().RestorePrivateControlProjection(event.SurfaceSessionID, receipt.InstanceID, receipt.CommandID, event.Command.Kind, event.Command.Target.ThreadID, "", ""); err != nil {
			return nil, err
		}
	}
	if event.Command.Kind == agentproto.CommandRequestRespond {
		if err = app.Service().RestorePrivateRequestProjection(event.SurfaceSessionID, receipt.InstanceID, event.Command.Request.RequestID, receipt.CommandID, event.Command.Target.ThreadID, event.Command.Target.TurnID); err != nil {
			return nil, err
		}
	}
	var events []eventcontract.Event
	if result, handled := app.Service().HandlePrivateQueryFailure(event.SurfaceSessionID, receipt.InstanceID, receipt.CommandID, problem); handled {
		events = result
	} else {
		events = app.Service().HandleKnownUnsent(event.SurfaceSessionID, receipt.CommandID, problem)
	}
	if receipt.Reason == "legacy_private_delivery" {
		events = append(events, eventcontract.Event{Kind: eventcontract.KindNotice, SurfaceSessionID: event.SurfaceSessionID, Notice: &control.Notice{Code: problem.Code, Text: problem.Message}})
	}
	return events, nil
}
