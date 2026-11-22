package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (a *App) queueTeamGoalPanel(ctx context.Context, source teamHumanSource, mode, note string) (string, error) {
	id := workui.ID(source.Key, "session-goal", mode)
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error { return a.queueTeamGoalPanelTx(tx, source, mode, note, id) })
	return id, err
}

func (a *App) queueTeamGoalPanelTx(tx *workstore.Tx, source teamHumanSource, mode, note, id string) error {
	if _, err := tx.GetUIView(id); err == nil {
		return nil
	} else if err != workstore.ErrNotFound {
		return err
	}
	front, revision, err := worksettings.FrozenContext(tx, source.Authority)
	if err != nil {
		return err
	}
	binding, err := tx.GetRoleBotBinding(source.RoleID)
	if err != nil {
		return err
	}
	goalState, controls, err := teamGoalControlState(tx, front)
	if err != nil {
		return err
	}
	panel := workui.Panel{Title: "会话目标", Paragraphs: []string{"会话目标只控制当前 Codex 会话的持续执行。"}}
	if note != "" {
		panel.Paragraphs = append(panel.Paragraphs, note)
	}
	switch mode {
	case "new", "set", "edit":
		panel.Title = "设置会话目标"
		panel.Paragraphs = append(panel.Paragraphs, "填写目标和 token 预算，例如：--budget 1200 完成登录流程。")
		panel.Forms = []workui.InputForm{{Name: "goal_request", Placeholder: "--budget 1200 完成当前工作", Submit: workui.Button{Label: "保存会话目标", Action: "private.goal-" + mode + "-submit", Values: map[string]any{"goal_state": goalState}}}}
	case "clear":
		panel.Title = "清除会话目标"
		canClear := false
		for _, button := range controls {
			canClear = canClear || button.Action == "private.goal-clear"
		}
		if canClear {
			panel.Paragraphs = append(panel.Paragraphs, "确认清除当前已核对的会话目标？会话历史会保留。")
			panel.Buttons = append(panel.Buttons, workui.Button{Label: "确认清除", Action: "private.goal-clear-confirm", Values: map[string]any{"goal_state": goalState}})
		} else {
			panel.Paragraphs = append(panel.Paragraphs, "当前没有已确认可清除的目标。可以点击「刷新会话目标」查看当前原生会话的实际状态。")
		}
	case "overview":
		lines, err := teamGoalObservedLines(tx, front)
		if err != nil {
			return err
		}
		panel.Paragraphs = append(panel.Paragraphs, lines...)
		for _, button := range []workui.Button{
			{Label: "新建会话目标", Action: "private.goal-new"},
			{Label: "编辑会话目标", Action: "private.goal-edit"},
		} {
			panel.Buttons = append(panel.Buttons, button)
		}
		for _, button := range controls {
			button.Values = map[string]any{"goal_state": goalState}
			panel.Buttons = append(panel.Buttons, button)
		}
	default:
		return fmt.Errorf("无法识别会话目标页面")
	}
	panel.Buttons = append(panel.Buttons, workui.Button{Label: "刷新会话目标", Action: "private.goal-refresh"}, workui.Button{Label: "返回常用工具", Action: "private.tools"})
	view := workstore.UIView{ID: id, ScopeID: source.ScopeID, ContextID: front.ID, Kind: "private.navigation", TargetID: source.ScopeID, TargetRevision: revision, Binding: binding, Tenant: source.Tenant, ChatID: source.ChatID, PrincipalID: source.PrincipalID, ReplyToMessageID: source.MessageID}
	if err = workui.PrivateNavigationFromInteraction(tx, &view, source.Authority.InteractionID); err != nil {
		return err
	}
	card, err := workui.Card(id, panel)
	if err != nil {
		return err
	}
	_, err = workui.Queue(tx, view, card)
	return err
}

// Show only confirmed responses for this context's current worker generation.
// An earlier worker's goal cannot describe a replacement native session.
func teamGoalObservedLines(tx *workstore.Tx, front work.WorkContext) ([]string, error) {
	workers, err := tx.ListWorkers(front.ScopeID)
	if err != nil {
		return nil, err
	}
	var worker work.WorkerSession
	for _, candidate := range workers {
		if candidate.ContextID == front.ID && candidate.State != work.WorkerRetired {
			if worker.ID != "" {
				return nil, fmt.Errorf("当前工作存在多个执行身份，需先核对恢复状态")
			}
			worker = candidate
		}
	}
	if worker.ID == "" {
		return []string{"当前还没有可查询的原生会话。"}, nil
	}
	if worker.Backend != work.BackendCodex {
		return []string{"会话目标仅适用于 Codex。请在参数设置中切换执行后端。"}, nil
	}
	operations, err := tx.ListOperations(front.ScopeID)
	if err != nil {
		return nil, err
	}
	// Delivery bookkeeping may touch UpdatedAt after a newer Goal was observed.
	// Keep query/mutation request order independent of its UI delivery lifecycle.
	sort.SliceStable(operations, func(i, j int) bool { return operations[i].CreatedAt.After(operations[j].CreatedAt) })
	for _, op := range operations {
		if op.Kind != "native.user.goal" || op.ContextID != front.ID || op.TargetID != worker.ID || op.TargetGeneration != worker.Generation || op.ExternalReceipt == "" {
			continue
		}
		body, err := tx.GetContent(front.ScopeID, op.ExternalReceipt)
		if err != nil {
			return nil, err
		}
		var event agentproto.Event
		if err = json.Unmarshal(body.Data, &event); err != nil {
			return nil, err
		}
		if event.ThreadID != worker.NativeSessionID {
			continue
		}
		if event.ErrorMessage != "" {
			return []string{"最近一次会话目标操作未完成：" + event.ErrorMessage}, nil
		}
		if op.State != work.OperationConfirmed {
			continue
		}
		if event.GoalMissing || event.GoalCleared {
			return []string{"最近一次已确认状态：当前会话没有持续执行目标。", "请求时间：" + op.CreatedAt.Local().Format("01-02 15:04:05")}, nil
		}
		if goal := event.ThreadGoal; goal != nil {
			lines := []string{"最近一次已确认目标：" + goal.Objective, "状态：" + goal.Status}
			if goal.TokenBudget != nil {
				lines = append(lines, fmt.Sprintf("token 预算：%d", *goal.TokenBudget))
			}
			return append(lines, "请求时间："+op.CreatedAt.Local().Format("01-02 15:04:05")), nil
		}
	}
	return []string{"尚未收到当前会话的目标反馈。请稍后刷新。"}, nil
}

func (a *App) handleTeamGoalViewOperation(ctx context.Context, source teamHumanSource, action string, form map[string]any) (string, error) {
	verb := strings.TrimPrefix(action, "private.goal-")
	switch verb {
	case "new", "set", "edit":
		return a.queueTeamGoalPanel(ctx, source, verb, "")
	case "clear":
		return a.queueTeamGoalPanel(ctx, source, "clear", "")
	case "refresh":
		return a.teamNativeGoal(ctx, source, control.Action{Kind: control.ActionGoalCommand, Text: "/goal"})
	case "clear-confirm":
		return a.teamNativeGoal(ctx, source, control.Action{Kind: control.ActionGoalCommand, Text: "/goal clear --confirm"})
	case "pause", "resume":
		return a.teamNativeGoal(ctx, source, control.Action{Kind: control.ActionGoalCommand, Text: "/goal " + verb})
	case "new-submit", "set-submit", "edit-submit":
		request, _ := form["goal_request"].(string)
		request = strings.TrimSpace(request)
		if request == "" || len(request) > 4096 || strings.ContainsRune(request, '\x00') {
			return "", fmt.Errorf("请填写会话目标和 token 预算")
		}
		verb = strings.TrimSuffix(verb, "-submit")
		return a.teamNativeGoal(ctx, source, control.Action{Kind: control.ActionGoalCommand, Text: "/goal " + verb + " " + request})
	}
	return "", fmt.Errorf("无法识别会话目标操作")
}

func teamGoalControlState(tx *workstore.Tx, front work.WorkContext) (string, []workui.Button, error) {
	workers, err := tx.ListWorkers(front.ScopeID)
	if err != nil {
		return "", nil, err
	}
	for _, worker := range workers {
		if worker.ContextID != front.ID || worker.State == work.WorkerRetired {
			continue
		}
		parts := []string{front.ID, worker.ID, fmt.Sprint(worker.Generation), worker.NativeSessionID, worker.CarrierIncarnation}
		controls := []workui.Button{}
		if worker.Backend == work.BackendCodex && worker.NativeSessionID != "" {
			grant, lookup := tx.GetContinuationGrant(worker.CarrierIncarnation, worker.NativeSessionID)
			if lookup != nil && lookup != workstore.ErrNotFound {
				return "", nil, lookup
			}
			carrier, e := tx.GetCarrier(worker.CarrierIncarnation)
			if e != nil && e != workstore.ErrNotFound {
				return "", nil, e
			}
			if e == nil && carrier.Matches(worker) {
				snapshot, e := tx.CaptureGoalControlSnapshot(worker, carrier)
				if e != nil {
					return "", nil, e
				}
				parts = append(parts, snapshot.ObservationID, snapshot.Status, fmt.Sprint(snapshot.GoalVersion), snapshot.Objective, fmt.Sprint(snapshot.TokenBudget))
				if !snapshot.NoGrant && snapshot.GoalVersion > 0 || snapshot.NoGrant && snapshot.ObservationID != "" && snapshot.Status != "missing" {
					controls = append(controls, workui.Button{Label: "清除会话目标", Action: "private.goal-clear"})
				}
			}
			if lookup == nil {
				parts = append(parts, grant.CommandID, fmt.Sprint(grant.GoalVersion), grant.Objective, fmt.Sprint(grant.TokenBudget), fmt.Sprint(grant.Active), grant.Status, fmt.Sprint(grant.Reconciled))
				attempt, occupied := tx.GetOccupiedAttemptForWorker(worker.ID)
				if occupied != nil && occupied != workstore.ErrNotFound {
					return "", nil, occupied
				}
				if grant.GoalVersion > 0 && grant.Reconciled && grant.TokenBudget > 0 && grant.Objective != "" && (occupied == workstore.ErrNotFound || (occupied == nil && attempt.State == work.AttemptRunning && grant.Binding.AttemptID == attempt.ID && grant.Binding.RunRef == attempt.RunRef.RunID)) {
					if grant.Status == "active" || grant.Active {
						controls = append(controls, workui.Button{Label: "暂停会话目标", Action: "private.goal-pause"})
					}
					if grant.Status == "paused" {
						controls = append(controls, workui.Button{Label: "继续会话目标", Action: "private.goal-resume"})
					}
				}
			}
		}
		return workui.ID(parts...), controls, nil
	}
	return workui.ID(front.ID, "no-native-session"), nil, nil
}

func teamGoalPrecondition(worker work.WorkerSession, carrier workstore.CarrierRegistration, grant workstore.ContinuationGrant) *agentproto.GoalPrecondition {
	status := grant.Status
	if status == "" && grant.Active {
		status = "active"
	}
	return &agentproto.GoalPrecondition{Incarnation: carrier.Incarnation, SessionID: worker.NativeSessionID, WorkerGeneration: worker.Generation, ControllerGeneration: worker.ControllerGeneration, GoalVersion: grant.GoalVersion, CommandID: grant.CommandID, Objective: grant.Objective, TokenBudget: grant.TokenBudget, Status: status}
}

func (a *App) validateTeamGoalSourceSnapshot(tx *workstore.Tx, source teamHumanSource) error {
	if source.Authority.InteractionID == "" {
		return nil
	}
	interaction, err := tx.GetHumanInteraction(source.Authority.InteractionID)
	if err != nil {
		return err
	}
	front, _, err := worksettings.FrozenContext(tx, source.Authority)
	if err != nil {
		return err
	}
	current, _, err := teamGoalControlState(tx, front)
	if err != nil {
		return err
	}
	frozen, _ := interaction.Envelope.Values["goal_state"].(string)
	if frozen == "" || frozen != current {
		return fmt.Errorf("会话目标已改变，请刷新目标页面后重新操作。")
	}
	return nil
}
