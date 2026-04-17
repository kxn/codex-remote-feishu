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
	if a.surfaceResumeRuntime.headlessRestore == nil {
		a.surfaceResumeRuntime.headlessRestore = map[string]*headlessRestoreRecoveryState{}
	}
	entries := map[string]SurfaceResumeEntry{}
	if a.surfaceResumeRuntime.store != nil {
		entries = a.surfaceResumeRuntime.store.Entries()
	}
	for surfaceID, entry := range entries {
		if !surfaceResumeEntrySupportsHeadlessRestore(entry) {
			delete(a.surfaceResumeRuntime.headlessRestore, surfaceID)
			continue
		}
		a.service.MaterializeSurface(surfaceID, entry.GatewayID, entry.ChatID, entry.ActorUserID)
		current := a.surfaceResumeRuntime.headlessRestore[surfaceID]
		if current == nil || !sameSurfaceResumeEntryContent(current.Entry, entry) {
			a.surfaceResumeRuntime.headlessRestore[surfaceID] = &headlessRestoreRecoveryState{Entry: entry}
			continue
		}
		current.Entry = entry
	}
	for surfaceID := range a.surfaceResumeRuntime.headlessRestore {
		if entry, ok := entries[surfaceID]; !ok || !surfaceResumeEntrySupportsHeadlessRestore(entry) {
			delete(a.surfaceResumeRuntime.headlessRestore, surfaceID)
		}
	}
}

func surfaceResumeEntrySupportsHeadlessRestore(entry SurfaceResumeEntry) bool {
	return state.NormalizeProductMode(state.ProductMode(entry.ProductMode)) == state.ProductModeNormal &&
		entry.ResumeHeadless &&
		strings.TrimSpace(entry.ResumeThreadID) != ""
}

func (a *App) shouldDeferHeadlessRestoreUntilInitialRefreshLocked(entry SurfaceResumeEntry, allowMissingThreadFailure bool) bool {
	if allowMissingThreadFailure {
		return false
	}
	instanceID := strings.TrimSpace(entry.ResumeInstanceID)
	if instanceID == "" {
		return false
	}
	inst := a.service.Instance(instanceID)
	if inst == nil {
		return false
	}
	// Give a connected visible-instance resume one startup refresh round before
	// falling back to a new headless launch for the same persisted target.
	return strings.TrimSpace(inst.Source) != "headless"
}

func (a *App) maybeRecoverHeadlessSurfacesLocked(now time.Time) []control.UIEvent {
	if len(a.surfaceResumeRuntime.headlessRestore) == 0 {
		return nil
	}
	surfaceIDs := make([]string, 0, len(a.surfaceResumeRuntime.headlessRestore))
	for surfaceID := range a.surfaceResumeRuntime.headlessRestore {
		surfaceIDs = append(surfaceIDs, surfaceID)
	}
	sort.Strings(surfaceIDs)
	allowMissingThreadFailure := a.initialThreadsRefreshRoundCompleteLocked()
	events := []control.UIEvent{}
	updatedSurfaceIDs := make([]string, 0, len(surfaceIDs))
	for _, surfaceID := range surfaceIDs {
		state := a.surfaceResumeRuntime.headlessRestore[surfaceID]
		if state == nil {
			continue
		}
		if a.shouldDeferHeadlessRestoreUntilInitialRefreshLocked(state.Entry, allowMissingThreadFailure) {
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
		if result.Status == orchestrator.HeadlessRestoreStatusAttached || result.Status == orchestrator.HeadlessRestoreStatusStarting {
			updatedSurfaceIDs = append(updatedSurfaceIDs, surfaceID)
		}
		events = append(events, restoreEvents...)
	}
	a.syncSurfaceResumeStateForSurfacesLocked(updatedSurfaceIDs, nil)
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
	state := a.surfaceResumeRuntime.headlessRestore[strings.TrimSpace(surfaceID)]
	if state == nil {
		return
	}
	state.NextAttemptAt = time.Time{}
	state.LastAttemptAt = time.Time{}
	state.LastFailureCode = ""
}

func (a *App) setHeadlessRestoreBackoffLocked(surfaceID, code string, now time.Time) {
	state := a.surfaceResumeRuntime.headlessRestore[strings.TrimSpace(surfaceID)]
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
	if a.surfaceResumeRuntime.startupRefreshPending == nil {
		a.surfaceResumeRuntime.startupRefreshPending = map[string]bool{}
	}
	a.surfaceResumeRuntime.startupRefreshSeen = true
	a.surfaceResumeRuntime.startupRefreshPending[instanceID] = true
}

func (a *App) markStartupThreadsRefreshSettledLocked(instanceID string) {
	delete(a.surfaceResumeRuntime.startupRefreshPending, strings.TrimSpace(instanceID))
}

func (a *App) initialThreadsRefreshRoundCompleteLocked() bool {
	return a.surfaceResumeRuntime.startupRefreshSeen && len(a.surfaceResumeRuntime.startupRefreshPending) == 0
}
