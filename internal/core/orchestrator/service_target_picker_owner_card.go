package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type targetPickerPendingKind string

const (
	targetPickerPendingNone      targetPickerPendingKind = ""
	targetPickerPendingUseThread targetPickerPendingKind = "use_thread"
	targetPickerPendingNewThread targetPickerPendingKind = "new_thread"
	targetPickerPendingGitImport targetPickerPendingKind = "git_import"
)

func (s *Service) clearTargetPickerRuntime(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	record := s.activePathPicker(surface)
	flow := s.activeOwnerCardFlow(surface)
	if record != nil {
		ownerFlowID := strings.TrimSpace(record.OwnerFlowID)
		if ownerFlowID == "" || (flow != nil && flow.Kind == ownerCardFlowKindTargetPicker && ownerFlowID == strings.TrimSpace(flow.FlowID)) {
			s.clearSurfacePathPicker(surface)
		}
	}
	s.clearSurfaceTargetPicker(surface)
	if flow != nil && flow.Kind == ownerCardFlowKindTargetPicker {
		s.clearSurfaceOwnerCardFlow(surface)
	}
}

func (s *Service) RecordTargetPickerMessage(surfaceID, pickerID, messageID string) {
	s.RecordOwnerCardFlowMessage(surfaceID, pickerID, messageID)
}

func (s *Service) requireActiveTargetPickerFlow(surface *state.SurfaceConsoleRecord, pickerID, actorUserID string) (*activeOwnerCardFlowRecord, *activeTargetPickerRecord, []control.UIEvent) {
	flow, blocked := s.requireActiveOwnerCardFlow(
		surface,
		ownerCardFlowKindTargetPicker,
		pickerID,
		actorUserID,
		"这个目标选择卡片已失效，请重新发送 /list、/use 或 /useall。",
		"这个目标选择卡片只允许发起者本人操作。",
	)
	if blocked != nil {
		return nil, nil, blocked
	}
	record := s.activeTargetPicker(surface)
	if record == nil || strings.TrimSpace(record.PickerID) != strings.TrimSpace(pickerID) {
		s.clearTargetPickerRuntime(surface)
		return nil, nil, notice(surface, "target_picker_expired", "这个目标选择卡片已失效，请重新发送 /list、/use 或 /useall。")
	}
	if !record.ExpiresAt.IsZero() && !record.ExpiresAt.After(s.now()) {
		s.clearTargetPickerRuntime(surface)
		return nil, nil, notice(surface, "target_picker_expired", "这个目标选择卡片已过期，请重新发送 /list、/use 或 /useall。")
	}
	return flow, record, nil
}

func resetTargetPickerEditingState(record *activeTargetPickerRecord) {
	if record == nil {
		return
	}
	record.Stage = control.FeishuTargetPickerStageEditing
	record.StatusTitle = ""
	record.StatusText = ""
	record.StatusSections = nil
	record.StatusFooter = ""
	record.Messages = nil
	record.PendingKind = targetPickerPendingNone
	record.PendingWorkspaceKey = ""
	record.PendingThreadID = ""
}

func setTargetPickerMessages(record *activeTargetPickerRecord, messages ...control.FeishuTargetPickerMessage) {
	if record == nil {
		return
	}
	record.Stage = control.FeishuTargetPickerStageEditing
	record.StatusTitle = ""
	record.StatusText = ""
	record.StatusSections = nil
	record.StatusFooter = ""
	record.Messages = append([]control.FeishuTargetPickerMessage(nil), messages...)
	record.PendingKind = targetPickerPendingNone
	record.PendingWorkspaceKey = ""
	record.PendingThreadID = ""
}

func (s *Service) startTargetPickerProcessing(
	surface *state.SurfaceConsoleRecord,
	flow *activeOwnerCardFlowRecord,
	record *activeTargetPickerRecord,
	pendingKind targetPickerPendingKind,
	workspaceKey, threadID, title, text string,
) []control.UIEvent {
	return s.startTargetPickerProcessingWithSections(surface, flow, record, pendingKind, workspaceKey, threadID, title, text, nil, "")
}

func (s *Service) startTargetPickerProcessingWithSections(
	surface *state.SurfaceConsoleRecord,
	flow *activeOwnerCardFlowRecord,
	record *activeTargetPickerRecord,
	pendingKind targetPickerPendingKind,
	workspaceKey, threadID, title, text string,
	sections []control.FeishuCardTextSection,
	footer string,
) []control.UIEvent {
	if flow == nil || record == nil {
		return nil
	}
	record.Stage = control.FeishuTargetPickerStageProcessing
	record.StatusTitle = strings.TrimSpace(title)
	record.StatusText = strings.TrimSpace(text)
	record.StatusSections = cloneFeishuCardSections(sections)
	record.StatusFooter = strings.TrimSpace(footer)
	record.Messages = nil
	record.PendingKind = pendingKind
	record.PendingWorkspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	record.PendingThreadID = strings.TrimSpace(threadID)
	refreshOwnerCardFlow(flow, ownerCardFlowPhaseRunning, s.now(), defaultTargetPickerTTL)
	view, err := s.buildTargetPickerView(surface, record)
	if err != nil {
		return notice(surface, "target_picker_unavailable", err.Error())
	}
	return []control.UIEvent{s.targetPickerViewEvent(surface, view, false)}
}

func (s *Service) finishTargetPickerWithStage(
	surface *state.SurfaceConsoleRecord,
	flow *activeOwnerCardFlowRecord,
	record *activeTargetPickerRecord,
	stage control.FeishuTargetPickerStage,
	title, text string,
	inline bool,
	appendEvents []control.UIEvent,
) []control.UIEvent {
	return s.finishTargetPickerWithStageAndSections(surface, flow, record, stage, title, text, nil, "", inline, appendEvents)
}

func (s *Service) finishTargetPickerWithStageAndSections(
	surface *state.SurfaceConsoleRecord,
	flow *activeOwnerCardFlowRecord,
	record *activeTargetPickerRecord,
	stage control.FeishuTargetPickerStage,
	title, text string,
	sections []control.FeishuCardTextSection,
	footer string,
	inline bool,
	appendEvents []control.UIEvent,
) []control.UIEvent {
	if record == nil {
		return append([]control.UIEvent(nil), appendEvents...)
	}
	record.Stage = stage
	record.StatusTitle = strings.TrimSpace(title)
	record.StatusText = strings.TrimSpace(text)
	record.StatusSections = cloneFeishuCardSections(sections)
	record.StatusFooter = strings.TrimSpace(footer)
	record.Messages = nil
	record.PendingKind = targetPickerPendingNone
	record.PendingWorkspaceKey = ""
	record.PendingThreadID = ""
	if flow != nil {
		switch stage {
		case control.FeishuTargetPickerStageSucceeded:
			refreshOwnerCardFlow(flow, ownerCardFlowPhaseCompleted, s.now(), defaultTargetPickerTTL)
		case control.FeishuTargetPickerStageCancelled:
			refreshOwnerCardFlow(flow, ownerCardFlowPhaseCancelled, s.now(), defaultTargetPickerTTL)
		case control.FeishuTargetPickerStageFailed:
			refreshOwnerCardFlow(flow, ownerCardFlowPhaseError, s.now(), defaultTargetPickerTTL)
		default:
			refreshOwnerCardFlow(flow, ownerCardFlowPhaseCompleted, s.now(), defaultTargetPickerTTL)
		}
	}
	view, err := s.buildTargetPickerView(surface, record)
	if err != nil {
		s.clearTargetPickerRuntime(surface)
		if len(appendEvents) != 0 {
			return append(notice(surface, "target_picker_unavailable", err.Error()), appendEvents...)
		}
		return notice(surface, "target_picker_unavailable", err.Error())
	}
	event := s.targetPickerViewEvent(surface, view, inline)
	s.clearTargetPickerRuntime(surface)
	return append([]control.UIEvent{event}, appendEvents...)
}

func targetPickerFilteredFollowupEvents(events []control.UIEvent) []control.UIEvent {
	if len(events) == 0 {
		return nil
	}
	filtered := make([]control.UIEvent, 0, len(events))
	for _, event := range events {
		switch event.Kind {
		case control.UIEventNotice, control.UIEventThreadSelectionChange:
			continue
		default:
			filtered = append(filtered, event)
		}
	}
	return filtered
}

func targetPickerFirstNoticeText(events []control.UIEvent) string {
	for _, event := range events {
		if event.Notice == nil {
			continue
		}
		if text := strings.TrimSpace(event.Notice.Text); text != "" {
			return text
		}
	}
	return ""
}

func cloneFeishuCardSections(sections []control.FeishuCardTextSection) []control.FeishuCardTextSection {
	if len(sections) == 0 {
		return nil
	}
	cloned := make([]control.FeishuCardTextSection, 0, len(sections))
	for _, section := range sections {
		normalized := section.Normalized()
		if normalized.Label == "" && len(normalized.Lines) == 0 {
			continue
		}
		cloned = append(cloned, normalized)
	}
	if len(cloned) == 0 {
		return nil
	}
	return cloned
}

func targetPickerThreadReady(surface *state.SurfaceConsoleRecord, threadID string) bool {
	if surface == nil || strings.TrimSpace(threadID) == "" {
		return false
	}
	return strings.TrimSpace(surface.SelectedThreadID) == strings.TrimSpace(threadID)
}

func targetPickerNewThreadReady(surface *state.SurfaceConsoleRecord, workspaceKey string) bool {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if surface == nil || workspaceKey == "" {
		return false
	}
	return surface.RouteMode == state.RouteModeNewThreadReady &&
		normalizeWorkspaceClaimKey(surface.PreparedThreadCWD) == workspaceKey
}

func targetPickerPendingStillRunning(surface *state.SurfaceConsoleRecord, record *activeTargetPickerRecord) bool {
	if surface == nil || record == nil || record.PendingKind == targetPickerPendingNone || surface.PendingHeadless == nil {
		return false
	}
	switch record.PendingKind {
	case targetPickerPendingUseThread:
		return strings.TrimSpace(record.PendingThreadID) != "" &&
			strings.TrimSpace(surface.PendingHeadless.ThreadID) == strings.TrimSpace(record.PendingThreadID)
	case targetPickerPendingNewThread:
		fallthrough
	case targetPickerPendingGitImport:
		return surface.PendingHeadless.PrepareNewThread &&
			normalizeWorkspaceClaimKey(surface.PendingHeadless.ThreadCWD) == normalizeWorkspaceClaimKey(record.PendingWorkspaceKey)
	default:
		return false
	}
}

func (s *Service) targetPickerHasBlockingProcessing(surface *state.SurfaceConsoleRecord) bool {
	record := s.activeTargetPicker(surface)
	if record == nil || record.Stage != control.FeishuTargetPickerStageProcessing {
		return false
	}
	return record.PendingKind == targetPickerPendingGitImport
}

func (s *Service) maybeFinalizePendingTargetPicker(surface *state.SurfaceConsoleRecord, events []control.UIEvent, fallbackFailureText string) []control.UIEvent {
	if surface == nil {
		return events
	}
	flow := s.activeOwnerCardFlow(surface)
	record := s.activeTargetPicker(surface)
	if flow == nil || flow.Kind != ownerCardFlowKindTargetPicker || record == nil || record.Stage != control.FeishuTargetPickerStageProcessing {
		return events
	}
	filtered := targetPickerFilteredFollowupEvents(events)
	switch record.PendingKind {
	case targetPickerPendingUseThread:
		if targetPickerThreadReady(surface, record.PendingThreadID) {
			return s.finishTargetPickerWithStage(surface, flow, record, control.FeishuTargetPickerStageSucceeded, "已切换会话", "当前工作目标已经切换完成。", false, filtered)
		}
	case targetPickerPendingNewThread:
		if targetPickerNewThreadReady(surface, record.PendingWorkspaceKey) {
			return s.finishTargetPickerWithStage(surface, flow, record, control.FeishuTargetPickerStageSucceeded, "已进入新会话待命", "当前工作目标已经准备完成，下一条文本会直接开启新会话。", false, filtered)
		}
	case targetPickerPendingGitImport:
		if targetPickerNewThreadReady(surface, record.PendingWorkspaceKey) {
			status := targetPickerGitImportSuccessStatus(record.PendingWorkspaceKey)
			return s.finishTargetPickerWithStageAndSections(surface, flow, record, control.FeishuTargetPickerStageSucceeded, "已进入新会话待命", "", status.Sections, status.Footer, false, filtered)
		}
	}
	failureText := strings.TrimSpace(firstNonEmpty(fallbackFailureText, targetPickerFirstNoticeText(events)))
	if failureText == "" && targetPickerPendingStillRunning(surface, record) {
		return filtered
	}
	if failureText == "" {
		failureText = "当前工作目标切换失败，请重新发送 /list、/use 或 /useall 再试一次。"
	}
	if record.PendingKind == targetPickerPendingGitImport {
		status := targetPickerGitImportPostCloneFailureStatus(record.PendingWorkspaceKey, failureText)
		return s.finishTargetPickerWithStageAndSections(surface, flow, record, control.FeishuTargetPickerStageFailed, "导入失败", "", status.Sections, status.Footer, false, filtered)
	}
	return s.finishTargetPickerWithStage(surface, flow, record, control.FeishuTargetPickerStageFailed, "切换失败", failureText, false, filtered)
}
