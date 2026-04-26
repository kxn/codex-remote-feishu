package orchestrator

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/workspaceimport"
)

const (
	targetPickerAddWorkspacePathPickerConsumerKind     = "target_picker_add_workspace"
	targetPickerAddWorkspaceMetaPickerID               = "picker_id"
	targetPickerAddWorkspaceMetaFieldKind              = "field_kind"
	targetPickerBusyWorkspacePathPickerEntryFilterKind = "target_picker_hide_busy_workspaces"
)

type targetPickerAddWorkspacePathPickerConsumer struct{}
type targetPickerBusyWorkspacePathPickerEntryFilter struct{}

type targetPickerLocalDirectoryState struct {
	ResolvedPath string
	CanConfirm   bool
	Messages     []control.FeishuTargetPickerMessage
}

type targetPickerGitImportState struct {
	ParentDir  string
	FinalPath  string
	CanConfirm bool
	Messages   []control.FeishuTargetPickerMessage
}

func (s *Service) applyTargetPickerDraftAnswers(record *activeTargetPickerRecord, answers map[string][]string) {
	if record == nil || len(answers) == 0 {
		return
	}
	if value, ok := targetPickerAnswerValue(answers, control.FeishuTargetPickerGitRepoURLFieldName); ok {
		record.GitRepoURL = strings.TrimSpace(value)
	}
	if value, ok := targetPickerAnswerValue(answers, control.FeishuTargetPickerGitDirectoryNameFieldName); ok {
		record.GitDirectoryName = strings.TrimSpace(value)
	}
	if value, ok := targetPickerAnswerValue(answers, control.FeishuTargetPickerWorktreeBranchFieldName); ok {
		record.WorktreeBranchName = strings.TrimSpace(value)
	}
	if value, ok := targetPickerAnswerValue(answers, control.FeishuTargetPickerWorktreeDirectoryFieldName); ok {
		record.WorktreeDirectoryName = strings.TrimSpace(value)
	}
}

func targetPickerAnswerValue(answers map[string][]string, key string) (string, bool) {
	values, ok := answers[strings.TrimSpace(key)]
	if !ok {
		return "", false
	}
	if len(values) == 0 {
		return "", true
	}
	return strings.TrimSpace(values[0]), true
}

func (s *Service) handleTargetPickerOpenPathPicker(surface *state.SurfaceConsoleRecord, pickerID, fieldKind, actorUserID string, answers map[string][]string) []eventcontract.Event {
	record, blocked := s.requireActiveTargetPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	resetTargetPickerEditingState(record)
	s.applyTargetPickerDraftAnswers(record, answers)
	switch strings.TrimSpace(fieldKind) {
	case control.FeishuTargetPickerPathFieldLocalDirectory, control.FeishuTargetPickerPathFieldGitParentDir:
	default:
		return notice(surface, "target_picker_selection_missing", "当前要选择的目录字段无效，请重新打开卡片。")
	}
	return s.openTargetPickerAddWorkspacePathPicker(surface, record, fieldKind)
}

func (s *Service) openTargetPickerAddWorkspacePathPicker(surface *state.SurfaceConsoleRecord, record *activeTargetPickerRecord, fieldKind string) []eventcontract.Event {
	if surface == nil || record == nil {
		return nil
	}
	rootPath, initialPath := workspacePickerPaths(s.surfaceCurrentWorkspaceKey(surface))
	title := "选择工作区与会话"
	stageLabel := ""
	question := ""
	hint := ""
	confirmLabel := "使用这个目录"
	cancelLabel := "返回"
	reqEntryFilterKind := ""
	switch strings.TrimSpace(fieldKind) {
	case control.FeishuTargetPickerPathFieldLocalDirectory:
		stageLabel = "目录/选择目录"
		question = "选择要接入的目录"
		hint = "确认后会回到上一张卡片继续确认。"
		reqEntryFilterKind = targetPickerBusyWorkspacePathPickerEntryFilterKind
		if current := strings.TrimSpace(record.LocalDirectoryPath); current != "" {
			initialPath = current
		}
	case control.FeishuTargetPickerPathFieldGitParentDir:
		stageLabel = "Git/选择目录"
		question = "选择仓库要落到哪个本地父目录"
		hint = "确认后会回到上一张卡片，并回填落地目录。"
		if current := strings.TrimSpace(record.GitParentDir); current != "" {
			initialPath = current
		}
	default:
		return notice(surface, "target_picker_selection_missing", "当前要选择的目录字段无效，请重新打开卡片。")
	}
	return s.openPathPickerWithInline(surface, surface.ActorUserID, control.PathPickerRequest{
		Mode:         control.PathPickerModeDirectory,
		Title:        title,
		StageLabel:   stageLabel,
		Question:     question,
		RootPath:     rootPath,
		InitialPath:  initialPath,
		Hint:         hint,
		ConfirmLabel: confirmLabel,
		CancelLabel:  cancelLabel,
		OwnerFlowID:  strings.TrimSpace(record.PickerID),
		ConsumerKind: targetPickerAddWorkspacePathPickerConsumerKind,
		ConsumerMeta: map[string]string{
			targetPickerAddWorkspaceMetaPickerID:  strings.TrimSpace(record.PickerID),
			targetPickerAddWorkspaceMetaFieldKind: strings.TrimSpace(fieldKind),
		},
		EntryFilterKind: reqEntryFilterKind,
	}, true)
}

func (targetPickerBusyWorkspacePathPickerEntryFilter) PathPickerFilterEntry(s *Service, surface *state.SurfaceConsoleRecord, _ *activePathPickerRecord, item control.FeishuPathPickerEntry, resolvedPath string) (control.FeishuPathPickerEntry, bool) {
	if s == nil || surface == nil || item.Kind != control.PathPickerEntryDirectory {
		return item, true
	}
	workspaceKey := normalizeWorkspaceClaimKey(resolvedPath)
	if workspaceKey == "" {
		return item, true
	}
	if owner := s.workspaceBusyOwnerForSurface(surface, workspaceKey); owner != nil {
		item.DisabledReason = "该目录对应的工作区当前正被其他飞书会话接管，请重新选择。"
		return item, false
	}
	return item, true
}

func (targetPickerAddWorkspacePathPickerConsumer) PathPickerConfirmed(s *Service, surface *state.SurfaceConsoleRecord, result control.PathPickerResult) []eventcontract.Event {
	if s == nil || surface == nil {
		return nil
	}
	record, ok := s.targetPickerRecordForPathReturn(surface, result.ConsumerMeta)
	if !ok {
		return notice(surface, "target_picker_expired", "原始添加工作区卡片已失效，请重新发送 /list。")
	}
	selectedPath, err := state.ResolveWorkspaceRootOnHost(result.SelectedPath)
	if err != nil {
		return s.restoreTargetPickerAfterPathReturn(surface, record, "path_picker_invalid", fmt.Sprintf("目录路径无效：%v", err))
	}
	switch strings.TrimSpace(result.ConsumerMeta[targetPickerAddWorkspaceMetaFieldKind]) {
	case control.FeishuTargetPickerPathFieldLocalDirectory:
		if owner := s.workspaceBusyOwnerForSurface(surface, normalizeWorkspaceClaimKey(selectedPath)); owner != nil {
			return s.restoreTargetPickerAfterPathReturn(surface, record, "path_picker_invalid_busy", "该目录对应的工作区当前正被其他飞书会话接管，请重新选择。")
		}
		record.LocalDirectoryPath = selectedPath
	case control.FeishuTargetPickerPathFieldGitParentDir:
		record.GitParentDir = selectedPath
	default:
		return s.restoreTargetPickerAfterPathReturn(surface, record, "target_picker_selection_missing", "当前要回填的目录字段无效，请重新打开卡片。")
	}
	return s.restoreTargetPickerAfterPathReturn(surface, record, "", "")
}

func (targetPickerAddWorkspacePathPickerConsumer) PathPickerCancelled(s *Service, surface *state.SurfaceConsoleRecord, result control.PathPickerResult) []eventcontract.Event {
	if s == nil || surface == nil {
		return nil
	}
	record, ok := s.targetPickerRecordForPathReturn(surface, result.ConsumerMeta)
	if !ok {
		return notice(surface, "target_picker_expired", "原始添加工作区卡片已失效，请重新发送 /list。")
	}
	return s.restoreTargetPickerAfterPathReturn(surface, record, "", "")
}

func (s *Service) targetPickerRecordForPathReturn(surface *state.SurfaceConsoleRecord, meta map[string]string) (*activeTargetPickerRecord, bool) {
	if surface == nil {
		return nil, false
	}
	record := s.activeTargetPicker(surface)
	if record == nil {
		return nil, false
	}
	if strings.TrimSpace(meta[targetPickerAddWorkspaceMetaPickerID]) == "" {
		return nil, false
	}
	if strings.TrimSpace(record.PickerID) != strings.TrimSpace(meta[targetPickerAddWorkspaceMetaPickerID]) {
		return nil, false
	}
	return record, true
}

func (s *Service) restoreTargetPickerAfterPathReturn(surface *state.SurfaceConsoleRecord, record *activeTargetPickerRecord, noticeCode, noticeText string) []eventcontract.Event {
	if surface == nil || record == nil {
		return nil
	}
	if strings.TrimSpace(noticeCode) != "" && strings.TrimSpace(noticeText) != "" {
		level := control.FeishuTargetPickerMessageInfo
		if strings.Contains(strings.ToLower(strings.TrimSpace(noticeCode)), "invalid") || strings.Contains(strings.ToLower(strings.TrimSpace(noticeCode)), "missing") {
			level = control.FeishuTargetPickerMessageDanger
		}
		setTargetPickerMessages(record, control.FeishuTargetPickerMessage{
			Level: level,
			Text:  strings.TrimSpace(noticeText),
		})
	} else {
		resetTargetPickerEditingState(record)
	}
	view, err := s.buildTargetPickerView(surface, record)
	if err != nil {
		return notice(surface, "target_picker_unavailable", err.Error())
	}
	// Path picker confirm/cancel callbacks are acknowledged asynchronously, so
	// this return step must patch the existing owner card by message id.
	return []eventcontract.Event{s.targetPickerViewEvent(surface, view, false)}
}

func (s *Service) buildTargetPickerLocalDirectoryState(surface *state.SurfaceConsoleRecord, record *activeTargetPickerRecord) targetPickerLocalDirectoryState {
	localState := targetPickerLocalDirectoryState{}
	if record == nil {
		return localState
	}
	selectedPath := strings.TrimSpace(record.LocalDirectoryPath)
	if selectedPath == "" {
		return localState
	}
	resolvedPath, err := state.ResolveWorkspaceRootOnHost(selectedPath)
	if err != nil {
		localState.Messages = append(localState.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  fmt.Sprintf("目录路径无效：%v", err),
		})
		return localState
	}
	localState.ResolvedPath = resolvedPath
	info, statErr := os.Stat(resolvedPath)
	switch {
	case statErr != nil:
		localState.Messages = append(localState.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  "该目录当前不可访问，请重新选择一个存在的本地目录。",
		})
		return localState
	case !info.IsDir():
		localState.Messages = append(localState.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  "当前选择不是目录，请重新选择一个本地目录。",
		})
		return localState
	}
	if owner := s.workspaceBusyOwnerForSurface(surface, normalizeWorkspaceClaimKey(resolvedPath)); owner != nil {
		localState.Messages = append(localState.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  "该目录对应的工作区当前正被其他飞书会话接管，暂时不能继续。",
		})
		return localState
	}
	if s.targetPickerDirectoryIsKnownWorkspace(surface, resolvedPath) {
		localState.Messages = append(localState.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  "该目录已经是已有工作区，请返回并通过“切换”选择要继续的会话。",
		})
		return localState
	}
	localState.CanConfirm = true
	localState.Messages = append(localState.Messages, control.FeishuTargetPickerMessage{
		Level: control.FeishuTargetPickerMessageInfo,
		Text:  "将以这个目录接入工作区，并进入新会话。",
	})
	return localState
}

func (s *Service) targetPickerDirectoryIsKnownWorkspace(surface *state.SurfaceConsoleRecord, workspaceKey string) bool {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if workspaceKey == "" {
		return false
	}
	if normalizeWorkspaceClaimKey(s.surfaceCurrentWorkspaceKey(surface)) == workspaceKey {
		return true
	}
	if s.resolveWorkspaceAttachInstance(surface, workspaceKey) != nil {
		return true
	}
	for _, view := range s.mergedThreadViews(surface) {
		if normalizeWorkspaceClaimKey(mergedThreadWorkspaceClaimKey(view)) == workspaceKey {
			return true
		}
	}
	_, ok := s.catalog.recentPersistedWorkspaces(persistedRecentWorkspaceLimit)[workspaceKey]
	return ok
}

func (s *Service) buildTargetPickerGitImportState(record *activeTargetPickerRecord) targetPickerGitImportState {
	gitState := targetPickerGitImportState{}
	if record == nil {
		return gitState
	}
	parentDir := strings.TrimSpace(record.GitParentDir)
	repoURL := strings.TrimSpace(record.GitRepoURL)
	directoryName := strings.TrimSpace(record.GitDirectoryName)
	if !s.config.GitAvailable {
		gitState.Messages = append(gitState.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  "当前机器未检测到 `git`，暂时不能直接从 Git URL 导入。",
		})
		return gitState
	}
	if parentDir == "" {
		return gitState
	}
	dirEntries, err := os.ReadDir(parentDir)
	if err != nil {
		gitState.Messages = append(gitState.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  "落地目录当前不可访问，请重新选择一个本地父目录。",
		})
		return gitState
	}
	gitState.ParentDir = parentDir
	if len(dirEntries) != 0 {
		gitState.Messages = append(gitState.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageWarning,
			Text:  "该目录下已有其他内容；导入时会在其中创建新的子目录。",
		})
	}
	if repoURL == "" && directoryName == "" {
		return gitState
	}
	previewRepo := repoURL
	if previewRepo == "" {
		previewRepo = "preview"
	}
	preview, previewErr := workspaceimport.Preview(workspaceimport.ImportRequest{
		RepoURL:       previewRepo,
		ParentDir:     parentDir,
		DirectoryName: directoryName,
	})
	if previewErr == nil {
		gitState.FinalPath = state.NormalizeWorkspaceKey(preview.DestinationPath)
		gitState.CanConfirm = repoURL != ""
		return gitState
	}
	var importErr *workspaceimport.ImportError
	if ok := errorAsImport(previewErr, &importErr); !ok || importErr == nil {
		gitState.Messages = append(gitState.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  "无法预检查最终路径，请重新确认落地目录和目录名。",
		})
		return gitState
	}
	if strings.TrimSpace(importErr.DestinationPath) != "" {
		gitState.FinalPath = state.NormalizeWorkspaceKey(importErr.DestinationPath)
	}
	switch importErr.Code {
	case workspaceimport.ImportErrorDestinationExists:
		destinationPath := strings.TrimSpace(firstNonEmpty(gitState.FinalPath, state.NormalizeWorkspaceKey(importErr.DestinationPath)))
		gitState.Messages = append(gitState.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  fmt.Sprintf("目标目录已存在：%s。请更换落地目录或本地目录名。", destinationPath),
		})
	case workspaceimport.ImportErrorInvalidDirectoryName:
		gitState.Messages = append(gitState.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  "本地目录名无效，请改成不含路径分隔符的普通目录名。",
		})
	default:
		gitState.Messages = append(gitState.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  "无法预检查最终路径，请重新确认落地目录和目录名。",
		})
	}
	return gitState
}

func errorAsImport(err error, target **workspaceimport.ImportError) bool {
	if err == nil || target == nil {
		return false
	}
	return errors.As(err, target)
}

func (s *Service) confirmTargetPickerLocalDirectory(surface *state.SurfaceConsoleRecord, flow *activeOwnerCardFlowRecord, record *activeTargetPickerRecord, view control.FeishuTargetPickerView) []eventcontract.Event {
	if surface == nil || record == nil {
		return nil
	}
	localState := s.buildTargetPickerLocalDirectoryState(surface, record)
	if !localState.CanConfirm || strings.TrimSpace(localState.ResolvedPath) == "" {
		message := targetPickerFirstBlockingMessage(localState.Messages)
		if message == "" {
			message = "请先选择一个可接入的本地目录。"
		}
		setTargetPickerMessages(record, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  message,
		})
		updatedView, err := s.buildTargetPickerView(surface, record)
		if err != nil {
			return notice(surface, "target_picker_unavailable", err.Error())
		}
		return []eventcontract.Event{s.targetPickerViewEvent(surface, updatedView, false)}
	}
	events := s.enterTargetPickerNewThread(surface, localState.ResolvedPath)
	filtered := targetPickerFilteredFollowupEvents(events)
	if targetPickerNewThreadReady(surface, localState.ResolvedPath) {
		return s.finishTargetPickerWithStage(surface, flow, record, control.FeishuTargetPickerStageSucceeded, "已进入新会话待命", "工作区已经准备完成，下一条文本会直接开启新会话。", false, filtered)
	}
	if surface.PendingHeadless != nil && surface.PendingHeadless.PrepareNewThread &&
		normalizeWorkspaceClaimKey(surface.PendingHeadless.ThreadCWD) == normalizeWorkspaceClaimKey(localState.ResolvedPath) {
		status := targetPickerLocalDirectoryProcessingStatus(localState.ResolvedPath)
		processing := s.startTargetPickerProcessingWithSections(
			surface,
			flow,
			record,
			targetPickerPendingNewThread,
			localState.ResolvedPath,
			"",
			"正在接入工作区",
			"",
			status.Sections,
			status.Footer,
		)
		return append(processing, filtered...)
	}
	failureText := strings.TrimSpace(firstNonEmpty(targetPickerFirstNoticeText(events), "当前目录暂时无法接入为工作区，请重新选择后再试。"))
	return s.finishTargetPickerWithStage(surface, flow, record, control.FeishuTargetPickerStageFailed, "接入失败", failureText, false, filtered)
}

func (s *Service) confirmTargetPickerGitImport(surface *state.SurfaceConsoleRecord, flow *activeOwnerCardFlowRecord, record *activeTargetPickerRecord, view control.FeishuTargetPickerView) []eventcontract.Event {
	if surface == nil || record == nil {
		return nil
	}
	gitState := s.buildTargetPickerGitImportState(record)
	if !gitState.CanConfirm || strings.TrimSpace(gitState.ParentDir) == "" {
		if message := targetPickerGitImportValidationMessage(record, gitState.Messages); message != "" &&
			!targetPickerHasBlockingMessage(view.SourceMessages, message) {
			view.SourceMessages = append([]control.FeishuTargetPickerMessage{{
				Level: control.FeishuTargetPickerMessageDanger,
				Text:  message,
			}}, view.SourceMessages...)
		}
		return []eventcontract.Event{s.targetPickerViewEvent(surface, view, false)}
	}
	finalPath := strings.TrimSpace(firstNonEmpty(gitState.FinalPath, gitState.ParentDir))
	record.GitFinalPath = finalPath
	status := targetPickerGitImportCloneProcessingStatus(strings.TrimSpace(record.GitRepoURL), finalPath)
	processing := s.startTargetPickerProcessingWithSections(
		surface,
		flow,
		record,
		targetPickerPendingGitImport,
		finalPath,
		"",
		"正在导入 Git 工作区",
		"",
		status.Sections,
		status.Footer,
	)
	return append(processing,
		eventcontract.Event{
			Kind:             eventcontract.KindDaemonCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			DaemonCommand: &control.DaemonCommand{
				Kind:             control.DaemonCommandGitWorkspaceImport,
				SurfaceSessionID: surface.SurfaceSessionID,
				PickerID:         strings.TrimSpace(record.PickerID),
				LocalPath:        strings.TrimSpace(gitState.ParentDir),
				RepoURL:          strings.TrimSpace(record.GitRepoURL),
				DirectoryName:    strings.TrimSpace(record.GitDirectoryName),
			},
		},
	)
}

func targetPickerFirstBlockingMessage(messages []control.FeishuTargetPickerMessage) string {
	for _, message := range messages {
		if message.Level == control.FeishuTargetPickerMessageDanger && strings.TrimSpace(message.Text) != "" {
			return strings.TrimSpace(message.Text)
		}
	}
	return ""
}

func targetPickerHasBlockingMessage(messages []control.FeishuTargetPickerMessage, text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	for _, message := range messages {
		if message.Level != control.FeishuTargetPickerMessageDanger {
			continue
		}
		if strings.TrimSpace(message.Text) == text {
			return true
		}
	}
	return false
}

func targetPickerGitImportValidationMessage(record *activeTargetPickerRecord, messages []control.FeishuTargetPickerMessage) string {
	if message := targetPickerFirstBlockingMessage(messages); message != "" {
		return message
	}
	missingParentDir := strings.TrimSpace(record.GitParentDir) == ""
	missingRepoURL := strings.TrimSpace(record.GitRepoURL) == ""
	switch {
	case missingParentDir && missingRepoURL:
		return "请先补全落地目录和 Git 仓库地址。"
	case missingParentDir:
		return "请先选择落地目录。"
	case missingRepoURL:
		return "请先填写 Git 仓库地址。"
	default:
		return "当前 Git 工作区配置还不能执行，请先修正阻塞项。"
	}
}
