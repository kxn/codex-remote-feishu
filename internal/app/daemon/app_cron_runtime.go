package daemon

import (
	"context"
	"log"
	"strings"
	"time"

	cronrt "github.com/kxn/codex-remote-feishu/internal/app/cronruntime"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func isCronInstanceID(instanceID string) bool {
	return strings.HasPrefix(strings.TrimSpace(instanceID), cronInstancePrefix)
}

func (a *App) handleCronHelloLocked(_ context.Context, hello agentproto.Hello) bool {
	instanceID := strings.TrimSpace(hello.Instance.InstanceID)
	if !isCronInstanceID(instanceID) {
		return false
	}
	now := time.Now().UTC()
	run := a.cronRuntime.runs[instanceID]
	if run == nil {
		log.Printf("cron hidden instance connected without active run: instance=%s pid=%d", instanceID, hello.Instance.PID)
		a.requestCronInstanceExitLocked(instanceID, hello.Instance.PID, now)
		return true
	}
	if hello.Instance.PID > 0 {
		run.PID = hello.Instance.PID
	}
	if strings.TrimSpace(run.CommandID) != "" {
		return true
	}
	command := agentproto.Command{
		CommandID: a.nextCommandID(),
		Kind:      agentproto.CommandPromptSend,
		Target: agentproto.Target{
			CreateThreadIfMissing: true,
			InternalHelper:        true,
			CWD:                   firstNonEmpty(strings.TrimSpace(run.RunDirectory), strings.TrimSpace(run.WorkspaceKey)),
		},
		Prompt: agentproto.Prompt{
			Inputs: []agentproto.Input{{
				Type: agentproto.InputText,
				Text: run.Prompt,
			}},
		},
	}
	a.mu.Unlock()
	err := a.sendAgentCommand(instanceID, command)
	a.mu.Lock()
	run = a.cronRuntime.runs[instanceID]
	if run == nil {
		return true
	}
	if strings.TrimSpace(run.CommandID) != "" {
		return true
	}
	if err != nil {
		run.CommandID = command.CommandID
		run.ErrorMessage = err.Error()
		a.completeCronRunLocked(instanceID, "failed", "cron 隐藏执行启动失败："+err.Error(), now, true)
		return true
	}
	run.CommandID = command.CommandID
	log.Printf("cron hidden prompt sent: instance=%s command=%s source=%s cwd=%s", instanceID, command.CommandID, run.SourceLabel, command.Target.CWD)
	return true
}

func (a *App) handleCronEventsLocked(_ context.Context, instanceID string, events []agentproto.Event) bool {
	if !isCronInstanceID(instanceID) {
		return false
	}
	run := a.cronRuntime.runs[instanceID]
	if run == nil {
		return true
	}
	for _, event := range events {
		now := time.Now().UTC()
		switch event.Kind {
		case agentproto.EventTurnStarted:
			if strings.TrimSpace(event.ThreadID) != "" {
				run.ThreadID = strings.TrimSpace(event.ThreadID)
			}
			if strings.TrimSpace(event.TurnID) != "" {
				run.TurnID = strings.TrimSpace(event.TurnID)
			}
			if run.StartedAt.IsZero() {
				run.StartedAt = now
			}
		case agentproto.EventItemStarted:
			if strings.TrimSpace(event.ItemKind) == "agent_message" {
				a.ensureCronItemBufferLocked(run, event.ItemID, event.ItemKind)
			}
		case agentproto.EventItemDelta:
			if strings.TrimSpace(event.ItemKind) == "agent_message" && strings.TrimSpace(event.Delta) != "" {
				buf := a.ensureCronItemBufferLocked(run, event.ItemID, event.ItemKind)
				buf.Chunks = append(buf.Chunks, event.Delta)
				run.PendingFinalText = strings.TrimSpace(strings.Join(buf.Chunks, ""))
			}
		case agentproto.EventItemCompleted:
			if strings.TrimSpace(event.ItemKind) == "agent_message" {
				text := ""
				if metadataText, _ := event.Metadata["text"].(string); strings.TrimSpace(metadataText) != "" {
					text = strings.TrimSpace(metadataText)
				}
				if buf := a.ensureCronItemBufferLocked(run, event.ItemID, event.ItemKind); len(buf.Chunks) > 0 {
					buffered := strings.TrimSpace(strings.Join(buf.Chunks, ""))
					if text == "" {
						text = buffered
					}
					delete(run.Buffers, cronItemBufferKey(event.ItemID))
				}
				if text != "" {
					run.FinalMessage = text
					run.PendingFinalText = text
				}
			}
		case agentproto.EventSystemError:
			message := strings.TrimSpace(event.ErrorMessage)
			if event.Problem != nil {
				message = firstNonEmpty(strings.TrimSpace(event.Problem.Message), message, event.Problem.Error())
			}
			a.completeCronRunLocked(instanceID, "failed", firstNonEmpty(message, "cron 隐藏执行遇到系统错误"), now, true)
			return true
		case agentproto.EventTurnCompleted:
			if strings.TrimSpace(event.ThreadID) != "" {
				run.ThreadID = strings.TrimSpace(event.ThreadID)
			}
			if strings.TrimSpace(event.TurnID) != "" {
				run.TurnID = strings.TrimSpace(event.TurnID)
			}
			status := strings.TrimSpace(event.Status)
			switch status {
			case "", "completed":
				a.completeCronRunLocked(instanceID, "completed", "", now, true)
			case "failed":
				a.completeCronRunLocked(instanceID, "failed", firstNonEmpty(strings.TrimSpace(event.ErrorMessage), "cron 隐藏执行失败"), now, true)
			case "cancelled", "canceled", "interrupted":
				a.completeCronRunLocked(instanceID, "failed", firstNonEmpty(strings.TrimSpace(event.ErrorMessage), "cron 隐藏执行被中断"), now, true)
			default:
				a.completeCronRunLocked(instanceID, "failed", firstNonEmpty(strings.TrimSpace(event.ErrorMessage), "cron 隐藏执行状态异常："+status), now, true)
			}
			return true
		}
	}
	return true
}

func (a *App) handleCronCommandAckLocked(_ context.Context, instanceID string, ack agentproto.CommandAck) bool {
	if !isCronInstanceID(instanceID) {
		return false
	}
	if ack.Accepted {
		return true
	}
	run := a.cronRuntime.runs[instanceID]
	if run == nil {
		return true
	}
	if strings.TrimSpace(run.CommandID) != "" && strings.TrimSpace(ack.CommandID) != "" && strings.TrimSpace(run.CommandID) != strings.TrimSpace(ack.CommandID) {
		return true
	}
	message := firstNonEmpty(strings.TrimSpace(ack.Error), "cron 隐藏执行命令未被接受")
	if ack.Problem != nil {
		message = firstNonEmpty(strings.TrimSpace(ack.Problem.Message), message, ack.Problem.Error())
	}
	a.completeCronRunLocked(instanceID, "failed", message, time.Now().UTC(), true)
	return true
}

func (a *App) handleCronDisconnectLocked(_ context.Context, instanceID string) bool {
	if !isCronInstanceID(instanceID) {
		return false
	}
	delete(a.cronRuntime.exitTargets, instanceID)
	run := a.cronRuntime.runs[instanceID]
	if run == nil {
		return true
	}
	a.completeCronRunLocked(instanceID, "failed", "cron 隐藏执行在完成前断开连接", time.Now().UTC(), false)
	return true
}

func (a *App) ensureCronItemBufferLocked(run *cronRunState, itemID, itemKind string) *cronItemBuffer {
	return cronrt.EnsureItemBuffer(run, itemID, itemKind)
}

func (a *App) addCronActiveRunLocked(jobRecordID, jobName, instanceID string) {
	activeKey := cronJobActiveKey(jobRecordID, jobName)
	instanceID = strings.TrimSpace(instanceID)
	if activeKey == "" || instanceID == "" {
		return
	}
	runs := a.cronRuntime.jobActiveRuns[activeKey]
	if runs == nil {
		runs = map[string]struct{}{}
		a.cronRuntime.jobActiveRuns[activeKey] = runs
	}
	runs[instanceID] = struct{}{}
}

func (a *App) removeCronActiveRunLocked(jobRecordID, jobName, instanceID string) {
	activeKey := cronJobActiveKey(jobRecordID, jobName)
	instanceID = strings.TrimSpace(instanceID)
	if activeKey == "" || instanceID == "" {
		return
	}
	runs := a.cronRuntime.jobActiveRuns[activeKey]
	if len(runs) == 0 {
		delete(a.cronRuntime.jobActiveRuns, activeKey)
		return
	}
	delete(runs, instanceID)
	if len(runs) == 0 {
		delete(a.cronRuntime.jobActiveRuns, activeKey)
	}
}

func (a *App) cronActiveRunCountLocked(jobRecordID, jobName string) int {
	activeKey := cronJobActiveKey(jobRecordID, jobName)
	if activeKey == "" {
		return 0
	}
	runs := a.cronRuntime.jobActiveRuns[activeKey]
	if len(runs) == 0 {
		delete(a.cronRuntime.jobActiveRuns, activeKey)
		return 0
	}
	activeCount := 0
	for instanceID := range runs {
		if a.cronRuntime.runs[instanceID] == nil {
			delete(runs, instanceID)
			continue
		}
		activeCount++
	}
	if len(runs) == 0 {
		delete(a.cronRuntime.jobActiveRuns, activeKey)
	}
	return activeCount
}

func (a *App) completeCronRunLocked(instanceID, status, errorMessage string, now time.Time, requestExit bool) {
	run := a.cronRuntime.runs[instanceID]
	if run == nil {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if run.CompletedAt.IsZero() {
		run.CompletedAt = now
	}
	run.Status = strings.TrimSpace(status)
	if strings.TrimSpace(errorMessage) != "" && strings.TrimSpace(run.ErrorMessage) == "" {
		run.ErrorMessage = strings.TrimSpace(errorMessage)
	}
	if strings.TrimSpace(run.FinalMessage) == "" && strings.TrimSpace(run.PendingFinalText) != "" {
		run.FinalMessage = strings.TrimSpace(run.PendingFinalText)
	}
	if run.StartedAt.IsZero() && run.Status == "timeout" {
		run.StartedAt = run.TriggeredAt
	}
	a.removeCronActiveRunLocked(run.JobRecordID, run.JobName, instanceID)
	writeTarget := run.WritebackTarget
	if !writeTarget.Valid() {
		writeTarget = a.snapshotCronWritebackLocked()
	}
	completedRun := *run
	if run.Buffers != nil {
		completedRun.Buffers = nil
	}
	delete(a.cronRuntime.runs, instanceID)
	if requestExit {
		a.requestCronInstanceExitLocked(instanceID, run.PID, now)
	}
	if !writeTarget.Valid() {
		log.Printf("cron run completed without writeback target: instance=%s status=%s", instanceID, completedRun.Status)
		go a.cleanupCronRunResources(completedRun)
		return
	}
	go a.writeCronRunResultAsync(writeTarget, completedRun)
	go a.cleanupCronRunResources(completedRun)
}

func (a *App) snapshotCronWritebackLocked() cronWritebackTarget {
	if !cronStateHasBinding(a.cronRuntime.state) || a.cronRuntime.state == nil || a.cronRuntime.state.Bitable == nil {
		return cronWritebackTarget{}
	}
	if strings.TrimSpace(a.cronRuntime.state.OwnerGatewayID) == "" {
		_, _ = a.migrateCronLegacyOwnerStateLocked(a.cronRuntime.state)
	}
	gatewayID := strings.TrimSpace(a.cronRuntime.state.OwnerGatewayID)
	target := cronWritebackTarget{
		GatewayID: gatewayID,
		Bitable:   *a.cronRuntime.state.Bitable,
	}
	return target
}

func (a *App) requestCronInstanceExitLocked(instanceID string, pid int, now time.Time) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return
	}
	if pid <= 0 {
		if snap, ok := a.snapshotRelayConnections()[instanceID]; ok && snap.PID > 0 {
			pid = snap.PID
		}
	}
	deadline := now.Add(cronExitGrace)
	command := agentproto.Command{
		CommandID: a.nextCommandID(),
		Kind:      agentproto.CommandProcessExit,
	}
	a.mu.Unlock()
	err := a.sendAgentCommand(instanceID, command)
	a.mu.Lock()
	if err != nil {
		log.Printf("cron process.exit send failed: instance=%s err=%v", instanceID, err)
		deadline = now
	}
	if pid > 0 {
		target := a.cronRuntime.exitTargets[instanceID]
		if target == nil {
			target = &cronExitTarget{InstanceID: instanceID}
			a.cronRuntime.exitTargets[instanceID] = target
		}
		target.PID = pid
		target.Deadline = deadline
	}
}

func (a *App) writeCronRunResultAsync(target cronWritebackTarget, run cronRunState) {
	api, err := a.cronBitableAPI(target.GatewayID)
	if err != nil {
		log.Printf("cron writeback failed: instance=%s err=%v", run.InstanceID, err)
		return
	}

	statusText := cronStatusText(run.Status)
	summary := cronRunSummary(firstNonEmpty(run.FinalMessage, run.ErrorMessage, statusText))
	taskName := firstNonEmpty(strings.TrimSpace(run.JobName), strings.TrimSpace(run.JobRecordID), strings.TrimSpace(run.InstanceID))
	runFields := map[string]any{
		"任务名":   taskName,
		"触发时间":  cronMilliseconds(run.TriggeredAt),
		"开始时间":  cronMilliseconds(run.StartedAt),
		"结束时间":  cronMilliseconds(run.CompletedAt),
		"状态":    statusText,
		"耗时（秒）": cronElapsedSeconds(run.StartedAt, run.CompletedAt),
		"工作区":   firstNonEmpty(strings.TrimSpace(run.SourceLabel), strings.TrimSpace(run.WorkspaceKey)),
		"结果摘要":  summary,
		"最终回复":  strings.TrimSpace(run.FinalMessage),
		"错误信息":  strings.TrimSpace(run.ErrorMessage),
	}
	runsCtx, cancelRuns := context.WithTimeout(context.Background(), cronWritebackRunsTTL)
	if _, err := api.CreateRecord(runsCtx, target.Bitable.AppToken, target.Bitable.Tables.Runs, runFields); err != nil {
		log.Printf("cron run history write failed: instance=%s job=%s err=%v", run.InstanceID, taskName, err)
	}
	cancelRuns()
	if strings.TrimSpace(run.JobRecordID) == "" {
		return
	}
	recentTime := run.CompletedAt
	if recentTime.IsZero() {
		recentTime = run.TriggeredAt
	}
	taskFields := map[string]any{
		"最近运行时间": cronMilliseconds(recentTime),
		"最近状态":   statusText,
		"最近结果摘要": summary,
		"最近错误":   strings.TrimSpace(run.ErrorMessage),
	}
	taskCtx, cancelTask := context.WithTimeout(context.Background(), cronWritebackTasksTTL)
	if _, err := api.UpdateRecord(taskCtx, target.Bitable.AppToken, target.Bitable.Tables.Tasks, run.JobRecordID, taskFields); err != nil {
		log.Printf("cron task status write failed: instance=%s job=%s record=%s err=%v", run.InstanceID, taskName, run.JobRecordID, err)
	}
	cancelTask()
}
