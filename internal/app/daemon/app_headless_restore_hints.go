package daemon

import (
	"log"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func (a *App) configureHeadlessRestoreHintsLocked(stateDir string) {
	path := headlessRestoreHintsStatePath(stateDir)
	store, err := loadHeadlessRestoreHintStore(path)
	if err != nil {
		log.Printf("load headless restore hints failed: path=%s err=%v", path, err)
		store = &headlessRestoreHintStore{
			path:    path,
			entries: map[string]HeadlessRestoreHint{},
		}
	}
	a.headlessRestoreHints = store
	a.refreshHeadlessRestoreHintsLocked()
}

func (a *App) HeadlessRestoreHint(surfaceID string) *HeadlessRestoreHint {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.headlessRestoreHints == nil {
		return nil
	}
	hint, ok := a.headlessRestoreHints.Get(surfaceID)
	if !ok {
		return nil
	}
	copy := hint
	return &copy
}

func (a *App) refreshHeadlessRestoreHintsLocked() {
	if a.headlessRestoreHints == nil {
		return
	}
	for _, surface := range a.service.Surfaces() {
		if surface == nil {
			continue
		}
		hint, ok := a.currentHeadlessRestoreHintLocked(surface.SurfaceSessionID)
		if !ok {
			continue
		}
		a.upsertHeadlessRestoreHintLocked(hint)
	}
}

func (a *App) syncHeadlessRestoreHintAfterActionLocked(action control.Action, before *control.Snapshot) {
	if a.headlessRestoreHints == nil {
		return
	}
	hint, ok := a.currentHeadlessRestoreHintLocked(action.SurfaceSessionID)
	if ok {
		a.upsertHeadlessRestoreHintLocked(hint)
		return
	}
	if a.shouldClearHeadlessRestoreHintLocked(action, before) {
		a.clearHeadlessRestoreHintLocked(action.SurfaceSessionID)
	}
}

func (a *App) shouldClearHeadlessRestoreHintLocked(action control.Action, before *control.Snapshot) bool {
	switch action.Kind {
	case control.ActionDetach:
		return true
	case control.ActionKillInstance:
		return snapshotCarriesHeadlessRestoreTarget(before)
	case control.ActionAttachInstance, control.ActionUseThread:
		after := a.service.SurfaceSnapshot(action.SurfaceSessionID)
		if after == nil {
			return false
		}
		if snapshotHasPendingHeadlessTarget(after) {
			return false
		}
		return strings.TrimSpace(after.Attachment.InstanceID) != "" &&
			(!after.Attachment.Managed || !strings.EqualFold(strings.TrimSpace(after.Attachment.Source), "headless"))
	default:
		return false
	}
}

func (a *App) currentHeadlessRestoreHintLocked(surfaceID string) (HeadlessRestoreHint, bool) {
	snapshot := a.service.SurfaceSnapshot(surfaceID)
	if snapshot == nil {
		return HeadlessRestoreHint{}, false
	}
	hint := HeadlessRestoreHint{
		SurfaceSessionID: strings.TrimSpace(surfaceID),
		GatewayID:        strings.TrimSpace(a.service.SurfaceGatewayID(surfaceID)),
		ChatID:           strings.TrimSpace(a.service.SurfaceChatID(surfaceID)),
		ActorUserID:      strings.TrimSpace(a.service.SurfaceActorUserID(surfaceID)),
	}
	switch {
	case snapshotHasPendingHeadlessTarget(snapshot):
		hint.ThreadID = strings.TrimSpace(snapshot.PendingHeadless.ThreadID)
		hint.ThreadTitle = strings.TrimSpace(snapshot.PendingHeadless.ThreadTitle)
		hint.ThreadCWD = strings.TrimSpace(snapshot.PendingHeadless.ThreadCWD)
	case snapshotHasAttachedHeadlessTarget(snapshot):
		hint.ThreadID = strings.TrimSpace(snapshot.Attachment.SelectedThreadID)
		hint.ThreadTitle = strings.TrimSpace(snapshot.Attachment.SelectedThreadTitle)
		if hint.ThreadTitle == "" {
			hint.ThreadTitle = hint.ThreadID
		}
		if inst := a.service.Instance(snapshot.Attachment.InstanceID); inst != nil {
			if thread := inst.Threads[hint.ThreadID]; thread != nil {
				hint.ThreadCWD = strings.TrimSpace(thread.CWD)
				if hint.ThreadTitle == hint.ThreadID && strings.TrimSpace(thread.Name) != "" {
					hint.ThreadTitle = strings.TrimSpace(thread.Name)
				}
			}
		}
	default:
		return HeadlessRestoreHint{}, false
	}
	return hint, true
}

func (a *App) upsertHeadlessRestoreHintLocked(hint HeadlessRestoreHint) {
	if a.headlessRestoreHints == nil {
		return
	}
	if normalized, ok := normalizeHeadlessRestoreHint(hint); ok {
		if current, exists := a.headlessRestoreHints.Get(normalized.SurfaceSessionID); exists && sameHeadlessRestoreHintContent(current, normalized) {
			return
		}
		normalized.UpdatedAt = time.Now().UTC()
		if err := a.headlessRestoreHints.Put(normalized); err != nil {
			log.Printf("persist headless restore hint failed: surface=%s thread=%s err=%v", normalized.SurfaceSessionID, normalized.ThreadID, err)
		}
	}
}

func (a *App) clearHeadlessRestoreHintLocked(surfaceID string) {
	if a.headlessRestoreHints == nil {
		return
	}
	if _, ok := a.headlessRestoreHints.Get(surfaceID); !ok {
		return
	}
	if err := a.headlessRestoreHints.Delete(surfaceID); err != nil {
		log.Printf("clear headless restore hint failed: surface=%s err=%v", surfaceID, err)
	}
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

func snapshotHasPendingHeadlessTarget(snapshot *control.Snapshot) bool {
	return snapshot != nil &&
		strings.TrimSpace(snapshot.PendingHeadless.InstanceID) != "" &&
		strings.TrimSpace(snapshot.PendingHeadless.ThreadID) != ""
}

func snapshotHasAttachedHeadlessTarget(snapshot *control.Snapshot) bool {
	return snapshot != nil &&
		strings.TrimSpace(snapshot.Attachment.InstanceID) != "" &&
		snapshot.Attachment.Managed &&
		strings.EqualFold(strings.TrimSpace(snapshot.Attachment.Source), "headless") &&
		strings.TrimSpace(snapshot.Attachment.SelectedThreadID) != ""
}

func snapshotCarriesHeadlessRestoreTarget(snapshot *control.Snapshot) bool {
	if snapshot == nil {
		return false
	}
	if snapshotHasPendingHeadlessTarget(snapshot) {
		return true
	}
	return strings.TrimSpace(snapshot.Attachment.InstanceID) != "" &&
		snapshot.Attachment.Managed &&
		strings.EqualFold(strings.TrimSpace(snapshot.Attachment.Source), "headless")
}
