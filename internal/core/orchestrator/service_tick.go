package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func turnCompletedSuccessfully(event agentproto.Event) bool {
	if event.Status != "completed" {
		return false
	}
	if strings.TrimSpace(event.ErrorMessage) != "" {
		return false
	}
	return event.Problem == nil
}

func shouldMaterializeFinalTurnOutput(event agentproto.Event, finalText string) bool {
	finalText = strings.TrimSpace(finalText)
	if turnCompletedSuccessfully(event) {
		return true
	}
	if finalText == "" {
		return false
	}
	if strings.TrimSpace(event.Status) == "completed" {
		return true
	}
	return strings.TrimSpace(event.Status) == "failed"
}

// Tick is the orchestrator's deadline driver.
// Keep it limited to in-memory expiry/backoff transitions that must still fire
// when no new ingress event arrives.
func (s *Service) Tick(now time.Time) []eventcontract.Event {
	if now.IsZero() {
		now = s.now()
	}
	var events []eventcontract.Event
	for surfaceID, until := range s.handoffUntil {
		if now.Before(until) {
			continue
		}
		delete(s.handoffUntil, surfaceID)
		surface := s.root.Surfaces[surfaceID]
		if surface == nil || surface.DispatchMode != state.DispatchModeHandoffWait {
			continue
		}
		s.restoreSurfaceDispatchNormal(surface)
		if len(surface.QueuedQueueItemIDs) == 0 {
			continue
		}
		events = append(events, eventcontract.Event{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code: "remote_queue_resumed",
				Text: "本地操作已结束，飞书队列继续处理。",
			},
		})
		events = append(events, s.dispatchNext(surface)...)
	}
	for surfaceID, until := range s.pausedUntil {
		if now.Before(until) {
			continue
		}
		delete(s.pausedUntil, surfaceID)
		surface := s.root.Surfaces[surfaceID]
		if surface == nil || surface.DispatchMode != state.DispatchModePausedForLocal {
			continue
		}
		s.restoreSurfaceDispatchNormal(surface)
		if len(surface.QueuedQueueItemIDs) == 0 {
			continue
		}
		events = append(events, eventcontract.Event{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code: "local_activity_watchdog_resumed",
				Text: "本地活动恢复信号超时，飞书队列已自动恢复处理。",
			},
		})
		events = append(events, s.dispatchNext(surface)...)
	}
	for surfaceID, until := range s.abandoningUntil {
		if now.Before(until) {
			continue
		}
		delete(s.abandoningUntil, surfaceID)
		surface := s.root.Surfaces[surfaceID]
		if surface == nil || !surface.Abandoning {
			continue
		}
		events = append(events, s.finalizeDetachedSurface(surface)...)
		events = append(events, eventcontract.Event{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code: "detach_timeout_forced",
				Text: s.detachTimeoutText(surface),
			},
		})
	}
	for _, surface := range s.root.Surfaces {
		if pending := surface.PendingHeadless; pending != nil && !pending.ExpiresAt.IsZero() && !now.Before(pending.ExpiresAt) {
			events = append(events, s.expirePendingHeadless(surface, pending)...)
		}
		if requestCaptureExpired(now, surface.ActiveRequestCapture) {
			clearSurfaceRequestCapture(surface)
			events = append(events, eventcontract.Event{
				Kind:             eventcontract.KindNotice,
				SurfaceSessionID: surface.SurfaceSessionID,
				Notice: &control.Notice{
					Code: "request_capture_expired",
					Text: "上一条确认反馈已过期，请重新点击卡片按钮后再发送处理意见。",
				},
			})
		}
		events = append(events, s.maybeDispatchPendingAutoWhip(surface, now)...)
		events = append(events, s.maybeDispatchPendingAutoContinue(surface, now)...)
		events = append(events, s.tickExecCommandProgressReasoning(surface, now)...)
	}
	return s.filterEventsForSurfaceVisibility(events)
}
