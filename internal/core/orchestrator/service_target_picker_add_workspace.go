package orchestrator

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/workspaceimport"
)

const (
	targetPickerAddWorkspacePathPickerConsumerKind = "target_picker_add_workspace"
	targetPickerAddWorkspaceMetaPickerID           = "picker_id"
	targetPickerAddWorkspaceMetaFieldKind          = "field_kind"
)

type targetPickerAddWorkspacePathPickerConsumer struct{}

type targetPickerLocalDirectoryState struct {
	ResolvedPath string
	FinalPath    string
	Checked      bool
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
	if value, ok := targetPickerAnswerValue(answers, control.FeishuTargetPickerLocalDirectoryNameFieldName); ok {
		record.LocalDirectoryName = strings.TrimSpace(value)
		record.LocalDirectoryChecked = false
		record.LocalDirectoryFinalPath = ""
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
	switch strings.TrimSpace(fieldKind) {
	case control.FeishuTargetPickerPathFieldLocalDirectory:
		stageLabel = "目录/选择目录"
		question = "选择要接入的目录"
		hint = "确认后会回到上一张卡片继续确认。"
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
	}, true)
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
		record.LocalDirectoryPath = selectedPath
		record.LocalDirectoryChecked = false
		record.LocalDirectoryFinalPath = ""
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
	finalPath := normalizeWorkspaceClaimKey(resolvedPath)
	if finalPath == "" {
		finalPath = state.NormalizeWorkspaceKey(resolvedPath)
	}
	directoryName := strings.TrimSpace(record.LocalDirectoryName)
	if directoryName != "" {
		if err := workspaceimport.ValidateDirectoryName(directoryName); err != nil {
			localState.Messages = append(localState.Messages, control.FeishuTargetPickerMessage{
				Level: control.FeishuTargetPickerMessageDanger,
				Text:  "本地目录名无效，请改成不含路径分隔符的普通目录名。",
			})
			return localState
		}
		finalPath = state.NormalizeWorkspaceKey(filepath.Join(resolvedPath, directoryName))
	}
	localState.FinalPath = finalPath
	localState.Checked = strings.TrimSpace(record.LocalDirectoryFinalPath) != "" &&
		normalizeWorkspaceClaimKey(record.LocalDirectoryFinalPath) == normalizeWorkspaceClaimKey(finalPath) &&
		record.LocalDirectoryChecked
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
	if !localState.Checked {
		return localState
	}
	if owner := s.workspaceBusyOwnerForSurface(surface, normalizeWorkspaceClaimKey(finalPath)); owner != nil {
		text := "该目录对应的工作区当前正被其他飞书会话接管，暂时不能继续。"
		if directoryName != "" {
			text = "该目标目录对应的工作区当前正被其他飞书会话接管，暂时不能继续。"
		}
		localState.Messages = append(localState.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  text,
		})
		return localState
	}
	localState.CanConfirm = true
	if directoryName != "" {
		if _, err := os.Stat(finalPath); err == nil {
			localState.CanConfirm = false
			if s.targetPickerDirectoryIsKnownWorkspace(surface, finalPath) {
				localState.Messages = append(localState.Messages, control.FeishuTargetPickerMessage{
					Level: control.FeishuTargetPickerMessageDanger,
					Text:  "这个目录已经是已接入工作区，不能再次新建。请直接切换到该工作区，或重新选择其他目录。",
				})
				return localState
			}
			localState.Messages = append(localState.Messages, control.FeishuTargetPickerMessage{
				Level: control.FeishuTargetPickerMessageDanger,
				Text:  "目标目录已存在，且不适合用于新建工作区。请修改目录名，或重新选择上级目录。",
			})
			return localState
		} else if !os.IsNotExist(err) {
			localState.CanConfirm = false
			localState.Messages = append(localState.Messages, control.FeishuTargetPickerMessage{
				Level: control.FeishuTargetPickerMessageDanger,
				Text:  "目标目录当前不可访问，请稍后重试或重新选择目录。",
			})
			return localState
		}
		localState.Messages = append(localState.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageInfo,
			Text:  "将先创建这个目录，再以它接入工作区并进入新会话。",
		})
		return localState
	}
	if s.targetPickerDirectoryIsKnownWorkspace(surface, finalPath) {
		localState.Messages = append(localState.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageInfo,
			Text:  "该目录已经是已有工作区；继续后会直接切到该工作区，并进入新会话待命。",
		})
		return localState
	}
	localState.Messages = append(localState.Messages, control.FeishuTargetPickerMessage{
		Level: control.FeishuTargetPickerMessageInfo,
		Text:  "将以这个目录接入工作区，并进入新会话。",
	})
	return localState
}

func (s *Service) evaluateCheckedTargetPickerLocalDirectoryState(surface *state.SurfaceConsoleRecord, record *activeTargetPickerRecord, finalPath string) targetPickerLocalDirectoryState {
	if record == nil {
		return targetPickerLocalDirectoryState{}
	}
	previousChecked := record.LocalDirectoryChecked
	previousFinalPath := record.LocalDirectoryFinalPath
	record.LocalDirectoryChecked = true
	record.LocalDirectoryFinalPath = strings.TrimSpace(finalPath)
	localState := s.buildTargetPickerLocalDirectoryState(surface, record)
	record.LocalDirectoryChecked = previousChecked
	record.LocalDirectoryFinalPath = previousFinalPath
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
	if backend, filterByBackend := s.normalModeThreadBackend(surface); filterByBackend {
		if len(s.workspaceOnlineInstancesForBackend(workspaceKey, backend)) != 0 {
			return true
		}
	} else if len(s.workspaceOnlineInstances(workspaceKey)) != 0 {
		return true
	}
	for _, view := range s.mergedThreadViews(surface) {
		if normalizeWorkspaceClaimKey(mergedThreadWorkspaceClaimKey(view)) == workspaceKey {
			return true
		}
	}
	workspaces := s.catalog.recentPersistedWorkspaces(persistedRecentWorkspaceLimit)
	if backend, filterByBackend := s.normalModeThreadBackend(surface); filterByBackend {
		workspaces = s.catalog.recentPersistedWorkspacesForBackend(backend, persistedRecentWorkspaceLimit)
	}
	_, ok := workspaces[workspaceKey]
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
	if !localState.Checked {
		checkedState := s.evaluateCheckedTargetPickerLocalDirectoryState(surface, record, localState.FinalPath)
		record.LocalDirectoryFinalPath = strings.TrimSpace(checkedState.FinalPath)
		record.LocalDirectoryChecked = checkedState.CanConfirm
		if !checkedState.CanConfirm || strings.TrimSpace(checkedState.FinalPath) == "" {
			record.LocalDirectoryChecked = false
			message := targetPickerFirstBlockingMessage(localState.Messages)
			if message == "" {
				message = targetPickerFirstBlockingMessage(checkedState.Messages)
			}
			if message == "" {
				message = "请先选择目录，并补全可用的目标目录。"
			}
			setTargetPickerMessages(record, control.FeishuTargetPickerMessage{
				Level: control.FeishuTargetPickerMessageDanger,
				Text:  message,
			})
		} else {
			resetTargetPickerEditingState(record)
		}
		updatedView, err := s.buildTargetPickerView(surface, record)
		if err != nil {
			return notice(surface, "target_picker_unavailable", err.Error())
		}
		return []eventcontract.Event{s.targetPickerViewEvent(surface, updatedView, false)}
	}
	localState = s.buildTargetPickerLocalDirectoryState(surface, record)
	if !localState.CanConfirm || strings.TrimSpace(localState.FinalPath) == "" {
		message := targetPickerFirstBlockingMessage(localState.Messages)
		if message == "" {
			message = "请先检查目标目录，并修正阻塞项。"
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
	finalPath := strings.TrimSpace(localState.FinalPath)
	if strings.TrimSpace(record.LocalDirectoryName) != "" {
		if err := os.MkdirAll(finalPath, 0o755); err != nil {
			setTargetPickerMessages(record, control.FeishuTargetPickerMessage{
				Level: control.FeishuTargetPickerMessageDanger,
				Text:  "创建目标目录失败，请检查权限后重试。",
			})
			updatedView, viewErr := s.buildTargetPickerView(surface, record)
			if viewErr != nil {
				return notice(surface, "target_picker_unavailable", viewErr.Error())
			}
			return []eventcontract.Event{s.targetPickerViewEvent(surface, updatedView, false)}
		}
	}
	events := s.enterTargetPickerNewThread(surface, finalPath)
	filtered := targetPickerFilteredFollowupEvents(events)
	if targetPickerNewThreadReady(surface, finalPath) {
		return s.finishTargetPickerWithStage(surface, flow, record, control.FeishuTargetPickerStageSucceeded, "已进入新会话待命", "工作区已经准备完成，下一条文本会直接开启新会话。", false, filtered)
	}
	if surface.PendingHeadless != nil && surface.PendingHeadless.PrepareNewThread &&
		normalizeWorkspaceClaimKey(firstNonEmpty(surface.PendingHeadless.WorkspaceKey, surface.PendingHeadless.ThreadCWD)) == normalizeWorkspaceClaimKey(finalPath) {
		status := targetPickerLocalDirectoryProcessingStatus(finalPath)
		processing := s.startTargetPickerProcessingWithSections(
			surface,
			flow,
			record,
			targetPickerPendingNewThread,
			finalPath,
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
