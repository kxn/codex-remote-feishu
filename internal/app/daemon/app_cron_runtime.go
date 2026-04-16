package daemon

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type cronWritebackTarget struct {
	GatewayID string
	Bitable   cronBitableState
}

func isCronInstanceID(instanceID string) bool {
	return strings.HasPrefix(strings.TrimSpace(instanceID), cronInstancePrefix)
}

func (a *App) handleCronHelloLocked(_ context.Context, hello agentproto.Hello) bool {
	instanceID := strings.TrimSpace(hello.Instance.InstanceID)
	if !isCronInstanceID(instanceID) {
		return false
	}
	now := time.Now().UTC()
	run := a.cronRuns[instanceID]
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
	run = a.cronRuns[instanceID]
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
	run := a.cronRuns[instanceID]
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
	run := a.cronRuns[instanceID]
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
	delete(a.cronExitTargets, instanceID)
	run := a.cronRuns[instanceID]
	if run == nil {
		return true
	}
	a.completeCronRunLocked(instanceID, "failed", "cron 隐藏执行在完成前断开连接", time.Now().UTC(), false)
	return true
}

func (a *App) ensureCronItemBufferLocked(run *cronRunState, itemID, itemKind string) *cronItemBuffer {
	if run == nil {
		return &cronItemBuffer{}
	}
	if run.Buffers == nil {
		run.Buffers = map[string]*cronItemBuffer{}
	}
	key := cronItemBufferKey(itemID)
	if existing := run.Buffers[key]; existing != nil {
		if existing.ItemKind == "" {
			existing.ItemKind = itemKind
		}
		return existing
	}
	buf := &cronItemBuffer{
		ItemID:   strings.TrimSpace(itemID),
		ItemKind: strings.TrimSpace(itemKind),
	}
	run.Buffers[key] = buf
	return buf
}

func cronItemBufferKey(itemID string) string {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return "__default__"
	}
	return itemID
}

func cronJobActiveKey(jobRecordID, jobName string) string {
	jobRecordID = strings.TrimSpace(jobRecordID)
	if jobRecordID != "" {
		return jobRecordID
	}
	jobName = strings.TrimSpace(jobName)
	if jobName != "" {
		return "name:" + jobName
	}
	return ""
}

func (a *App) completeCronRunLocked(instanceID, status, errorMessage string, now time.Time, requestExit bool) {
	run := a.cronRuns[instanceID]
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
	if activeKey := cronJobActiveKey(run.JobRecordID, run.JobName); activeKey != "" {
		if activeInstanceID := strings.TrimSpace(a.cronJobActiveRuns[activeKey]); activeInstanceID == instanceID {
			delete(a.cronJobActiveRuns, activeKey)
		}
	}
	writeTarget := run.WritebackTarget
	if !writeTarget.valid() {
		writeTarget = a.snapshotCronWritebackLocked()
	}
	completedRun := *run
	if run.Buffers != nil {
		completedRun.Buffers = nil
	}
	delete(a.cronRuns, instanceID)
	if requestExit {
		a.requestCronInstanceExitLocked(instanceID, run.PID, now)
	}
	if !writeTarget.valid() {
		log.Printf("cron run completed without writeback target: instance=%s status=%s", instanceID, completedRun.Status)
		go a.cleanupCronRunResources(completedRun)
		return
	}
	go a.writeCronRunResultAsync(writeTarget, completedRun)
	go a.cleanupCronRunResources(completedRun)
}

func (a *App) snapshotCronWritebackLocked() cronWritebackTarget {
	if !cronStateHasBinding(a.cronState) || a.cronState == nil || a.cronState.Bitable == nil {
		return cronWritebackTarget{}
	}
	gatewayID := strings.TrimSpace(a.cronState.OwnerGatewayID)
	if gatewayID == "" {
		gatewayID = strings.TrimSpace(a.cronState.GatewayID)
	}
	target := cronWritebackTarget{
		GatewayID: gatewayID,
		Bitable:   *a.cronState.Bitable,
	}
	return target
}

func (t cronWritebackTarget) valid() bool {
	return strings.TrimSpace(t.GatewayID) != "" && strings.TrimSpace(t.Bitable.AppToken) != "" && strings.TrimSpace(t.Bitable.Tables.Runs) != "" && strings.TrimSpace(t.Bitable.Tables.Tasks) != ""
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
		a.cronExitTargets[instanceID] = &cronExitTarget{
			InstanceID: instanceID,
			PID:        pid,
			Deadline:   deadline,
		}
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
