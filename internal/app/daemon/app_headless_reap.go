package daemon

import (
	"fmt"
	"log"
	"time"
)

type managedHeadlessIdleStopTarget struct {
	InstanceID string
	PID        int
	IdleSince  time.Time
}

type managedHeadlessIdleStopResult struct {
	Target managedHeadlessIdleStopTarget
	Err    error
}

func (a *App) reapIdleHeadless(now time.Time) {
	if a.headlessRuntime.IdleTTL <= 0 {
		return
	}
	targets := a.collectIdleHeadlessStopTargetsLocked(now)
	if len(targets) == 0 {
		return
	}

	results := make([]managedHeadlessIdleStopResult, 0, len(targets))
	a.mu.Unlock()
	for _, target := range targets {
		results = append(results, managedHeadlessIdleStopResult{
			Target: target,
			Err:    a.stopProcess(target.PID, a.headlessRuntime.KillGrace),
		})
	}
	a.mu.Lock()

	for _, result := range results {
		a.finishIdleHeadlessStopLocked(now, result)
	}
}

func (a *App) collectIdleHeadlessStopTargetsLocked(now time.Time) []managedHeadlessIdleStopTarget {
	targets := make([]managedHeadlessIdleStopTarget, 0)
	for instanceID, managed := range a.managedHeadless {
		if managed == nil {
			delete(a.managedHeadless, instanceID)
			continue
		}
		if managed.Status != managedHeadlessStatusIdle || managed.IdleSince.IsZero() {
			continue
		}
		if now.Sub(managed.IdleSince) < a.headlessRuntime.IdleTTL {
			continue
		}
		if inst := a.service.Instance(instanceID); inst != nil && inst.PID > 0 {
			managed.PID = inst.PID
		}
		if managed.PID == 0 {
			log.Printf("headless idle cleanup skipped: instance=%s err=missing pid", instanceID)
			continue
		}
		managed.Status = managedHeadlessStatusStopping
		managed.LastError = ""
		managed.RefreshInFlight = false
		managed.RefreshCommandID = ""
		targets = append(targets, managedHeadlessIdleStopTarget{
			InstanceID: instanceID,
			PID:        managed.PID,
			IdleSince:  managed.IdleSince,
		})
	}
	return targets
}

func (a *App) finishIdleHeadlessStopLocked(now time.Time, result managedHeadlessIdleStopResult) {
	if result.Err != nil {
		log.Printf("headless idle cleanup failed: instance=%s pid=%d err=%v", result.Target.InstanceID, result.Target.PID, result.Err)
		a.restoreIdleHeadlessStopFailureLocked(now, result)
		return
	}
	log.Printf(
		"headless idle cleanup: instance=%s pid=%d idle_since=%s",
		result.Target.InstanceID,
		result.Target.PID,
		result.Target.IdleSince.Format(time.RFC3339),
	)
	delete(a.managedHeadless, result.Target.InstanceID)
	a.service.RemoveInstance(result.Target.InstanceID)
}

func (a *App) restoreIdleHeadlessStopFailureLocked(now time.Time, result managedHeadlessIdleStopResult) {
	managed := a.managedHeadless[result.Target.InstanceID]
	if managed == nil {
		return
	}
	managed.LastError = fmt.Sprintf("后台 idle cleanup stop 失败：%v", result.Err)
	if managed.Status == managedHeadlessStatusStopping {
		managed.Status = managedHeadlessStatusOffline
	}
	a.syncManagedHeadlessLocked(now)
	managed = a.managedHeadless[result.Target.InstanceID]
	if managed != nil && managed.Status == managedHeadlessStatusIdle && !result.Target.IdleSince.IsZero() {
		managed.IdleSince = result.Target.IdleSince
	}
}
