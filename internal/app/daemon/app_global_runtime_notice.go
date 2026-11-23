package daemon

import (
	"context"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

type runtimeNoticeFlush struct {
	retired bool
}

func normalizeGlobalRuntimeNoticeEvent(event eventcontract.Event) (eventcontract.Event, bool) {
	if event.Kind != eventcontract.KindNotice || event.Notice == nil || !event.Notice.IsGlobalRuntime() {
		return event, false
	}
	event.SourceMessageID = ""
	event.SourceMessagePreview = ""
	return event, true
}

func globalRuntimeNoticeIdentity(event eventcontract.Event) (surfaceID, dedupKey string, ok bool) {
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

func globalRuntimeNoticeThrottleWindow(event eventcontract.Event) time.Duration {
	if event.Notice == nil {
		return 0
	}
	switch event.Notice.DeliveryFamily {
	case control.NoticeDeliveryFamilySurfaceResume,
		control.NoticeDeliveryFamilyVSCodeResume,
		control.NoticeDeliveryFamilyVSCodeOpenPrompt:
		return 2 * time.Second
	case control.NoticeDeliveryFamilyTransportDegraded,
		control.NoticeDeliveryFamilyGatewayApplyFailure,
		control.NoticeDeliveryFamilyPromptOverrideGuard:
		return 15 * time.Second
	case control.NoticeDeliveryFamilyDaemonShutdown:
		return time.Minute
	default:
		return 0
	}
}

func (a *App) shouldSuppressGlobalRuntimeNoticeLocked(event eventcontract.Event, now time.Time) bool {
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

func (a *App) recordGlobalRuntimeNoticeLocked(event eventcontract.Event, now time.Time) {
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
	if len(a.pendingGlobalRuntimeNotices[surfaceID]) == 0 || a.runtimeNoticeFlushes[surfaceID] != nil {
		return
	}
	flush := &runtimeNoticeFlush{}
	a.runtimeNoticeFlushes[surfaceID] = flush
	defer func() {
		if !flush.retired {
			delete(a.runtimeNoticeFlushes, surfaceID)
		}
	}()
	for len(a.pendingGlobalRuntimeNotices[surfaceID]) != 0 {
		event, _ := normalizeGlobalRuntimeNoticeEvent(a.pendingGlobalRuntimeNotices[surfaceID][0])
		if attention := a.globalRuntimeAttentionAnnotationForEventLocked(event, time.Now(), false); !attention.Empty() {
			event.Meta.Attention = attention
		}
		if err := a.deliverUIEventLocked(context.Background(), event); err != nil || flush.retired {
			return
		}
		a.recordGlobalRuntimeNoticeLocked(event, time.Now())
		// Delivery releases the lock, so commit the head against the live queue.
		pending := a.pendingGlobalRuntimeNotices[surfaceID]
		clear(pending[:1])
		a.pendingGlobalRuntimeNotices[surfaceID] = pending[1:]
	}
	delete(a.pendingGlobalRuntimeNotices, surfaceID)
}

func (a *App) purgeGlobalRuntimeNoticesLocked(surfaceID string) {
	delete(a.pendingGlobalRuntimeNotices, surfaceID)
	delete(a.recentGlobalRuntimeNotices, surfaceID)
	if flush := a.runtimeNoticeFlushes[surfaceID]; flush != nil {
		flush.retired = true
		delete(a.runtimeNoticeFlushes, surfaceID)
	}
}

func (a *App) queueGlobalRuntimeNoticeLocked(event eventcontract.Event) {
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
