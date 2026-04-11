package daemon

import (
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const headlessRestoreRetryBackoff = 30 * time.Second

func (a *App) syncHeadlessRestoreStateLocked() {
	if a.headlessRestoreState == nil {
		a.headlessRestoreState = map[string]*headlessRestoreRecoveryState{}
	}
	entries := map[string]SurfaceResumeEntry{}
	if a.surfaceResumeState != nil {
		entries = a.surfaceResumeState.Entries()
	}
	for surfaceID, entry := range entries {
		if !surfaceResumeEntrySupportsHeadlessRestore(entry) {
			delete(a.headlessRestoreState, surfaceID)
			continue
		}
		a.service.MaterializeSurface(surfaceID, entry.GatewayID, entry.ChatID, entry.ActorUserID)
		current := a.headlessRestoreState[surfaceID]
		if current == nil || !sameSurfaceResumeEntryContent(current.Entry, entry) {
			a.headlessRestoreState[surfaceID] = &headlessRestoreRecoveryState{Entry: entry}
			continue
		}
		current.Entry = entry
	}
	for surfaceID := range a.headlessRestoreState {
		if entry, ok := entries[surfaceID]; !ok || !surfaceResumeEntrySupportsHeadlessRestore(entry) {
			delete(a.headlessRestoreState, surfaceID)
		}
	}
}

func surfaceResumeEntrySupportsHeadlessRestore(entry SurfaceResumeEntry) bool {
	return state.NormalizeProductMode(state.ProductMode(entry.ProductMode)) == state.ProductModeNormal &&
		entry.ResumeHeadless &&
		strings.TrimSpace(entry.ResumeThreadID) != ""
}

func (a *App) maybeRecoverHeadlessSurfacesLocked(now time.Time) []control.UIEvent {
	if len(a.headlessRestoreState) == 0 {
		return nil
	}
	surfaceIDs := make([]string, 0, len(a.headlessRestoreState))
	for surfaceID := range a.headlessRestoreState {
		surfaceIDs = append(surfaceIDs, surfaceID)
	}
	sort.Strings(surfaceIDs)
	allowMissingThreadFailure := a.initialThreadsRefreshRoundCompleteLocked()
	events := []control.UIEvent{}
	for _, surfaceID := range surfaceIDs {
		state := a.headlessRestoreState[surfaceID]
		if state == nil {
			continue
		}
		if !state.NextAttemptAt.IsZero() && now.Before(state.NextAttemptAt) {
			continue
		}
		restoreEvents, result := a.service.TryAutoRestoreHeadless(surfaceID, orchestrator.HeadlessRestoreAttempt{
			ThreadID:    strings.TrimSpace(state.Entry.ResumeThreadID),
			ThreadTitle: strings.TrimSpace(state.Entry.ResumeThreadTitle),
			ThreadCWD:   strings.TrimSpace(state.Entry.ResumeThreadCWD),
		}, allowMissingThreadFailure)
		a.applyHeadlessRestoreAttemptResultLocked(surfaceID, result, now)
		events = append(events, restoreEvents...)
	}
	return events
}

func (a *App) applyHeadlessRestoreAttemptResultLocked(surfaceID string, result orchestrator.HeadlessRestoreResult, now time.Time) {
	switch result.Status {
	case orchestrator.HeadlessRestoreStatusAttached, orchestrator.HeadlessRestoreStatusStarting:
		a.clearHeadlessRestoreBackoffLocked(surfaceID)
	case orchestrator.HeadlessRestoreStatusFailed:
		a.setHeadlessRestoreBackoffLocked(surfaceID, result.FailureCode, now)
	}
}

func (a *App) clearHeadlessRestoreBackoffLocked(surfaceID string) {
	state := a.headlessRestoreState[strings.TrimSpace(surfaceID)]
	if state == nil {
		return
	}
	state.NextAttemptAt = time.Time{}
	state.LastAttemptAt = time.Time{}
	state.LastFailureCode = ""
}

func (a *App) setHeadlessRestoreBackoffLocked(surfaceID, code string, now time.Time) {
	state := a.headlessRestoreState[strings.TrimSpace(surfaceID)]
	if state == nil {
		return
	}
	state.LastAttemptAt = now
	state.NextAttemptAt = now.Add(headlessRestoreRetryBackoff)
	state.LastFailureCode = strings.TrimSpace(code)
}

func (a *App) recordHeadlessRestoreOutcomeEventsLocked(events []control.UIEvent, now time.Time) {
	for _, event := range events {
		if event.Notice == nil {
			continue
		}
		switch strings.TrimSpace(event.Notice.Code) {
		case "headless_restore_attached":
			a.clearHeadlessRestoreBackoffLocked(event.SurfaceSessionID)
		case "headless_restore_thread_busy",
			"headless_restore_thread_not_found",
			"headless_restore_thread_cwd_missing",
			"headless_restore_start_failed",
			"headless_restore_start_timeout":
			a.setHeadlessRestoreBackoffLocked(event.SurfaceSessionID, event.Notice.Code, now)
		}
	}
}

func (a *App) markStartupThreadsRefreshRequestedLocked(instanceID string) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return
	}
	if a.startupRefreshPending == nil {
		a.startupRefreshPending = map[string]bool{}
	}
	a.startupRefreshSeen = true
	a.startupRefreshPending[instanceID] = true
}

func (a *App) markStartupThreadsRefreshSettledLocked(instanceID string) {
	delete(a.startupRefreshPending, strings.TrimSpace(instanceID))
}

func (a *App) initialThreadsRefreshRoundCompleteLocked() bool {
	return a.startupRefreshSeen && len(a.startupRefreshPending) == 0
}
