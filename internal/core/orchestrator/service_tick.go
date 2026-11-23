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
	return s.tickSurface(now, "")
}

// TickPrivateSurface applies only the requested private owner's timers.
func (s *Service) TickPrivateSurface(surfaceID string, now time.Time) []eventcontract.Event {
	if !s.config.PrivateApplication || s.root.Surfaces[surfaceID] == nil {
		return nil
	}
	if _, held := s.privateRecoveryHolds[surfaceID]; held {
		return nil
	}
	events := s.tickSurface(now, surfaceID)
	// A proof-released migration can contain queued inputs with no active turn.
	// The old queue dispatcher supplies all normal online, busy, and policy
	// gates; the host calls this only under the attached carrier's lease.
	if s.privateMigrationReleased[surfaceID] {
		surface := s.root.Surfaces[surfaceID]
		if surface.ActiveQueueItemID == "" && len(surface.QueuedQueueItemIDs) != 0 {
			events = append(events, s.dispatchNext(surface)...)
		}
		if surface.ActiveQueueItemID == "" && len(surface.QueuedQueueItemIDs) == 0 {
			delete(s.privateMigrationReleased, surfaceID)
		}
	}
	return s.filterEventsForSurfaceVisibility(events)
}

func (s *Service) tickSurface(now time.Time, only string) []eventcontract.Event {
	if now.IsZero() {
		now = s.now()
	}
	var events []eventcontract.Event
	for surfaceID, until := range s.handoffUntil {
		if only != "" && surfaceID != only {
			continue
		}
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
		if only != "" && surfaceID != only {
			continue
		}
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
		if only != "" && surfaceID != only {
			continue
		}
		if now.Before(until) {
			continue
		}
		delete(s.abandoningUntil, surfaceID)
		surface := s.root.Surfaces[surfaceID]
		if surface == nil || !surface.Abandoning {
			continue
		}
		instanceID := surface.AttachedInstanceID
		events = append(events, s.finalizeDetachedSurface(surface)...)
		events = append(events, s.releaseManagedHeadlessAfterDetach(surface, instanceID)...)
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
		if only != "" && surface.SurfaceSessionID != only {
			continue
		}
		if _, held := s.privateRecoveryHolds[surface.SurfaceSessionID]; held {
			continue
		}
		pruneAsyncUserInputs(s.surfaceUIRuntimeState(surface), now)
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

// PrivateTickDue conservatively checks every source visited by Tick. It is a
// scheduling hint only: callers still execute Tick transactionally, and should
// check again on the next scheduling interval when this returns false.
func (s *Service) PrivateTickDue(surfaceID string, now time.Time) bool {
	if !s.config.PrivateApplication {
		return false
	}
	surface := s.root.Surfaces[surfaceID]
	if surface == nil {
		return false
	}
	if _, held := s.privateRecoveryHolds[surfaceID]; held {
		return false
	}
	for _, deadlines := range []map[string]time.Time{s.handoffUntil, s.pausedUntil, s.abandoningUntil} {
		if until, ok := deadlines[surfaceID]; ok && !now.Before(until) {
			return true
		}
	}
	if pending := surface.PendingHeadless; pending != nil && !pending.ExpiresAt.IsZero() && !now.Before(pending.ExpiresAt) {
		return true
	}
	if requestCaptureExpired(now, surface.ActiveRequestCapture) {
		return true
	}
	if surface.AutoWhip.Enabled && surface.AutoWhip.PendingReason != "" && !surface.AutoWhip.PendingDueAt.IsZero() && !now.Before(surface.AutoWhip.PendingDueAt) {
		return true
	}
	// The old auto-continue path may retire a mismatched route even before its
	// deadline, so retain a conservative positive for a scheduled episode.
	if episode := activeAutoContinueEpisode(surface); episode != nil && surface.AutoContinue.Enabled && episode.State == state.AutoContinueEpisodeScheduled {
		return true
	}
	if surfaceShowsVisibleReasoning(surface.Verbosity) && execCommandProgressReasoningDirty(surface.ActiveExecProgress) && s.execCommandProgressReasoningFlushDue(surface.ActiveExecProgress, now) {
		return true
	}
	if s.privateMigrationReleased[surfaceID] && surface.ActiveQueueItemID == "" {
		return true
	}
	if ui := s.surfaceUIRuntime[surfaceID]; ui != nil {
		if len(ui.AsyncUserInputs) > asyncUserInputMaxRecords {
			return true
		}
		drafts := 0
		for _, record := range ui.AsyncUserInputs {
			if record == nil || !now.Before(record.CreatedAt.Add(asyncUserInputSeenTTL)) || (!record.Submitted && !now.Before(record.CreatedAt.Add(asyncUserInputDraftTTL))) {
				return true
			}
			if !record.Submitted {
				drafts++
			}
		}
		if drafts > asyncUserInputMaxDrafts {
			return true
		}
	}
	return false
}
