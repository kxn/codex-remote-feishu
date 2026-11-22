package daemon

import (
	"context"
	"encoding/json"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (a *App) queueTeamGoalCommandPending(ctx context.Context, source teamHumanSource, commandID string) (string, error) {
	viewID := workui.ID(source.Key, "session-goal", "overview")
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := a.queueTeamGoalPanelTx(tx, source, "overview", "会话目标操作已记录，正在等待原生会话反馈。结果会在这里更新。", viewID); err != nil {
			return err
		}
		return attachTeamGoalPending(tx, commandID, viewID, time.Now().UTC())
	})
	return viewID, err
}

func attachTeamGoalPending(tx *workstore.Tx, commandID, viewID string, now time.Time) error {
	op, err := tx.GetOperation(commandID)
	if err != nil {
		return err
	}
	for _, step := range op.Steps {
		if step.ID == "ui_pending" {
			if step.Receipt != viewID {
				return workstore.ErrIdempotencyConflict
			}
			return nil
		}
	}
	if op.Kind != "native.user.goal" {
		return workstore.ErrScopeDenied
	}
	body, err := tx.GetContent(op.ScopeID, op.RequestRef)
	if err != nil {
		return err
	}
	var command agentproto.Command
	if err = json.Unmarshal(body.Data, &command); err != nil {
		return err
	}
	worker, err := tx.GetWorker(op.TargetID)
	if err != nil {
		return err
	}
	incarnation := worker.CarrierIncarnation
	if registered, lookup := tx.GetNativeCommand(commandID); lookup == nil {
		incarnation = registered.Incarnation
	} else if lookup != workstore.ErrNotFound {
		return lookup
	}
	op.Steps = append(op.Steps, work.OperationStep{ID: "ui_pending", State: work.OperationConfirmed, Receipt: viewID}, work.OperationStep{ID: "carrier", State: work.OperationConfirmed, Receipt: incarnation}, work.OperationStep{ID: "dispatch_started", State: work.OperationConfirmed, Receipt: command.IssuedAt.Format(time.RFC3339Nano)})
	return touchTeamPrivateNativeUI(tx, op, now)
}

func (a *App) queueTeamGoalIntentPending(ctx context.Context, source teamHumanSource, intentID string) (string, error) {
	viewID := workui.ID(source.Key, "session-goal", "overview")
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error { return a.queueTeamGoalIntentPendingTx(tx, source, intentID, viewID) })
	return viewID, err
}

func (a *App) queueTeamGoalIntentPendingTx(tx *workstore.Tx, source teamHumanSource, intentID, viewID string) error {
	if err := a.queueTeamGoalPanelTx(tx, source, "overview", "会话目标已排队，将在正常执行接纳后生效。暂停中的工作会继续等待；结果会在这里更新。", viewID); err != nil {
		return err
	}
	intent, err := tx.GetSessionGoalIntent(intentID)
	if err != nil {
		return err
	}
	if intent.ScopeID != source.ScopeID {
		return workstore.ErrScopeDenied
	}
	id := intentID + ":ui"
	if _, err = tx.GetOperation(id); err == nil {
		return nil
	} else if err != workstore.ErrNotFound {
		return err
	}
	now := time.Now().UTC()
	raw, err := json.Marshal(intentID)
	if err != nil {
		return err
	}
	body := work.NewContent(id+":request", intent.ScopeID, "application/json", raw, now)
	if err = tx.PutContent(body); err != nil {
		return err
	}
	_, _, err = tx.PutOperation(work.Operation{ID: id, ScopeID: intent.ScopeID, ContextID: intent.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "session-goal-ui"}, Kind: "private.goal.intent-ui", TargetID: intentID, TargetGeneration: 1, RequestRef: body.ID, RequestHash: body.SHA256, IdempotencyKey: id, State: work.OperationSending, Steps: []work.OperationStep{{ID: "ui_pending", State: work.OperationConfirmed, Receipt: viewID}}, Revision: 1, CreatedAt: now, UpdatedAt: now}, 0)
	return err
}
