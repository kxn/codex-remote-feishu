package daemon

import (
	"fmt"
	"log"
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

	cronZone := cronConfiguredTimeZone(stateValue)
	now = cronSchedulerTimeIn(now, cronZone)
	dirty := false
	launches := []cronLaunchRequest{}
	for idx := range stateValue.Jobs {
		job := &stateValue.Jobs[idx]
		if job.NextRunAt.IsZero() {
			job.NextRunAt = cronNextRunAtIn(*job, now, cronZone)
			dirty = true
		}
		if job.NextRunAt.IsZero() || job.NextRunAt.After(now) {
			continue
		}
		currentDueAt := job.NextRunAt
		nextRunAt := cronAdvanceRunAtIn(*job, currentDueAt, now, cronZone)
		activeCount := a.cronActiveRunCountLocked(job.RecordID, job.Name)
		maxConcurrency := cronDefaultMaxConcurrency(job.MaxConcurrency)
		if activeCount >= maxConcurrency {
			a.recordCronImmediateResultLocked(*job, now, "skipped", fmt.Sprintf("当前运行中实例数已达到并发上限（%d），本轮跳过。", maxConcurrency))
			job.NextRunAt = nextRunAt
			dirty = true
			continue
		}
		job.NextRunAt = nextRunAt
		dirty = true
		launches = append(launches, a.newCronLaunchRequestLocked(*job, now))
	}
	if dirty {
		if err := a.writeCronStateLocked(); err != nil {
			log.Printf("cron scheduler state write failed: %v", err)
		}
	}
	a.launchCronRequestsLocked(launches)
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

func (a *App) recordCronImmediateResultLocked(job cronJobState, triggeredAt time.Time, status, errorMessage string) {
	a.recordCronImmediateResultWithTargetLocked(a.snapshotCronWritebackLocked(), job, triggeredAt, status, errorMessage)
}

func (a *App) reapCronExitTargetsLocked(now time.Time) {
	targets := make([]cronExitTarget, 0)
	for instanceID, target := range a.cronExitTargets {
		if target == nil {
			delete(a.cronExitTargets, instanceID)
			continue
		}
		if target.StopInFlight || target.Deadline.IsZero() || now.Before(target.Deadline) {
			continue
		}
		if target.PID <= 0 {
			delete(a.cronExitTargets, instanceID)
			continue
		}
		target.StopInFlight = true
		target.LastStopAttemptAt = now
		targets = append(targets, *target)
	}
	if len(targets) == 0 {
		return
	}

	type cronForcedStopResult struct {
		Target cronExitTarget
		Err    error
	}
	results := make([]cronForcedStopResult, 0, len(targets))
	a.mu.Unlock()
	for _, target := range targets {
		results = append(results, cronForcedStopResult{
			Target: target,
			Err:    a.stopProcess(target.PID, 0),
		})
	}
	a.mu.Lock()

	for _, result := range results {
		if result.Err != nil {
			log.Printf("cron forced stop failed: instance=%s pid=%d err=%v", result.Target.InstanceID, result.Target.PID, result.Err)
			if target := a.cronExitTargets[result.Target.InstanceID]; target != nil {
				target.StopInFlight = false
			}
			continue
		}
		log.Printf("cron forced stop: instance=%s pid=%d", result.Target.InstanceID, result.Target.PID)
		delete(a.cronExitTargets, result.Target.InstanceID)
	}
}
