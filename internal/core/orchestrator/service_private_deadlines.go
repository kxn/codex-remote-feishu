package orchestrator

import (
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"time"
)

// PrivateOwnerDeadlineDue covers timers which do not require an online native
// carrier. Queue dispatch and other native-driven timers remain lease-bound.
func (s *Service) PrivateOwnerDeadlineDue(surfaceID string, now time.Time) bool {
	if !s.config.PrivateApplication {
		return false
	}
	surface := s.root.Surfaces[surfaceID]
	if surface == nil {
		return false
	}
	if held, ok := s.privateRecoveryHolds[surfaceID]; ok {
		_, launch := expiredPrivateHeldLaunch(held, now)
		return launch != nil
	}
	return surface.PendingHeadless != nil && !surface.PendingHeadless.ExpiresAt.IsZero() && !now.Before(surface.PendingHeadless.ExpiresAt) || requestCaptureExpired(now, surface.ActiveRequestCapture)
}

// TickPrivateOwnerDeadlines produces durable host effects only, never native
// work or queued input dispatch. The pending instance is frozen into the kill.
func (s *Service) TickPrivateOwnerDeadlines(surfaceID string, now time.Time) []eventcontract.Event {
	if !s.PrivateOwnerDeadlineDue(surfaceID, now) {
		return nil
	}
	surface := s.root.Surfaces[surfaceID]
	if held, ok := s.privateRecoveryHolds[surfaceID]; ok {
		index, launch := expiredPrivateHeldLaunch(held, now)
		if launch == nil || surface.PendingHeadless != nil {
			return nil
		}
		held.Holds = append(held.Holds[:index], held.Holds[index+1:]...)
		if len(held.Holds) == 0 && held.ActiveQueueItemID == "" && (len(held.QueuedQueueItemIDs) == 0 || held.MigrationRecoveryReleased) && held.AutoWhip.PendingReason == "" && held.AutoContinue.Episode == nil {
			delete(s.privateRecoveryHolds, surfaceID)
			surface.DispatchMode = state.DispatchModeNormal
		} else {
			s.privateRecoveryHolds[surfaceID] = held
		}
		surface.PendingHeadless = launch
	}
	var events []eventcontract.Event
	if pending := surface.PendingHeadless; pending != nil && !pending.ExpiresAt.IsZero() && !now.Before(pending.ExpiresAt) {
		events = append(events, s.expirePendingHeadless(surface, pending)...)
	}
	if requestCaptureExpired(now, surface.ActiveRequestCapture) {
		clearSurfaceRequestCapture(surface)
		events = append(events, eventcontract.Event{Kind: eventcontract.KindNotice, SurfaceSessionID: surfaceID, Notice: &control.Notice{Code: "request_capture_expired", Text: "上一条确认反馈已过期，请重新点击卡片按钮后再发送处理意见。"}})
	}
	return s.filterEventsForSurfaceVisibility(events)
}

// A restored launch remains frozen evidence; expiry only removes its own hold.
func expiredPrivateHeldLaunch(c PrivateCheckpoint, now time.Time) (int, *state.HeadlessLaunchRecord) {
	index := -1
	var launch state.HeadlessLaunchRecord
	for i, h := range c.Holds {
		if h.Kind != "headless_launch" {
			continue
		}
		if index >= 0 || json.Unmarshal(h.Frozen, &launch) != nil || launch.InstanceID == "" {
			return -1, nil
		}
		index = i
	}
	if index < 0 || launch.ExpiresAt.IsZero() || now.Before(launch.ExpiresAt) {
		return -1, nil
	}
	return index, &launch
}

// PrivateOwnerLaunchExited handles only the exact pending instance after the
// host has proven its process identity exited; it never claims descendants are
// quiescent. The kill effect continues the normal durable stop protocol.
func (s *Service) PrivateOwnerLaunchExited(surfaceID, instanceID string) []eventcontract.Event {
	if !s.config.PrivateApplication || instanceID == "" {
		return nil
	}
	surface := s.root.Surfaces[surfaceID]
	if surface == nil {
		return nil
	}
	if held, ok := s.privateRecoveryHolds[surfaceID]; ok {
		index := -1
		var launch state.HeadlessLaunchRecord
		for i, h := range held.Holds {
			if h.Kind != "headless_launch" {
				continue
			}
			if index >= 0 || json.Unmarshal(h.Frozen, &launch) != nil {
				return nil
			}
			index = i
		}
		if index < 0 || launch.InstanceID != instanceID || surface.PendingHeadless != nil {
			return nil
		}
		held.Holds = append(held.Holds[:index], held.Holds[index+1:]...)
		if len(held.Holds) == 0 && held.ActiveQueueItemID == "" && (len(held.QueuedQueueItemIDs) == 0 || held.MigrationRecoveryReleased) && held.AutoWhip.PendingReason == "" && held.AutoContinue.Episode == nil {
			delete(s.privateRecoveryHolds, surfaceID)
			surface.DispatchMode = state.DispatchModeNormal
		} else {
			s.privateRecoveryHolds[surfaceID] = held
		}
		surface.PendingHeadless = &launch
	}
	pending := surface.PendingHeadless
	if pending == nil || pending.InstanceID != instanceID {
		return nil
	}
	kill := eventcontract.Event{Kind: eventcontract.KindDaemonCommand, SurfaceSessionID: surfaceID, DaemonCommand: &control.DaemonCommand{Kind: control.DaemonCommandKillHeadless, SurfaceSessionID: surfaceID, InstanceID: instanceID, ThreadID: pending.ThreadID, ThreadTitle: pending.ThreadTitle, WorkspaceKey: pending.WorkspaceKey, ThreadCWD: pending.ThreadCWD}}
	events := s.HandleHeadlessLaunchFailed(surfaceID, instanceID, fmt.Errorf("后台进程在启动期间退出"))
	for i := range events {
		if events[i].Notice != nil {
			events[i].Notice.Text = "启动进程已退出，请查看 codex-remote 服务日志后重试。"
		}
	}
	return append([]eventcontract.Event{kill}, events...)
}

// PrivatePendingLaunchInstance identifies only a current or frozen pending
// launch; it does not confer any authority over the carrier.
func (s *Service) PrivatePendingLaunchInstance(surfaceID string) string {
	if !s.config.PrivateApplication {
		return ""
	}
	v := s.root.Surfaces[surfaceID]
	if v == nil {
		return ""
	}
	if v.PendingHeadless != nil {
		return v.PendingHeadless.InstanceID
	}
	held, ok := s.privateRecoveryHolds[surfaceID]
	if !ok {
		return ""
	}
	id := ""
	for _, h := range held.Holds {
		if h.Kind != "headless_launch" {
			continue
		}
		var p state.HeadlessLaunchRecord
		if id != "" || json.Unmarshal(h.Frozen, &p) != nil || p.InstanceID == "" {
			return ""
		}
		id = p.InstanceID
	}
	return id
}
