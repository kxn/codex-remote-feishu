package daemon

import (
	"fmt"
	"log"
	"time"

	cronrt "github.com/kxn/codex-remote-feishu/internal/app/cronruntime"
)

func (a *App) maybeScheduleCronJobsLocked(now time.Time) {
	if a.cronRuntime.syncInFlight {
		return
	}
	if !a.cronRuntime.nextScheduleScan.IsZero() && now.Before(a.cronRuntime.nextScheduleScan) {
		return
	}
	a.cronRuntime.nextScheduleScan = nextCronScheduleScan(now)
	a.maybeTimeoutCronRunsLocked(now)

	stateValue, err := a.loadCronStateLocked(false)
	if err != nil {
		log.Printf("cron scheduler skipped: load state failed: %v", err)
		return
	}
	if stateValue == nil || len(stateValue.Jobs) == 0 {
		return
	}

	cronZone := cronrt.ConfiguredTimeZone(stateValue)
	now = cronrt.SchedulerTimeIn(now, cronZone)
	dirty := false
	launches := []cronLaunchRequest{}
	for idx := range stateValue.Jobs {
		job := &stateValue.Jobs[idx]
		if job.NextRunAt.IsZero() {
			job.NextRunAt = cronrt.NextRunAtIn(*job, now, cronZone)
			dirty = true
		}
		if job.NextRunAt.IsZero() || job.NextRunAt.After(now) {
			continue
		}
		currentDueAt := job.NextRunAt
		nextRunAt := cronrt.AdvanceRunAtIn(*job, currentDueAt, now, cronZone)
		activeCount := a.cronActiveRunCountLocked(job.RecordID, job.Name)
		maxConcurrency := cronrt.DefaultMaxConcurrency(job.MaxConcurrency)
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
	for instanceID, run := range a.cronRuntime.runs {
		if run == nil {
			delete(a.cronRuntime.runs, instanceID)
			continue
		}
		timeoutAt := run.TriggeredAt
		if !run.StartedAt.IsZero() {
			timeoutAt = run.StartedAt
		}
		if timeoutAt.IsZero() {
			continue
		}
		timeout := time.Duration(cronrt.DefaultTimeoutMinutes(run.TimeoutMinutes)) * time.Minute
		if timeout <= 0 || now.Before(timeoutAt.Add(timeout)) {
			continue
		}
		a.completeCronRunLocked(instanceID, "timeout", fmt.Sprintf("任务超过 %d 分钟未完成，已按超时结束。", cronrt.DefaultTimeoutMinutes(run.TimeoutMinutes)), now, true)
	}
}

func (a *App) recordCronImmediateResultLocked(job cronrt.JobState, triggeredAt time.Time, status, errorMessage string) {
	a.recordCronImmediateResultWithTargetLocked(a.snapshotCronWritebackLocked(), job, triggeredAt, status, errorMessage)
}

func (a *App) reapCronExitTargetsLocked(now time.Time) {
	targets := make([]cronrt.ExitTarget, 0)
	for instanceID, target := range a.cronRuntime.exitTargets {
		if target == nil {
			delete(a.cronRuntime.exitTargets, instanceID)
			continue
		}
		if target.StopInFlight || target.Deadline.IsZero() || now.Before(target.Deadline) {
			continue
		}
		if target.PID <= 0 {
			delete(a.cronRuntime.exitTargets, instanceID)
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
		Target cronrt.ExitTarget
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
			if target := a.cronRuntime.exitTargets[result.Target.InstanceID]; target != nil {
				target.StopInFlight = false
			}
			continue
		}
		log.Printf("cron forced stop: instance=%s pid=%d", result.Target.InstanceID, result.Target.PID)
		delete(a.cronRuntime.exitTargets, result.Target.InstanceID)
	}
}
