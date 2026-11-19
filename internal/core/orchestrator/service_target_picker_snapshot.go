package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type targetPickerOptionSnapshot struct {
	Workspaces []workspaceSelectionEntry
	Sessions   map[string][]control.FeishuTargetPickerSessionOption
}

func targetPickerEffectivePage(record *activeTargetPickerRecord) control.FeishuTargetPickerPage {
	if record.PageOverride != "" {
		return record.PageOverride
	}
	return targetPickerDefaultPage(record.Source)
}

func (s *Service) freezeTargetPickerOptions(surface *state.SurfaceConsoleRecord, record *activeTargetPickerRecord) {
	entries := s.targetPickerWorkspaceEntriesForRecord(surface, record)
	views := s.threadViewsVisibleInNormalList(surface, s.mergedThreadViews(surface))
	snapshot := &targetPickerOptionSnapshot{
		Workspaces: append([]workspaceSelectionEntry(nil), entries...),
		Sessions:   make(map[string][]control.FeishuTargetPickerSessionOption, len(entries)),
	}
	for _, entry := range entries {
		snapshot.Sessions[entry.workspaceKey] = s.targetPickerSessionOptionsFromViews(surface, entry, record.Source, record.AllowNewThread, views)
	}
	record.OptionSnapshot = snapshot
}

func (s *Service) targetPickerSessionOptionsForRecord(surface *state.SurfaceConsoleRecord, record *activeTargetPickerRecord, entry workspaceSelectionEntry) []control.FeishuTargetPickerSessionOption {
	if record.OptionSnapshot != nil && targetPickerEffectivePage(record) == control.FeishuTargetPickerPageTarget {
		return append([]control.FeishuTargetPickerSessionOption(nil), record.OptionSnapshot.Sessions[entry.workspaceKey]...)
	}
	return s.targetPickerSessionOptions(surface, entry, record.Source, record.AllowNewThread)
}

// IsTargetPickerDraftNavigation recognizes the bounded in-memory navigation
// episode. Forms and final confirmation remain on the ordinary mutation path.
func (s *Service) IsTargetPickerDraftNavigation(action control.Action) bool {
	switch action.Kind {
	case control.ActionTargetPickerSelectWorkspace, control.ActionTargetPickerSelectSession, control.ActionTargetPickerPage:
	default:
		return false
	}
	surface := s.root.Surfaces[strings.TrimSpace(action.SurfaceSessionID)]
	record := s.activeTargetPicker(surface)
	return record != nil && record.OptionSnapshot != nil && record.Stage == control.FeishuTargetPickerStageEditing &&
		record.PickerID == strings.TrimSpace(action.PickerID) && targetPickerEffectivePage(record) == control.FeishuTargetPickerPageTarget
}
