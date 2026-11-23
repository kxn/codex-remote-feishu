package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worktransport"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// The frozen model turn authorizes the send, but does not own execution of a
// session-level UserShell command. Its actual item receipt settles the source.
func (a *App) executeTeamPrivateShell(ctx context.Context, source teamHumanSource, worker work.WorkerSession, turn workstore.NativeTurn, original agentproto.Command, parts []agentproto.Input) (string, error) {
	payload, err := teamReactionShellPayload(parts)
	if err != nil {
		return "", err
	}
	id := teamID("private-shell", source.Key)
	owner := turn.Binding
	owner.AttemptID, owner.RunRef = "", ""
	command := agentproto.Command{CommandID: id + ":command", Kind: agentproto.CommandThreadShellCommand, Execution: &owner, InstructionRevision: original.InstructionRevision, Target: agentproto.Target{ThreadID: turn.SessionID, TurnID: turn.TurnID}, ShellCommand: agentproto.ShellCommand{OperationID: id, Payload: payload}, IssuedAt: time.Now().UTC()}
	created := false
	err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		action, e := tx.GetPrivateReactionAction(source.Authority.ReactionID)
		if e != nil {
			return e
		}
		_, created, e = tx.BeginPrivateShellOperation(workstore.BeginPrivateShellOperationRequest{ID: id, Source: source.Authority, InputID: action.InputID, InputIDs: action.InputIDs, Command: command, Now: time.Now().UTC()})
		return e
	})
	if err != nil {
		return "", err
	}
	if !created {
		return id, nil
	}
	if err = a.teamRuntime.driver.SendCommand(ctx, worker.InstanceID, command); err != nil {
		saveErr := a.teamRuntime.store.Within(context.WithoutCancel(ctx), func(tx *workstore.Tx) error {
			var unsent *worktransport.PreSendError
			if errors.As(err, &unsent) {
				return tx.RejectPrivateShellBeforeSend(command.CommandID, err.Error())
			}
			return tx.MarkPrivateShellOperationUnknown(command.CommandID, err.Error())
		})
		if saveErr != nil {
			return "", fmt.Errorf("%v; 保存原生命令状态失败：%w", err, saveErr)
		}
		var unsent *worktransport.PreSendError
		if errors.As(err, &unsent) {
			return id, errTeamPrivateShellUnsent
		}
	}
	return id, nil
}
