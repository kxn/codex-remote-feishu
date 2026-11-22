package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (a *App) advanceTeamGoalUI(ctx context.Context, now time.Time) error {
	var pending []work.Operation
	if err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if _, err := tx.CancelStaleSessionGoalResumes(now); err != nil {
			return err
		}
		var err error
		pending, err = tx.ListPrivateGoalUI()
		return err
	}); err != nil {
		return err
	}
	var failures []error
	for _, op := range pending {
		if err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error { return a.advanceTeamGoalResult(tx, op.ID, now) }); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (a *App) advanceTeamGoalResult(tx *workstore.Tx, id string, now time.Time) error {
	op, err := tx.GetOperation(id)
	if err != nil {
		return err
	}
	for _, step := range op.Steps {
		if step.ID == "ui_result" {
			return nil
		}
	}
	if op.Kind == "private.goal.intent-ui" {
		intent, err := tx.GetSessionGoalIntent(op.TargetID)
		if err != nil {
			return err
		}
		rejection := intent.RejectionReason
		if intent.CommandID == "" && rejection == "" {
			input, err := tx.GetInbox(intent.InputID)
			if err != nil {
				return err
			}
			if input.State == work.InboxCancelled {
				rejection = "这项会话目标的排队请求已取消。"
			}
		}
		if rejection != "" {
			op.State, op.LastError = work.OperationTerminal, rejection
			presentation := op
			presentation.ContextID = intent.ContextID
			panel, err := teamGoalResultPanel(tx, presentation, []string{"会话目标操作未执行：" + rejection})
			if err != nil {
				return err
			}
			return a.presentTeamPrivateNativeResult(tx, op, panel, now, false)
		}
		if intent.CommandID == "" {
			return touchTeamPrivateNativeUI(tx, op, now)
		}
		viewID := ""
		for _, step := range op.Steps {
			if step.ID == "ui_pending" {
				viewID = step.Receipt
			}
		}
		if err = attachTeamGoalPending(tx, intent.CommandID, viewID, now); err != nil {
			return err
		}
		op.State, op.ExternalReceipt = work.OperationConfirmed, intent.CommandID
		return finishTeamPrivateNativeUI(tx, op, "transferred:"+intent.CommandID, now)
	}
	lines := []string{}
	if op.ExternalReceipt != "" && (op.State == work.OperationConfirmed || op.State == work.OperationTerminal) {
		event, err := tx.NativeGoalCommandResult(op.ID)
		if err != nil {
			return err
		}
		lines = teamGoalReceiptLines(event)
	} else if op.State == work.OperationPrepared {
		return touchTeamPrivateNativeUI(tx, op, now)
	} else if op.State == work.OperationUnknown && op.LastError != "" {
		lines = []string{"会话目标操作的结果尚未确认：" + op.LastError, "这次操作不会自动重发。"}
	} else if teamMCPReadTimedOut(op, now) {
		lines = []string{"尚未收到原生会话的目标反馈，结果暂未确认。可以查询当前目标；这次操作不会自动重发。"}
	} else {
		return touchTeamPrivateNativeUI(tx, op, now)
	}
	panel, err := teamGoalResultPanel(tx, op, lines)
	if err != nil {
		return err
	}
	return a.presentTeamPrivateNativeResult(tx, op, panel, now, true)
}

func teamGoalReceiptLines(event agentproto.Event) []string {
	if event.ErrorMessage != "" {
		return []string{"会话目标操作未完成：" + event.ErrorMessage}
	}
	if event.GoalMissing || event.GoalCleared {
		return []string{"当前会话没有持续执行目标。"}
	}
	if goal := event.ThreadGoal; goal != nil {
		lines := []string{"会话目标：" + goal.Objective, "状态：" + goal.Status}
		if goal.TokenBudget != nil {
			lines = append(lines, fmt.Sprintf("token 预算：%d", *goal.TokenBudget))
		}
		lines = append(lines, fmt.Sprintf("已使用 token：%d", goal.TokensUsed))
		return lines
	}
	return []string{"会话目标操作已有回执，但未返回可显示的目标状态。请查询当前会话目标。"}
}

func teamGoalResultPanel(tx *workstore.Tx, op work.Operation, lines []string) (workui.Panel, error) {
	front, err := tx.GetContext(op.ContextID)
	if err != nil {
		return workui.Panel{}, err
	}
	state, controls, err := teamGoalControlState(tx, front)
	if err != nil {
		return workui.Panel{}, err
	}
	panel := workui.Panel{Title: "会话目标", Paragraphs: lines, Buttons: []workui.Button{{Label: "新建会话目标", Action: "private.goal-new"}, {Label: "编辑会话目标", Action: "private.goal-edit"}}}
	for _, button := range controls {
		button.Values = map[string]any{"goal_state": state}
		panel.Buttons = append(panel.Buttons, button)
	}
	panel.Buttons = append(panel.Buttons, workui.Button{Label: "刷新会话目标", Action: "private.goal-refresh"}, workui.Button{Label: "返回常用工具", Action: "private.tools"})
	return panel, nil
}
