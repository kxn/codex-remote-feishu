package daemon

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	managedHeadlessStatusStarting = "starting"
	managedHeadlessStatusBusy     = "busy"
	managedHeadlessStatusIdle     = "idle"
	managedHeadlessStatusOffline  = "offline"
)

func isManagedHeadlessInstance(inst *state.InstanceRecord) bool {
	return inst != nil && strings.EqualFold(strings.TrimSpace(inst.Source), "headless") && inst.Managed
}

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
	for instanceID, managed := range a.managedHeadless {
		if managed == nil {
			delete(a.managedHeadless, instanceID)
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
	for instanceID, managed := range a.managedHeadless {
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

func managedHeadlessLastRefreshActivity(managed *managedHeadlessProcess) time.Time {
	if managed == nil {
		return time.Time{}
	}
	last := managed.LastRefreshCompletedAt
	for _, candidate := range []time.Time{
		managed.LastRefreshRequestedAt,
		managed.LastHelloAt,
		managed.StartedAt,
		managed.RequestedAt,
	} {
		if candidate.After(last) {
			last = candidate
		}
	}
	return last
}

func (a *App) markManagedThreadsRefreshRequestedLocked(instanceID, commandID string, now time.Time) {
	managed := a.managedHeadless[instanceID]
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
	if err := a.sendAgentCommand(instanceID, command); err != nil {
		if managed := a.managedHeadless[instanceID]; managed != nil {
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
	managed := a.managedHeadless[instanceID]
	if managed == nil {
		return
	}
	managed.LastRefreshCompletedAt = now
	managed.RefreshInFlight = false
	managed.RefreshCommandID = ""
	managed.LastError = ""
}

func (a *App) noteManagedRefreshAckLocked(instanceID string, ack agentproto.CommandAck) bool {
	managed := a.managedHeadless[instanceID]
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
