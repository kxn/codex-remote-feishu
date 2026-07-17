package daemon

import (
	"log"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/threadtitle"
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
	path := surfaceresume.StatePath(stateDir)
	store, err := surfaceresume.LoadStore(path)
	if err != nil {
		log.Printf("load surface resume state failed: path=%s err=%v", path, err)
		store = surfaceresume.NewStore(path)
	}
	if store != nil && store.Dirty() {
		if err := store.Save(); err != nil {
			log.Printf("persist sanitized surface resume state failed: path=%s err=%v", path, err)
		}
	}
	a.surfaceResumeRuntime.store = store
	a.materializeSurfaceResumeStateLocked()
	a.syncSurfaceResumeRecoveryStateLocked()
	a.surfaceResumeRuntime.vscodeStartupCheckDue = storedVSCodeResumeExists(store)
	a.markVSCodeDetachedPromptScanDueLocked()
}

func (a *App) SurfaceResumeState(surfaceID string) *surfaceresume.Entry {
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
		a.service.MaterializeSurfaceResumeContract(
			entry.SurfaceSessionID,
			entry.GatewayID,
			entry.ChatID,
			entry.ActorUserID,
			state.PersistedSurfaceBackendContract(
				state.ProductMode(entry.ProductMode),
				agentproto.Backend(entry.Backend),
				entry.CodexProviderID,
				entry.ClaudeProfileID,
			),
			state.SurfaceVerbosity(entry.Verbosity),
			state.PlanModeSettingOff,
		)
	}
}

func storedVSCodeResumeExists(store *surfaceresume.Store) bool {
	if store == nil {
		return false
	}
	for _, entry := range store.Entries() {
		if state.IsVSCodeProductMode(state.ProductMode(entry.ProductMode)) {
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
	desired := map[string]surfaceresume.Entry{}
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
		a.putSurfaceResumeEntryLocked(entry, now)
	}
	for surfaceID := range existing {
		if _, ok := desired[surfaceID]; ok {
			continue
		}
		a.deleteSurfaceResumeEntryLocked(surfaceID)
	}
	a.syncVSCodeResumeNoticeStateLocked(desired)
	a.syncSurfaceResumeRecoveryStateLocked()
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
			a.deleteSurfaceResumeEntryLocked(strings.TrimSpace(surface.SurfaceSessionID))
			continue
		}
		a.putSurfaceResumeEntryLocked(entry, now)
	}
	if !touched {
		return
	}
	a.syncVSCodeResumeNoticeStateLocked(nil)
	a.syncSurfaceResumeRecoveryStateLocked()
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
			a.deleteSurfaceResumeEntryLocked(surfaceID)
			touched = true
			continue
		}
		clearResumeTarget := false
		if clearTargets != nil {
			clearResumeTarget = clearTargets[surfaceID]
		}
		entry, ok := a.currentSurfaceResumeEntryLocked(surface, clearResumeTarget)
		if !ok {
			a.deleteSurfaceResumeEntryLocked(surfaceID)
			touched = true
			continue
		}
		if a.putSurfaceResumeEntryLocked(entry, now) {
			touched = true
		}
	}
	if !touched {
		return
	}
	a.syncVSCodeResumeNoticeStateLocked(nil)
	a.syncSurfaceResumeRecoveryStateLocked()
}

func (a *App) putSurfaceResumeEntryLocked(entry surfaceresume.Entry, now time.Time) bool {
	if a.surfaceResumeRuntime.store == nil {
		return false
	}
	if current, ok := a.surfaceResumeRuntime.store.Get(entry.SurfaceSessionID); ok && surfaceresume.SameEntryContent(current, entry) {
		return false
	}
	entry.UpdatedAt = now
	if err := a.surfaceResumeRuntime.store.Put(entry); err != nil {
		log.Printf("persist surface resume state failed: surface=%s err=%v", entry.SurfaceSessionID, err)
	}
	a.markVSCodeDetachedPromptScanDueLocked()
	return true
}

func (a *App) deleteSurfaceResumeEntryLocked(surfaceID string) bool {
	if a.surfaceResumeRuntime.store == nil {
		return false
	}
	surfaceID = strings.TrimSpace(surfaceID)
	if surfaceID == "" {
		return false
	}
	if err := a.surfaceResumeRuntime.store.Delete(surfaceID); err != nil {
		log.Printf("clear surface resume state failed: surface=%s err=%v", surfaceID, err)
	}
	a.markVSCodeDetachedPromptScanDueLocked()
	return true
}

func (a *App) currentSurfaceResumeEntryLocked(surface *state.SurfaceConsoleRecord, clearResumeTarget bool) (surfaceresume.Entry, bool) {
	if surface == nil {
		return surfaceresume.Entry{}, false
	}
	entry := surfaceresume.Entry{
		SurfaceSessionID: strings.TrimSpace(surface.SurfaceSessionID),
		GatewayID:        strings.TrimSpace(surface.GatewayID),
		ChatID:           strings.TrimSpace(surface.ChatID),
		ActorUserID:      strings.TrimSpace(surface.ActorUserID),
		ProductMode:      string(state.NormalizeProductMode(surface.ProductMode)),
		Backend:          string(a.service.SurfaceBackend(surface.SurfaceSessionID)),
		CodexProviderID:  strings.TrimSpace(a.service.SurfaceCodexProviderID(surface.SurfaceSessionID)),
		ClaudeProfileID:  strings.TrimSpace(a.service.SurfaceClaudeProfileID(surface.SurfaceSessionID)),
		Verbosity:        string(state.NormalizeSurfaceVerbosity(surface.Verbosity)),
	}
	if entry.SurfaceSessionID == "" {
		return surfaceresume.Entry{}, false
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
	normalized, ok := surfaceresume.NormalizeEntry(entry)
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
		if snapshot != nil {
			target.ResumeHeadless = target.ResumeThreadID != "" &&
				snapshot.Attachment.Managed &&
				strings.EqualFold(strings.TrimSpace(snapshot.Attachment.Source), "headless")
			target.ResumeThreadTitle = strings.TrimSpace(snapshot.Attachment.SelectedThreadTitle)
		}
		if target.ResumeThreadID != "" {
			var thread *state.ThreadRecord
			if inst := a.service.Instance(target.ResumeInstanceID); inst != nil {
				if current := inst.Threads[target.ResumeThreadID]; current != nil {
					thread = current
					target.ResumeThreadCWD = state.ResolveWorkspaceKey(thread.CWD)
					target.ResumeWorkspaceKey = state.ResolveWorkspaceKey(target.ResumeWorkspaceKey, thread.WorkspaceKey, inst.WorkspaceKey, inst.WorkspaceRoot)
				}
			}
			target.ResumeThreadTitle = threadtitle.StoredTitle(target.ResumeThreadTitle, threadtitle.Context{
				ThreadID:     target.ResumeThreadID,
				ThreadCWD:    target.ResumeThreadCWD,
				WorkspaceKey: target.ResumeWorkspaceKey,
			}, thread)
		}
		return target, true
	}
	if pending := surface.PendingHeadless; pending != nil {
		if pending.Purpose == state.HeadlessLaunchPurposeFreshWorkspace {
			routeMode := state.RouteModeUnbound
			if pending.PrepareNewThread {
				routeMode = state.RouteModeNewThreadReady
			}
			if resumeWorkspaceKey := state.ResolveWorkspaceKey(workspaceKey, pending.WorkspaceKey, pending.ThreadCWD); resumeWorkspaceKey != "" {
				return surfaceResumeTarget{
					ResumeWorkspaceKey: resumeWorkspaceKey,
					ResumeRouteMode:    string(routeMode),
				}, true
			}
			return surfaceResumeTarget{}, false
		}
		return surfaceResumeTarget{
			ResumeThreadID:     strings.TrimSpace(pending.ThreadID),
			ResumeThreadTitle:  strings.TrimSpace(pending.ThreadTitle),
			ResumeThreadCWD:    state.ResolveWorkspaceKey(pending.ThreadCWD),
			ResumeWorkspaceKey: state.ResolveWorkspaceKey(workspaceKey, pending.WorkspaceKey, pending.ThreadCWD),
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
		if strings.EqualFold(strings.TrimSpace(before.ProductMode), strings.TrimSpace(after.ProductMode)) &&
			agentproto.NormalizeBackend(before.Backend) == agentproto.NormalizeBackend(after.Backend) {
			return false
		}
		if afterSurface := a.surfaceByIDLocked(action.SurfaceSessionID); afterSurface != nil {
			if _, ok := a.currentSurfaceResumeTargetLocked(afterSurface); ok {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func (a *App) syncSurfaceResumeRecoveryStateLocked() {
	if a.surfaceResumeRuntime.recovery == nil {
		a.surfaceResumeRuntime.recovery = map[string]*surfaceResumeRecoveryState{}
	}
	entries := map[string]surfaceresume.Entry{}
	if a.surfaceResumeRuntime.store != nil {
		entries = a.surfaceResumeRuntime.store.Entries()
	}
	for surfaceID, entry := range entries {
		if !surfaceResumeEntryNeedsRecovery(entry) {
			delete(a.surfaceResumeRuntime.recovery, surfaceID)
			continue
		}
		current := a.surfaceResumeRuntime.recovery[surfaceID]
		if current == nil || !sameSurfaceResumeRecoveryTarget(current.Entry, entry) {
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

func (a *App) markVSCodeDetachedPromptScanDueLocked() {
	a.surfaceResumeRuntime.vscodeDetachedPromptScanDue = true
}

func sameSurfaceResumeRecoveryTarget(left, right surfaceresume.Entry) bool {
	return strings.TrimSpace(left.SurfaceSessionID) == strings.TrimSpace(right.SurfaceSessionID) &&
		strings.TrimSpace(left.ProductMode) == strings.TrimSpace(right.ProductMode) &&
		state.NormalizeHeadlessBackend(agentproto.Backend(left.Backend)) == state.NormalizeHeadlessBackend(agentproto.Backend(right.Backend)) &&
		strings.TrimSpace(left.CodexProviderID) == strings.TrimSpace(right.CodexProviderID) &&
		strings.TrimSpace(left.ClaudeProfileID) == strings.TrimSpace(right.ClaudeProfileID) &&
		strings.TrimSpace(left.ResumeInstanceID) == strings.TrimSpace(right.ResumeInstanceID) &&
		strings.TrimSpace(left.ResumeThreadID) == strings.TrimSpace(right.ResumeThreadID) &&
		state.NormalizeWorkspaceKey(left.ResumeThreadCWD) == state.NormalizeWorkspaceKey(right.ResumeThreadCWD) &&
		state.NormalizeWorkspaceKey(left.ResumeWorkspaceKey) == state.NormalizeWorkspaceKey(right.ResumeWorkspaceKey) &&
		strings.TrimSpace(left.ResumeRouteMode) == strings.TrimSpace(right.ResumeRouteMode) &&
		left.ResumeHeadless == right.ResumeHeadless
}

func surfaceResumeEntryNeedsRecovery(entry surfaceresume.Entry) bool {
	switch {
	case state.IsHeadlessProductMode(state.ProductMode(entry.ProductMode)):
		return strings.TrimSpace(entry.ResumeThreadID) != "" || state.NormalizeWorkspaceKey(entry.ResumeWorkspaceKey) != ""
	case state.IsVSCodeProductMode(state.ProductMode(entry.ProductMode)):
		return strings.TrimSpace(entry.ResumeInstanceID) != ""
	default:
		return false
	}
}

func surfaceResumeRecoveryDue(recovery *surfaceResumeRecoveryState, now time.Time) bool {
	if recovery == nil {
		return false
	}
	return recovery.NextAttemptAt.IsZero() || !now.Before(recovery.NextAttemptAt)
}

func surfaceResumeRecoveryDueForProductMode(recovery *surfaceResumeRecoveryState, now time.Time, productMode state.ProductMode) bool {
	if !surfaceResumeRecoveryDue(recovery, now) {
		return false
	}
	return state.NormalizeProductMode(state.ProductMode(recovery.Entry.ProductMode)) == state.NormalizeProductMode(productMode)
}

func (a *App) hasRunnableHeadlessSurfaceRecoveryLocked(now time.Time, allowMissingTargetFailure bool) bool {
	for _, recovery := range a.surfaceResumeRuntime.recovery {
		if recovery == nil || !surfaceResumeRecoveryDue(recovery, now) {
			continue
		}
		if !state.IsHeadlessProductMode(state.ProductMode(recovery.Entry.ProductMode)) {
			continue
		}
		if recovery.Entry.ResumeHeadless && a.shouldDeferHeadlessResumeUntilInitialRefreshLocked(recovery.Entry, allowMissingTargetFailure) {
			continue
		}
		return true
	}
	return false
}

func (a *App) hasDueHeadlessSurfaceRecoveryLocked(now time.Time) bool {
	for _, recovery := range a.surfaceResumeRuntime.recovery {
		if recovery == nil || !surfaceResumeRecoveryDue(recovery, now) {
			continue
		}
		if state.IsHeadlessProductMode(state.ProductMode(recovery.Entry.ProductMode)) {
			return true
		}
	}
	return false
}

func (a *App) hasDueVSCodeSurfaceRecoveryLocked(now time.Time) bool {
	for _, recovery := range a.surfaceResumeRuntime.recovery {
		if surfaceResumeRecoveryDueForProductMode(recovery, now, state.ProductModeVSCode) {
			return true
		}
	}
	return false
}

func (a *App) maybeRecoverHeadlessSurfacesLocked(now time.Time) []eventcontract.Event {
	if len(a.surfaceResumeRuntime.recovery) == 0 {
		return nil
	}
	allowMissingTargetFailure := a.initialThreadsRefreshRoundCompleteLocked()
	if !a.hasRunnableHeadlessSurfaceRecoveryLocked(now, allowMissingTargetFailure) {
		return nil
	}
	surfaceIDs := make([]string, 0, len(a.surfaceResumeRuntime.recovery))
	for surfaceID := range a.surfaceResumeRuntime.recovery {
		surfaceIDs = append(surfaceIDs, surfaceID)
	}
	sort.Strings(surfaceIDs)
	events := []eventcontract.Event{}
	updatedSurfaceIDs := make([]string, 0, len(surfaceIDs))
	for _, surfaceID := range surfaceIDs {
		recovery := a.surfaceResumeRuntime.recovery[surfaceID]
		if recovery == nil {
			continue
		}
		if !surfaceResumeRecoveryDue(recovery, now) {
			continue
		}
		if recovery.Entry.ResumeHeadless && a.shouldDeferHeadlessResumeUntilInitialRefreshLocked(recovery.Entry, allowMissingTargetFailure) {
			continue
		}
		restoreEvents, result := a.service.TryAutoResumeHeadlessSurface(surfaceID, orchestrator.SurfaceResumeAttempt{
			InstanceID:       recovery.Entry.ResumeInstanceID,
			ThreadID:         recovery.Entry.ResumeThreadID,
			ThreadTitle:      recovery.Entry.ResumeThreadTitle,
			ThreadCWD:        recovery.Entry.ResumeThreadCWD,
			WorkspaceKey:     recovery.Entry.ResumeWorkspaceKey,
			Backend:          agentproto.Backend(recovery.Entry.Backend),
			PrepareNewThread: strings.TrimSpace(recovery.Entry.ResumeRouteMode) == string(state.RouteModeNewThreadReady),
			ResumeHeadless:   recovery.Entry.ResumeHeadless,
		}, allowMissingTargetFailure)
		switch result.Status {
		case orchestrator.SurfaceResumeStatusStarting:
			a.clearSurfaceResumeAttemptProgressLocked(surfaceID)
			events = append(events, restoreEvents...)
			updatedSurfaceIDs = append(updatedSurfaceIDs, surfaceID)
		case orchestrator.SurfaceResumeStatusThreadAttached, orchestrator.SurfaceResumeStatusWorkspaceAttached:
			a.clearSurfaceResumeBackoffLocked(surfaceID)
			events = append(events, restoreEvents...)
			updatedSurfaceIDs = append(updatedSurfaceIDs, surfaceID)
		case orchestrator.SurfaceResumeStatusFailed:
			displayCode, emit := a.recordSurfaceResumeFailureLocked(surfaceID, result.FailureCode, now)
			restoreEvents = rewriteHeadlessRestoreFailureEvents(restoreEvents, displayCode, emit)
			events = append(events, restoreEvents...)
			if recovery.Entry.ResumeHeadless {
				continue
			}
			if !emit {
				continue
			}
			notice := orchestrator.NoticeForSurfaceResumeFailure(displayCode)
			if notice != nil {
				events = append(events, eventcontract.Event{
					Kind:             eventcontract.KindNotice,
					SurfaceSessionID: surfaceID,
					Notice:           notice,
				})
			}
		}
	}
	a.syncSurfaceResumeStateForSurfacesLocked(updatedSurfaceIDs, nil)
	a.syncClaudeWorkspaceProfileStateLocked()
	return events
}

func (a *App) maybeRecoverVSCodeSurfacesLocked(now time.Time) []eventcontract.Event {
	if len(a.surfaceResumeRuntime.recovery) == 0 || !a.hasDueVSCodeSurfaceRecoveryLocked(now) {
		return nil
	}
	surfaceIDs := make([]string, 0, len(a.surfaceResumeRuntime.recovery))
	for surfaceID := range a.surfaceResumeRuntime.recovery {
		surfaceIDs = append(surfaceIDs, surfaceID)
	}
	sort.Strings(surfaceIDs)
	events := []eventcontract.Event{}
	updatedSurfaceIDs := make([]string, 0, len(surfaceIDs))
	for _, surfaceID := range surfaceIDs {
		recovery := a.surfaceResumeRuntime.recovery[surfaceID]
		if recovery == nil || !state.IsVSCodeProductMode(state.ProductMode(recovery.Entry.ProductMode)) {
			continue
		}
		if !surfaceResumeRecoveryDue(recovery, now) {
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
				events = append(events, eventcontract.Event{
					Kind:             eventcontract.KindNotice,
					SurfaceSessionID: surfaceID,
					Notice:           notice,
				})
			}
		}
	}
	a.syncSurfaceResumeStateForSurfacesLocked(updatedSurfaceIDs, nil)
	a.syncClaudeWorkspaceProfileStateLocked()
	return events
}

func (a *App) maybePromptDetachedVSCodeSurfacesLocked() []eventcontract.Event {
	if a.surfaceResumeRuntime.store == nil || !a.surfaceResumeRuntime.vscodeDetachedPromptScanDue {
		return nil
	}
	a.surfaceResumeRuntime.vscodeDetachedPromptScanDue = false
	entries := a.surfaceResumeRuntime.store.Entries()
	if len(entries) == 0 {
		return nil
	}
	surfaceIDs := make([]string, 0, len(entries))
	for surfaceID := range entries {
		surfaceIDs = append(surfaceIDs, surfaceID)
	}
	sort.Strings(surfaceIDs)
	events := make([]eventcontract.Event, 0, len(surfaceIDs))
	for _, surfaceID := range surfaceIDs {
		entry := entries[surfaceID]
		if !state.IsVSCodeProductMode(state.ProductMode(entry.ProductMode)) {
			continue
		}
		if !entryPredatesDaemonStart(a.daemonStartedAt, entry.UpdatedAt) {
			continue
		}
		if a.surfaceResumeRuntime.vscodeResumeNotices[strings.TrimSpace(surfaceID)] {
			continue
		}
		snapshot := a.service.SurfaceSnapshot(surfaceID)
		if snapshot == nil || !state.IsVSCodeProductMode(state.ProductMode(snapshot.ProductMode)) {
			continue
		}
		if strings.TrimSpace(snapshot.Attachment.InstanceID) != "" || strings.TrimSpace(snapshot.PendingHeadless.InstanceID) != "" {
			continue
		}
		a.surfaceResumeRuntime.vscodeResumeNotices[strings.TrimSpace(surfaceID)] = true
		events = append(events, eventcontract.Event{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surfaceID,
			Notice:           orchestrator.NoticeForVSCodeOpenPrompt(strings.TrimSpace(entry.ResumeInstanceID) != ""),
		})
	}
	return events
}

func (a *App) syncVSCodeResumeNoticeStateLocked(entries map[string]surfaceresume.Entry) {
	if a.surfaceResumeRuntime.vscodeResumeNotices == nil {
		a.surfaceResumeRuntime.vscodeResumeNotices = map[string]bool{}
	}
	if entries == nil {
		entries = map[string]surfaceresume.Entry{}
		if a.surfaceResumeRuntime.store != nil {
			entries = a.surfaceResumeRuntime.store.Entries()
		}
	}
	for surfaceID := range a.surfaceResumeRuntime.vscodeResumeNotices {
		entry, ok := entries[surfaceID]
		if !ok || !state.IsVSCodeProductMode(state.ProductMode(entry.ProductMode)) {
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
	recovery.StickyFailureCode = ""
	recovery.LastNoticeCode = ""
}

func (a *App) clearSurfaceResumeAttemptProgressLocked(surfaceID string) {
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

func surfaceResumeFailureSpecificity(code string) int {
	switch strings.TrimSpace(code) {
	case "headless_restore_provider_unavailable",
		"headless_restore_claude_profile_unavailable":
		return 3
	case "headless_restore_runtime_unavailable":
		return 2
	case "headless_restore_start_failed",
		"headless_restore_start_timeout":
		return 1
	default:
		return 0
	}
}

func shouldUpgradeSurfaceResumeStickyFailure(current, next string) bool {
	return surfaceResumeFailureSpecificity(next) > surfaceResumeFailureSpecificity(current)
}

func (a *App) recordSurfaceResumeFailureLocked(surfaceID, code string, now time.Time) (string, bool) {
	recovery := a.surfaceResumeRuntime.recovery[strings.TrimSpace(surfaceID)]
	if recovery == nil {
		return strings.TrimSpace(code), false
	}
	code = strings.TrimSpace(code)
	recovery.LastAttemptAt = now
	recovery.NextAttemptAt = now.Add(surfaceResumeRetryBackoff)
	recovery.LastFailureCode = code
	if shouldUpgradeSurfaceResumeStickyFailure(recovery.StickyFailureCode, code) {
		recovery.StickyFailureCode = code
	}
	displayCode := strings.TrimSpace(firstNonEmpty(recovery.StickyFailureCode, code))
	if displayCode == "" {
		return "", false
	}
	if recovery.LastNoticeCode == "" {
		recovery.LastNoticeCode = displayCode
		return displayCode, true
	}
	if displayCode == recovery.LastNoticeCode {
		return displayCode, false
	}
	if recovery.StickyFailureCode != "" {
		recovery.LastNoticeCode = displayCode
		return displayCode, true
	}
	return displayCode, false
}

func rewriteHeadlessRestoreFailureEvents(events []eventcontract.Event, displayCode string, emit bool) []eventcontract.Event {
	if !emit {
		return nil
	}
	displayCode = strings.TrimSpace(displayCode)
	if displayCode == "" {
		return events
	}
	rewritten := make([]eventcontract.Event, 0, len(events))
	for _, event := range events {
		if event.Kind == eventcontract.KindNotice && event.Notice != nil {
			if notice := orchestrator.NoticeForHeadlessRestoreFailure(displayCode); notice != nil {
				event.Notice = notice
			}
		}
		rewritten = append(rewritten, event)
	}
	return rewritten
}

func (a *App) shouldDeferHeadlessResumeUntilInitialRefreshLocked(entry surfaceresume.Entry, allowMissingTargetFailure bool) bool {
	if allowMissingTargetFailure {
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
	// falling back to a managed headless restart for the same persisted target.
	return strings.TrimSpace(inst.Source) != "headless"
}

func (a *App) recordManagedHeadlessResumeOutcomeEventsLocked(events []eventcontract.Event, now time.Time) {
	for _, event := range events {
		if event.Notice == nil {
			continue
		}
		switch strings.TrimSpace(event.Notice.Code) {
		case "headless_restore_attached":
			a.clearSurfaceResumeBackoffLocked(event.SurfaceSessionID)
		case "headless_restore_thread_busy",
			"headless_restore_thread_not_found",
			"headless_restore_thread_cwd_missing",
			"headless_restore_provider_unavailable",
			"headless_restore_claude_profile_unavailable",
			"headless_restore_runtime_unavailable",
			"headless_restore_start_failed",
			"headless_restore_start_timeout":
			a.recordSurfaceResumeFailureLocked(event.SurfaceSessionID, event.Notice.Code, now)
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
	a.surfaceResumeRuntime.startupRefreshSeen = true
	delete(a.surfaceResumeRuntime.startupRefreshPending, strings.TrimSpace(instanceID))
}

func (a *App) initialThreadsRefreshRoundCompleteLocked() bool {
	return a.surfaceResumeRuntime.startupRefreshSeen && len(a.surfaceResumeRuntime.startupRefreshPending) == 0
}
