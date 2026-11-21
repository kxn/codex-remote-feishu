package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (a *App) queueTeamIdleGoal(ctx context.Context, source teamHumanSource, goal agentproto.GoalCommand) (string, error) {
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		front, _, e := worksettings.FrozenContext(tx, source.Authority)
		if e != nil {
			return e
		}
		_, _, e = tx.QueueSessionGoalIntent(teamID("user-goal-intent", source.Key), front.ID, goal, source.Authority, time.Now().UTC())
		return e
	})
	if err != nil {
		return "", err
	}
	return a.queuePrivatePanel(ctx, source, "status", "会话目标已排队，开始实际执行后生效；暂停中的工作会继续等待。")
}
func (a *App) dispatchTeamPendingGoals(ctx context.Context) error {
	var list []work.Operation
	if err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error { var e error; list, e = tx.ListPreparedSessionGoalOperations(); return e }); err != nil {
		return err
	}
	var failures []error
	for _, pending := range list {
		var command agentproto.Command
		var instance string
		send := false
		err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
			op, e := tx.GetOperation(pending.ID)
			if e != nil {
				return e
			}
			if op.State != work.OperationPrepared {
				return nil
			}
			body, e := tx.GetContent(op.ScopeID, op.RequestRef)
			if e != nil {
				return e
			}
			if e = json.Unmarshal(body.Data, &command); e != nil {
				return e
			}
			if command.Execution == nil {
				return workstore.ErrScopeDenied
			}
			attempt, e := tx.GetAttempt(command.Execution.AttemptID)
			if e != nil {
				return e
			}
			worker, e := tx.GetWorker(command.Execution.WorkerSessionID)
			if e != nil {
				return e
			}
			if attempt.State != work.AttemptRunning || attempt.RunRef.RunID != command.Execution.RunRef || worker.Generation != command.Execution.SessionGeneration || worker.NativeSessionID != command.Target.ThreadID {
				return workstore.ErrConflict
			}
			before := op.Revision
			op.Revision++
			op.State = work.OperationSending
			op.UpdatedAt = time.Now().UTC()
			if _, _, e = tx.PutOperation(op, before); e != nil {
				return e
			}
			instance = worker.InstanceID
			send = true
			return nil
		})
		if err == nil && send {
			err = a.teamRuntime.driver.SendCommand(ctx, instance, command)
			if err != nil {
				saveErr := a.teamRuntime.store.Within(context.WithoutCancel(ctx), func(tx *workstore.Tx) error {
					op, e := tx.GetOperation(command.CommandID)
					if e != nil {
						return e
					}
					before := op.Revision
					op.Revision++
					op.State = work.OperationUnknown
					op.LastError = err.Error()
					_, _, e = tx.PutOperation(op, before)
					return e
				})
				err = errors.Join(err, saveErr)
			}
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
