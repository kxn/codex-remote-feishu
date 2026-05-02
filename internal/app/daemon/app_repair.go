package daemon

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type repairChildAction string

const (
	repairChildActionNone            repairChildAction = "none"
	repairChildActionRestart         repairChildAction = "restart"
	repairChildActionRecoverHeadless repairChildAction = "recover_headless"
	repairChildActionSkipBusy        repairChildAction = "skip_busy"
	repairChildActionSkipUnsupported repairChildAction = "skip_unsupported"
)

type repairPlan struct {
	SurfaceID      string
	GatewayID      string
	InstanceID     string
	DisplayName    string
	ChildAction    repairChildAction
	ChildSkipCode  string
	ChildSkipText  string
	HeadlessThread string
	HeadlessTitle  string
	HeadlessCWD    string
}

type repairRunResult struct {
	FeishuReconnected bool
	FeishuSkipped     bool
	FeishuText        string
	ChildText         string
	Success           bool
}

func (a *App) handleRepairDaemonCommand(command control.DaemonCommand) []eventcontract.Event {
	plan := a.repairPlanLocked(command)
	go a.runRepairCommand(plan)
	return []eventcontract.Event{repairNoticeEvent(
		plan.SurfaceID,
		"repair_started",
		"正在修复当前飞书连接和运行时；如果当前实例可安全重启，会自动重启 provider child。",
	)}
}

func (a *App) repairPlanLocked(command control.DaemonCommand) repairPlan {
	surfaceID := strings.TrimSpace(command.SurfaceSessionID)
	gatewayID := firstNonEmpty(strings.TrimSpace(command.GatewayID), a.service.SurfaceGatewayID(surfaceID))
	plan := repairPlan{
		SurfaceID:   surfaceID,
		GatewayID:   gatewayID,
		ChildAction: repairChildActionNone,
	}

	instanceID := strings.TrimSpace(a.service.AttachedInstanceID(surfaceID))
	if instanceID == "" {
		return plan
	}
	inst := a.service.Instance(instanceID)
	if inst == nil {
		plan.ChildAction = repairChildActionSkipUnsupported
		plan.ChildSkipCode = "repair_instance_missing"
		plan.ChildSkipText = "当前接管实例记录已失效；已尝试重连飞书 bot，请重新接管工作区后继续。"
		return plan
	}
	displayName := firstNonEmpty(strings.TrimSpace(inst.DisplayName), strings.TrimSpace(inst.ShortName), instanceID)
	plan.InstanceID = instanceID
	plan.DisplayName = displayName

	if reason := restartChildBusyMessage(a.service.Surface(surfaceID), inst, a.service.PendingRemoteTurns(), a.service.ActiveRemoteTurns(), instanceID, displayName); reason != "" {
		plan.ChildAction = repairChildActionSkipBusy
		plan.ChildSkipCode = "repair_child_busy"
		plan.ChildSkipText = reason
		return plan
	}
	if a.childRestartWaitInFlightLocked(instanceID) {
		plan.ChildAction = repairChildActionSkipBusy
		plan.ChildSkipCode = "repair_child_already_running"
		plan.ChildSkipText = fmt.Sprintf("%s 已经在等待上一次 child restart 结果，请稍后再试。", displayName)
		return plan
	}
	if inst.Online {
		plan.ChildAction = repairChildActionRestart
		return plan
	}
	if headless, ok := repairHeadlessRestoreAttempt(a.service.Surface(surfaceID), inst); ok {
		plan.ChildAction = repairChildActionRecoverHeadless
		plan.HeadlessThread = headless.ThreadID
		plan.HeadlessTitle = headless.ThreadTitle
		plan.HeadlessCWD = headless.ThreadCWD
		return plan
	}
	plan.ChildAction = repairChildActionSkipUnsupported
	plan.ChildSkipCode = "repair_instance_offline"
	plan.ChildSkipText = fmt.Sprintf("%s 当前不在线，且没有足够的 headless 恢复信息；已尝试重连飞书 bot，请重新接管工作区后继续。", displayName)
	return plan
}

func repairHeadlessRestoreAttempt(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) (orchestrator.HeadlessRestoreAttempt, bool) {
	if surface == nil || inst == nil {
		return orchestrator.HeadlessRestoreAttempt{}, false
	}
	if !isManagedHeadlessInstance(inst) {
		return orchestrator.HeadlessRestoreAttempt{}, false
	}
	threadID := strings.TrimSpace(surface.SelectedThreadID)
	if threadID == "" {
		threadID = strings.TrimSpace(inst.ActiveThreadID)
	}
	if threadID == "" {
		threadID = strings.TrimSpace(inst.ObservedFocusedThreadID)
	}
	if threadID == "" {
		return orchestrator.HeadlessRestoreAttempt{}, false
	}
	threadTitle := ""
	threadCWD := ""
	if thread := inst.Threads[threadID]; thread != nil {
		threadTitle = strings.TrimSpace(thread.Name)
		threadCWD = strings.TrimSpace(thread.CWD)
	}
	if threadCWD == "" {
		threadCWD = firstNonEmpty(strings.TrimSpace(surface.PreparedThreadCWD), strings.TrimSpace(inst.WorkspaceRoot), strings.TrimSpace(inst.WorkspaceKey))
	}
	if threadCWD == "" {
		return orchestrator.HeadlessRestoreAttempt{}, false
	}
	return orchestrator.HeadlessRestoreAttempt{
		ThreadID:    threadID,
		ThreadTitle: threadTitle,
		ThreadCWD:   threadCWD,
	}, true
}

func isManagedHeadlessInstance(inst *state.InstanceRecord) bool {
	return inst != nil && strings.EqualFold(strings.TrimSpace(inst.Source), "headless") && inst.Managed
}

func (a *App) runRepairCommand(plan repairPlan) {
	result := repairRunResult{Success: true}
	if text, ok, err := a.reconnectFeishuRuntimeForRepair(plan.GatewayID); err != nil {
		result.Success = false
		result.FeishuText = fmt.Sprintf("飞书 bot runtime 重连失败：%v", err)
	} else {
		result.FeishuReconnected = ok
		result.FeishuSkipped = !ok
		result.FeishuText = text
		if !ok {
			result.Success = false
		}
	}

	switch plan.ChildAction {
	case repairChildActionRestart:
		text, ok := a.restartChildForRepair(plan)
		result.ChildText = text
		if !ok {
			result.Success = false
		}
	case repairChildActionRecoverHeadless:
		text, ok := a.recoverHeadlessForRepair(plan)
		result.ChildText = text
		if !ok {
			result.Success = false
		}
	case repairChildActionSkipBusy, repairChildActionSkipUnsupported:
		result.ChildText = plan.ChildSkipText
		result.Success = false
	default:
		result.ChildText = "当前没有可重启的 attached instance；飞书 bot runtime 已按需处理。"
	}

	a.finishRepairCommand(plan.SurfaceID, result)
}

func (a *App) reconnectFeishuRuntimeForRepair(gatewayID string) (string, bool, error) {
	gatewayID = canonicalGatewayID(gatewayID)
	if gatewayID == "" {
		return "当前 surface 没有可识别的飞书 bot gateway；跳过 bot runtime 重连。", false, nil
	}
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return "", false, err
	}
	if !repairRuntimeConfigHasGateway(loaded.Config, a.snapshotAdminRuntime(), gatewayID) {
		return fmt.Sprintf("找不到 gateway `%s` 的飞书 bot runtime 配置；跳过 bot runtime 重连。", gatewayID), false, nil
	}
	if err := a.applyRuntimeFeishuConfig(loaded.Config, gatewayID); err != nil {
		return "", false, err
	}
	logRepairGatewayStatus(a.gateway, gatewayID)
	return fmt.Sprintf("已重连飞书 bot `%s`。", gatewayID), true, nil
}

func repairRuntimeConfigHasGateway(cfg config.AppConfig, admin adminRuntimeState, gatewayID string) bool {
	gatewayID = canonicalGatewayID(gatewayID)
	for _, app := range cfg.Feishu.Apps {
		if canonicalGatewayID(app.ID) == gatewayID {
			return true
		}
	}
	return admin.envOverrideActive && canonicalGatewayID(admin.envOverrideGatewayID) == gatewayID
}

func (a *App) restartChildForRepair(plan repairPlan) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), childRestartOutcomeTimeout)
	defer cancel()
	skipped, skipOK, err := a.restartRelayChildCodexAndWaitForRepair(ctx, plan)
	if strings.TrimSpace(skipped) != "" {
		return skipped, skipOK
	}
	if err != nil {
		return fmt.Sprintf("重启 %s 的 provider child 失败：%s", plan.DisplayName, err.Error()), false
	}
	return fmt.Sprintf("已重启 %s 的 provider child。", plan.DisplayName), true
}

func (a *App) restartRelayChildCodexAndWaitForRepair(ctx context.Context, plan repairPlan) (string, bool, error) {
	command, err := a.newRelayChildCodexRestartCommand(plan.InstanceID)
	if err != nil {
		return "", false, err
	}

	a.mu.Lock()
	skipped, skipOK := a.repairRestartSkipMessageLocked(plan)
	var waitCh <-chan error
	if strings.TrimSpace(skipped) == "" {
		waitCh = a.registerChildRestartWaitLocked(plan.InstanceID, command.CommandID)
	}
	a.mu.Unlock()
	if strings.TrimSpace(skipped) != "" {
		return skipped, skipOK, nil
	}

	if err := a.sendRelayChildRestartCommand(plan.InstanceID, command); err != nil {
		a.mu.Lock()
		a.unregisterChildRestartWaitLocked(command.CommandID)
		a.mu.Unlock()
		return "", false, err
	}

	select {
	case err := <-waitCh:
		return "", false, err
	case <-ctx.Done():
		a.mu.Lock()
		waiter := a.unregisterChildRestartWaitLocked(command.CommandID)
		a.mu.Unlock()
		if ctx.Err() != nil && ctx.Err() != context.DeadlineExceeded {
			return "", false, ctx.Err()
		}
		return "", false, childRestartWaitTimeoutProblem(command.CommandID, waiter)
	}
}

func (a *App) repairRestartSkipMessageLocked(plan repairPlan) (string, bool) {
	instanceID := strings.TrimSpace(plan.InstanceID)
	displayName := firstNonEmpty(strings.TrimSpace(plan.DisplayName), instanceID)
	if a.shuttingDown {
		return "daemon 正在关闭；已跳过 provider child 修复。", false
	}
	surface := a.service.Surface(plan.SurfaceID)
	if surface == nil {
		return "当前 surface 状态已经变化；已重连飞书 bot，请重新接管工作区后继续。", false
	}
	if strings.TrimSpace(surface.AttachedInstanceID) != instanceID {
		return "当前接管实例已经变化；已重连飞书 bot，请继续在新的接管状态下使用。", true
	}
	inst := a.service.Instance(instanceID)
	if inst == nil {
		return "当前接管实例记录已失效；已重连飞书 bot，请重新接管工作区后继续。", false
	}
	if !inst.Online {
		return fmt.Sprintf("%s 当前已离线；已重连飞书 bot，请重新接管工作区后继续。", displayName), false
	}
	if a.childRestartWaitInFlightLocked(instanceID) {
		return fmt.Sprintf("%s 已经在等待上一次 child restart 结果，请稍后再试。", displayName), false
	}
	if reason := restartChildBusyMessage(surface, inst, a.service.PendingRemoteTurns(), a.service.ActiveRemoteTurns(), instanceID, displayName); reason != "" {
		return reason, false
	}
	return "", true
}

func (a *App) recoverHeadlessForRepair(plan repairPlan) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.shuttingDown {
		return "", false
	}
	inst := a.service.Instance(plan.InstanceID)
	surface := a.service.Surface(plan.SurfaceID)
	if inst == nil || surface == nil || strings.TrimSpace(surface.AttachedInstanceID) != plan.InstanceID {
		return "当前 headless 实例状态已经变化；请稍后重试。", false
	}
	if inst.Online {
		return fmt.Sprintf("%s 已重新在线；请直接继续发送消息。", firstNonEmpty(strings.TrimSpace(plan.DisplayName), plan.InstanceID)), true
	}
	if !isManagedHeadlessInstance(inst) {
		return "当前实例已不再是 managed headless；已重连飞书 bot，但不会清理或重建这个实例。", false
	}
	if reason := restartChildBusyMessage(surface, inst, a.service.PendingRemoteTurns(), a.service.ActiveRemoteTurns(), plan.InstanceID, firstNonEmpty(strings.TrimSpace(plan.DisplayName), plan.InstanceID)); reason != "" {
		return reason, false
	}
	delete(a.managedHeadlessRuntime.Processes, plan.InstanceID)
	a.service.RemoveInstance(plan.InstanceID)
	events, result := a.service.TryAutoRestoreHeadless(plan.SurfaceID, orchestrator.HeadlessRestoreAttempt{
		ThreadID:    plan.HeadlessThread,
		ThreadTitle: plan.HeadlessTitle,
		ThreadCWD:   plan.HeadlessCWD,
	}, true)
	if len(events) != 0 {
		a.handleUIEventsLocked(context.Background(), events)
	}
	a.syncSurfaceResumeStateForSurfacesLocked([]string{plan.SurfaceID}, nil)
	switch result.Status {
	case orchestrator.HeadlessRestoreStatusAttached:
		return "已移除离线 headless，并恢复到可用实例。", true
	case orchestrator.HeadlessRestoreStatusStarting:
		return "已移除离线 headless，并启动新的 headless 接手当前会话。", true
	case orchestrator.HeadlessRestoreStatusWaiting, orchestrator.HeadlessRestoreStatusSkipped:
		return "已清理离线 headless；恢复流程仍在等待可用目标，请稍后再试。", true
	default:
		return "已清理离线 headless，但自动恢复没有成功；请重新接管工作区。", false
	}
}

func (a *App) finishRepairCommand(surfaceID string, result repairRunResult) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.shuttingDown {
		return
	}
	code := "repair_succeeded"
	if !result.Success {
		code = "repair_partial"
	}
	a.handleUIEventsLocked(context.Background(), []eventcontract.Event{repairNoticeEvent(surfaceID, code, formatRepairResultText(result))})
}

func formatRepairResultText(result repairRunResult) string {
	lines := make([]string, 0, 3)
	if strings.TrimSpace(result.FeishuText) != "" {
		lines = append(lines, strings.TrimSpace(result.FeishuText))
	}
	if strings.TrimSpace(result.ChildText) != "" {
		lines = append(lines, strings.TrimSpace(result.ChildText))
	}
	if result.Success {
		lines = append(lines, "现在可以继续使用；如果当前任务仍在运行，请等待完成或按提示处理。")
	} else {
		lines = append(lines, "修复未完全完成；请按提示重试或重新接管。")
	}
	return strings.Join(lines, "\n")
}

func repairNoticeEvent(surfaceID, code, text string) eventcontract.Event {
	return eventcontract.Event{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: strings.TrimSpace(surfaceID),
		Notice: &control.Notice{
			Code:  strings.TrimSpace(code),
			Title: "Repair",
			Text:  strings.TrimSpace(text),
		},
	}
}

func gatewayStatusByID(gateway feishu.Gateway, gatewayID string) (feishu.GatewayStatus, bool) {
	gatewayID = canonicalGatewayID(gatewayID)
	if gatewayID == "" {
		return feishu.GatewayStatus{}, false
	}
	for _, status := range gatewayStatuses(gateway) {
		if canonicalGatewayID(status.GatewayID) == gatewayID {
			return status, true
		}
	}
	return feishu.GatewayStatus{}, false
}

func logRepairGatewayStatus(gateway feishu.Gateway, gatewayID string) {
	if status, ok := gatewayStatusByID(gateway, gatewayID); ok {
		log.Printf("repair feishu gateway status after reconnect: gateway=%s state=%s last_error=%s", status.GatewayID, status.State, status.LastError)
	}
}
