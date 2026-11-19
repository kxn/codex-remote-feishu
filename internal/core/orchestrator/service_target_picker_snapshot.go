package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type targetPickerOptionSnapshot struct {
	Workspaces          []workspaceSelectionEntry
	Sessions            map[string][]control.FeishuTargetPickerSessionOption
	DefaultSessions     map[string]string
	CurrentWorkspaceKey string
}

func (s *Service) targetPickerDraftSelectionError(surface *state.SurfaceConsoleRecord, record *activeTargetPickerRecord) []eventcontract.Event {
	event, err := s.buildTargetPickerEvent(surface, record, true)
	if err != nil {
		return notice(surface, "target_picker_unavailable", err.Error())
	}
	return []eventcontract.Event{event}
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
	currentWorkspaceKey := s.surfaceCurrentWorkspaceKey(surface)
	snapshot := &targetPickerOptionSnapshot{
		Workspaces:          append([]workspaceSelectionEntry(nil), entries...),
		Sessions:            make(map[string][]control.FeishuTargetPickerSessionOption, len(entries)),
		DefaultSessions:     make(map[string]string, len(entries)),
		CurrentWorkspaceKey: currentWorkspaceKey,
	}
	for _, entry := range entries {
		snapshot.Sessions[entry.workspaceKey] = s.targetPickerSessionOptionsFromViews(surface, entry, record.Source, record.AllowNewThread, views)
		snapshot.DefaultSessions[entry.workspaceKey] = targetPickerDefaultSessionValueFromRoute(surface, record.Source, entry.workspaceKey, currentWorkspaceKey, snapshot.Sessions[entry.workspaceKey])
	}
	record.OptionSnapshot = snapshot
}

func (s *Service) targetPickerCurrentWorkspaceKeyForRecord(surface *state.SurfaceConsoleRecord, record *activeTargetPickerRecord) string {
	if record.OptionSnapshot != nil && targetPickerEffectivePage(record) == control.FeishuTargetPickerPageTarget {
		return record.OptionSnapshot.CurrentWorkspaceKey
	}
	return s.surfaceCurrentWorkspaceKey(surface)
}

func (s *Service) targetPickerNavigationWorkspaceProjection(surface *state.SurfaceConsoleRecord) string {
	if record := s.activeTargetPicker(surface); record != nil && record.OptionSnapshot != nil {
		return record.OptionSnapshot.CurrentWorkspaceKey
	}
	// A stale/replayed callback may have no picker left. Admission still uses
	// the materialized canonical route identity, without repairing that route.
	workspaceKey := state.ResolveWorkspaceKey(surface.ClaimedWorkspaceKey, surface.PreparedThreadCWD)
	if inst := s.root.Instances[surface.AttachedInstanceID]; workspaceKey == "" && inst != nil {
		workspaceKey = state.ResolveWorkspaceKey(inst.WorkspaceKey, inst.WorkspaceRoot)
	}
	return workspaceKey
}

func (s *Service) targetPickerDefaultSessionValueForRecord(surface *state.SurfaceConsoleRecord, record *activeTargetPickerRecord, workspaceKey string, options []control.FeishuTargetPickerSessionOption) string {
	if record.OptionSnapshot != nil && targetPickerEffectivePage(record) == control.FeishuTargetPickerPageTarget {
		return record.OptionSnapshot.DefaultSessions[workspaceKey]
	}
	return s.defaultTargetPickerSessionValue(surface, record.Source, workspaceKey, options)
}

func (s *Service) targetPickerSessionOptionsForRecord(surface *state.SurfaceConsoleRecord, record *activeTargetPickerRecord, entry workspaceSelectionEntry) []control.FeishuTargetPickerSessionOption {
	if record.OptionSnapshot != nil && targetPickerEffectivePage(record) == control.FeishuTargetPickerPageTarget {
		return append([]control.FeishuTargetPickerSessionOption(nil), record.OptionSnapshot.Sessions[entry.workspaceKey]...)
	}
	return s.targetPickerSessionOptions(surface, entry, record.Source, record.AllowNewThread)
}

// IsTargetPickerDraftNavigation recognizes in-memory navigation or rejection of
// a stale owner-card callback. This classifies IO needs, not callback trust.
// Forms and final confirmation remain on the ordinary mutation path.
func (s *Service) IsTargetPickerDraftNavigation(action control.Action) bool {
	if s == nil || s.root == nil {
		return false
	}
	switch action.Kind {
	case control.ActionTargetPickerSelectWorkspace, control.ActionTargetPickerSelectSession, control.ActionTargetPickerPage:
	default:
		return false
	}
	surface := s.root.Surfaces[strings.TrimSpace(action.SurfaceSessionID)]
	if surface == nil {
		return false
	}
	record := s.activeTargetPicker(surface)
	if record == nil || record.PickerID != strings.TrimSpace(action.PickerID) {
		return true
	}
	return record.OptionSnapshot != nil && targetPickerEffectivePage(record) == control.FeishuTargetPickerPageTarget
}
