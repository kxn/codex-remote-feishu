package orchestrator

import (
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	frontstagecontract "github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

const (
	defaultTargetPickerTTL          = 10 * time.Minute
	targetPickerNewThreadValue      = "new_thread"
	targetPickerWorktreeCreateValue = "worktree_create"
	targetPickerThreadPrefix        = "thread:"
	targetPickerAutoSession         = "__auto__"
)

type targetPickerOpenOptions struct {
	PreferredWorkspaceKey string
	BackValue             map[string]any
	SourceMessageID       string
	Inline                bool
	LockedWorkspaceKey    string
	AllowNewThread        bool
	CatalogFamilyID       string
	CatalogVariantID      string
	CatalogBackend        agentproto.Backend
}

func (s *Service) openTargetPicker(surface *state.SurfaceConsoleRecord, source control.TargetPickerRequestSource, preferredWorkspaceKey string, backValue map[string]any, sourceMessageID string, inline bool) []eventcontract.Event {
	return s.openTargetPickerWithOptions(surface, source, targetPickerOpenOptions{
		PreferredWorkspaceKey: preferredWorkspaceKey,
		BackValue:             cloneTargetPickerActionPayload(backValue),
		SourceMessageID:       sourceMessageID,
		Inline:                inline,
	})
}

func (s *Service) openTargetPickerForAction(surface *state.SurfaceConsoleRecord, action control.Action, preferredWorkspaceKey string, backValue map[string]any, sourceMessageID string, inline bool) []eventcontract.Event {
	source := control.TargetPickerRequestSourceList
	if flow, ok := control.ResolveFeishuWorkspaceSessionFlowFromAction(action); ok && flow.TargetPicker != "" {
		source = flow.TargetPicker
	}
	return s.openTargetPickerWithSourceForAction(surface, source, action, preferredWorkspaceKey, backValue, sourceMessageID, inline)
}

func (s *Service) openTargetPickerWithSourceForAction(surface *state.SurfaceConsoleRecord, source control.TargetPickerRequestSource, action control.Action, preferredWorkspaceKey string, backValue map[string]any, sourceMessageID string, inline bool) []eventcontract.Event {
	familyID, variantID, backend := s.catalogProvenanceForAction(surface, action)
	return s.openTargetPickerWithOptions(surface, source, targetPickerOpenOptions{
		PreferredWorkspaceKey: preferredWorkspaceKey,
		BackValue:             cloneTargetPickerActionPayload(backValue),
		SourceMessageID:       sourceMessageID,
		Inline:                inline,
		CatalogFamilyID:       familyID,
		CatalogVariantID:      variantID,
		CatalogBackend:        backend,
	})
}

func (s *Service) openTargetPickerWithOptions(surface *state.SurfaceConsoleRecord, source control.TargetPickerRequestSource, opts targetPickerOpenOptions) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	if !s.surfaceIsHeadless(surface) {
		return nil
	}
	var record *activeTargetPickerRecord
	return s.openPickerRuntime(
		surface,
		func() error {
			s.clearThreadHistoryRuntime(surface)
			s.clearTargetPickerRuntime(surface)
			s.clearWorkspacePageRuntime(surface)
			next, err := s.newTargetPickerRecord(surface, source, opts)
			if err != nil {
				return err
			}
			flow := newOwnerCardFlowRecord(ownerCardFlowKindTargetPicker, next.PickerID, xutil.FirstNonEmpty(surface.ActorUserID), s.now(), defaultTargetPickerTTL, ownerCardFlowPhaseEditing)
			if opts.Inline {
				flow.MessageID = strings.TrimSpace(opts.SourceMessageID)
			}
			s.setActiveOwnerCardFlow(surface, flow)
			s.setActiveTargetPicker(surface, next)
			record = next
			return nil
		},
		func() {
			s.clearTargetPickerRuntime(surface)
		},
		func(err error) []eventcontract.Event {
			return notice(surface, "target_picker_unavailable", err.Error())
		},
		func() (eventcontract.Event, error) {
			return s.buildTargetPickerEvent(surface, record, opts.Inline)
		},
		func(err error) []eventcontract.Event {
			return notice(surface, "target_picker_unavailable", err.Error())
		},
	)
}

func (s *Service) openLockedWorkspaceTargetPicker(surface *state.SurfaceConsoleRecord, workspaceKey string, allowNewThread bool) []eventcontract.Event {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if surface == nil || workspaceKey == "" {
		return nil
	}
	return s.openTargetPickerWithOptions(surface, control.TargetPickerRequestSourceWorkspace, targetPickerOpenOptions{
		PreferredWorkspaceKey: workspaceKey,
		LockedWorkspaceKey:    workspaceKey,
		AllowNewThread:        allowNewThread,
	})
}

func (s *Service) buildTargetPickerEvent(surface *state.SurfaceConsoleRecord, record *activeTargetPickerRecord, inline bool) (eventcontract.Event, error) {
	view, err := s.buildTargetPickerView(surface, record)
	if err != nil {
		return eventcontract.Event{}, err
	}
	return s.targetPickerViewEvent(surface, view, inline), nil
}

func (s *Service) newTargetPickerRecord(surface *state.SurfaceConsoleRecord, source control.TargetPickerRequestSource, opts targetPickerOpenOptions) (*activeTargetPickerRecord, error) {
	if surface == nil {
		return nil, fmt.Errorf("目标选择器不可用")
	}
	preferredWorkspaceKey := normalizeWorkspaceClaimKey(xutil.FirstNonEmpty(opts.PreferredWorkspaceKey, s.surfaceCurrentWorkspaceKey(surface)))
	lockedWorkspaceKey := normalizeWorkspaceClaimKey(opts.LockedWorkspaceKey)
	if source == control.TargetPickerRequestSourceWorkspace && lockedWorkspaceKey == "" {
		lockedWorkspaceKey = preferredWorkspaceKey
	}
	allowNewThread := opts.AllowNewThread
	if !allowNewThread {
		allowNewThread = targetPickerSourceDefaultsToAllowNewThread(source)
	}
	selectedWorkspaceKey := preferredWorkspaceKey
	if lockedWorkspaceKey != "" {
		selectedWorkspaceKey = lockedWorkspaceKey
	}
	expiresAt := s.now().Add(defaultTargetPickerTTL)
	return &activeTargetPickerRecord{
		PickerID:             s.pickers.nextTargetPickerToken(),
		OwnerUserID:          strings.TrimSpace(xutil.FirstNonEmpty(surface.ActorUserID)),
		Source:               source,
		CatalogFamilyID:      strings.TrimSpace(opts.CatalogFamilyID),
		CatalogVariantID:     strings.TrimSpace(opts.CatalogVariantID),
		CatalogBackend:       agentproto.NormalizeBackend(opts.CatalogBackend),
		Stage:                control.FeishuTargetPickerStageEditing,
		Page:                 targetPickerDefaultPage(source),
		BackValue:            cloneTargetPickerActionPayload(opts.BackValue),
		LockedWorkspaceKey:   lockedWorkspaceKey,
		AllowNewThread:       allowNewThread,
		WorkspaceCursor:      -1,
		SessionCursor:        -1,
		SelectedWorkspaceKey: selectedWorkspaceKey,
		SelectedSessionValue: targetPickerAutoSession,
		CreatedAt:            s.now(),
		ExpiresAt:            expiresAt,
	}, nil
}

func (s *Service) handleTargetPickerSelectWorkspace(surface *state.SurfaceConsoleRecord, pickerID, workspaceKey, actorUserID string, answers map[string][]string) []eventcontract.Event {
	record, blocked := s.requireActiveTargetPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	resetTargetPickerEditingState(record)
	s.applyTargetPickerDraftAnswers(record, answers)
	lockedWorkspaceKey := normalizeTargetPickerWorkspaceSelection(record.LockedWorkspaceKey)
	requestedWorkspaceKey := normalizeTargetPickerWorkspaceSelection(workspaceKey)
	if lockedWorkspaceKey != "" {
		record.SelectedWorkspaceKey = lockedWorkspaceKey
		record.SelectedSessionValue = ""
		if requestedWorkspaceKey != "" && requestedWorkspaceKey != lockedWorkspaceKey {
			setTargetPickerMessages(record, control.FeishuTargetPickerMessage{
				Level: control.FeishuTargetPickerMessageWarning,
				Text:  "当前工作区已锁定，不能在这里切换到其他工作区。",
			})
		}
		return mutatePickerAndRebuild(
			nil,
			func() (eventcontract.Event, error) {
				return s.buildTargetPickerEvent(surface, record, true)
			},
			func(err error) []eventcontract.Event {
				return notice(surface, "target_picker_unavailable", err.Error())
			},
		)
	}
	return mutatePickerAndRebuild(
		func() {
			record.SelectedWorkspaceKey = normalizeTargetPickerWorkspaceSelection(workspaceKey)
			record.SessionCursor = 0
			if record.Source == control.TargetPickerRequestSourceList {
				record.SelectedSessionValue = targetPickerAutoSession
			} else {
				record.SelectedSessionValue = ""
			}
		},
		func() (eventcontract.Event, error) {
			return s.buildTargetPickerEvent(surface, record, true)
		},
		func(err error) []eventcontract.Event {
			return notice(surface, "target_picker_unavailable", err.Error())
		},
	)
}

func (s *Service) handleTargetPickerSelectSession(surface *state.SurfaceConsoleRecord, pickerID, value, actorUserID string, answers map[string][]string) []eventcontract.Event {
	record, blocked := s.requireActiveTargetPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	resetTargetPickerEditingState(record)
	s.applyTargetPickerDraftAnswers(record, answers)
	return mutatePickerAndRebuild(
		func() {
			record.SelectedSessionValue = strings.TrimSpace(value)
		},
		func() (eventcontract.Event, error) {
			return s.buildTargetPickerEvent(surface, record, true)
		},
		func(err error) []eventcontract.Event {
			return notice(surface, "target_picker_unavailable", err.Error())
		},
	)
}

func (s *Service) handleTargetPickerPage(surface *state.SurfaceConsoleRecord, pickerID, fieldName string, cursor int, actorUserID string, answers map[string][]string) []eventcontract.Event {
	record, blocked := s.requireActiveTargetPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	resetTargetPickerEditingState(record)
	s.applyTargetPickerDraftAnswers(record, answers)
	switch strings.TrimSpace(fieldName) {
	case frontstagecontract.CardTargetPickerWorkspaceFieldName:
		if normalizeTargetPickerWorkspaceSelection(record.LockedWorkspaceKey) != "" {
			view, err := s.buildTargetPickerView(surface, record)
			if err != nil {
				return notice(surface, "target_picker_unavailable", err.Error())
			}
			return []eventcontract.Event{s.targetPickerViewEvent(surface, view, true)}
		}
		options := targetPickerWorkspaceOptions(s.targetPickerWorkspaceEntriesForRecord(surface, record))
		record.WorkspaceCursor = normalizeTargetPickerDropdownCursor(cursor, len(options))
		record.SelectedWorkspaceKey = targetPickerWorkspaceValueAtCursor(options, record.WorkspaceCursor)
		record.SessionCursor = -1
		record.SelectedSessionValue = targetPickerAutoSession
	case frontstagecontract.CardTargetPickerSessionFieldName:
		workspaceEntries := s.targetPickerWorkspaceEntriesForRecord(surface, record)
		entry, _ := targetPickerWorkspaceEntryByKey(workspaceEntries, record.SelectedWorkspaceKey)
		options := s.targetPickerSessionOptions(surface, entry, record.Source, record.AllowNewThread)
		record.SessionCursor = normalizeTargetPickerDropdownCursor(cursor, len(options))
		record.SelectedSessionValue = ""
	default:
		return notice(surface, "target_picker_invalid_page_action", "当前翻页动作无效，请重新打开目标选择器。")
	}
	return mutatePickerAndRebuild(
		nil,
		func() (eventcontract.Event, error) {
			return s.buildTargetPickerEvent(surface, record, true)
		},
		func(err error) []eventcontract.Event {
			return notice(surface, "target_picker_unavailable", err.Error())
		},
	)
}

func (s *Service) handleTargetPickerCancel(surface *state.SurfaceConsoleRecord, pickerID, actorUserID string) []eventcontract.Event {
	flow, record, blocked := s.requireActiveTargetPickerFlow(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	if record.Stage == control.FeishuTargetPickerStageProcessing {
		switch record.PendingKind {
		case targetPickerPendingGitImport:
			appendEvents := s.cancelTargetPickerGitImport(surface, record)
			status := targetPickerGitImportCancelledStatus(record.PendingWorkspaceKey)
			return s.finishTargetPickerWithStageAndSections(surface, flow, record, control.FeishuTargetPickerStageCancelled, "已取消导入", "", status.Sections, status.Footer, false, appendEvents)
		case targetPickerPendingWorktreeCreate:
			appendEvents := s.cancelTargetPickerWorktreeCreate(surface, record)
			status := targetPickerWorktreeCreateCancelledStatus(record.PendingWorkspaceKey)
			return s.finishTargetPickerWithStageAndSections(surface, flow, record, control.FeishuTargetPickerStageCancelled, "已取消创建", "", status.Sections, status.Footer, false, appendEvents)
		}
	}
	return s.finishTargetPickerWithStage(surface, flow, record, control.FeishuTargetPickerStageCancelled, "已取消", "当前选择流程已结束，工作目标保持不变。", true, nil)
}

func (s *Service) handleTargetPickerBack(surface *state.SurfaceConsoleRecord, pickerID, actorUserID string) []eventcontract.Event {
	record, blocked := s.requireActiveTargetPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	resetTargetPickerEditingState(record)
	if record.PageOverride == "" {
		view, err := s.buildTargetPickerView(surface, record)
		if err != nil {
			return notice(surface, "target_picker_unavailable", err.Error())
		}
		return []eventcontract.Event{s.targetPickerViewEvent(surface, view, true)}
	}
	record.PageOverride = ""
	record.SelectedSessionValue = targetPickerAutoSession
	record.SessionCursor = -1
	record.WorktreeBranchName = ""
	record.WorktreeDirectoryName = ""
	record.WorktreeFinalPath = ""
	view, err := s.buildTargetPickerView(surface, record)
	if err != nil {
		return notice(surface, "target_picker_unavailable", err.Error())
	}
	return []eventcontract.Event{s.targetPickerViewEvent(surface, view, false)}
}

func (s *Service) handleTargetPickerConfirm(surface *state.SurfaceConsoleRecord, pickerID, actorUserID, workspaceKey, sessionValue string, answers map[string][]string) []eventcontract.Event {
	flow, record, blocked := s.requireActiveTargetPickerFlow(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	s.applyTargetPickerDraftAnswers(record, answers)
	requestedWorkspaceKey := normalizeTargetPickerWorkspaceSelection(record.SelectedWorkspaceKey)
	requestedSessionValue := strings.TrimSpace(record.SelectedSessionValue)
	lockedWorkspaceKey := normalizeTargetPickerWorkspaceSelection(record.LockedWorkspaceKey)
	if key := normalizeTargetPickerWorkspaceSelection(workspaceKey); key != "" {
		if lockedWorkspaceKey != "" && key != lockedWorkspaceKey {
			setTargetPickerMessages(record, control.FeishuTargetPickerMessage{
				Level: control.FeishuTargetPickerMessageWarning,
				Text:  "当前工作区已锁定，请在当前工作区内重新确认会话。",
			})
			view, err := s.buildTargetPickerView(surface, record)
			if err != nil {
				return notice(surface, "target_picker_unavailable", err.Error())
			}
			return []eventcontract.Event{s.targetPickerViewEvent(surface, view, false)}
		}
		record.SelectedWorkspaceKey = key
		requestedWorkspaceKey = key
	}
	if lockedWorkspaceKey != "" {
		record.SelectedWorkspaceKey = lockedWorkspaceKey
		requestedWorkspaceKey = lockedWorkspaceKey
	}
	if strings.TrimSpace(sessionValue) != "" {
		record.SelectedSessionValue = strings.TrimSpace(sessionValue)
		requestedSessionValue = strings.TrimSpace(sessionValue)
	}
	view, err := s.buildTargetPickerView(surface, record)
	if err != nil {
		return notice(surface, "target_picker_unavailable", err.Error())
	}
	if (requestedWorkspaceKey != "" && view.SelectedWorkspaceKey != requestedWorkspaceKey) ||
		(requestedSessionValue != "" && view.SelectedSessionValue != requestedSessionValue) {
		setTargetPickerMessages(record, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageWarning,
			Text:  "可选目标刚刚发生变化，请在最新卡片上重新确认。",
		})
		view, err = s.buildTargetPickerView(surface, record)
		if err != nil {
			return notice(surface, "target_picker_unavailable", err.Error())
		}
		return []eventcontract.Event{s.targetPickerViewEvent(surface, view, false)}
	}
	if !view.CanConfirm {
		if view.ConfirmValidatesOnSubmit {
			return s.dispatchTargetPickerConfirmed(surface, flow, record, view)
		}
		message := "请选择工作区和会话后再确认。"
		switch view.Page {
		case control.FeishuTargetPickerPageLocalDirectory:
			localState := s.buildTargetPickerLocalDirectoryState(surface, record)
			message = targetPickerFirstBlockingMessage(localState.Messages)
			if message == "" {
				message = "请先选择一个可接入的本地目录。"
			}
		case control.FeishuTargetPickerPageGit:
			gitState := s.buildTargetPickerGitImportState(record)
			message = targetPickerGitImportValidationMessage(record, gitState.Messages)
		case control.FeishuTargetPickerPageWorktree:
			worktreeState := s.buildTargetPickerWorktreeState(record)
			message = targetPickerWorktreeValidationMessage(record, worktreeState.Messages)
		}
		setTargetPickerMessages(record, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  message,
		})
		view, err = s.buildTargetPickerView(surface, record)
		if err != nil {
			return notice(surface, "target_picker_unavailable", err.Error())
		}
		return []eventcontract.Event{s.targetPickerViewEvent(surface, view, false)}
	}
	return s.dispatchTargetPickerConfirmed(surface, flow, record, view)
}

func (s *Service) dispatchTargetPickerConfirmed(surface *state.SurfaceConsoleRecord, flow *activeOwnerCardFlowRecord, record *activeTargetPickerRecord, view control.FeishuTargetPickerView) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	switch view.Page {
	case control.FeishuTargetPickerPageLocalDirectory:
		return s.confirmTargetPickerLocalDirectory(surface, flow, record, view)
	case control.FeishuTargetPickerPageGit:
		return s.confirmTargetPickerGitImport(surface, flow, record, view)
	case control.FeishuTargetPickerPageWorktree:
		return s.confirmTargetPickerWorktree(surface, flow, record, view)
	}
	workspaceKey := normalizeTargetPickerWorkspaceSelection(view.SelectedWorkspaceKey)
	sessionValue := strings.TrimSpace(view.SelectedSessionValue)
	if workspaceKey == "" || sessionValue == "" {
		return notice(surface, "target_picker_selection_missing", "请选择工作区和会话后再确认。")
	}
	kind, threadID := parseTargetPickerSessionValue(sessionValue)
	var events []eventcontract.Event
	succeeded := false
	switch kind {
	case control.FeishuTargetPickerSessionThread:
		events = s.useThreadPreservingTargetPicker(surface, threadID, true)
		succeeded = targetPickerThreadReady(surface, threadID)
	case control.FeishuTargetPickerSessionNewThread:
		events = s.enterTargetPickerNewThread(surface, workspaceKey)
		succeeded = targetPickerNewThreadReady(surface, workspaceKey)
	case control.FeishuTargetPickerSessionWorktree:
		record.PageOverride = control.FeishuTargetPickerPageWorktree
		record.SelectedWorkspaceKey = workspaceKey
		record.SelectedSessionValue = ""
		record.SessionCursor = -1
		record.WorktreeBranchName = ""
		record.WorktreeDirectoryName = ""
		record.WorktreeFinalPath = ""
		view, err := s.buildTargetPickerView(surface, record)
		if err != nil {
			return notice(surface, "target_picker_unavailable", err.Error())
		}
		return []eventcontract.Event{s.targetPickerViewEvent(surface, view, false)}
	default:
		return notice(surface, "target_picker_selection_missing", "当前选择的目标无效，请重新选择。")
	}
	if succeeded {
		filtered := targetPickerFilteredFollowupEvents(events)
		title := "已切换会话"
		text := "当前工作目标已经切换完成。"
		if kind == control.FeishuTargetPickerSessionNewThread {
			title = "已进入新会话待命"
			text = "当前工作目标已经准备完成，下一条文本会直接开启新会话。"
		}
		result := s.finishTargetPickerWithStage(surface, flow, record, control.FeishuTargetPickerStageSucceeded, title, text, false, filtered)
		// Replay any pending text input that was saved when the user first
		// sent a message in unbound state.
		if pending := s.takePendingTextInput(surface); pending != nil {
			result = append(result, s.replayPendingTextInput(surface, pending)...)
		}
		return result
	}
	if kind == control.FeishuTargetPickerSessionThread && surface.PendingHeadless != nil && strings.TrimSpace(surface.PendingHeadless.ThreadID) == threadID {
		filtered := targetPickerFilteredFollowupEvents(events)
		status := targetPickerSwitchProcessingStatus(view.SelectedWorkspaceLabel, view.SelectedSessionLabel)
		processing := s.startTargetPickerProcessingWithSections(
			surface,
			flow,
			record,
			targetPickerPendingUseThread,
			workspaceKey,
			threadID,
			"正在切换工作区 / 会话",
			"",
			status.Sections,
			status.Footer,
		)
		return append(processing, filtered...)
	}
	if kind == control.FeishuTargetPickerSessionNewThread && surface.PendingHeadless != nil && surface.PendingHeadless.PrepareNewThread &&
		normalizeWorkspaceClaimKey(xutil.FirstNonEmpty(surface.PendingHeadless.WorkspaceKey, surface.PendingHeadless.ThreadCWD)) == workspaceKey {
		filtered := targetPickerFilteredFollowupEvents(events)
		status := targetPickerSwitchProcessingStatus(view.SelectedWorkspaceLabel, "新会话")
		processing := s.startTargetPickerProcessingWithSections(
			surface,
			flow,
			record,
			targetPickerPendingNewThread,
			workspaceKey,
			"",
			"正在准备新会话",
			"",
			status.Sections,
			status.Footer,
		)
		return append(processing, filtered...)
	}
	filtered := targetPickerFilteredFollowupEvents(events)
	failureText := strings.TrimSpace(xutil.FirstNonEmpty(targetPickerFirstNoticeText(events), "当前工作目标切换失败，请重新发送 /list、/use 或 /useall 再试一次。"))
	return s.finishTargetPickerWithStage(surface, flow, record, control.FeishuTargetPickerStageFailed, "切换失败", failureText, false, filtered)
}

func (s *Service) enterTargetPickerNewThread(surface *state.SurfaceConsoleRecord, workspaceKey string) []eventcontract.Event {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if workspaceKey == "" {
		return notice(surface, "workspace_not_found", "目标工作区不存在，请重新发送 /list。")
	}
	if !s.surfaceIsHeadless(surface) {
		return notice(surface, "new_thread_disabled_vscode", "当前处于 vscode 模式，不能在这里直接新建会话。")
	}
	if currentWorkspace := s.surfaceCurrentWorkspaceKey(surface); currentWorkspace == workspaceKey && strings.TrimSpace(surface.AttachedInstanceID) != "" {
		return s.prepareNewThreadPreservingTargetPicker(surface)
	}
	targetBackend := s.surfaceBackend(surface)
	continuation := s.buildHeadlessWorkspaceContinuation(surface, workspaceKey, targetBackend, true)
	resolution := s.resolveWorkspaceContract(surface, workspaceKey, targetBackend)
	return s.executeResolvedWorkspaceContinuation(surface, continuation, resolution, attachWorkspaceOptions{
		PrepareNewThread: true,
		OverlayCleanup:   surfaceOverlayRouteCleanupOptions{PreserveTargetPicker: true},
	})
}

func targetPickerNewThreadSucceeded(surface *state.SurfaceConsoleRecord, workspaceKey string) bool {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if surface == nil || workspaceKey == "" {
		return false
	}
	return (surface.RouteMode == state.RouteModeNewThreadReady && normalizeWorkspaceClaimKey(surface.PreparedThreadCWD) == workspaceKey) ||
		(surface.PendingHeadless != nil && normalizeWorkspaceClaimKey(xutil.FirstNonEmpty(surface.PendingHeadless.WorkspaceKey, surface.PendingHeadless.ThreadCWD)) == workspaceKey && surface.PendingHeadless.PrepareNewThread)
}

func (s *Service) requireActiveTargetPicker(surface *state.SurfaceConsoleRecord, pickerID, actorUserID string) (*activeTargetPickerRecord, []eventcontract.Event) {
	_, record, blocked := s.requireActiveTargetPickerFlow(surface, pickerID, actorUserID)
	if blocked != nil {
		return nil, blocked
	}
	return record, nil
}

func (s *Service) buildTargetPickerView(surface *state.SurfaceConsoleRecord, record *activeTargetPickerRecord) (control.FeishuTargetPickerView, error) {
	if surface == nil || record == nil {
		return control.FeishuTargetPickerView{}, fmt.Errorf("目标选择器不存在")
	}
	stage := record.Stage
	if stage == "" {
		stage = control.FeishuTargetPickerStageEditing
	}
	page := targetPickerDefaultPage(record.Source)
	if record.PageOverride != "" {
		page = record.PageOverride
	}
	record.Page = page
	workspaceEntries := s.targetPickerWorkspaceEntriesForRecord(surface, record)

	lockedWorkspaceKey := normalizeTargetPickerWorkspaceSelection(record.LockedWorkspaceKey)
	workspaceSelectionLocked := lockedWorkspaceKey != ""
	workspaceOptions := targetPickerWorkspaceOptions(workspaceEntries)
	selectedWorkspace := normalizeTargetPickerWorkspaceSelection(record.SelectedWorkspaceKey)
	if workspaceSelectionLocked {
		selectedWorkspace = lockedWorkspaceKey
	} else {
		if !targetPickerHasWorkspaceOption(workspaceOptions, selectedWorkspace) {
			selectedWorkspace = normalizeWorkspaceClaimKey(s.surfaceCurrentWorkspaceKey(surface))
		}
		if !targetPickerHasWorkspaceOption(workspaceOptions, selectedWorkspace) {
			selectedWorkspace = targetPickerDefaultWorkspaceSelection(workspaceOptions)
		}
	}
	record.SelectedWorkspaceKey = selectedWorkspace

	sessionOptions := []control.FeishuTargetPickerSessionOption(nil)
	selectedWorkspaceEntry, _ := targetPickerWorkspaceEntryByKey(workspaceEntries, selectedWorkspace)
	usesSessionSelection := page == control.FeishuTargetPickerPageTarget && targetPickerUsesSessionSelection(record.Source)
	if usesSessionSelection {
		sessionOptions = s.targetPickerSessionOptions(surface, selectedWorkspaceEntry, record.Source, record.AllowNewThread)
	}
	selectedSession := strings.TrimSpace(record.SelectedSessionValue)
	if usesSessionSelection {
		switch {
		case selectedSession == targetPickerAutoSession:
			selectedSession = s.defaultTargetPickerSessionValue(surface, record.Source, selectedWorkspace, sessionOptions)
		case selectedSession == "":
			if targetPickerShouldAutoSelectNewThread(sessionOptions, record.AllowNewThread) {
				selectedSession = targetPickerNewThreadValue
			}
		case !targetPickerHasSessionOption(sessionOptions, selectedSession):
			if targetPickerShouldAutoSelectNewThread(sessionOptions, record.AllowNewThread) {
				selectedSession = targetPickerNewThreadValue
			} else {
				selectedSession = ""
			}
		}
	} else {
		selectedSession = ""
	}
	record.SelectedSessionValue = selectedSession
	workspaceCursor := 0
	if !workspaceSelectionLocked {
		workspaceCursor = record.WorkspaceCursor
		if workspaceCursor < 0 {
			workspaceCursor = targetPickerWorkspaceOptionIndex(workspaceOptions, selectedWorkspace)
		}
		workspaceCursor = normalizeTargetPickerDropdownCursor(workspaceCursor, len(workspaceOptions))
	}
	record.WorkspaceCursor = workspaceCursor
	sessionCursor := record.SessionCursor
	if sessionCursor < 0 {
		sessionCursor = targetPickerSessionOptionIndex(sessionOptions, selectedSession)
	}
	sessionCursor = normalizeTargetPickerDropdownCursor(sessionCursor, len(sessionOptions))
	record.SessionCursor = sessionCursor

	selectedWorkspaceLabel, selectedWorkspaceMeta := targetPickerSelectedWorkspaceSummary(workspaceOptions, selectedWorkspace)
	if workspaceSelectionLocked && selectedWorkspace != "" && strings.TrimSpace(selectedWorkspaceLabel) == "" {
		selectedWorkspaceLabel, selectedWorkspaceMeta = targetPickerLockedWorkspaceSummary(workspaceEntries, selectedWorkspace)
	}
	selectedSessionLabel, selectedSessionMeta := targetPickerSelectedSessionSummary(sessionOptions, selectedSession)
	localDirectoryPath := strings.TrimSpace(record.LocalDirectoryPath)
	localDirectoryName := strings.TrimSpace(record.LocalDirectoryName)
	localDirectoryFinalPath := ""
	localDirectoryChecked := false
	gitParentDir := strings.TrimSpace(record.GitParentDir)
	gitRepoURL := strings.TrimSpace(record.GitRepoURL)
	gitDirectoryName := strings.TrimSpace(record.GitDirectoryName)
	gitFinalPath := strings.TrimSpace(record.GitFinalPath)
	hint := ""
	messages := append([]control.FeishuTargetPickerMessage(nil), record.Messages...)
	sourceMessages := []control.FeishuTargetPickerMessage(nil)
	if targetPickerRequiresWorkspaceSelection(record.Source) && !workspaceSelectionLocked && len(workspaceOptions) == 0 {
		text := "当前还没有可切换的工作区，请先从目录或 GIT URL 新建。"
		if page == control.FeishuTargetPickerPageWorktree || record.Source == control.TargetPickerRequestSourceWorktree {
			text = "当前还没有可用的 Git 工作区，请先接入一个目录或导入一个仓库。"
		}
		messages = append(messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageWarning,
			Text:  text,
		})
	}
	if targetPickerUsesSessionSelection(record.Source) && workspaceSelectionLocked && targetPickerOnlyNewThreadSessionOption(sessionOptions) {
		messages = append(messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageInfo,
			Text:  "当前工作区没有可恢复会话，可直接新建会话。",
		})
	}
	confirmLabel := "确认切换"
	confirmValidatesOnSubmit := false
	canConfirm := false
	backValue := cloneTargetPickerActionPayload(record.BackValue)
	internalBack := stage == control.FeishuTargetPickerStageEditing && record.PageOverride != "" && page != targetPickerDefaultPage(record.Source)
	if internalBack {
		backValue = targetPickerInternalBackPayload(record.PickerID)
	}
	canGoBack := stage == control.FeishuTargetPickerStageEditing && len(backValue) != 0
	backLabel := ""
	if canGoBack {
		backLabel = "返回上一层"
		if internalBack {
			backLabel = "返回选择"
		}
	}
	switch page {
	case control.FeishuTargetPickerPageTarget:
		canConfirm = selectedWorkspace != "" && selectedSession != ""
		if selectedSession == targetPickerWorktreeCreateValue {
			confirmLabel = "继续创建"
		} else if selectedSession == targetPickerNewThreadValue {
			confirmLabel = "新建会话"
		} else {
			confirmLabel = "切换"
		}
	case control.FeishuTargetPickerPageLocalDirectory:
		localState := s.buildTargetPickerLocalDirectoryState(surface, record)
		if strings.TrimSpace(localState.ResolvedPath) != "" {
			localDirectoryPath = strings.TrimSpace(localState.ResolvedPath)
		}
		localDirectoryFinalPath = strings.TrimSpace(localState.FinalPath)
		localDirectoryChecked = localState.Checked
		sourceMessages = append(sourceMessages, localState.Messages...)
		canConfirm = localState.CanConfirm
		confirmValidatesOnSubmit = true
		if localState.Checked {
			if strings.TrimSpace(localDirectoryName) != "" {
				confirmLabel = "创建并继续"
			} else {
				confirmLabel = "接入并继续"
			}
		} else {
			confirmLabel = "检查目标目录"
		}
	case control.FeishuTargetPickerPageGit:
		gitState := s.buildTargetPickerGitImportState(record)
		if strings.TrimSpace(gitState.ParentDir) != "" {
			gitParentDir = strings.TrimSpace(gitState.ParentDir)
		}
		gitFinalPath = strings.TrimSpace(xutil.FirstNonEmpty(record.GitFinalPath, gitState.FinalPath))
		sourceMessages = append(sourceMessages, gitState.Messages...)
		confirmValidatesOnSubmit = true
		canConfirm = gitState.CanConfirm
		confirmLabel = "克隆并继续"
	case control.FeishuTargetPickerPageWorktree:
		worktreeState := s.buildTargetPickerWorktreeState(record)
		worktreeFinalPath := strings.TrimSpace(xutil.FirstNonEmpty(record.WorktreeFinalPath, worktreeState.FinalPath))
		sourceMessages = append(sourceMessages, worktreeState.Messages...)
		confirmValidatesOnSubmit = true
		canConfirm = worktreeState.CanConfirm
		confirmLabel = "创建并进入"
		record.WorktreeFinalPath = worktreeFinalPath
	default:
		canConfirm = selectedWorkspace != "" && selectedSession != ""
	}
	record.GitFinalPath = gitFinalPath
	showWorkspaceSelect := page == control.FeishuTargetPickerPageTarget && !workspaceSelectionLocked
	showSessionSelect := page == control.FeishuTargetPickerPageTarget
	canCancelProcessing := stage == control.FeishuTargetPickerStageProcessing &&
		(record.PendingKind == targetPickerPendingGitImport || record.PendingKind == targetPickerPendingWorktreeCreate)
	processingCancelLabel := ""
	if canCancelProcessing {
		if record.PendingKind == targetPickerPendingWorktreeCreate {
			processingCancelLabel = "取消创建"
		} else {
			processingCancelLabel = "取消导入"
		}
	}
	bodySections := targetPickerBodySections(
		page,
		selectedWorkspaceLabel,
		selectedWorkspaceMeta,
		selectedSessionLabel,
		selectedSessionMeta,
		localDirectoryPath,
		record.GitRepoURL,
		gitParentDir,
		gitFinalPath,
		strings.TrimSpace(record.WorktreeBranchName),
		strings.TrimSpace(record.WorktreeFinalPath),
	)
	noticeSections := targetPickerStatusNoticeSections(record)
	return control.NormalizeFeishuTargetPickerView(control.FeishuTargetPickerView{
		PickerID:                 record.PickerID,
		Title:                    targetPickerTitle(record.Source),
		Source:                   record.Source,
		CatalogFamilyID:          strings.TrimSpace(record.CatalogFamilyID),
		CatalogVariantID:         strings.TrimSpace(record.CatalogVariantID),
		CatalogBackend:           agentproto.NormalizeBackend(record.CatalogBackend),
		Stage:                    stage,
		Page:                     page,
		StageLabel:               targetPickerViewStageLabel(record, page),
		Question:                 targetPickerViewQuestion(record, page),
		BodySections:             bodySections,
		NoticeSections:           noticeSections,
		StatusTitle:              strings.TrimSpace(record.StatusTitle),
		StatusText:               strings.TrimSpace(record.StatusText),
		StatusSections:           cloneFeishuCardSections(record.StatusSections),
		StatusFooter:             strings.TrimSpace(record.StatusFooter),
		CanCancelProcessing:      canCancelProcessing,
		ProcessingCancelLabel:    processingCancelLabel,
		CanGoBack:                canGoBack,
		BackLabel:                backLabel,
		BackValue:                backValue,
		ShowWorkspaceSelect:      showWorkspaceSelect,
		ShowSessionSelect:        showSessionSelect,
		WorkspaceSelectionLocked: workspaceSelectionLocked,
		LockedWorkspaceKey:       lockedWorkspaceKey,
		AllowNewThread:           record.AllowNewThread,
		WorkspacePlaceholder:     "选择工作区",
		SessionPlaceholder:       "选择会话",
		WorkspaceCursor:          workspaceCursor,
		SessionCursor:            sessionCursor,
		SelectedWorkspaceKey:     selectedWorkspace,
		SelectedSessionValue:     selectedSession,
		SelectedWorkspaceLabel:   selectedWorkspaceLabel,
		SelectedWorkspaceMeta:    selectedWorkspaceMeta,
		SelectedSessionLabel:     selectedSessionLabel,
		SelectedSessionMeta:      selectedSessionMeta,
		ConfirmLabel:             confirmLabel,
		ConfirmValidatesOnSubmit: confirmValidatesOnSubmit,
		CanConfirm:               canConfirm,
		Hint:                     hint,
		WorkspaceOptions:         workspaceOptions,
		SessionOptions:           sessionOptions,
		LocalDirectoryPath:       localDirectoryPath,
		LocalDirectoryName:       localDirectoryName,
		LocalDirectoryFinalPath:  localDirectoryFinalPath,
		LocalDirectoryChecked:    localDirectoryChecked,
		GitParentDir:             gitParentDir,
		GitRepoURL:               gitRepoURL,
		GitDirectoryName:         gitDirectoryName,
		GitFinalPath:             gitFinalPath,
		WorktreeBranchName:       strings.TrimSpace(record.WorktreeBranchName),
		WorktreeDirectoryName:    strings.TrimSpace(record.WorktreeDirectoryName),
		WorktreeFinalPath:        strings.TrimSpace(record.WorktreeFinalPath),
		Messages:                 messages,
		SourceMessages:           sourceMessages,
	}), nil
}

func cloneTargetPickerActionPayload(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(value))
	for key, current := range value {
		cloned[key] = current
	}
	return cloned
}

func targetPickerInternalBackPayload(pickerID string) map[string]any {
	pickerID = strings.TrimSpace(pickerID)
	if pickerID == "" {
		return nil
	}
	return map[string]any{
		frontstagecontract.CardActionPayloadKeyKind:     frontstagecontract.CardActionKindTargetPickerBack,
		frontstagecontract.CardActionPayloadKeyPickerID: pickerID,
	}
}

func (s *Service) catalogProvenanceForAction(surface *state.SurfaceConsoleRecord, action control.Action) (string, string, agentproto.Backend) {
	action = s.resolveCatalogActionFromSurfaceContext(surface, action)
	return strings.TrimSpace(action.CatalogFamilyID), strings.TrimSpace(action.CatalogVariantID), agentproto.NormalizeBackend(action.CatalogBackend)
}
