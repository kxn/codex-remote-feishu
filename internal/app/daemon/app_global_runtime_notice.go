package daemon

import (
	"context"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func normalizeGlobalRuntimeNoticeEvent(event control.UIEvent) (control.UIEvent, bool) {
	if event.Kind != control.UIEventNotice || event.Notice == nil || !event.Notice.IsGlobalRuntime() {
		return event, false
	}
	event.SourceMessageID = ""
	event.SourceMessagePreview = ""
	return event, true
}

func globalRuntimeNoticeIdentity(event control.UIEvent) (surfaceID, dedupKey string, ok bool) {
	event, ok = normalizeGlobalRuntimeNoticeEvent(event)
	if !ok {
		return "", "", false
	}
	surfaceID = strings.TrimSpace(event.SurfaceSessionID)
	if surfaceID == "" {
		return "", "", false
	}
	family := strings.TrimSpace(string(event.Notice.DeliveryFamily))
	if family == "" {
		return "", "", false
	}
	dedupKey = family + ":" + event.Notice.DeliveryDedupIdentity()
	return surfaceID, dedupKey, true
}

func globalRuntimeNoticeThrottleWindow(event control.UIEvent) time.Duration {
	if event.Notice == nil {
		return 0
	}
	switch event.Notice.DeliveryFamily {
	case control.NoticeDeliveryFamilySurfaceResume,
		control.NoticeDeliveryFamilyVSCodeResume,
		control.NoticeDeliveryFamilyVSCodeOpenPrompt:
		return 2 * time.Second
	case control.NoticeDeliveryFamilyTransportDegraded,
		control.NoticeDeliveryFamilyGatewayApplyFailure:
		return 15 * time.Second
	case control.NoticeDeliveryFamilyDaemonShutdown:
		return time.Minute
	default:
		return 0
	}
}

func (a *App) shouldSuppressGlobalRuntimeNoticeLocked(event control.UIEvent, now time.Time) bool {
	surfaceID, dedupKey, ok := globalRuntimeNoticeIdentity(event)
	if !ok {
		return false
	}
	window := globalRuntimeNoticeThrottleWindow(event)
	if window <= 0 {
		return false
	}
	surfaceState := a.recentGlobalRuntimeNotices[surfaceID]
	if surfaceState == nil {
		return false
	}
	last := surfaceState[dedupKey]
	return !last.IsZero() && now.Sub(last) < window
}

func (a *App) recordGlobalRuntimeNoticeLocked(event control.UIEvent, now time.Time) {
	surfaceID, dedupKey, ok := globalRuntimeNoticeIdentity(event)
	if !ok {
		return
	}
	if a.recentGlobalRuntimeNotices[surfaceID] == nil {
		a.recentGlobalRuntimeNotices[surfaceID] = map[string]time.Time{}
	}
	a.recentGlobalRuntimeNotices[surfaceID][dedupKey] = now
	a.pruneGlobalRuntimeNoticesLocked(now.Add(-2 * time.Hour))
}

func (a *App) pruneGlobalRuntimeNoticesLocked(cutoff time.Time) {
	for surfaceID, surfaceState := range a.recentGlobalRuntimeNotices {
		for dedupKey, seenAt := range surfaceState {
			if seenAt.Before(cutoff) {
				delete(surfaceState, dedupKey)
			}
		}
		if len(surfaceState) == 0 {
			delete(a.recentGlobalRuntimeNotices, surfaceID)
		}
	}
}

func (a *App) flushPendingGlobalRuntimeNoticesLocked(surfaceID string) {
	surfaceID = strings.TrimSpace(surfaceID)
	if surfaceID == "" {
		return
	}
	pending := a.pendingGlobalRuntimeNotices[surfaceID]
	if len(pending) == 0 {
		return
	}
	for _, event := range pending {
		event, _ = normalizeGlobalRuntimeNoticeEvent(event)
		if err := a.deliverUIEventLocked(context.Background(), event); err != nil {
			return
		}
		a.recordGlobalRuntimeNoticeLocked(event, time.Now())
	}
	delete(a.pendingGlobalRuntimeNotices, surfaceID)
}

func (a *App) queueGlobalRuntimeNoticeLocked(event control.UIEvent) {
	event, ok := normalizeGlobalRuntimeNoticeEvent(event)
	if !ok {
		return
	}
	now := time.Now()
	if a.shouldSuppressGlobalRuntimeNoticeLocked(event, now) {
		return
	}
	surfaceID, dedupKey, ok := globalRuntimeNoticeIdentity(event)
	if !ok {
		return
	}
	pending := a.pendingGlobalRuntimeNotices[surfaceID]
	for _, queued := range pending {
		_, queuedDedupKey, ok := globalRuntimeNoticeIdentity(queued)
		if ok && queuedDedupKey == dedupKey {
			return
		}
	}
	a.pendingGlobalRuntimeNotices[surfaceID] = append(pending, event)
	a.recordGlobalRuntimeNoticeLocked(event, now)
}
