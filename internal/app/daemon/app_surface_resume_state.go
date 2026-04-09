package daemon

import (
	"log"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type surfaceResumeTarget struct {
	ResumeInstanceID   string
	ResumeThreadID     string
	ResumeWorkspaceKey string
	ResumeRouteMode    string
}

func (a *App) configureSurfaceResumeStateLocked(stateDir string) {
	path := surfaceResumeStatePath(stateDir)
	store, err := loadSurfaceResumeStore(path)
	if err != nil {
		log.Printf("load surface resume state failed: path=%s err=%v", path, err)
		store = &surfaceResumeStore{
			path:    path,
			entries: map[string]SurfaceResumeEntry{},
		}
	}
	a.surfaceResumeState = store
	a.materializeSurfaceResumeStateLocked()
}

func (a *App) SurfaceResumeState(surfaceID string) *SurfaceResumeEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.surfaceResumeState == nil {
		return nil
	}
	entry, ok := a.surfaceResumeState.Get(surfaceID)
	if !ok {
		return nil
	}
	copy := entry
	return &copy
}

func (a *App) materializeSurfaceResumeStateLocked() {
	if a.surfaceResumeState == nil {
		return
	}
	entries := a.surfaceResumeState.Entries()
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
		)
	}
}

func (a *App) syncSurfaceResumeStateLocked(clearTargets map[string]bool) {
	if a.surfaceResumeState == nil {
		return
	}
	existing := a.surfaceResumeState.Entries()
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
		if current, ok := a.surfaceResumeState.Get(entry.SurfaceSessionID); ok && sameSurfaceResumeEntryContent(current, entry) {
			continue
		}
		entry.UpdatedAt = now
		if err := a.surfaceResumeState.Put(entry); err != nil {
			log.Printf("persist surface resume state failed: surface=%s err=%v", entry.SurfaceSessionID, err)
		}
	}
	for surfaceID := range existing {
		if _, ok := desired[surfaceID]; ok {
			continue
		}
		if err := a.surfaceResumeState.Delete(surfaceID); err != nil {
			log.Printf("clear surface resume state failed: surface=%s err=%v", surfaceID, err)
		}
	}
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
	}
	if entry.SurfaceSessionID == "" {
		return SurfaceResumeEntry{}, false
	}
	if !clearResumeTarget {
		if target, ok := a.currentSurfaceResumeTargetLocked(surface); ok {
			entry.ResumeInstanceID = target.ResumeInstanceID
			entry.ResumeThreadID = target.ResumeThreadID
			entry.ResumeWorkspaceKey = target.ResumeWorkspaceKey
			entry.ResumeRouteMode = target.ResumeRouteMode
		} else if previous, ok := a.surfaceResumeState.Get(entry.SurfaceSessionID); ok {
			entry.ResumeInstanceID = previous.ResumeInstanceID
			entry.ResumeThreadID = previous.ResumeThreadID
			entry.ResumeWorkspaceKey = previous.ResumeWorkspaceKey
			entry.ResumeRouteMode = previous.ResumeRouteMode
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
		return surfaceResumeTarget{
			ResumeInstanceID:   strings.TrimSpace(surface.AttachedInstanceID),
			ResumeThreadID:     strings.TrimSpace(surface.SelectedThreadID),
			ResumeWorkspaceKey: state.ResolveWorkspaceKey(workspaceKey, surface.ClaimedWorkspaceKey, surface.PreparedThreadCWD),
			ResumeRouteMode:    strings.TrimSpace(string(surface.RouteMode)),
		}, true
	}
	if pending := surface.PendingHeadless; pending != nil {
		return surfaceResumeTarget{
			ResumeThreadID:     strings.TrimSpace(pending.ThreadID),
			ResumeWorkspaceKey: state.ResolveWorkspaceKey(workspaceKey, pending.ThreadCWD),
			ResumeRouteMode:    string(state.RouteModePinned),
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
