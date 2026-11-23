package daemon

import (
	"context"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"path/filepath"
	"sort"
	"time"
)

func (a *App) steerTeamPendingInputs(ctx context.Context, source teamHumanSource) (string, error) {
	return a.admitTeamPendingInputs(ctx, source, "", false)
}

func (a *App) admitTeamPendingInputs(ctx context.Context, source teamHumanSource, onlyInput string, shell bool) (string, error) {
	var worker work.WorkerSession
	var attempt work.ExecutionAttempt
	var turn workstore.NativeTurn
	var original agentproto.Command
	var ids []string
	active := false
	nativeCapable := false
	replyTargetValidated := false
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		front, _, e := worksettings.FrozenContext(tx, source.Authority)
		if e != nil {
			return e
		}
		if front.Purpose == work.PurposeReview || front.Purpose == work.PurposeControl {
			return workstore.ErrScopeDenied
		}
		pending, e := tx.ListPendingInputs(front.ScopeID, front.RoleID)
		if e != nil {
			return e
		}
		selected := map[string]bool{}
		if onlyInput != "" {
			if source.Authority.ReactionID != "" {
				action, e := tx.GetPrivateReactionAction(source.Authority.ReactionID)
				if e != nil {
					return e
				}
				for _, id := range action.InputIDs {
					selected[id] = true
				}
			} else {
				selected[onlyInput] = true
			}
		}
		sort.SliceStable(pending, func(i, j int) bool {
			if pending[i].AvailableAt.Equal(pending[j].AvailableAt) {
				return pending[i].ID < pending[j].ID
			}
			return pending[i].AvailableAt.Before(pending[j].AvailableAt)
		})
		for _, input := range pending {
			if input.RecipientContextID == front.ID && input.ContentState != work.InputContentPending && (onlyInput == "" || selected[input.ID]) {
				ids = append(ids, input.ID)
			}
		}
		if len(ids) == 0 {
			return nil
		}
		var carrier workstore.CarrierRegistration
		worker, carrier, e = a.teamNativeTarget(tx, source)
		if e != nil {
			return nil
		}
		nativeCapable = worker.Backend == work.BackendCodex || (!shell && worker.Backend == work.BackendClaude)
		attempt, e = tx.GetOccupiedAttemptForWorker(worker.ID)
		if e == workstore.ErrNotFound {
			return nil
		}
		if e != nil {
			return e
		}
		if attempt.State != work.AttemptRunning {
			return nil
		}
		if source.Authority.IngressID != "" {
			if e = tx.ValidatePrivateReplyAdmission(source.Authority, attempt, time.Now().UTC()); e != nil {
				return e
			}
			replyTargetValidated = true
		}
		if !nativeCapable {
			return nil
		}
		matching := ids[:0]
		for _, id := range ids {
			input, e := tx.GetInbox(id)
			if e != nil {
				return e
			}
			if input.TaskID == attempt.TaskID && input.ControlOfInputID == "" && input.SessionGoalIntentID == "" {
				matching = append(matching, id)
			}
		}
		ids = matching
		if len(ids) == 0 {
			return nil
		}
		turns, e := tx.ListNativeTurns(attempt.ID)
		if e != nil {
			return e
		}
		for _, candidate := range turns {
			if candidate.CarrierIncarnation != worker.CarrierIncarnation || candidate.SessionID != worker.NativeSessionID || !candidate.Started || candidate.TerminalStatus != "" {
				continue
			}
			if turn.TurnID != "" {
				return fmt.Errorf("当前有多个执行轮次，消息保留到下一轮，未猜测注入目标")
			}
			turn = candidate
		}
		if turn.TurnID == "" {
			return nil
		}
		original, e = tx.NativeCommandPayload(attempt.CommandID, carrier.Incarnation)
		if e != nil {
			return e
		}
		active = true
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(ids) == 0 {
		if source.Authority.ReactionID != "" || source.Authority.IngressID != "" {
			return "", nil
		}
		return a.queuePrivatePanel(ctx, source, "status", "当前没有可并入执行的排队消息。")
	}
	if !active {
		note := "当前没有正在执行的可注入轮次，消息会保留到下一轮处理。"
		if worker.ID != "" && !nativeCapable {
			note = "当前后端没有提供可验证的轮内注入接口，排队消息会保留到下一轮处理。"
		}
		if source.Authority.IngressID != "" {
			if replyTargetValidated && !nativeCapable {
				return a.queuePrivatePanel(ctx, source, "status", "当前后端不支持轮内追加；回复文字保留在队列中，独立图片会等待后续文字。")
			}
			return "", nil
		}
		if source.Authority.ReactionID != "" {
			if worker.ID != "" && !nativeCapable {
				return "", errTeamReactionUnsupported
			}
			return "", fmt.Errorf("%s", note)
		}
		return a.queuePrivatePanel(ctx, source, "status", note)
	}
	var parts []agentproto.Input
	attachmentRoot := filepath.Join(a.headlessRuntime.Paths.StateDir, "team-attachments")
	if source.Authority.IngressID != "" {
		parts, err = a.teamRuntime.service.BuildPrivateReplySteerPrompt(ctx, attempt.ID, ids[0], source.MessageID, attachmentRoot)
	} else {
		parts, err = a.teamRuntime.service.BuildSteerWorkPrompt(ctx, attempt.ID, ids, attachmentRoot)
	}
	if err != nil {
		return "", err
	}
	if shell {
		return a.executeTeamPrivateShell(ctx, source, worker, turn, original, parts)
	}
	id := teamID("input-admission", source.Key)
	binding := turn.Binding
	command := agentproto.Command{CommandID: id + ":command", Kind: agentproto.CommandTurnSteer, Execution: &binding, InstructionRevision: original.InstructionRevision, Target: agentproto.Target{ThreadID: turn.SessionID, TurnID: turn.TurnID}, Prompt: agentproto.Prompt{Inputs: parts}, NativeInputAdmission: &agentproto.NativeInputAdmissionCommand{Version: agentproto.NativeInputAdmissionProtocolVersion, ID: id, InputIDs: ids}, IssuedAt: time.Now().UTC()}
	created := false
	err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		_, added, e := tx.BeginAttemptInputAdmission(workstore.BeginAttemptInputAdmissionRequest{ID: id, AttemptID: attempt.ID, RunRef: attempt.RunRef, ExpectedWorkerRevision: worker.Revision, InputIDs: ids, Command: command, Source: source.Authority, Now: time.Now().UTC()})
		created = added
		return e
	})
	if err != nil {
		return "", err
	}
	if created {
		if err = a.teamRuntime.driver.SendCommand(ctx, worker.InstanceID, command); err != nil {
			saveErr := a.teamRuntime.store.Within(context.WithoutCancel(ctx), func(tx *workstore.Tx) error {
				return tx.MarkAttemptInputAdmissionUnknown(command.CommandID, err.Error())
			})
			if saveErr != nil {
				return "", fmt.Errorf("%v; 保存执行待核对状态失败：%w", err, saveErr)
			}
			if source.Authority.ReactionID != "" || source.Authority.IngressID != "" {
				return id, nil
			}
			return a.queuePrivatePanel(ctx, source, "status", "这次轮内输入的接收结果还不确定，原消息已保留，核对前不会重复注入。")
		}
	}
	if source.Authority.ReactionID != "" || source.Authority.IngressID != "" {
		return id, nil
	}
	return a.queuePrivatePanel(ctx, source, "status", fmt.Sprintf("已记录并发送 %d 条排队输入的轮内请求，正在等待原生确认；确认前不会把消息算作已处理。", len(ids)))
}
