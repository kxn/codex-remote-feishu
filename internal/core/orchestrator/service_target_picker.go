package orchestrator

import (
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	defaultTargetPickerTTL           = 10 * time.Minute
	targetPickerCreateWorkspaceValue = "__create_workspace__"
	targetPickerNewThreadValue       = "new_thread"
	targetPickerThreadPrefix         = "thread:"
	targetPickerAutoSession          = "__auto__"
)

func (s *Service) openTargetPicker(surface *state.SurfaceConsoleRecord, source control.TargetPickerRequestSource, preferredWorkspaceKey string, inline bool) []control.UIEvent {
	if surface == nil {
		return nil
	}
	if s.normalizeSurfaceProductMode(surface) != state.ProductModeNormal {
		return nil
	}
	record, err := s.newTargetPickerRecord(surface, source, preferredWorkspaceKey)
	if err != nil {
		return notice(surface, "target_picker_unavailable", err.Error())
	}
	s.setActiveTargetPicker(surface, record)
	view, err := s.buildTargetPickerView(surface, record)
	if err != nil {
		s.clearSurfaceTargetPicker(surface)
		return notice(surface, "target_picker_unavailable", err.Error())
	}
	return []control.UIEvent{s.targetPickerViewEvent(surface, view, inline)}
}

func (s *Service) newTargetPickerRecord(surface *state.SurfaceConsoleRecord, source control.TargetPickerRequestSource, preferredWorkspaceKey string) (*activeTargetPickerRecord, error) {
	if surface == nil {
		return nil, fmt.Errorf("目标选择器不可用")
	}
	preferredWorkspaceKey = normalizeWorkspaceClaimKey(firstNonEmpty(preferredWorkspaceKey, s.surfaceCurrentWorkspaceKey(surface)))
	expiresAt := s.now().Add(defaultTargetPickerTTL)
	return &activeTargetPickerRecord{
		PickerID:             s.nextTargetPickerToken(),
		OwnerUserID:          strings.TrimSpace(firstNonEmpty(surface.ActorUserID)),
		Source:               source,
		SelectedMode:         targetPickerDefaultMode(source),
		SelectedSource:       control.FeishuTargetPickerSourceLocalDirectory,
		SelectedWorkspaceKey: preferredWorkspaceKey,
		SelectedSessionValue: targetPickerAutoSession,
		CreatedAt:            s.now(),
		ExpiresAt:            expiresAt,
	}, nil
}

func (s *Service) nextTargetPickerToken() string {
	s.nextTargetPickerID++
	return fmt.Sprintf("target-picker-%d", s.nextTargetPickerID)
}

func (s *Service) handleTargetPickerSelectMode(surface *state.SurfaceConsoleRecord, pickerID, value, actorUserID string, answers map[string][]string) []control.UIEvent {
	record, blocked := s.requireActiveTargetPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	s.applyTargetPickerDraftAnswers(record, answers)
	mode := normalizeTargetPickerMode(value)
	if mode == "" {
		return notice(surface, "target_picker_selection_missing", "当前选择的模式无效，请重新选择。")
	}
	record.SelectedMode = mode
	if mode == control.FeishuTargetPickerModeExistingWorkspace {
		record.SelectedSessionValue = targetPickerAutoSession
	}
	view, err := s.buildTargetPickerView(surface, record)
	if err != nil {
		return notice(surface, "target_picker_unavailable", err.Error())
	}
	return []control.UIEvent{s.targetPickerViewEvent(surface, view, true)}
}

func (s *Service) handleTargetPickerSelectSource(surface *state.SurfaceConsoleRecord, pickerID, value, actorUserID string, answers map[string][]string) []control.UIEvent {
	record, blocked := s.requireActiveTargetPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	s.applyTargetPickerDraftAnswers(record, answers)
	sourceKind := normalizeTargetPickerSourceKind(value)
	if sourceKind == "" {
		return notice(surface, "target_picker_selection_missing", "当前选择的工作区来源无效，请重新选择。")
	}
	record.SelectedSource = sourceKind
	view, err := s.buildTargetPickerView(surface, record)
	if err != nil {
		return notice(surface, "target_picker_unavailable", err.Error())
	}
	return []control.UIEvent{s.targetPickerViewEvent(surface, view, true)}
}

func (s *Service) handleTargetPickerSelectWorkspace(surface *state.SurfaceConsoleRecord, pickerID, workspaceKey, actorUserID string, answers map[string][]string) []control.UIEvent {
	record, blocked := s.requireActiveTargetPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	s.applyTargetPickerDraftAnswers(record, answers)
	record.SelectedWorkspaceKey = normalizeTargetPickerWorkspaceSelection(workspaceKey)
	record.SelectedSessionValue = ""
	view, err := s.buildTargetPickerView(surface, record)
	if err != nil {
		return notice(surface, "target_picker_unavailable", err.Error())
	}
	return []control.UIEvent{s.targetPickerViewEvent(surface, view, true)}
}

func (s *Service) handleTargetPickerSelectSession(surface *state.SurfaceConsoleRecord, pickerID, value, actorUserID string, answers map[string][]string) []control.UIEvent {
	record, blocked := s.requireActiveTargetPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	s.applyTargetPickerDraftAnswers(record, answers)
	record.SelectedSessionValue = strings.TrimSpace(value)
	view, err := s.buildTargetPickerView(surface, record)
	if err != nil {
		return notice(surface, "target_picker_unavailable", err.Error())
	}
	return []control.UIEvent{s.targetPickerViewEvent(surface, view, true)}
}

func (s *Service) handleTargetPickerCancel(surface *state.SurfaceConsoleRecord, pickerID, actorUserID string) []control.UIEvent {
	if _, blocked := s.requireActiveTargetPicker(surface, pickerID, actorUserID); blocked != nil {
		return blocked
	}
	s.clearSurfaceTargetPicker(surface)
	return []control.UIEvent{{
		Kind:                     control.UIEventNotice,
		GatewayID:                surface.GatewayID,
		SurfaceSessionID:         surface.SurfaceSessionID,
		InlineReplaceCurrentCard: true,
		Notice: &control.Notice{
			Code: "target_picker_cancelled",
			Text: "已取消选择工作区/会话。",
		},
	}}
}

func (s *Service) handleTargetPickerConfirm(surface *state.SurfaceConsoleRecord, pickerID, actorUserID, workspaceKey, sessionValue string, answers map[string][]string) []control.UIEvent {
	record, blocked := s.requireActiveTargetPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	s.applyTargetPickerDraftAnswers(record, answers)
	requestedMode := normalizeTargetPickerMode(string(record.SelectedMode))
	requestedSourceKind := normalizeTargetPickerSourceKind(string(record.SelectedSource))
	requestedWorkspaceKey := normalizeTargetPickerWorkspaceSelection(record.SelectedWorkspaceKey)
	requestedSessionValue := strings.TrimSpace(record.SelectedSessionValue)
	if requestedMode == control.FeishuTargetPickerModeExistingWorkspace {
		if key := normalizeTargetPickerWorkspaceSelection(workspaceKey); key != "" {
			record.SelectedWorkspaceKey = key
			requestedWorkspaceKey = key
		}
		if strings.TrimSpace(sessionValue) != "" {
			record.SelectedSessionValue = strings.TrimSpace(sessionValue)
			requestedSessionValue = strings.TrimSpace(sessionValue)
		}
	}
	view, err := s.buildTargetPickerView(surface, record)
	if err != nil {
		return notice(surface, "target_picker_unavailable", err.Error())
	}
	if view.SelectedMode != requestedMode ||
		view.SelectedSource != requestedSourceKind ||
		(requestedMode == control.FeishuTargetPickerModeExistingWorkspace &&
			((requestedWorkspaceKey != "" && view.SelectedWorkspaceKey != requestedWorkspaceKey) ||
				(requestedSessionValue != "" && view.SelectedSessionValue != requestedSessionValue))) {
		events := []control.UIEvent{s.targetPickerViewEvent(surface, view, false)}
		events = append(events, notice(surface, "target_picker_selection_changed", "可选目标刚刚发生变化，请在最新卡片上重新确认。")...)
		return events
	}
	if !view.CanConfirm {
		if view.SelectedMode == control.FeishuTargetPickerModeAddWorkspace {
			switch view.SelectedSource {
			case control.FeishuTargetPickerSourceLocalDirectory:
				return notice(surface, "target_picker_selection_missing", "请先选择一个可接入的本地目录。")
			case control.FeishuTargetPickerSourceGitURL:
				return notice(surface, "target_picker_selection_missing", "请先补全可执行的 Git 工作区配置。")
			default:
				return notice(surface, "target_picker_selection_missing", "请选择一个可用的工作区来源后再确认。")
			}
		}
		return notice(surface, "target_picker_selection_missing", "请选择工作区和会话后再确认。")
	}
	result := control.TargetPickerResult{
		PickerID:     record.PickerID,
		Source:       record.Source,
		Mode:         view.SelectedMode,
		SourceKind:   view.SelectedSource,
		WorkspaceKey: view.SelectedWorkspaceKey,
		SessionValue: view.SelectedSessionValue,
		OwnerUserID:  record.OwnerUserID,
		CreatedAt:    record.CreatedAt,
		ExpiresAt:    record.ExpiresAt,
	}
	return s.dispatchTargetPickerConfirmed(surface, record, result, view)
}

func (s *Service) dispatchTargetPickerConfirmed(surface *state.SurfaceConsoleRecord, record *activeTargetPickerRecord, result control.TargetPickerResult, view control.FeishuTargetPickerView) []control.UIEvent {
	if surface == nil {
		return nil
	}
	if result.Mode == control.FeishuTargetPickerModeAddWorkspace {
		switch normalizeTargetPickerSourceKind(string(result.SourceKind)) {
		case control.FeishuTargetPickerSourceLocalDirectory:
			return s.confirmTargetPickerLocalDirectory(surface, record, view)
		case control.FeishuTargetPickerSourceGitURL:
			return s.confirmTargetPickerGitImport(surface, record, view)
		default:
			return notice(surface, "target_picker_selection_missing", "请选择一个可用的工作区来源后再确认。")
		}
	}
	workspaceKey := normalizeTargetPickerWorkspaceSelection(result.WorkspaceKey)
	sessionValue := strings.TrimSpace(result.SessionValue)
	if workspaceKey == "" || sessionValue == "" {
		return notice(surface, "target_picker_selection_missing", "请选择工作区和会话后再确认。")
	}
	kind, threadID := parseTargetPickerSessionValue(sessionValue)
	var events []control.UIEvent
	succeeded := false
	switch kind {
	case control.FeishuTargetPickerSessionThread:
		events = s.useThread(surface, threadID, true)
		succeeded = surface.SelectedThreadID == threadID || (surface.PendingHeadless != nil && strings.TrimSpace(surface.PendingHeadless.ThreadID) == threadID)
	case control.FeishuTargetPickerSessionNewThread:
		events = s.enterTargetPickerNewThread(surface, workspaceKey)
		succeeded = targetPickerNewThreadSucceeded(surface, workspaceKey)
	default:
		return notice(surface, "target_picker_selection_missing", "当前选择的目标无效，请重新选择。")
	}
	if succeeded {
		s.clearSurfaceTargetPicker(surface)
	}
	return events
}

func (s *Service) enterTargetPickerNewThread(surface *state.SurfaceConsoleRecord, workspaceKey string) []control.UIEvent {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if workspaceKey == "" {
		return notice(surface, "workspace_not_found", "目标工作区不存在，请重新发送 /list。")
	}
	if s.normalizeSurfaceProductMode(surface) != state.ProductModeNormal {
		return notice(surface, "new_thread_disabled_vscode", "当前处于 vscode 模式，不能在这里直接新建会话。")
	}
	if currentWorkspace := s.surfaceCurrentWorkspaceKey(surface); currentWorkspace == workspaceKey && strings.TrimSpace(surface.AttachedInstanceID) != "" {
		return s.prepareNewThread(surface)
	}
	if inst := s.resolveWorkspaceAttachInstance(surface, workspaceKey); inst != nil {
		return s.attachWorkspaceWithMode(surface, workspaceKey, attachWorkspaceModeTargetPickerNewThread)
	}
	return s.startFreshWorkspaceHeadlessWithOptions(surface, workspaceKey, true)
}

func targetPickerNewThreadSucceeded(surface *state.SurfaceConsoleRecord, workspaceKey string) bool {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if surface == nil || workspaceKey == "" {
		return false
	}
	return (surface.RouteMode == state.RouteModeNewThreadReady && normalizeWorkspaceClaimKey(surface.PreparedThreadCWD) == workspaceKey) ||
		(surface.PendingHeadless != nil && normalizeWorkspaceClaimKey(surface.PendingHeadless.ThreadCWD) == workspaceKey && surface.PendingHeadless.PrepareNewThread)
}

func (s *Service) requireActiveTargetPicker(surface *state.SurfaceConsoleRecord, pickerID, actorUserID string) (*activeTargetPickerRecord, []control.UIEvent) {
	if surface == nil || s.activeTargetPicker(surface) == nil {
		return nil, notice(surface, "target_picker_expired", "这个目标选择卡片已失效，请重新发送 /list、/use 或 /useall。")
	}
	record := s.activeTargetPicker(surface)
	if !record.ExpiresAt.IsZero() && !record.ExpiresAt.After(s.now()) {
		s.clearSurfaceTargetPicker(surface)
		return nil, notice(surface, "target_picker_expired", "这个目标选择卡片已过期，请重新发送 /list、/use 或 /useall。")
	}
	if strings.TrimSpace(pickerID) == "" || strings.TrimSpace(record.PickerID) != strings.TrimSpace(pickerID) {
		return nil, notice(surface, "target_picker_expired", "这个旧目标选择卡片已失效，请重新发送 /list、/use 或 /useall。")
	}
	actorUserID = strings.TrimSpace(firstNonEmpty(actorUserID, surface.ActorUserID))
	if ownerUserID := strings.TrimSpace(record.OwnerUserID); ownerUserID != "" && actorUserID != "" && ownerUserID != actorUserID {
		return nil, notice(surface, "target_picker_unauthorized", "这个目标选择卡片只允许发起者本人操作。")
	}
	return record, nil
}

func (s *Service) buildTargetPickerView(surface *state.SurfaceConsoleRecord, record *activeTargetPickerRecord) (control.FeishuTargetPickerView, error) {
	if surface == nil || record == nil {
		return control.FeishuTargetPickerView{}, fmt.Errorf("目标选择器不存在")
	}
	workspaceEntries := s.targetPickerWorkspaceEntries(surface)
	addSupported := targetPickerSupportsAddWorkspace(record.Source)
	if len(workspaceEntries) == 0 && !addSupported {
		return control.FeishuTargetPickerView{}, fmt.Errorf("当前没有可操作的工作区。请先连接一个 VS Code 会话，或等待可恢复工作区出现。")
	}
	mode := normalizeTargetPickerMode(string(record.SelectedMode))
	if mode == "" {
		mode = targetPickerDefaultMode(record.Source)
	}
	if len(workspaceEntries) == 0 && addSupported {
		mode = control.FeishuTargetPickerModeAddWorkspace
	}
	if mode == control.FeishuTargetPickerModeAddWorkspace && !addSupported {
		mode = control.FeishuTargetPickerModeExistingWorkspace
	}
	record.SelectedMode = mode

	modeOptions := targetPickerModeOptions(addSupported, mode)
	sourceOptions := s.targetPickerSourceOptions()
	sourceKind := normalizeTargetPickerSourceKind(string(record.SelectedSource))
	if sourceKind == "" {
		sourceKind = control.FeishuTargetPickerSourceLocalDirectory
	}
	if !targetPickerHasSourceOption(sourceOptions, sourceKind) {
		sourceKind = targetPickerDefaultSourceSelection(sourceOptions)
	}
	record.SelectedSource = sourceKind

	workspaceOptions := make([]control.FeishuTargetPickerWorkspaceOption, 0, len(workspaceEntries))
	for _, entry := range workspaceEntries {
		workspaceOptions = append(workspaceOptions, control.FeishuTargetPickerWorkspaceOption{
			Value:           entry.workspaceKey,
			Label:           entry.label,
			MetaText:        workspaceSelectionMetaText(entry.ageText, entry.hasVSCodeActivity, false, false, entry.recoverableOnly),
			RecoverableOnly: entry.recoverableOnly,
		})
	}
	selectedWorkspace := normalizeTargetPickerWorkspaceSelection(record.SelectedWorkspaceKey)
	if !targetPickerHasWorkspaceOption(workspaceOptions, selectedWorkspace) {
		selectedWorkspace = normalizeWorkspaceClaimKey(s.surfaceCurrentWorkspaceKey(surface))
	}
	if !targetPickerHasWorkspaceOption(workspaceOptions, selectedWorkspace) {
		selectedWorkspace = targetPickerDefaultWorkspaceSelection(workspaceOptions)
	}
	record.SelectedWorkspaceKey = selectedWorkspace

	sessionOptions := s.targetPickerSessionOptions(surface, selectedWorkspace)
	selectedSession := strings.TrimSpace(record.SelectedSessionValue)
	if mode == control.FeishuTargetPickerModeExistingWorkspace {
		switch {
		case selectedSession == targetPickerAutoSession:
			selectedSession = s.defaultTargetPickerSessionValue(surface, selectedWorkspace, sessionOptions)
		case selectedSession == "":
			// Keep the session dropdown visibly empty after a workspace switch.
		case !targetPickerHasSessionOption(sessionOptions, selectedSession):
			selectedSession = ""
		}
	} else {
		selectedSession = ""
	}
	record.SelectedSessionValue = selectedSession

	selectedWorkspaceLabel, selectedWorkspaceMeta := targetPickerSelectedWorkspaceSummary(workspaceOptions, selectedWorkspace)
	selectedSessionLabel, selectedSessionMeta := targetPickerSelectedSessionSummary(sessionOptions, selectedSession)
	confirmLabel := "使用会话"
	hint := "下拉变化不会立即切换，点击下方按钮后才会真正生效。"
	canConfirm := selectedWorkspace != "" && selectedSession != ""
	sourceUnavailableHint := ""
	addModeSummary := ""
	addModeDetail := ""
	localDirectoryPath := strings.TrimSpace(record.LocalDirectoryPath)
	gitParentDir := strings.TrimSpace(record.GitParentDir)
	gitRepoURL := strings.TrimSpace(record.GitRepoURL)
	gitDirectoryName := strings.TrimSpace(record.GitDirectoryName)
	gitFinalPath := ""
	sourceMessages := []control.FeishuTargetPickerMessage(nil)
	showWorkspaceSelect := mode == control.FeishuTargetPickerModeExistingWorkspace
	showSessionSelect := mode == control.FeishuTargetPickerModeExistingWorkspace
	showSourceSelect := mode == control.FeishuTargetPickerModeAddWorkspace
	sessionPlaceholder := "选择会话"
	if mode == control.FeishuTargetPickerModeAddWorkspace {
		canConfirm = targetPickerSourceAvailable(sourceOptions, sourceKind)
		hint = "填写完成后再点击下方按钮，未确认前不会切换当前工作目标。"
		addModeSummary = "准备一个新的工作区"
		addModeDetail = "完成后会直接进入新会话待命。"
		switch sourceKind {
		case control.FeishuTargetPickerSourceLocalDirectory:
			localState := s.buildTargetPickerLocalDirectoryState(surface, record)
			if strings.TrimSpace(localState.ResolvedPath) != "" {
				localDirectoryPath = strings.TrimSpace(localState.ResolvedPath)
			}
			sourceMessages = append(sourceMessages, localState.Messages...)
			canConfirm = canConfirm && localState.CanConfirm
			confirmLabel = "接入并继续"
		case control.FeishuTargetPickerSourceGitURL:
			gitState := s.buildTargetPickerGitImportState(record)
			if strings.TrimSpace(gitState.ParentDir) != "" {
				gitParentDir = strings.TrimSpace(gitState.ParentDir)
			}
			gitFinalPath = strings.TrimSpace(gitState.FinalPath)
			sourceMessages = append(sourceMessages, gitState.Messages...)
			canConfirm = canConfirm && gitState.CanConfirm
			confirmLabel = "克隆并继续"
		default:
			confirmLabel = "继续"
		}
		sourceUnavailableHint = targetPickerSourceUnavailableReason(sourceOptions, sourceKind)
		if sourceUnavailableHint != "" {
			hint = "当前选择的来源暂不可用，请改选其他来源后再继续。"
		}
	} else if kind, _ := parseTargetPickerSessionValue(selectedSession); kind == control.FeishuTargetPickerSessionNewThread {
		confirmLabel = "进入新会话"
	}
	return control.FeishuTargetPickerView{
		PickerID:               record.PickerID,
		Title:                  targetPickerTitle(record.Source),
		Source:                 record.Source,
		SelectedMode:           mode,
		SelectedSource:         sourceKind,
		ShowModeSwitch:         addSupported,
		ShowWorkspaceSelect:    showWorkspaceSelect,
		ShowSessionSelect:      showSessionSelect,
		ShowSourceSelect:       showSourceSelect,
		ModePlaceholder:        "选择模式",
		WorkspacePlaceholder:   "选择工作区",
		SessionPlaceholder:     sessionPlaceholder,
		SourcePlaceholder:      "选择工作区来源",
		SelectedWorkspaceKey:   selectedWorkspace,
		SelectedSessionValue:   selectedSession,
		SelectedWorkspaceLabel: selectedWorkspaceLabel,
		SelectedWorkspaceMeta:  selectedWorkspaceMeta,
		SelectedSessionLabel:   selectedSessionLabel,
		SelectedSessionMeta:    selectedSessionMeta,
		ConfirmLabel:           confirmLabel,
		CanConfirm:             canConfirm,
		Hint:                   hint,
		ModeOptions:            modeOptions,
		WorkspaceOptions:       workspaceOptions,
		SessionOptions:         sessionOptions,
		SourceOptions:          sourceOptions,
		AddModeSummary:         addModeSummary,
		AddModeDetail:          addModeDetail,
		SourceUnavailableHint:  sourceUnavailableHint,
		LocalDirectoryPath:     localDirectoryPath,
		GitParentDir:           gitParentDir,
		GitRepoURL:             gitRepoURL,
		GitDirectoryName:       gitDirectoryName,
		GitFinalPath:           gitFinalPath,
		SourceMessages:         sourceMessages,
	}, nil
}

func targetPickerTitle(source control.TargetPickerRequestSource) string {
	switch source {
	case control.TargetPickerRequestSourceUse:
		return "选择当前工作目标"
	case control.TargetPickerRequestSourceUseAll:
		return "选择工作区与会话"
	default:
		return "选择工作区与会话"
	}
}

func (s *Service) targetPickerWorkspaceEntries(surface *state.SurfaceConsoleRecord) []workspaceSelectionEntry {
	grouped := map[string][]*state.InstanceRecord{}
	for _, inst := range s.root.Instances {
		if inst == nil || !inst.Online {
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
	s.mergeWorkspaceSelectionRecencyFromOnlineThreads(recoverableWorkspaces, recoverableWorkspaceSeen, visibleWorkspaces)
	s.mergeWorkspaceSelectionRecencyFromPersistedWorkspaces(recoverableWorkspaces, recoverableWorkspaceSeen, visibleWorkspaces)

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
		attachable := s.resolveWorkspaceAttachInstanceFromCandidates(surface, workspaceKey, instances) != nil
		recoverableOnly := !attachable && len(instances) == 0 && recoverableWorkspaceSeen[workspaceKey]
		busy := s.workspaceBusyOwnerForSurface(surface, workspaceKey) != nil
		if busy || (!attachable && !recoverableOnly) {
			continue
		}
		entries = append(entries, workspaceSelectionEntry{
			workspaceKey:      workspaceKey,
			latestUsedAt:      latestUsedAt,
			label:             workspaceSelectionLabel(workspaceKey),
			ageText:           ageText,
			hasVSCodeActivity: hasVSCodeActivity,
			busy:              busy,
			attachable:        attachable,
			recoverableOnly:   recoverableOnly,
		})
	}
	sortWorkspaceSelectionEntries(entries)
	return entries
}

func (s *Service) targetPickerSessionOptions(surface *state.SurfaceConsoleRecord, workspaceKey string) []control.FeishuTargetPickerSessionOption {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if workspaceKey == "" {
		return nil
	}
	views := s.threadViewsVisibleInNormalList(surface, s.mergedThreadViews(surface))
	options := make([]control.FeishuTargetPickerSessionOption, 0, len(views)+1)
	for _, view := range views {
		if mergedThreadWorkspaceClaimKey(view) != workspaceKey {
			continue
		}
		target := s.resolveThreadTargetFromView(surface, view)
		if target.Mode == threadAttachUnavailable {
			continue
		}
		entry := s.threadSelectionViewEntry(surface, view, true)
		meta := s.threadSelectionMetaText(surface, view, entry.Status)
		options = append(options, control.FeishuTargetPickerSessionOption{
			Value:    targetPickerThreadValue(view.ThreadID),
			Kind:     control.FeishuTargetPickerSessionThread,
			Label:    entry.Summary,
			MetaText: meta,
		})
	}
	options = append(options, control.FeishuTargetPickerSessionOption{
		Value:    targetPickerNewThreadValue,
		Kind:     control.FeishuTargetPickerSessionNewThread,
		Label:    "新建会话",
		MetaText: "在这个工作区里开始一个新的会话",
	})
	return options
}

func (s *Service) defaultTargetPickerSessionValue(surface *state.SurfaceConsoleRecord, workspaceKey string, options []control.FeishuTargetPickerSessionOption) string {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if workspaceKey == "" {
		return ""
	}
	if s.surfaceCurrentWorkspaceKey(surface) != workspaceKey {
		return ""
	}
	if surface != nil && surface.RouteMode == state.RouteModeNewThreadReady {
		return targetPickerNewThreadValue
	}
	if surface != nil && strings.TrimSpace(surface.SelectedThreadID) != "" {
		value := targetPickerThreadValue(surface.SelectedThreadID)
		if targetPickerHasSessionOption(options, value) {
			return value
		}
	}
	return ""
}

func targetPickerHasWorkspaceOption(options []control.FeishuTargetPickerWorkspaceOption, value string) bool {
	for _, option := range options {
		if option.Value == value {
			return true
		}
	}
	return false
}

func targetPickerDefaultWorkspaceSelection(options []control.FeishuTargetPickerWorkspaceOption) string {
	for _, option := range options {
		if strings.TrimSpace(option.Value) == "" || option.Synthetic {
			continue
		}
		return option.Value
	}
	for _, option := range options {
		if strings.TrimSpace(option.Value) != "" {
			return option.Value
		}
	}
	return ""
}

func normalizeTargetPickerWorkspaceSelection(value string) string {
	return normalizeWorkspaceClaimKey(value)
}

func targetPickerHasSessionOption(options []control.FeishuTargetPickerSessionOption, value string) bool {
	for _, option := range options {
		if option.Value == value {
			return true
		}
	}
	return false
}

func targetPickerSelectedWorkspaceSummary(options []control.FeishuTargetPickerWorkspaceOption, value string) (string, string) {
	for _, option := range options {
		if option.Value == value {
			return option.Label, option.MetaText
		}
	}
	return "", ""
}

func targetPickerSelectedSessionSummary(options []control.FeishuTargetPickerSessionOption, value string) (string, string) {
	for _, option := range options {
		if option.Value == value {
			return option.Label, option.MetaText
		}
	}
	return "", ""
}

func targetPickerThreadValue(threadID string) string {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return ""
	}
	return targetPickerThreadPrefix + threadID
}

func targetPickerSupportsAddWorkspace(source control.TargetPickerRequestSource) bool {
	switch source {
	case control.TargetPickerRequestSourceList, control.TargetPickerRequestSourceUse, control.TargetPickerRequestSourceUseAll:
		return true
	default:
		return false
	}
}

func targetPickerDefaultMode(source control.TargetPickerRequestSource) control.FeishuTargetPickerMode {
	if targetPickerSupportsAddWorkspace(source) {
		return control.FeishuTargetPickerModeExistingWorkspace
	}
	return control.FeishuTargetPickerModeExistingWorkspace
}

func normalizeTargetPickerMode(value string) control.FeishuTargetPickerMode {
	switch control.FeishuTargetPickerMode(strings.TrimSpace(value)) {
	case control.FeishuTargetPickerModeExistingWorkspace, control.FeishuTargetPickerModeAddWorkspace:
		return control.FeishuTargetPickerMode(strings.TrimSpace(value))
	default:
		return ""
	}
}

func targetPickerModeOptions(addSupported bool, selected control.FeishuTargetPickerMode) []control.FeishuTargetPickerModeOption {
	if !addSupported {
		return nil
	}
	return []control.FeishuTargetPickerModeOption{
		{Value: control.FeishuTargetPickerModeExistingWorkspace, Label: "已有工作区", Selected: selected == control.FeishuTargetPickerModeExistingWorkspace},
		{Value: control.FeishuTargetPickerModeAddWorkspace, Label: "添加工作区", Selected: selected == control.FeishuTargetPickerModeAddWorkspace},
	}
}

func normalizeTargetPickerSourceKind(value string) control.FeishuTargetPickerSourceKind {
	switch control.FeishuTargetPickerSourceKind(strings.TrimSpace(value)) {
	case control.FeishuTargetPickerSourceLocalDirectory, control.FeishuTargetPickerSourceGitURL:
		return control.FeishuTargetPickerSourceKind(strings.TrimSpace(value))
	default:
		return ""
	}
}

func (s *Service) targetPickerSourceOptions() []control.FeishuTargetPickerSourceOption {
	options := []control.FeishuTargetPickerSourceOption{{
		Value:     control.FeishuTargetPickerSourceLocalDirectory,
		Label:     "本地目录",
		MetaText:  "选择本机上已经存在的目录，并在完成后进入新会话待命",
		Available: true,
	}}
	gitOption := control.FeishuTargetPickerSourceOption{
		Value:     control.FeishuTargetPickerSourceGitURL,
		Label:     "Git URL",
		MetaText:  "填写仓库地址后拉取到本地，并在完成后进入新会话待命",
		Available: s.config.GitAvailable,
	}
	if !gitOption.Available {
		gitOption.MetaText = "需要本机已安装 git 后才能使用"
		gitOption.UnavailableReason = "当前机器未检测到 `git`，暂时不能直接从 Git URL 导入。"
	}
	options = append(options, gitOption)
	return options
}

func targetPickerHasSourceOption(options []control.FeishuTargetPickerSourceOption, value control.FeishuTargetPickerSourceKind) bool {
	for _, option := range options {
		if option.Value == value {
			return true
		}
	}
	return false
}

func targetPickerDefaultSourceSelection(options []control.FeishuTargetPickerSourceOption) control.FeishuTargetPickerSourceKind {
	for _, option := range options {
		if option.Value != "" {
			return option.Value
		}
	}
	return ""
}

func targetPickerSourceAvailable(options []control.FeishuTargetPickerSourceOption, value control.FeishuTargetPickerSourceKind) bool {
	for _, option := range options {
		if option.Value == value {
			return option.Available
		}
	}
	return false
}

func targetPickerSourceUnavailableReason(options []control.FeishuTargetPickerSourceOption, value control.FeishuTargetPickerSourceKind) string {
	for _, option := range options {
		if option.Value == value {
			return strings.TrimSpace(option.UnavailableReason)
		}
	}
	return ""
}

func parseTargetPickerSessionValue(value string) (control.FeishuTargetPickerSessionKind, string) {
	value = strings.TrimSpace(value)
	switch {
	case value == targetPickerNewThreadValue:
		return control.FeishuTargetPickerSessionNewThread, ""
	case strings.HasPrefix(value, targetPickerThreadPrefix):
		return control.FeishuTargetPickerSessionThread, strings.TrimPrefix(value, targetPickerThreadPrefix)
	default:
		return "", ""
	}
}
