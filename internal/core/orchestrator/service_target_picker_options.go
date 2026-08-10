package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type targetPickerWorkspaceEntryMode int

const (
	targetPickerWorkspaceEntryModeAttach targetPickerWorkspaceEntryMode = iota
	targetPickerWorkspaceEntryModeList
	targetPickerWorkspaceEntryModeWorktreeBase
)

func (s *Service) targetPickerWorkspaceEntriesForRecord(surface *state.SurfaceConsoleRecord, record *activeTargetPickerRecord) []workspaceSelectionEntry {
	mode := targetPickerWorkspaceEntryModeAttach
	if record != nil {
		page := targetPickerDefaultPage(record.Source)
		if record.PageOverride != "" {
			page = record.PageOverride
		}
		switch {
		case page == control.FeishuTargetPickerPageWorktree || record.Source == control.TargetPickerRequestSourceWorktree:
			mode = targetPickerWorkspaceEntryModeWorktreeBase
		case record.Source == control.TargetPickerRequestSourceList:
			mode = targetPickerWorkspaceEntryModeList
		}
	}
	return s.targetPickerWorkspaceEntriesForMode(surface, mode)
}

func (s *Service) targetPickerWorkspaceEntriesForMode(surface *state.SurfaceConsoleRecord, mode targetPickerWorkspaceEntryMode) []workspaceSelectionEntry {
	grouped := map[string][]*state.InstanceRecord{}
	targetBackend, filterByBackend := s.normalModeThreadBackend(surface)
	for _, inst := range s.root.Instances {
		if inst == nil || !inst.Online {
			continue
		}
		if filterByBackend && state.EffectiveInstanceBackend(inst) != targetBackend {
			continue
		}
		for _, workspaceKey := range instanceWorkspaceSelectionKeys(inst) {
			grouped[workspaceKey] = append(grouped[workspaceKey], inst)
		}
	}
	views := s.mergedThreadViews(surface)
	visibleWorkspaces := s.normalModeListWorkspaceSetWithViews(surface, views)
	if len(visibleWorkspaces) == 0 {
		return nil
	}
	recoverableWorkspaces := map[string]time.Time{}
	recoverableWorkspaceSeen := map[string]bool{}
	for _, view := range views {
		workspaceKey := mergedThreadWorkspaceClaimKey(view)
		if workspaceKey == "" {
			continue
		}
		recoverableWorkspaceSeen[workspaceKey] = true
		usedAt := threadLastUsedAt(view)
		if current, ok := recoverableWorkspaces[workspaceKey]; !ok || usedAt.After(current) {
			recoverableWorkspaces[workspaceKey] = usedAt
		}
	}
	s.mergeWorkspaceSelectionRecencyFromOnlineThreads(surface, recoverableWorkspaces, recoverableWorkspaceSeen, visibleWorkspaces)
	s.mergeWorkspaceSelectionRecencyFromPersistedWorkspaces(surface, recoverableWorkspaces, recoverableWorkspaceSeen, visibleWorkspaces)

	entries := make([]workspaceSelectionEntry, 0, len(visibleWorkspaces))
	seenWorkspaceKeys := map[string]struct{}{}
	for workspaceKey := range visibleWorkspaces {
		workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
		if workspaceKey == "" {
			continue
		}
		if _, exists := seenWorkspaceKeys[workspaceKey]; exists {
			continue
		}
		seenWorkspaceKeys[workspaceKey] = struct{}{}
		instances := append([]*state.InstanceRecord(nil), grouped[workspaceKey]...)
		s.sortWorkspaceAttachInstances(surface, workspaceKey, instances)
		latestUsedAt := recoverableWorkspaces[workspaceKey]
		ageText := ""
		if !latestUsedAt.IsZero() {
			ageText = humanizeRelativeTime(s.now(), latestUsedAt)
		}
		hasVSCodeActivity := s.workspaceHasVSCodeActivity(instances)
		attachable := false
		recoverableOnly := len(instances) == 0 && recoverableWorkspaceSeen[workspaceKey]
		if filterByBackend {
			switch s.resolveWorkspaceContract(surface, workspaceKey, targetBackend).Mode {
			case contractResolutionAttachVisible, contractResolutionReuseManaged, contractResolutionRestartManaged:
				attachable = true
			}
		} else {
			attachable = s.resolveWorkspaceAttachInstanceFromCandidates(surface, workspaceKey, instances) != nil
		}
		gitInfo := gitmeta.WorkspaceInfo{}
		if !recoverableOnly || mode != targetPickerWorkspaceEntryModeAttach {
			gitInfo = inspectWorkspaceDisplayInfo(workspaceKey)
		}
		worktreeBase := s.config.GitAvailable && gitInfo.InRepo()
		busy := s.workspaceBusyOwnerForSurface(surface, workspaceKey) != nil
		switch mode {
		case targetPickerWorkspaceEntryModeWorktreeBase:
			if !worktreeBase {
				continue
			}
		case targetPickerWorkspaceEntryModeList:
			if busy && !worktreeBase {
				continue
			}
		default:
			if busy {
				continue
			}
		}
		entries = append(entries, workspaceSelectionEntry{
			workspaceKey:      workspaceKey,
			latestUsedAt:      latestUsedAt,
			label:             workspaceSelectionLabel(workspaceKey),
			gitInfo:           gitInfo,
			ageText:           ageText,
			hasVSCodeActivity: hasVSCodeActivity,
			busy:              busy,
			attachable:        attachable,
			worktreeBase:      worktreeBase,
			recoverableOnly:   recoverableOnly,
		})
	}
	sortWorkspaceSelectionEntries(entries)
	return entries
}

func (s *Service) targetPickerSessionOptions(surface *state.SurfaceConsoleRecord, entry workspaceSelectionEntry, source control.TargetPickerRequestSource, allowNewThread bool) []control.FeishuTargetPickerSessionOption {
	workspaceKey := normalizeWorkspaceClaimKey(entry.workspaceKey)
	if workspaceKey == "" {
		return nil
	}
	views := s.threadViewsVisibleInNormalList(surface, s.mergedThreadViews(surface))
	options := make([]control.FeishuTargetPickerSessionOption, 0, len(views)+1)
	canUseWorkspace := !entry.busy
	if canUseWorkspace && targetPickerAllowsNewThread(source, allowNewThread) && source == control.TargetPickerRequestSourceList {
		options = append(options, control.FeishuTargetPickerSessionOption{
			Value:    targetPickerNewThreadValue,
			Kind:     control.FeishuTargetPickerSessionNewThread,
			Label:    "新建会话",
			MetaText: "在这个工作区里开始一个新的会话",
		})
	}
	if canUseWorkspace {
		for _, view := range views {
			if mergedThreadWorkspaceClaimKey(view) != workspaceKey {
				continue
			}
			if !s.mergedThreadViewHasCompatibleVisibleInstance(surface, view) && strings.TrimSpace(threadCWD(view)) == "" {
				continue
			}
			target := s.resolveThreadTargetFromView(surface, view)
			if target.Mode == threadAttachUnavailable {
				if !s.mergedThreadViewHasCompatibleVisibleInstance(surface, view) {
					entry := s.threadSelectionViewEntry(surface, view, true)
					meta := targetPickerSessionMetaText(source, s.threadSelectionMetaText(surface, view, entry.Status))
					options = append(options, control.FeishuTargetPickerSessionOption{
						Value:    targetPickerThreadValue(view.ThreadID),
						Kind:     control.FeishuTargetPickerSessionThread,
						Label:    entry.Summary,
						MetaText: meta,
					})
				}
				continue
			}
			entry := s.threadSelectionViewEntry(surface, view, true)
			meta := targetPickerSessionMetaText(source, s.threadSelectionMetaText(surface, view, entry.Status))
			options = append(options, control.FeishuTargetPickerSessionOption{
				Value:    targetPickerThreadValue(view.ThreadID),
				Kind:     control.FeishuTargetPickerSessionThread,
				Label:    entry.Summary,
				MetaText: meta,
			})
		}
	}
	if source == control.TargetPickerRequestSourceList && entry.worktreeBase {
		options = append(options, control.FeishuTargetPickerSessionOption{
			Value:    targetPickerWorktreeCreateValue,
			Kind:     control.FeishuTargetPickerSessionWorktree,
			Label:    "从这个工作区创建 Worktree",
			MetaText: "基于这个 Git 工作区创建新的并行工作区",
		})
	}
	if canUseWorkspace && targetPickerAllowsNewThread(source, allowNewThread) && source != control.TargetPickerRequestSourceList {
		options = append(options, control.FeishuTargetPickerSessionOption{
			Value:    targetPickerNewThreadValue,
			Kind:     control.FeishuTargetPickerSessionNewThread,
			Label:    "新建会话",
			MetaText: "在这个工作区里开始一个新的会话",
		})
	}
	return options
}

func (s *Service) defaultTargetPickerSessionValue(surface *state.SurfaceConsoleRecord, source control.TargetPickerRequestSource, workspaceKey string, options []control.FeishuTargetPickerSessionOption) string {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if workspaceKey == "" {
		return ""
	}
	if source == control.TargetPickerRequestSourceList && targetPickerHasSessionOption(options, targetPickerNewThreadValue) {
		return targetPickerNewThreadValue
	}
	if source == control.TargetPickerRequestSourceList && len(options) == 1 && options[0].Kind == control.FeishuTargetPickerSessionWorktree {
		return options[0].Value
	}
	if s.surfaceCurrentWorkspaceKey(surface) != workspaceKey {
		return ""
	}
	if surface != nil && surface.RouteMode == state.RouteModeNewThreadReady {
		if targetPickerHasSessionOption(options, targetPickerNewThreadValue) {
			return targetPickerNewThreadValue
		}
		return ""
	}
	if surface != nil && strings.TrimSpace(surface.SelectedThreadID) != "" {
		value := targetPickerThreadValue(surface.SelectedThreadID)
		if targetPickerHasSessionOption(options, value) {
			return value
		}
	}
	if targetPickerOnlyNewThreadSessionOption(options) {
		return targetPickerNewThreadValue
	}
	return ""
}
