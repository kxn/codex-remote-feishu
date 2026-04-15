package daemon

import (
	"fmt"
	"log"
	"strings"
	"time"
)

func (a *App) maybeScheduleCronJobsLocked(now time.Time) {
	if a.cronSyncInFlight {
		return
	}
	if !a.cronNextScheduleScan.IsZero() && now.Before(a.cronNextScheduleScan) {
		return
	}
	a.cronNextScheduleScan = nextCronScheduleScan(now)
	a.maybeTimeoutCronRunsLocked(now)

	stateValue, err := a.loadCronStateLocked(false)
	if err != nil {
		log.Printf("cron scheduler skipped: load state failed: %v", err)
		return
	}
	if stateValue == nil || len(stateValue.Jobs) == 0 {
		return
	}

	now = cronSchedulerTime(now)
	dirty := false
	for idx := range stateValue.Jobs {
		job := &stateValue.Jobs[idx]
		if job.NextRunAt.IsZero() {
			job.NextRunAt = cronNextRunAt(*job, now)
			dirty = true
		}
		if job.NextRunAt.IsZero() || job.NextRunAt.After(now) {
			continue
		}
		currentDueAt := job.NextRunAt
		nextRunAt := cronAdvanceRunAt(*job, currentDueAt, now)
		if activeInstanceID := strings.TrimSpace(a.cronJobActiveRuns[cronJobActiveKey(job.RecordID, job.Name)]); activeInstanceID != "" {
			if a.cronRuns[activeInstanceID] != nil {
				a.recordCronImmediateResultLocked(*job, now, "skipped", "上一轮运行尚未结束，本次按 V1 规则跳过。")
				job.NextRunAt = nextRunAt
				dirty = true
				continue
			}
			delete(a.cronJobActiveRuns, cronJobActiveKey(job.RecordID, job.Name))
		}
		job.NextRunAt = nextRunAt
		dirty = true
		if err := a.launchCronRunLocked(*job, now); err != nil {
			a.recordCronImmediateResultLocked(*job, now, "failed", err.Error())
		}
	}
	if dirty {
		if err := a.writeCronStateLocked(); err != nil {
			log.Printf("cron scheduler state write failed: %v", err)
		}
	}
}

func (a *App) maybeTimeoutCronRunsLocked(now time.Time) {
	for instanceID, run := range a.cronRuns {
		if run == nil {
			delete(a.cronRuns, instanceID)
			continue
		}
		timeoutAt := run.TriggeredAt
		if !run.StartedAt.IsZero() {
			timeoutAt = run.StartedAt
		}
		if timeoutAt.IsZero() {
			continue
		}
		timeout := time.Duration(cronDefaultTimeoutMinutes(run.TimeoutMinutes)) * time.Minute
		if timeout <= 0 || now.Before(timeoutAt.Add(timeout)) {
			continue
		}
		a.completeCronRunLocked(instanceID, "timeout", fmt.Sprintf("任务超过 %d 分钟未完成，已按超时结束。", cronDefaultTimeoutMinutes(run.TimeoutMinutes)), now, true)
	}
}

func (a *App) launchCronRunLocked(job cronJobState, now time.Time) error {
	cfg := a.headlessRuntime
	if strings.TrimSpace(cfg.BinaryPath) == "" {
		return fmt.Errorf("headless binary 未配置，无法执行 Cron 任务")
	}
	workspaceRoot, err := normalizeWorkspaceRoot(job.WorkspaceKey)
	if err != nil {
		return fmt.Errorf("工作区不可用：%w", err)
	}
	instanceID := cronInstanceIDForRun(job.RecordID, now)
	displayName := firstNonEmpty(strings.TrimSpace(job.Name), "cron")
	env := append([]string{}, cfg.BaseEnv...)
	env = append(env,
		"CODEX_REMOTE_INSTANCE_ID="+instanceID,
		"CODEX_REMOTE_INSTANCE_SOURCE=headless",
		"CODEX_REMOTE_LIFETIME=daemon-owned",
		"CODEX_REMOTE_INSTANCE_DISPLAY_NAME=cron:"+displayName,
	)
	pid, err := a.startHeadless(controlToHeadlessLaunch(cfg, env, workspaceRoot, instanceID))
	if err != nil {
		return fmt.Errorf("启动隐藏执行失败：%w", err)
	}
	writebackTarget := a.snapshotCronWritebackLocked()
	run := &cronRunState{
		RunID:           instanceID,
		InstanceID:      instanceID,
		GatewayID:       strings.TrimSpace(writebackTarget.GatewayID),
		WritebackTarget: writebackTarget,
		JobRecordID:     strings.TrimSpace(job.RecordID),
		JobName:         firstNonEmpty(strings.TrimSpace(job.Name), displayName),
		WorkspaceKey:    workspaceRoot,
		Prompt:          strings.TrimSpace(job.Prompt),
		TimeoutMinutes:  cronDefaultTimeoutMinutes(job.TimeoutMinutes),
		TriggeredAt:     now,
		PID:             pid,
		Status:          "starting",
		Buffers:         map[string]*cronItemBuffer{},
	}
	a.cronRuns[instanceID] = run
	a.cronJobActiveRuns[cronJobActiveKey(job.RecordID, job.Name)] = instanceID
	delete(a.cronExitTargets, instanceID)
	log.Printf("cron hidden run requested: instance=%s job=%s workspace=%s pid=%d", instanceID, run.JobName, run.WorkspaceKey, pid)
	return nil
}

func (a *App) recordCronImmediateResultLocked(job cronJobState, triggeredAt time.Time, status, errorMessage string) {
	target := a.snapshotCronWritebackLocked()
	if !target.valid() {
		log.Printf("cron immediate result skipped: no writeback target job=%s status=%s", job.Name, status)
		return
	}
	run := cronRunState{
		RunID:           cronInstanceIDForRun(job.RecordID, triggeredAt),
		InstanceID:      cronInstanceIDForRun(job.RecordID, triggeredAt),
		GatewayID:       target.GatewayID,
		WritebackTarget: target,
		JobRecordID:     strings.TrimSpace(job.RecordID),
		JobName:         firstNonEmpty(strings.TrimSpace(job.Name), strings.TrimSpace(job.RecordID)),
		WorkspaceKey:    strings.TrimSpace(job.WorkspaceKey),
		Prompt:          strings.TrimSpace(job.Prompt),
		TimeoutMinutes:  cronDefaultTimeoutMinutes(job.TimeoutMinutes),
		TriggeredAt:     triggeredAt,
		CompletedAt:     triggeredAt,
		Status:          strings.TrimSpace(status),
		ErrorMessage:    strings.TrimSpace(errorMessage),
	}
	go a.writeCronRunResultAsync(target, run)
}

func (a *App) reapCronExitTargetsLocked(now time.Time) {
	for instanceID, target := range a.cronExitTargets {
		if target == nil {
			delete(a.cronExitTargets, instanceID)
			continue
		}
		if target.Deadline.IsZero() || now.Before(target.Deadline) {
			continue
		}
		if target.PID > 0 {
			if err := a.stopProcess(target.PID, 0); err != nil {
				log.Printf("cron forced stop failed: instance=%s pid=%d err=%v", instanceID, target.PID, err)
			} else {
				log.Printf("cron forced stop: instance=%s pid=%d", instanceID, target.PID)
			}
		}
		delete(a.cronExitTargets, instanceID)
	}
}
