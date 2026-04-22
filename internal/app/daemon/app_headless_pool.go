package daemon

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (a *App) syncManagedHeadlessLocked(now time.Time) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	attached := map[string]bool{}
	for _, surface := range a.service.Surfaces() {
		if surface == nil || strings.TrimSpace(surface.AttachedInstanceID) == "" {
			continue
		}
		attached[surface.AttachedInstanceID] = true
	}
	for instanceID, managed := range a.managedHeadlessRuntime.Processes {
		if managed == nil {
			delete(a.managedHeadlessRuntime.Processes, instanceID)
			continue
		}
		inst := a.service.Instance(instanceID)
		if inst != nil {
			if inst.PID > 0 {
				managed.PID = inst.PID
			}
			if strings.TrimSpace(inst.DisplayName) != "" {
				managed.DisplayName = inst.DisplayName
			}
			if strings.TrimSpace(inst.WorkspaceRoot) != "" {
				managed.WorkspaceRoot = inst.WorkspaceRoot
			}
		}
		switch {
		case strings.TrimSpace(managed.Status) == managedHeadlessStatusStopping:
			managed.RefreshInFlight = false
			managed.RefreshCommandID = ""
		case isManagedHeadlessInstance(inst) && inst.Online:
			if attached[instanceID] || strings.TrimSpace(inst.ActiveTurnID) != "" {
				managed.Status = managedHeadlessStatusBusy
				managed.IdleSince = time.Time{}
			} else {
				if managed.IdleSince.IsZero() {
					managed.IdleSince = now
				}
				managed.Status = managedHeadlessStatusIdle
			}
		case strings.TrimSpace(managed.Status) == managedHeadlessStatusStarting && (managed.StartedAt.IsZero() || now.Sub(managed.StartedAt) < a.headlessRuntime.StartTTL):
			managed.IdleSince = time.Time{}
		default:
			managed.Status = managedHeadlessStatusOffline
			managed.IdleSince = time.Time{}
			if managed.RefreshInFlight {
				managed.RefreshInFlight = false
				managed.RefreshCommandID = ""
			}
			if strings.TrimSpace(managed.LastError) == "" && !managed.StartedAt.IsZero() && now.Sub(managed.StartedAt) >= a.headlessRuntime.StartTTL && (inst == nil || !inst.Online) {
				managed.LastError = "等待 headless 实例连回 relay 超时。"
			}
		}
	}
}

func (a *App) maybeRefreshIdleManagedHeadlessLocked(now time.Time) {
	if a.headlessRuntime.IdleRefreshInterval <= 0 {
		return
	}
	for instanceID, managed := range a.managedHeadlessRuntime.Processes {
		if managed == nil {
			continue
		}
		if managed.RefreshInFlight {
			if !managed.LastRefreshRequestedAt.IsZero() && a.headlessRuntime.IdleRefreshTimeout > 0 && now.Sub(managed.LastRefreshRequestedAt) >= a.headlessRuntime.IdleRefreshTimeout {
				managed.RefreshInFlight = false
				managed.RefreshCommandID = ""
				managed.LastError = "后台 threads.refresh 超时，等待下一轮重试。"
			}
			continue
		}
		if strings.TrimSpace(managed.Status) != managedHeadlessStatusIdle {
			continue
		}
		lastAttempt := managedHeadlessLastRefreshActivity(managed)
		if !lastAttempt.IsZero() && now.Sub(lastAttempt) < a.headlessRuntime.IdleRefreshInterval {
			continue
		}
		if err := a.sendManagedThreadsRefreshLocked(instanceID, now, "idle_pool_maintenance"); err != nil {
			log.Printf("managed headless refresh failed: instance=%s err=%v", instanceID, err)
		}
	}
}

func (a *App) markManagedThreadsRefreshRequestedLocked(instanceID, commandID string, now time.Time) {
	managed := a.managedHeadlessRuntime.Processes[instanceID]
	if managed == nil {
		return
	}
	managed.RefreshCommandID = strings.TrimSpace(commandID)
	managed.RefreshInFlight = managed.RefreshCommandID != ""
	managed.LastRefreshRequestedAt = now
	managed.LastError = ""
}

func (a *App) sendManagedThreadsRefreshLocked(instanceID string, now time.Time, reason string) error {
	command := agentproto.Command{
		CommandID: a.nextCommandID(),
		Kind:      agentproto.CommandThreadsRefresh,
	}
	a.markManagedThreadsRefreshRequestedLocked(instanceID, command.CommandID, now)
	a.mu.Unlock()
	err := a.sendAgentCommand(instanceID, command)
	a.mu.Lock()
	if err != nil {
		if managed := a.managedHeadlessRuntime.Processes[instanceID]; managed != nil {
			managed.RefreshInFlight = false
			managed.RefreshCommandID = ""
			managed.LastError = fmt.Sprintf("后台 threads.refresh 发送失败：%v", err)
		}
		return err
	}
	log.Printf("managed headless refresh requested: instance=%s command=%s reason=%s", instanceID, command.CommandID, reason)
	return nil
}

func (a *App) noteManagedThreadsSnapshotLocked(instanceID string, now time.Time) {
	managed := a.managedHeadlessRuntime.Processes[instanceID]
	if managed == nil {
		return
	}
	managed.LastRefreshCompletedAt = now
	managed.RefreshInFlight = false
	managed.RefreshCommandID = ""
	managed.LastError = ""
}

func (a *App) noteManagedRefreshAckLocked(instanceID string, ack agentproto.CommandAck) bool {
	managed := a.managedHeadlessRuntime.Processes[instanceID]
	if managed == nil || strings.TrimSpace(managed.RefreshCommandID) == "" || managed.RefreshCommandID != strings.TrimSpace(ack.CommandID) {
		return false
	}
	if ack.Accepted {
		return true
	}
	managed.RefreshInFlight = false
	managed.RefreshCommandID = ""
	if ack.Problem != nil && strings.TrimSpace(ack.Problem.Message) != "" {
		managed.LastError = ack.Problem.Message
	} else if strings.TrimSpace(ack.Error) != "" {
		managed.LastError = ack.Error
	} else {
		managed.LastError = "后台 threads.refresh 被 wrapper 拒绝。"
	}
	log.Printf("managed headless refresh rejected: instance=%s command=%s error=%s", instanceID, ack.CommandID, managed.LastError)
	return true
}

func (a *App) ensureMinIdleManagedHeadlessLocked(now time.Time) {
	if a.headlessRuntime.MinIdle <= 0 || strings.TrimSpace(a.headlessRuntime.BinaryPath) == "" {
		return
	}
	launches := a.reserveMinIdleManagedHeadlessLocked(now)
	for _, launch := range launches {
		if err := a.startReservedPoolManagedHeadlessLocked(launch); err != nil {
			log.Printf("managed headless prewarm failed: err=%v", err)
			return
		}
	}
}

func (a *App) reserveMinIdleManagedHeadlessLocked(now time.Time) []managedHeadlessPrewarmLaunch {
	missing := a.headlessRuntime.MinIdle - a.countWarmManagedHeadlessLocked(now)
	if missing <= 0 {
		return nil
	}
	launches := make([]managedHeadlessPrewarmLaunch, 0, missing)
	for i := 0; i < missing; i++ {
		launches = append(launches, a.reservePoolManagedHeadlessLaunchLocked(now, i+1))
	}
	return launches
}

func (a *App) countWarmManagedHeadlessLocked(now time.Time) int {
	count := 0
	for _, managed := range a.managedHeadlessRuntime.Processes {
		if managed == nil {
			continue
		}
		switch strings.TrimSpace(managed.Status) {
		case managedHeadlessStatusIdle:
			count++
		case managedHeadlessStatusStarting:
			if managed.StartedAt.IsZero() || now.Sub(managed.StartedAt) < a.headlessRuntime.StartTTL {
				count++
			}
		}
	}
	return count
}

func (a *App) reservePoolManagedHeadlessLaunchLocked(now time.Time, seq int) managedHeadlessPrewarmLaunch {
	cfg := a.headlessRuntime
	workDir := strings.TrimSpace(cfg.Paths.StateDir)
	if workDir == "" {
		workDir = "."
	}
	instanceID := fmt.Sprintf("inst-headless-pool-%d-%d", now.UnixNano(), seq)
	env := append([]string{}, cfg.BaseEnv...)
	env = append(env,
		"CODEX_REMOTE_INSTANCE_ID="+instanceID,
		"CODEX_REMOTE_INSTANCE_SOURCE=headless",
		"CODEX_REMOTE_INSTANCE_MANAGED=1",
		"CODEX_REMOTE_LIFETIME=daemon-owned",
		"CODEX_REMOTE_INSTANCE_DISPLAY_NAME=headless",
	)
	a.managedHeadlessRuntime.Processes[instanceID] = &managedHeadlessProcess{
		InstanceID:    instanceID,
		RequestedAt:   now,
		StartedAt:     now,
		ThreadCWD:     workDir,
		WorkspaceRoot: workDir,
		DisplayName:   "headless",
		Status:        managedHeadlessStatusStarting,
	}
	return managedHeadlessPrewarmLaunch{
		InstanceID: instanceID,
		Options:    controlToHeadlessLaunch(cfg, env, workDir, instanceID),
	}
}

func (a *App) startReservedPoolManagedHeadlessLocked(launch managedHeadlessPrewarmLaunch) error {
	a.mu.Unlock()
	pid, err := a.startHeadless(launch.Options)
	a.mu.Lock()
	if err != nil {
		delete(a.managedHeadlessRuntime.Processes, launch.InstanceID)
		return err
	}
	managed := a.managedHeadlessRuntime.Processes[launch.InstanceID]
	if managed == nil || a.shuttingDown {
		a.mu.Unlock()
		stopErr := a.stopProcess(pid, a.headlessRuntime.KillGrace)
		a.mu.Lock()
		if stopErr != nil {
			log.Printf("managed headless prewarm cleanup failed: instance=%s pid=%d err=%v", launch.InstanceID, pid, stopErr)
		}
		if a.shuttingDown {
			return fmt.Errorf("daemon is shutting down during prewarm launch")
		}
		return fmt.Errorf("prewarm reservation missing after launch: %s", launch.InstanceID)
	}
	if managed.PID <= 0 {
		managed.PID = pid
	}
	log.Printf("managed headless prewarm start requested: instance=%s pid=%d", launch.InstanceID, pid)
	return nil
}
