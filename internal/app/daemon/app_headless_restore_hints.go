package daemon

import (
	"log"
	"os"
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (a *App) migrateLegacyHeadlessRestoreHintsLocked(stateDir string) {
	path := headlessRestoreHintsStatePath(stateDir)
	store, err := loadHeadlessRestoreHintStore(path)
	if err != nil {
		log.Printf("load legacy headless restore hints failed: path=%s err=%v", path, err)
		return
	}
	entries := store.Entries()
	if len(entries) == 0 || a.surfaceResumeState == nil {
		return
	}

	surfaceIDs := make([]string, 0, len(entries))
	for surfaceID := range entries {
		surfaceIDs = append(surfaceIDs, surfaceID)
	}
	sort.Strings(surfaceIDs)

	imported := false
	keepLegacyFile := false
	for _, surfaceID := range surfaceIDs {
		hint := entries[surfaceID]
		if a.legacyHeadlessRestoreHintCoveredLocked(hint) {
			continue
		}
		if !a.importLegacyHeadlessRestoreHintLocked(hint) {
			keepLegacyFile = true
			log.Printf("keep unresolved legacy headless restore hint: surface=%s thread=%s", hint.SurfaceSessionID, hint.ThreadID)
			continue
		}
		imported = true
	}
	if imported {
		a.materializeSurfaceResumeStateLocked()
		a.syncVSCodeResumeNoticeStateLocked(nil)
		a.syncSurfaceResumeRecoveryStateLocked()
		a.syncHeadlessRestoreStateLocked()
		a.vscodeStartupCheckDue = storedVSCodeResumeExists(a.surfaceResumeState)
	}
	if keepLegacyFile {
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.Printf("remove migrated headless restore hints failed: path=%s err=%v", path, err)
	}
}

func (a *App) HeadlessRestoreHint(surfaceID string) *HeadlessRestoreHint {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.surfaceResumeState == nil {
		return nil
	}
	entry, ok := a.surfaceResumeState.Get(surfaceID)
	if !ok {
		return nil
	}
	hint, ok := headlessRestoreHintFromSurfaceResumeEntry(entry)
	if !ok {
		return nil
	}
	copy := hint
	return &copy
}

func (a *App) legacyHeadlessRestoreHintCoveredLocked(hint HeadlessRestoreHint) bool {
	if a.surfaceResumeState == nil {
		return false
	}
	hint, ok := normalizeHeadlessRestoreHint(hint)
	if !ok {
		return false
	}
	entry, ok := a.surfaceResumeState.Get(hint.SurfaceSessionID)
	if !ok {
		return false
	}
	derived, ok := headlessRestoreHintFromSurfaceResumeEntry(entry)
	return ok && sameHeadlessRestoreHintContent(derived, hint)
}

func (a *App) importLegacyHeadlessRestoreHintLocked(hint HeadlessRestoreHint) bool {
	if a.surfaceResumeState == nil {
		return false
	}
	hint, ok := normalizeHeadlessRestoreHint(hint)
	if !ok {
		return false
	}

	entry := SurfaceResumeEntry{
		SurfaceSessionID:   hint.SurfaceSessionID,
		GatewayID:          hint.GatewayID,
		ChatID:             hint.ChatID,
		ActorUserID:        hint.ActorUserID,
		ProductMode:        string(state.ProductModeNormal),
		ResumeThreadID:     hint.ThreadID,
		ResumeThreadTitle:  firstNonEmpty(hint.ThreadTitle, hint.ThreadID),
		ResumeThreadCWD:    state.NormalizeWorkspaceKey(hint.ThreadCWD),
		ResumeWorkspaceKey: state.ResolveWorkspaceKey(hint.ThreadCWD),
		ResumeRouteMode:    string(state.RouteModePinned),
		ResumeHeadless:     true,
		UpdatedAt:          hint.UpdatedAt,
	}

	if existing, ok := a.surfaceResumeState.Get(hint.SurfaceSessionID); ok {
		if state.NormalizeProductMode(state.ProductMode(existing.ProductMode)) == state.ProductModeVSCode {
			return false
		}
		entry = existing
		entry.GatewayID = firstNonEmpty(strings.TrimSpace(entry.GatewayID), hint.GatewayID)
		entry.ChatID = firstNonEmpty(strings.TrimSpace(entry.ChatID), hint.ChatID)
		entry.ActorUserID = firstNonEmpty(strings.TrimSpace(entry.ActorUserID), hint.ActorUserID)
		if strings.TrimSpace(entry.ProductMode) == "" {
			entry.ProductMode = string(state.ProductModeNormal)
		}
		if strings.TrimSpace(entry.ResumeThreadID) == "" {
			entry.ResumeThreadID = hint.ThreadID
		}
		if strings.TrimSpace(entry.ResumeThreadTitle) == "" {
			entry.ResumeThreadTitle = firstNonEmpty(hint.ThreadTitle, hint.ThreadID)
		}
		if state.NormalizeWorkspaceKey(entry.ResumeThreadCWD) == "" {
			entry.ResumeThreadCWD = state.NormalizeWorkspaceKey(hint.ThreadCWD)
		}
		if state.NormalizeWorkspaceKey(entry.ResumeWorkspaceKey) == "" {
			entry.ResumeWorkspaceKey = state.ResolveWorkspaceKey(entry.ResumeThreadCWD, hint.ThreadCWD)
		}
		if strings.TrimSpace(entry.ResumeRouteMode) == "" {
			entry.ResumeRouteMode = string(state.RouteModePinned)
		}
		entry.ResumeHeadless = true
		if entry.UpdatedAt.IsZero() {
			entry.UpdatedAt = hint.UpdatedAt
		}
	}

	normalized, ok := normalizeSurfaceResumeEntry(entry)
	if !ok {
		return false
	}
	derived, ok := headlessRestoreHintFromSurfaceResumeEntry(normalized)
	if !ok || !sameHeadlessRestoreHintContent(derived, hint) {
		return false
	}
	if current, ok := a.surfaceResumeState.Get(normalized.SurfaceSessionID); ok && sameSurfaceResumeEntryContent(current, normalized) {
		return true
	}
	if normalized.UpdatedAt.IsZero() {
		normalized.UpdatedAt = hint.UpdatedAt
	}
	if err := a.surfaceResumeState.Put(normalized); err != nil {
		log.Printf("migrate legacy headless restore hint failed: surface=%s thread=%s err=%v", normalized.SurfaceSessionID, normalized.ResumeThreadID, err)
		return false
	}
	return true
}

func sameHeadlessRestoreHintContent(left, right HeadlessRestoreHint) bool {
	return strings.TrimSpace(left.SurfaceSessionID) == strings.TrimSpace(right.SurfaceSessionID) &&
		strings.TrimSpace(left.GatewayID) == strings.TrimSpace(right.GatewayID) &&
		strings.TrimSpace(left.ChatID) == strings.TrimSpace(right.ChatID) &&
		strings.TrimSpace(left.ActorUserID) == strings.TrimSpace(right.ActorUserID) &&
		strings.TrimSpace(left.ThreadID) == strings.TrimSpace(right.ThreadID) &&
		strings.TrimSpace(left.ThreadTitle) == strings.TrimSpace(right.ThreadTitle) &&
		strings.TrimSpace(left.ThreadCWD) == strings.TrimSpace(right.ThreadCWD)
}

func headlessRestoreHintFromSurfaceResumeEntry(entry SurfaceResumeEntry) (HeadlessRestoreHint, bool) {
	if !entry.ResumeHeadless {
		return HeadlessRestoreHint{}, false
	}
	hint := HeadlessRestoreHint{
		SurfaceSessionID: strings.TrimSpace(entry.SurfaceSessionID),
		GatewayID:        strings.TrimSpace(entry.GatewayID),
		ChatID:           strings.TrimSpace(entry.ChatID),
		ActorUserID:      strings.TrimSpace(entry.ActorUserID),
		ThreadID:         strings.TrimSpace(entry.ResumeThreadID),
		ThreadTitle:      firstNonEmpty(strings.TrimSpace(entry.ResumeThreadTitle), strings.TrimSpace(entry.ResumeThreadID)),
		ThreadCWD:        state.NormalizeWorkspaceKey(entry.ResumeThreadCWD),
	}
	return normalizeHeadlessRestoreHint(hint)
}
