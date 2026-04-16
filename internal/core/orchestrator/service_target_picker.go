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
	surface.ActiveTargetPicker = record
	view, err := s.buildTargetPickerView(surface, record)
	if err != nil {
		surface.ActiveTargetPicker = nil
		return notice(surface, "target_picker_unavailable", err.Error())
	}
	return []control.UIEvent{s.targetPickerViewEvent(surface, view, inline)}
}

func (s *Service) newTargetPickerRecord(surface *state.SurfaceConsoleRecord, source control.TargetPickerRequestSource, preferredWorkspaceKey string) (*state.ActiveTargetPickerRecord, error) {
	if surface == nil {
		return nil, fmt.Errorf("目标选择器不可用")
	}
	preferredWorkspaceKey = normalizeWorkspaceClaimKey(firstNonEmpty(preferredWorkspaceKey, s.surfaceCurrentWorkspaceKey(surface)))
	expiresAt := s.now().Add(defaultTargetPickerTTL)
	return &state.ActiveTargetPickerRecord{
		PickerID:             s.nextTargetPickerToken(),
		OwnerUserID:          strings.TrimSpace(firstNonEmpty(surface.ActorUserID)),
		Source:               source,
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

func (s *Service) handleTargetPickerSelectWorkspace(surface *state.SurfaceConsoleRecord, pickerID, workspaceKey, actorUserID string) []control.UIEvent {
	record, blocked := s.requireActiveTargetPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	record.SelectedWorkspaceKey = normalizeTargetPickerWorkspaceSelection(workspaceKey)
	if isTargetPickerCreateWorkspaceSelection(record.SelectedWorkspaceKey) {
		record.SelectedSessionValue = targetPickerNewThreadValue
	} else {
		record.SelectedSessionValue = ""
	}
	view, err := s.buildTargetPickerView(surface, record)
	if err != nil {
		return notice(surface, "target_picker_unavailable", err.Error())
	}
	return []control.UIEvent{s.targetPickerViewEvent(surface, view, true)}
}

func (s *Service) handleTargetPickerSelectSession(surface *state.SurfaceConsoleRecord, pickerID, value, actorUserID string) []control.UIEvent {
	record, blocked := s.requireActiveTargetPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	record.SelectedSessionValue = strings.TrimSpace(value)
	view, err := s.buildTargetPickerView(surface, record)
	if err != nil {
		return notice(surface, "target_picker_unavailable", err.Error())
	}
	return []control.UIEvent{s.targetPickerViewEvent(surface, view, true)}
}

func (s *Service) handleTargetPickerConfirm(surface *state.SurfaceConsoleRecord, pickerID, actorUserID, workspaceKey, sessionValue string) []control.UIEvent {
	record, blocked := s.requireActiveTargetPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	requestedWorkspaceKey := normalizeTargetPickerWorkspaceSelection(record.SelectedWorkspaceKey)
	if key := normalizeTargetPickerWorkspaceSelection(workspaceKey); key != "" {
		record.SelectedWorkspaceKey = key
		requestedWorkspaceKey = key
	}
	requestedSessionValue := strings.TrimSpace(record.SelectedSessionValue)
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
		events := []control.UIEvent{s.targetPickerViewEvent(surface, view, false)}
		events = append(events, notice(surface, "target_picker_selection_changed", "可选目标刚刚发生变化，请在最新卡片上重新确认。")...)
		return events
	}
	if !view.CanConfirm {
		return notice(surface, "target_picker_selection_missing", "请选择工作区和会话后再确认。")
	}
	result := control.TargetPickerResult{
		PickerID:     record.PickerID,
		Source:       record.Source,
		WorkspaceKey: view.SelectedWorkspaceKey,
		SessionValue: view.SelectedSessionValue,
		OwnerUserID:  record.OwnerUserID,
		CreatedAt:    record.CreatedAt,
		ExpiresAt:    record.ExpiresAt,
	}
	return s.dispatchTargetPickerConfirmed(surface, result)
}

func (s *Service) dispatchTargetPickerConfirmed(surface *state.SurfaceConsoleRecord, result control.TargetPickerResult) []control.UIEvent {
	if surface == nil {
		return nil
	}
	workspaceKey := normalizeTargetPickerWorkspaceSelection(result.WorkspaceKey)
	sessionValue := strings.TrimSpace(result.SessionValue)
	if workspaceKey == "" || sessionValue == "" {
		return notice(surface, "target_picker_selection_missing", "请选择工作区和会话后再确认。")
	}
	if isTargetPickerCreateWorkspaceSelection(workspaceKey) {
		if sessionValue != targetPickerNewThreadValue {
			return notice(surface, "target_picker_selection_missing", "当前选择的目标无效，请重新选择。")
		}
		return s.openTargetPickerWorkspaceCreatePicker(surface)
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
		clearSurfaceTargetPicker(surface)
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

func (s *Service) requireActiveTargetPicker(surface *state.SurfaceConsoleRecord, pickerID, actorUserID string) (*state.ActiveTargetPickerRecord, []control.UIEvent) {
	if surface == nil || surface.ActiveTargetPicker == nil {
		return nil, notice(surface, "target_picker_expired", "这个目标选择卡片已失效，请重新发送 /list、/use 或 /useall。")
	}
	record := surface.ActiveTargetPicker
	if !record.ExpiresAt.IsZero() && !record.ExpiresAt.After(s.now()) {
		clearSurfaceTargetPicker(surface)
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

func clearSurfaceTargetPicker(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	surface.ActiveTargetPicker = nil
}

func (s *Service) buildTargetPickerView(surface *state.SurfaceConsoleRecord, record *state.ActiveTargetPickerRecord) (control.FeishuTargetPickerView, error) {
	if surface == nil || record == nil {
		return control.FeishuTargetPickerView{}, fmt.Errorf("目标选择器不存在")
	}
	workspaceEntries := s.targetPickerWorkspaceEntries(surface)
	if len(workspaceEntries) == 0 && !targetPickerSupportsCreateWorkspace(record.Source) {
		return control.FeishuTargetPickerView{}, fmt.Errorf("当前没有可操作的工作区。请先连接一个 VS Code 会话，或等待可恢复工作区出现。")
	}
	workspaceOptions := make([]control.FeishuTargetPickerWorkspaceOption, 0, len(workspaceEntries))
	if targetPickerSupportsCreateWorkspace(record.Source) {
		workspaceOptions = append(workspaceOptions, targetPickerCreateWorkspaceOption())
	}
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

	sessionOptions := s.targetPickerSessionOptionsForSelection(surface, selectedWorkspace)
	selectedSession := strings.TrimSpace(record.SelectedSessionValue)
	if isTargetPickerCreateWorkspaceSelection(selectedWorkspace) {
		if !targetPickerHasSessionOption(sessionOptions, selectedSession) {
			selectedSession = targetPickerNewThreadValue
		}
	} else {
		switch {
		case selectedSession == targetPickerAutoSession:
			selectedSession = s.defaultTargetPickerSessionValue(surface, selectedWorkspace, sessionOptions)
		case selectedSession == "":
			// Keep the session dropdown visibly empty after a workspace switch.
		case !targetPickerHasSessionOption(sessionOptions, selectedSession):
			selectedSession = ""
		}
	}
	record.SelectedSessionValue = selectedSession

	selectedWorkspaceLabel, selectedWorkspaceMeta := targetPickerSelectedWorkspaceSummary(workspaceOptions, selectedWorkspace)
	selectedSessionLabel, selectedSessionMeta := targetPickerSelectedSessionSummary(sessionOptions, selectedSession)
	confirmLabel := "使用会话"
	sessionPlaceholder := "选择会话"
	hint := "下拉变化不会立即切换，点击下方按钮后才会真正生效。"
	if isTargetPickerCreateWorkspaceSelection(selectedWorkspace) {
		confirmLabel = "选择目录"
		sessionPlaceholder = "将在接入后新建"
		hint = "下拉变化不会立即切换。选择目录后，再确认要接入的本地目录。"
	} else if kind, _ := parseTargetPickerSessionValue(selectedSession); kind == control.FeishuTargetPickerSessionNewThread {
		confirmLabel = "新建会话"
	}
	return control.FeishuTargetPickerView{
		PickerID:               record.PickerID,
		Title:                  targetPickerTitle(record.Source),
		Source:                 record.Source,
		WorkspacePlaceholder:   "选择工作区",
		SessionPlaceholder:     sessionPlaceholder,
		SelectedWorkspaceKey:   selectedWorkspace,
		SelectedSessionValue:   selectedSession,
		SelectedWorkspaceLabel: selectedWorkspaceLabel,
		SelectedWorkspaceMeta:  selectedWorkspaceMeta,
		SelectedSessionLabel:   selectedSessionLabel,
		SelectedSessionMeta:    selectedSessionMeta,
		ConfirmLabel:           confirmLabel,
		CanConfirm:             selectedWorkspace != "" && selectedSession != "",
		Hint:                   hint,
		WorkspaceOptions:       workspaceOptions,
		SessionOptions:         sessionOptions,
	}, nil
}

func (s *Service) targetPickerSessionOptionsForSelection(surface *state.SurfaceConsoleRecord, workspaceKey string) []control.FeishuTargetPickerSessionOption {
	if isTargetPickerCreateWorkspaceSelection(workspaceKey) {
		return []control.FeishuTargetPickerSessionOption{{
			Value:    targetPickerNewThreadValue,
			Kind:     control.FeishuTargetPickerSessionNewThread,
			Label:    "新建会话",
			MetaText: "接入目录后将在这个工作区里开始新的会话",
		}}
	}
	return s.targetPickerSessionOptions(surface, workspaceKey)
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
	for workspaceKey := range visibleWorkspaces {
		workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
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
	if surface != nil && surface.RouteMode == state.RouteModeNewThreadReady && s.surfaceCurrentWorkspaceKey(surface) == workspaceKey {
		return targetPickerNewThreadValue
	}
	if surface != nil && strings.TrimSpace(surface.SelectedThreadID) != "" && s.surfaceCurrentWorkspaceKey(surface) == workspaceKey {
		value := targetPickerThreadValue(surface.SelectedThreadID)
		if targetPickerHasSessionOption(options, value) {
			return value
		}
	}
	for _, option := range options {
		if option.Kind == control.FeishuTargetPickerSessionThread {
			return option.Value
		}
	}
	return targetPickerNewThreadValue
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

func targetPickerSupportsCreateWorkspace(source control.TargetPickerRequestSource) bool {
	return source == control.TargetPickerRequestSourceList
}

func targetPickerCreateWorkspaceOption() control.FeishuTargetPickerWorkspaceOption {
	return control.FeishuTargetPickerWorkspaceOption{
		Value:     targetPickerCreateWorkspaceValue,
		Label:     "添加工作区…",
		MetaText:  "选择本地目录，并在接入后开始新的会话",
		Synthetic: true,
	}
}

func isTargetPickerCreateWorkspaceSelection(value string) bool {
	return strings.TrimSpace(value) == targetPickerCreateWorkspaceValue
}

func normalizeTargetPickerWorkspaceSelection(value string) string {
	value = strings.TrimSpace(value)
	if isTargetPickerCreateWorkspaceSelection(value) {
		return targetPickerCreateWorkspaceValue
	}
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
