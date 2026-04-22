package daemon

import (
	"log"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type surfaceResumeTarget struct {
	ResumeInstanceID   string
	ResumeThreadID     string
	ResumeThreadTitle  string
	ResumeThreadCWD    string
	ResumeWorkspaceKey string
	ResumeRouteMode    string
	ResumeHeadless     bool
}

const surfaceResumeRetryBackoff = 30 * time.Second

func (a *App) configureSurfaceResumeStateLocked(stateDir string) {
	path := surfaceResumeStatePath(stateDir)
	store, err := loadSurfaceResumeStore(path)
	if err != nil {
		log.Printf("load surface resume state failed: path=%s err=%v", path, err)
		store = newSurfaceResumeStore(path)
	}
	if store != nil && store.Dirty() {
		if err := store.Save(); err != nil {
			log.Printf("persist sanitized surface resume state failed: path=%s err=%v", path, err)
		}
	}
	a.surfaceResumeRuntime.store = store
	a.materializeSurfaceResumeStateLocked()
	a.syncSurfaceResumeRecoveryStateLocked()
	a.syncHeadlessRestoreStateLocked()
	a.surfaceResumeRuntime.vscodeStartupCheckDue = storedVSCodeResumeExists(store)
}

func (a *App) SurfaceResumeState(surfaceID string) *SurfaceResumeEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.surfaceResumeRuntime.store == nil {
		return nil
	}
	entry, ok := a.surfaceResumeRuntime.store.Get(surfaceID)
	if !ok {
		return nil
	}
	copy := entry
	return &copy
}

func (a *App) materializeSurfaceResumeStateLocked() {
	if a.surfaceResumeRuntime.store == nil {
		return
	}
	entries := a.surfaceResumeRuntime.store.Entries()
	surfaceIDs := make([]string, 0, len(entries))
	for surfaceID := range entries {
		surfaceIDs = append(surfaceIDs, surfaceID)
	}
	sort.Strings(surfaceIDs)
	for _, surfaceID := range surfaceIDs {
		entry := entries[surfaceID]
		a.service.MaterializeSurfaceResume(
			entry.SurfaceSessionID,
			entry.GatewayID,
			entry.ChatID,
			entry.ActorUserID,
			state.ProductMode(entry.ProductMode),
			state.SurfaceVerbosity(entry.Verbosity),
			state.PlanModeSetting(entry.PlanMode),
		)
	}
}

func storedVSCodeResumeExists(store *surfaceResumeStore) bool {
	if store == nil {
		return false
	}
	for _, entry := range store.Entries() {
		if state.NormalizeProductMode(state.ProductMode(entry.ProductMode)) == state.ProductModeVSCode {
			return true
		}
	}
	return false
}

func (a *App) syncSurfaceResumeStateLocked(clearTargets map[string]bool) {
	if a.surfaceResumeRuntime.store == nil {
		return
	}
	existing := a.surfaceResumeRuntime.store.Entries()
	desired := map[string]SurfaceResumeEntry{}
	now := time.Now().UTC()
	for _, surface := range a.service.Surfaces() {
		if surface == nil {
			continue
		}
		clearResumeTarget := false
		if clearTargets != nil {
			clearResumeTarget = clearTargets[strings.TrimSpace(surface.SurfaceSessionID)]
		}
		entry, ok := a.currentSurfaceResumeEntryLocked(surface, clearResumeTarget)
		if !ok {
			continue
		}
		desired[entry.SurfaceSessionID] = entry
		if current, ok := a.surfaceResumeRuntime.store.Get(entry.SurfaceSessionID); ok && sameSurfaceResumeEntryContent(current, entry) {
			continue
		}
		entry.UpdatedAt = now
		if err := a.surfaceResumeRuntime.store.Put(entry); err != nil {
			log.Printf("persist surface resume state failed: surface=%s err=%v", entry.SurfaceSessionID, err)
		}
	}
	for surfaceID := range existing {
		if _, ok := desired[surfaceID]; ok {
			continue
		}
		if err := a.surfaceResumeRuntime.store.Delete(surfaceID); err != nil {
			log.Printf("clear surface resume state failed: surface=%s err=%v", surfaceID, err)
		}
	}
	a.syncVSCodeResumeNoticeStateLocked(desired)
	a.syncSurfaceResumeRecoveryStateLocked()
	a.syncHeadlessRestoreStateLocked()
}

func (a *App) syncSurfaceResumeStateForInstanceLocked(instanceID string, clearTargets map[string]bool) {
	if a.surfaceResumeRuntime.store == nil {
		return
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return
	}
	now := time.Now().UTC()
	touched := false
	for _, surface := range a.service.Surfaces() {
		if surface == nil || strings.TrimSpace(surface.AttachedInstanceID) != instanceID {
			continue
		}
		touched = true
		clearResumeTarget := false
		if clearTargets != nil {
			clearResumeTarget = clearTargets[strings.TrimSpace(surface.SurfaceSessionID)]
		}
		entry, ok := a.currentSurfaceResumeEntryLocked(surface, clearResumeTarget)
		if !ok {
			if err := a.surfaceResumeRuntime.store.Delete(strings.TrimSpace(surface.SurfaceSessionID)); err != nil {
				log.Printf("clear surface resume state failed: surface=%s err=%v", surface.SurfaceSessionID, err)
			}
			continue
		}
		if current, ok := a.surfaceResumeRuntime.store.Get(entry.SurfaceSessionID); ok && sameSurfaceResumeEntryContent(current, entry) {
			continue
		}
		entry.UpdatedAt = now
		if err := a.surfaceResumeRuntime.store.Put(entry); err != nil {
			log.Printf("persist surface resume state failed: surface=%s err=%v", entry.SurfaceSessionID, err)
		}
	}
	if !touched {
		return
	}
	a.syncVSCodeResumeNoticeStateLocked(nil)
	a.syncSurfaceResumeRecoveryStateLocked()
	a.syncHeadlessRestoreStateLocked()
}

func (a *App) syncSurfaceResumeStateForSurfacesLocked(surfaceIDs []string, clearTargets map[string]bool) {
	if a.surfaceResumeRuntime.store == nil || len(surfaceIDs) == 0 {
		return
	}
	surfacesByID := map[string]*state.SurfaceConsoleRecord{}
	for _, surface := range a.service.Surfaces() {
		if surface == nil {
			continue
		}
		surfacesByID[strings.TrimSpace(surface.SurfaceSessionID)] = surface
	}
	now := time.Now().UTC()
	touched := false
	for _, surfaceID := range surfaceIDs {
		surfaceID = strings.TrimSpace(surfaceID)
		if surfaceID == "" {
			continue
		}
		surface := surfacesByID[surfaceID]
		if surface == nil {
			if err := a.surfaceResumeRuntime.store.Delete(surfaceID); err != nil {
				log.Printf("clear surface resume state failed: surface=%s err=%v", surfaceID, err)
			}
			touched = true
			continue
		}
		clearResumeTarget := false
		if clearTargets != nil {
			clearResumeTarget = clearTargets[surfaceID]
		}
		entry, ok := a.currentSurfaceResumeEntryLocked(surface, clearResumeTarget)
		if !ok {
			if err := a.surfaceResumeRuntime.store.Delete(surfaceID); err != nil {
				log.Printf("clear surface resume state failed: surface=%s err=%v", surfaceID, err)
			}
			touched = true
			continue
		}
		if current, ok := a.surfaceResumeRuntime.store.Get(entry.SurfaceSessionID); ok && sameSurfaceResumeEntryContent(current, entry) {
			continue
		}
		entry.UpdatedAt = now
		if err := a.surfaceResumeRuntime.store.Put(entry); err != nil {
			log.Printf("persist surface resume state failed: surface=%s err=%v", entry.SurfaceSessionID, err)
		}
		touched = true
	}
	if !touched {
		return
	}
	a.syncVSCodeResumeNoticeStateLocked(nil)
	a.syncSurfaceResumeRecoveryStateLocked()
	a.syncHeadlessRestoreStateLocked()
}

func (a *App) currentSurfaceResumeEntryLocked(surface *state.SurfaceConsoleRecord, clearResumeTarget bool) (SurfaceResumeEntry, bool) {
	if surface == nil {
		return SurfaceResumeEntry{}, false
	}
	entry := SurfaceResumeEntry{
		SurfaceSessionID: strings.TrimSpace(surface.SurfaceSessionID),
		GatewayID:        strings.TrimSpace(surface.GatewayID),
		ChatID:           strings.TrimSpace(surface.ChatID),
		ActorUserID:      strings.TrimSpace(surface.ActorUserID),
		ProductMode:      string(state.NormalizeProductMode(surface.ProductMode)),
		Verbosity:        string(state.NormalizeSurfaceVerbosity(surface.Verbosity)),
		PlanMode:         string(state.NormalizePlanModeSetting(surface.PlanMode)),
	}
	if entry.SurfaceSessionID == "" {
		return SurfaceResumeEntry{}, false
	}
	if !clearResumeTarget {
		if target, ok := a.currentSurfaceResumeTargetLocked(surface); ok {
			entry.ResumeInstanceID = target.ResumeInstanceID
			entry.ResumeThreadID = target.ResumeThreadID
			entry.ResumeThreadTitle = target.ResumeThreadTitle
			entry.ResumeThreadCWD = target.ResumeThreadCWD
			entry.ResumeWorkspaceKey = target.ResumeWorkspaceKey
			entry.ResumeRouteMode = target.ResumeRouteMode
			entry.ResumeHeadless = target.ResumeHeadless
		} else if previous, ok := a.surfaceResumeRuntime.store.Get(entry.SurfaceSessionID); ok {
			entry.ResumeInstanceID = previous.ResumeInstanceID
			entry.ResumeThreadID = previous.ResumeThreadID
			entry.ResumeThreadTitle = previous.ResumeThreadTitle
			entry.ResumeThreadCWD = previous.ResumeThreadCWD
			entry.ResumeWorkspaceKey = previous.ResumeWorkspaceKey
			entry.ResumeRouteMode = previous.ResumeRouteMode
			entry.ResumeHeadless = previous.ResumeHeadless
		}
	}
	normalized, ok := normalizeSurfaceResumeEntry(entry)
	return normalized, ok
}

func (a *App) currentSurfaceResumeTargetLocked(surface *state.SurfaceConsoleRecord) (surfaceResumeTarget, bool) {
	if surface == nil {
		return surfaceResumeTarget{}, false
	}
	snapshot := a.service.SurfaceSnapshot(surface.SurfaceSessionID)
	workspaceKey := ""
	if snapshot != nil {
		workspaceKey = state.ResolveWorkspaceKey(snapshot.WorkspaceKey)
	}
	if strings.TrimSpace(surface.AttachedInstanceID) != "" {
		target := surfaceResumeTarget{
			ResumeInstanceID:   strings.TrimSpace(surface.AttachedInstanceID),
			ResumeThreadID:     strings.TrimSpace(surface.SelectedThreadID),
			ResumeWorkspaceKey: state.ResolveWorkspaceKey(workspaceKey, surface.ClaimedWorkspaceKey, surface.PreparedThreadCWD),
			ResumeRouteMode:    strings.TrimSpace(string(surface.RouteMode)),
		}
		threadName := ""
		if snapshot != nil {
			target.ResumeHeadless = snapshot.Attachment.Managed && strings.EqualFold(strings.TrimSpace(snapshot.Attachment.Source), "headless")
			target.ResumeThreadTitle = strings.TrimSpace(snapshot.Attachment.SelectedThreadTitle)
		}
		if target.ResumeThreadID != "" {
			if inst := a.service.Instance(target.ResumeInstanceID); inst != nil {
				if thread := inst.Threads[target.ResumeThreadID]; thread != nil {
					threadName = strings.TrimSpace(thread.Name)
					target.ResumeThreadCWD = state.ResolveWorkspaceKey(thread.CWD)
				}
			}
			target.ResumeThreadTitle = storedResumeThreadTitle(target.ResumeThreadTitle, target.ResumeThreadID, target.ResumeThreadCWD, target.ResumeWorkspaceKey, threadName)
		}
		return target, true
	}
	if pending := surface.PendingHeadless; pending != nil {
		return surfaceResumeTarget{
			ResumeThreadID:     strings.TrimSpace(pending.ThreadID),
			ResumeThreadTitle:  firstNonEmpty(strings.TrimSpace(pending.ThreadTitle), strings.TrimSpace(pending.ThreadID)),
			ResumeThreadCWD:    state.ResolveWorkspaceKey(pending.ThreadCWD),
			ResumeWorkspaceKey: state.ResolveWorkspaceKey(workspaceKey, pending.ThreadCWD),
			ResumeRouteMode:    string(state.RouteModePinned),
			ResumeHeadless:     true,
		}, true
	}
	if surface.RouteMode == state.RouteModeNewThreadReady {
		workspaceKey = state.ResolveWorkspaceKey(workspaceKey, surface.PreparedThreadCWD)
		if workspaceKey != "" {
			return surfaceResumeTarget{
				ResumeWorkspaceKey: workspaceKey,
				ResumeRouteMode:    string(state.RouteModeNewThreadReady),
			}, true
		}
	}
	return surfaceResumeTarget{}, false
}

func (a *App) shouldClearSurfaceResumeTargetLocked(action control.Action, before *control.Snapshot) bool {
	switch action.Kind {
	case control.ActionDetach:
		return true
	case control.ActionModeCommand:
		after := a.service.SurfaceSnapshot(action.SurfaceSessionID)
		if before == nil || after == nil {
			return false
		}
		return !strings.EqualFold(strings.TrimSpace(before.ProductMode), strings.TrimSpace(after.ProductMode))
	default:
		return false
	}
}

func (a *App) syncSurfaceResumeRecoveryStateLocked() {
	if a.surfaceResumeRuntime.recovery == nil {
		a.surfaceResumeRuntime.recovery = map[string]*surfaceResumeRecoveryState{}
	}
	entries := map[string]SurfaceResumeEntry{}
	if a.surfaceResumeRuntime.store != nil {
		entries = a.surfaceResumeRuntime.store.Entries()
	}
	for surfaceID, entry := range entries {
		if !surfaceResumeEntryNeedsRecovery(entry) {
			delete(a.surfaceResumeRuntime.recovery, surfaceID)
			continue
		}
		current := a.surfaceResumeRuntime.recovery[surfaceID]
		if current == nil || !sameSurfaceResumeEntryContent(current.Entry, entry) {
			a.surfaceResumeRuntime.recovery[surfaceID] = &surfaceResumeRecoveryState{Entry: entry}
			continue
		}
		current.Entry = entry
	}
	for surfaceID := range a.surfaceResumeRuntime.recovery {
		if entry, ok := entries[surfaceID]; !ok || !surfaceResumeEntryNeedsRecovery(entry) {
			delete(a.surfaceResumeRuntime.recovery, surfaceID)
		}
	}
}

func surfaceResumeEntryNeedsRecovery(entry SurfaceResumeEntry) bool {
	switch state.NormalizeProductMode(state.ProductMode(entry.ProductMode)) {
	case state.ProductModeNormal:
		return strings.TrimSpace(entry.ResumeThreadID) != "" || state.NormalizeWorkspaceKey(entry.ResumeWorkspaceKey) != ""
	case state.ProductModeVSCode:
		return strings.TrimSpace(entry.ResumeInstanceID) != ""
	default:
		return false
	}
}

func (a *App) maybeRecoverNormalSurfacesLocked(now time.Time) []control.UIEvent {
	if len(a.surfaceResumeRuntime.recovery) == 0 {
		return nil
	}
	surfaceIDs := make([]string, 0, len(a.surfaceResumeRuntime.recovery))
	for surfaceID := range a.surfaceResumeRuntime.recovery {
		surfaceIDs = append(surfaceIDs, surfaceID)
	}
	sort.Strings(surfaceIDs)
	allowMissingTargetFailure := a.initialThreadsRefreshRoundCompleteLocked()
	events := []control.UIEvent{}
	updatedSurfaceIDs := make([]string, 0, len(surfaceIDs))
	for _, surfaceID := range surfaceIDs {
		recovery := a.surfaceResumeRuntime.recovery[surfaceID]
		if recovery == nil {
			continue
		}
		if !recovery.NextAttemptAt.IsZero() && now.Before(recovery.NextAttemptAt) {
			continue
		}
		workspaceKey := recovery.Entry.ResumeWorkspaceKey
		if recovery.Entry.ResumeHeadless {
			workspaceKey = ""
		}
		restoreEvents, result := a.service.TryAutoResumeNormalSurface(surfaceID, orchestrator.SurfaceResumeAttempt{
			InstanceID:   recovery.Entry.ResumeInstanceID,
			ThreadID:     recovery.Entry.ResumeThreadID,
			WorkspaceKey: workspaceKey,
		}, allowMissingTargetFailure)
		switch result.Status {
		case orchestrator.SurfaceResumeStatusThreadAttached, orchestrator.SurfaceResumeStatusWorkspaceAttached:
			a.clearSurfaceResumeBackoffLocked(surfaceID)
			events = append(events, restoreEvents...)
			updatedSurfaceIDs = append(updatedSurfaceIDs, surfaceID)
		case orchestrator.SurfaceResumeStatusFailed:
			a.setSurfaceResumeBackoffLocked(surfaceID, result.FailureCode, now)
			if recovery.Entry.ResumeHeadless {
				continue
			}
			notice := orchestrator.NoticeForSurfaceResumeFailure(result.FailureCode)
			if notice != nil {
				events = append(events, control.UIEvent{
					Kind:             control.UIEventNotice,
					SurfaceSessionID: surfaceID,
					Notice:           notice,
				})
			}
		}
	}
	a.syncSurfaceResumeStateForSurfacesLocked(updatedSurfaceIDs, nil)
	return events
}

func (a *App) maybeRecoverVSCodeSurfacesLocked(now time.Time) []control.UIEvent {
	if len(a.surfaceResumeRuntime.recovery) == 0 {
		return nil
	}
	surfaceIDs := make([]string, 0, len(a.surfaceResumeRuntime.recovery))
	for surfaceID := range a.surfaceResumeRuntime.recovery {
		surfaceIDs = append(surfaceIDs, surfaceID)
	}
	sort.Strings(surfaceIDs)
	events := []control.UIEvent{}
	updatedSurfaceIDs := make([]string, 0, len(surfaceIDs))
	for _, surfaceID := range surfaceIDs {
		recovery := a.surfaceResumeRuntime.recovery[surfaceID]
		if recovery == nil || state.NormalizeProductMode(state.ProductMode(recovery.Entry.ProductMode)) != state.ProductModeVSCode {
			continue
		}
		if !recovery.NextAttemptAt.IsZero() && now.Before(recovery.NextAttemptAt) {
			continue
		}
		restoreEvents, result := a.service.TryAutoResumeVSCodeSurface(surfaceID, recovery.Entry.ResumeInstanceID)
		switch result.Status {
		case orchestrator.SurfaceResumeStatusInstanceAttached:
			a.clearSurfaceResumeBackoffLocked(surfaceID)
			events = append(events, restoreEvents...)
			updatedSurfaceIDs = append(updatedSurfaceIDs, surfaceID)
		case orchestrator.SurfaceResumeStatusFailed:
			a.setSurfaceResumeBackoffLocked(surfaceID, result.FailureCode, now)
			notice := orchestrator.NoticeForVSCodeSurfaceResumeFailure(result.FailureCode)
			if notice != nil {
				events = append(events, control.UIEvent{
					Kind:             control.UIEventNotice,
					SurfaceSessionID: surfaceID,
					Notice:           notice,
				})
			}
		}
	}
	a.syncSurfaceResumeStateForSurfacesLocked(updatedSurfaceIDs, nil)
	return events
}

func (a *App) maybePromptDetachedVSCodeSurfacesLocked() []control.UIEvent {
	if a.surfaceResumeRuntime.store == nil {
		return nil
	}
	entries := a.surfaceResumeRuntime.store.Entries()
	if len(entries) == 0 {
		return nil
	}
	surfaceIDs := make([]string, 0, len(entries))
	for surfaceID := range entries {
		surfaceIDs = append(surfaceIDs, surfaceID)
	}
	sort.Strings(surfaceIDs)
	events := make([]control.UIEvent, 0, len(surfaceIDs))
	for _, surfaceID := range surfaceIDs {
		entry := entries[surfaceID]
		if state.NormalizeProductMode(state.ProductMode(entry.ProductMode)) != state.ProductModeVSCode {
			continue
		}
		if !entryPredatesDaemonStart(a.daemonStartedAt, entry.UpdatedAt) {
			continue
		}
		if a.surfaceResumeRuntime.vscodeResumeNotices[strings.TrimSpace(surfaceID)] {
			continue
		}
		snapshot := a.service.SurfaceSnapshot(surfaceID)
		if snapshot == nil || state.NormalizeProductMode(state.ProductMode(snapshot.ProductMode)) != state.ProductModeVSCode {
			continue
		}
		if strings.TrimSpace(snapshot.Attachment.InstanceID) != "" || strings.TrimSpace(snapshot.PendingHeadless.InstanceID) != "" {
			continue
		}
		a.surfaceResumeRuntime.vscodeResumeNotices[strings.TrimSpace(surfaceID)] = true
		events = append(events, control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surfaceID,
			Notice:           orchestrator.NoticeForVSCodeOpenPrompt(strings.TrimSpace(entry.ResumeInstanceID) != ""),
		})
	}
	return events
}

func (a *App) syncVSCodeResumeNoticeStateLocked(entries map[string]SurfaceResumeEntry) {
	if a.surfaceResumeRuntime.vscodeResumeNotices == nil {
		a.surfaceResumeRuntime.vscodeResumeNotices = map[string]bool{}
	}
	if entries == nil {
		entries = map[string]SurfaceResumeEntry{}
		if a.surfaceResumeRuntime.store != nil {
			entries = a.surfaceResumeRuntime.store.Entries()
		}
	}
	for surfaceID := range a.surfaceResumeRuntime.vscodeResumeNotices {
		entry, ok := entries[surfaceID]
		if !ok || state.NormalizeProductMode(state.ProductMode(entry.ProductMode)) != state.ProductModeVSCode {
			delete(a.surfaceResumeRuntime.vscodeResumeNotices, surfaceID)
		}
	}
}

func entryPredatesDaemonStart(daemonStartedAt, updatedAt time.Time) bool {
	if updatedAt.IsZero() {
		return true
	}
	if daemonStartedAt.IsZero() {
		return true
	}
	return !updatedAt.After(daemonStartedAt)
}

func (a *App) clearSurfaceResumeBackoffLocked(surfaceID string) {
	recovery := a.surfaceResumeRuntime.recovery[strings.TrimSpace(surfaceID)]
	if recovery == nil {
		return
	}
	recovery.NextAttemptAt = time.Time{}
	recovery.LastAttemptAt = time.Time{}
	recovery.LastFailureCode = ""
}

func (a *App) setSurfaceResumeBackoffLocked(surfaceID, code string, now time.Time) {
	recovery := a.surfaceResumeRuntime.recovery[strings.TrimSpace(surfaceID)]
	if recovery == nil {
		return
	}
	recovery.LastAttemptAt = now
	recovery.NextAttemptAt = now.Add(surfaceResumeRetryBackoff)
	recovery.LastFailureCode = strings.TrimSpace(code)
}
