package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type teamRecoveryView struct {
	Worker work.WorkerSession
	Gate   workstore.RuntimeGate
}

func (a *App) runTeamRuntimeViews(ctx context.Context) error {
	var workers []work.WorkerSession
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		scopes, e := tx.ListScopes()
		if e != nil {
			return e
		}
		for _, scope := range scopes {
			list, e := tx.ListWorkers(scope.ID)
			if e != nil {
				return e
			}
			workers = append(workers, list...)
		}
		return nil
	})
	if err != nil {
		return err
	}
	var failures []error
	for _, worker := range workers {
		err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
			current, e := tx.GetWorker(worker.ID)
			if e != nil {
				return e
			}
			gate, e := tx.GetRuntimeGate(worker.ID)
			if errors.Is(e, workstore.ErrNotFound) {
				return a.retireTeamRecoveryView(tx, current, false)
			}
			if e != nil {
				return e
			}
			if gate.Reason == "snapshot_rebuild_required" {
				return a.retireTeamRecoveryView(tx, current, true)
			}
			if runtimeGateStillReconciling(gate, time.Now()) {
				return nil
			}
			_, e = a.queueTeamRecoveryView(tx, current, gate)
			return e
		})
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
func (a *App) queueTeamRecoveryView(tx *workstore.Tx, worker work.WorkerSession, gate workstore.RuntimeGate) (string, error) {
	view, err := workui.ContextDestination(tx, worker.ContextID)
	if err != nil {
		return "", err
	}
	role, err := tx.GetRole(worker.RoleID)
	if err != nil {
		return "", err
	}
	canRecover := false
	if gate.AttemptID != "" && worker.Controller == work.ControllerAutomatic {
		if _, e := tx.GetQuiescenceReceipt(gate.AttemptID); e == nil {
			canRecover = true
		} else if !errors.Is(e, workstore.ErrNotFound) {
			return "", e
		}
	}
	view.ID = workui.ID("runtime", worker.ID, strconv.FormatInt(worker.Generation, 10), strconv.FormatInt(worker.ControllerGeneration, 10), strconv.FormatInt(gate.Revision, 10), strconv.FormatBool(canRecover))
	if old, e := tx.GetUIView(view.ID); e == nil {
		return old.ID, nil
	} else if !errors.Is(e, workstore.ErrNotFound) {
		return "", e
	}
	view.Kind = "runtime.worker"
	view.SurfaceKey = "runtime:" + worker.ID
	view.TargetID = worker.ID
	view.TargetRevision = worker.Revision
	text := "这项工作正在核对执行状态。未处理的输入和已有成果会保留，确认后才能重新执行。"
	switch gate.Reason {
	case "native_resume_rejected":
		text = "原生会话无法恢复，原输入和检查点已保留。自动重建的证据不足，请核查后恢复工作。"
	case "missing_processing_report":
		text = "上次执行没有留下完整的处理记录。原输入已保留；恢复时会先核查已经做过的操作，避免重复执行。"
	case "carrier_disconnected", "runtime_disconnected":
		text = "与执行进程失去联系，目前还不能确认它是否停止。系统正在核对，不会同时启动另一份执行。"
	case "native_goal_budget_exhausted":
		text = "这轮连续工作的预算已用完，任务还没有完成。已有成果和待办会保留；可以核查后恢复，再告诉负责人是否继续以及新的预算。"
	case "native_goal_budget_required":
		text = "正在为这轮连续工作确认预算；确认前不会另开一轮执行。若等待较久，可以重新核对状态。"
	case "control_requested":
		text = "正在核对旧执行的控制状态和后台任务。核查完成前保持暂停，不会重新开启已取消的原生接管流程。"
	}
	panel := workui.Panel{Title: role.Name + "的工作需要核对", Paragraphs: []string{text}}
	if gate.AttemptID != "" {
		panel.Buttons = append(panel.Buttons, workui.Button{Label: "重新核对", Action: "runtime.observe"})
	}
	if canRecover {
		panel.Buttons = append(panel.Buttons, workui.Button{Label: "核查后恢复工作", Action: "runtime.recover"})
	}
	state, err := json.Marshal(teamRecoveryView{Worker: worker, Gate: gate})
	if err != nil {
		return "", err
	}
	if err = tx.PutContent(work.NewContent(view.ID+":state", view.ScopeID, "application/json", state, time.Now().UTC())); err != nil {
		return "", err
	}
	card, err := workui.Card(view.ID, panel)
	if err != nil {
		return "", err
	}
	if err = reuseTeamRecoveryView(tx, &view); err != nil {
		return "", err
	}
	_, err = workui.Queue(tx, view, card)
	return view.ID, err
}

func (a *App) handleTeamRecoveryAction(ctx context.Context, interaction workstore.HumanInteraction, view workstore.UIView, action string) error {
	if view.Kind != "runtime.worker" {
		return workstore.ErrScopeDenied
	}
	var state teamRecoveryView
	var source work.SourceRef
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if e := tx.ValidateViewInteraction(view, interaction); e != nil {
			return e
		}
		body, e := tx.GetContent(view.ScopeID, view.ID+":state")
		if e != nil {
			return e
		}
		if e = json.Unmarshal(body.Data, &state); e != nil {
			return e
		}
		if state.Worker.ID != view.TargetID || state.Worker.ScopeID != view.ScopeID {
			return workstore.ErrScopeDenied
		}
		raw, e := json.Marshal(interaction)
		if e != nil {
			return e
		}
		evidence := work.NewContent(interaction.ID+":runtime", view.ScopeID, "application/json", raw, time.Now().UTC())
		if e = tx.PutContent(evidence); e != nil {
			return e
		}
		source = work.SourceRef{Kind: "human_interaction", ID: interaction.ID, Version: strconv.FormatInt(interaction.Revision, 10), EvidenceRef: evidence.ID}
		return nil
	})
	if err != nil {
		return err
	}
	actor := work.Actor{Kind: work.ActorPrincipal, ID: interaction.PrincipalID}
	var projectScope bool
	if err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		scope, e := tx.GetScope(view.ScopeID)
		projectScope = scope.Kind == work.ScopeProject
		return e
	}); err != nil {
		return err
	}
	if projectScope {
		if err = a.verifyWorkMembership(ctx, interaction.PrincipalID, view.ScopeID, interaction.Envelope.Tenant, interaction.Envelope.SenderIDs, interaction.Gateway); err != nil {
			return err
		}
	}
	if err = a.authorizeTeamControl(ctx, actor, view.ScopeID, state.Worker.ID); err != nil {
		return err
	}
	result := "runtime_observed"
	switch action {
	case "runtime.recover":
		recovered, e := a.teamRuntime.coordinator.Recover(ctx, workruntime.RecoveryRequest{ControlRequest: workruntime.ControlRequest{ScopeID: state.Worker.ScopeID, WorkerID: state.Worker.ID, AttemptID: state.Gate.AttemptID, ExpectedControllerGeneration: state.Worker.ControllerGeneration, Actor: actor}, GateReason: state.Gate.Reason, ExpectedGateRevision: state.Gate.Revision, Source: source})
		if e != nil {
			return e
		}
		result = recovered.OperationID
	case "runtime.observe":
		if state.Gate.AttemptID == "" {
			return fmt.Errorf("没有可关联的原生执行回执；需要核对执行进程，不能直接解除占用。")
		}
		if err = a.teamRuntime.coordinator.Reconcile(ctx, state.Gate.AttemptID); err != nil {
			return err
		}
	default:
		return fmt.Errorf("这个执行操作已不可用。")
	}
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		worker, e := tx.GetWorker(state.Worker.ID)
		if e != nil {
			return e
		}
		if e = a.refreshTeamRecoveryView(tx, worker); e != nil {
			return e
		}
		return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, result)
	})
}

// Retire visible controls when the gate changes without a human callback.
// This is a presentation update, never evidence that the task itself finished.
func (a *App) retireTeamRecoveryView(tx *workstore.Tx, worker work.WorkerSession, rebuilding bool) error {
	views, err := tx.ListUIViews(worker.ScopeID)
	if err != nil {
		return err
	}
	for _, old := range views {
		if old.SurfaceKey != "runtime:"+worker.ID || old.Invalidated || (old.Kind != "runtime.worker" && old.Kind != "runtime.rebuilding") {
			continue
		}
		if rebuilding && old.Kind == "runtime.rebuilding" {
			continue
		}
		view, err := workui.ContextDestination(tx, worker.ContextID)
		if err != nil {
			return err
		}
		view.ID = workui.ID("runtime-status", old.ID, strconv.FormatBool(rebuilding))
		view.SurfaceKey = old.SurfaceKey
		view.Kind = "runtime.status"
		if rebuilding {
			view.Kind = "runtime.rebuilding"
		}
		view.TargetID = worker.ID
		view.TargetRevision = worker.Revision
		operation, err := tx.GetOperation(old.OperationID)
		if err != nil {
			return err
		}
		if old.ExternalMessageID != "" && operation.State == work.OperationConfirmed && old.Binding == view.Binding && old.ChatID == view.ChatID && old.PrincipalID == view.PrincipalID {
			view.ReplacesViewID = old.ID
		}
		text := "上次的执行限制已经解除，原工作和待处理输入仍保留。后续进展会在这里更新。"
		if rebuilding {
			text = "已开始恢复执行，正在接回原工作和待处理输入。"
		}
		card, err := workui.Card(view.ID, workui.Panel{Title: "执行状态已更新", Paragraphs: []string{text}})
		if err != nil {
			return err
		}
		if _, err = workui.Queue(tx, view, card); err != nil {
			return err
		}
	}
	return nil
}

// Reconciliation starts on every terminal turn, before asynchronous observation.
// An unchanged gate retains CreatedAt in PutRuntimeGate, so repeated probes
// cannot postpone a persistent problem indefinitely.
func runtimeGateStillReconciling(gate workstore.RuntimeGate, now time.Time) bool {
	return gate.Reason == "native_reconciliation" && gate.RecoveryReason == "" && now.Sub(gate.CreatedAt) < 30*time.Second
}

func reuseTeamRecoveryView(tx *workstore.Tx, next *workstore.UIView) error {
	views, err := tx.ListUIViews(next.ScopeID)
	if err != nil {
		return err
	}
	for _, old := range views {
		if old.Invalidated || old.SurfaceKey != next.SurfaceKey || old.Binding != next.Binding || old.Tenant != next.Tenant || old.ChatID != next.ChatID || old.PrincipalID != next.PrincipalID || old.ExternalMessageID == "" {
			continue
		}
		operation, e := tx.GetOperation(old.OperationID)
		if e != nil {
			return e
		}
		if operation.State == work.OperationConfirmed {
			next.ReplacesViewID = old.ID
			return nil
		}
	}
	return nil
}

// A successful control request only refreshes observed state; it never implies
// that a remaining gate was cleared or that original work has finished.
func (a *App) refreshTeamRecoveryView(tx *workstore.Tx, worker work.WorkerSession) error {
	gate, err := tx.GetRuntimeGate(worker.ID)
	if errors.Is(err, workstore.ErrNotFound) {
		return a.retireTeamRecoveryView(tx, worker, false)
	}
	if err != nil {
		return err
	}
	if gate.Reason == "snapshot_rebuild_required" {
		return a.retireTeamRecoveryView(tx, worker, true)
	}
	_, err = a.queueTeamRecoveryView(tx, worker, gate)
	return err
}
