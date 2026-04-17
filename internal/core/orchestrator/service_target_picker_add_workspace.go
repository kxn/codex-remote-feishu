package orchestrator

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/gitworkspace"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	targetPickerAddWorkspacePathPickerConsumerKind = "target_picker_add_workspace"
	targetPickerAddWorkspaceMetaPickerID           = "picker_id"
	targetPickerAddWorkspaceMetaFieldKind          = "field_kind"
)

type targetPickerAddWorkspacePathPickerConsumer struct{}

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

func (s *Service) handleTargetPickerOpenPathPicker(surface *state.SurfaceConsoleRecord, pickerID, fieldKind, actorUserID string, answers map[string][]string) []control.UIEvent {
	record, blocked := s.requireActiveTargetPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	s.applyTargetPickerDraftAnswers(record, answers)
	switch strings.TrimSpace(fieldKind) {
	case control.FeishuTargetPickerPathFieldLocalDirectory, control.FeishuTargetPickerPathFieldGitParentDir:
	default:
		return notice(surface, "target_picker_selection_missing", "当前要选择的目录字段无效，请重新打开卡片。")
	}
	return s.openTargetPickerAddWorkspacePathPicker(surface, record, fieldKind)
}

func (s *Service) openTargetPickerAddWorkspacePathPicker(surface *state.SurfaceConsoleRecord, record *activeTargetPickerRecord, fieldKind string) []control.UIEvent {
	if surface == nil || record == nil {
		return nil
	}
	rootPath, initialPath := workspacePickerPaths(s.surfaceCurrentWorkspaceKey(surface))
	title := "选择目录"
	hint := ""
	confirmLabel := "使用这个目录"
	cancelLabel := "返回"
	switch strings.TrimSpace(fieldKind) {
	case control.FeishuTargetPickerPathFieldLocalDirectory:
		title = "选择目录路径"
		hint = "选择本机上已有的目录。确认后会回到主卡，你再决定是否继续接入。"
		if current := strings.TrimSpace(record.LocalDirectoryPath); current != "" {
			initialPath = current
		}
	case control.FeishuTargetPickerPathFieldGitParentDir:
		title = "选择落地目录"
		hint = "选择仓库要克隆到哪个本地父目录。确认后会回到主卡并回填落地目录。"
		if current := strings.TrimSpace(record.GitParentDir); current != "" {
			initialPath = current
		}
	default:
		return notice(surface, "target_picker_selection_missing", "当前要选择的目录字段无效，请重新打开卡片。")
	}
	return s.openPathPickerWithInline(surface, surface.ActorUserID, control.PathPickerRequest{
		Mode:         control.PathPickerModeDirectory,
		Title:        title,
		RootPath:     rootPath,
		InitialPath:  initialPath,
		Hint:         hint,
		ConfirmLabel: confirmLabel,
		CancelLabel:  cancelLabel,
		ConsumerKind: targetPickerAddWorkspacePathPickerConsumerKind,
		ConsumerMeta: map[string]string{
			targetPickerAddWorkspaceMetaPickerID:  strings.TrimSpace(record.PickerID),
			targetPickerAddWorkspaceMetaFieldKind: strings.TrimSpace(fieldKind),
		},
	}, true)
}

func (targetPickerAddWorkspacePathPickerConsumer) PathPickerConfirmed(s *Service, surface *state.SurfaceConsoleRecord, result control.PathPickerResult) []control.UIEvent {
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
	case control.FeishuTargetPickerPathFieldGitParentDir:
		record.GitParentDir = selectedPath
	default:
		return s.restoreTargetPickerAfterPathReturn(surface, record, "target_picker_selection_missing", "当前要回填的目录字段无效，请重新打开卡片。")
	}
	return s.restoreTargetPickerAfterPathReturn(surface, record, "", "")
}

func (targetPickerAddWorkspacePathPickerConsumer) PathPickerCancelled(s *Service, surface *state.SurfaceConsoleRecord, result control.PathPickerResult) []control.UIEvent {
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

func (s *Service) restoreTargetPickerAfterPathReturn(surface *state.SurfaceConsoleRecord, record *activeTargetPickerRecord, noticeCode, noticeText string) []control.UIEvent {
	if surface == nil || record == nil {
		return nil
	}
	view, err := s.buildTargetPickerView(surface, record)
	if err != nil {
		if noticeCode == "" {
			return notice(surface, "target_picker_unavailable", err.Error())
		}
		return append([]control.UIEvent{}, notice(surface, noticeCode, noticeText)...)
	}
	events := []control.UIEvent{s.targetPickerViewEvent(surface, view, true)}
	if strings.TrimSpace(noticeCode) != "" && strings.TrimSpace(noticeText) != "" {
		events = append(events, notice(surface, noticeCode, noticeText)...)
	}
	return events
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
	if owner := s.workspaceBusyOwnerForSurface(surface, resolvedPath); owner != nil {
		localState.Messages = append(localState.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  "该目录对应的工作区当前正被其他飞书会话接管，暂时不能继续。",
		})
		return localState
	}
	localState.CanConfirm = true
	if s.targetPickerDirectoryIsKnownWorkspace(surface, resolvedPath) {
		localState.Messages = append(localState.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageInfo,
			Text:  "该目录已是已有工作区。继续后会复用该工作区并进入新会话，不会重复创建。",
		})
		return localState
	}
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
	_, ok := s.recentPersistedWorkspaces(persistedRecentWorkspaceLimit)[workspaceKey]
	return ok
}

func (s *Service) buildTargetPickerGitImportState(record *activeTargetPickerRecord) targetPickerGitImportState {
	state := targetPickerGitImportState{}
	if record == nil {
		return state
	}
	parentDir := strings.TrimSpace(record.GitParentDir)
	repoURL := strings.TrimSpace(record.GitRepoURL)
	directoryName := strings.TrimSpace(record.GitDirectoryName)
	if !s.config.GitAvailable {
		state.Messages = append(state.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  "当前机器未检测到 `git`，暂时不能直接从 Git URL 导入。",
		})
		return state
	}
	if parentDir == "" {
		return state
	}
	dirEntries, err := os.ReadDir(parentDir)
	if err != nil {
		state.Messages = append(state.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  "落地目录当前不可访问，请重新选择一个本地父目录。",
		})
		return state
	}
	state.ParentDir = parentDir
	if len(dirEntries) != 0 {
		state.Messages = append(state.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageWarning,
			Text:  "该目录下已有其他内容；导入时会在其中创建新的子目录。",
		})
	}
	if repoURL == "" && directoryName == "" {
		return state
	}
	previewRepo := repoURL
	if previewRepo == "" {
		previewRepo = "preview"
	}
	preview, previewErr := gitworkspace.Preview(gitworkspace.ImportRequest{
		RepoURL:       previewRepo,
		ParentDir:     parentDir,
		DirectoryName: directoryName,
	})
	if previewErr == nil {
		state.FinalPath = preview.DestinationPath
		state.CanConfirm = repoURL != ""
		return state
	}
	var importErr *gitworkspace.ImportError
	if ok := errorAsImport(previewErr, &importErr); !ok || importErr == nil {
		state.Messages = append(state.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  "无法预检查最终路径，请重新确认落地目录和目录名。",
		})
		return state
	}
	if strings.TrimSpace(importErr.DestinationPath) != "" {
		state.FinalPath = strings.TrimSpace(importErr.DestinationPath)
	}
	switch importErr.Code {
	case gitworkspace.ImportErrorDestinationExists:
		state.Messages = append(state.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  fmt.Sprintf("目标目录已存在：%s。请更换落地目录或本地目录名。", strings.TrimSpace(importErr.DestinationPath)),
		})
	case gitworkspace.ImportErrorInvalidDirectoryName:
		state.Messages = append(state.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  "本地目录名无效，请改成不含路径分隔符的普通目录名。",
		})
	default:
		state.Messages = append(state.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  "无法预检查最终路径，请重新确认落地目录和目录名。",
		})
	}
	return state
}

func errorAsImport(err error, target **gitworkspace.ImportError) bool {
	if err == nil || target == nil {
		return false
	}
	return errors.As(err, target)
}

func (s *Service) confirmTargetPickerLocalDirectory(surface *state.SurfaceConsoleRecord, record *activeTargetPickerRecord, view control.FeishuTargetPickerView) []control.UIEvent {
	if surface == nil || record == nil {
		return nil
	}
	localState := s.buildTargetPickerLocalDirectoryState(surface, record)
	if !localState.CanConfirm || strings.TrimSpace(localState.ResolvedPath) == "" {
		if message := targetPickerFirstBlockingMessage(localState.Messages); message != "" {
			return notice(surface, "workspace_create_invalid", message)
		}
		return notice(surface, "workspace_create_invalid", "请先选择一个可接入的本地目录。")
	}
	events := s.enterTargetPickerNewThread(surface, localState.ResolvedPath)
	if targetPickerNewThreadSucceeded(surface, localState.ResolvedPath) {
		s.clearSurfaceTargetPicker(surface)
	}
	return events
}

func (s *Service) confirmTargetPickerGitImport(surface *state.SurfaceConsoleRecord, record *activeTargetPickerRecord, view control.FeishuTargetPickerView) []control.UIEvent {
	if surface == nil || record == nil {
		return nil
	}
	gitState := s.buildTargetPickerGitImportState(record)
	if !gitState.CanConfirm || strings.TrimSpace(gitState.ParentDir) == "" {
		if message := targetPickerFirstBlockingMessage(gitState.Messages); message != "" {
			return notice(surface, "git_import_clone_failed", message)
		}
		if strings.TrimSpace(record.GitParentDir) == "" || strings.TrimSpace(record.GitRepoURL) == "" {
			return notice(surface, "git_import_clone_failed", "请先补全落地目录和 Git 仓库地址。")
		}
		return notice(surface, "git_import_clone_failed", "当前 Git 工作区配置还不能执行，请先修正阻塞项。")
	}
	finalPath := strings.TrimSpace(firstNonEmpty(gitState.FinalPath, gitState.ParentDir))
	noticeText := fmt.Sprintf("正在把 `%s` 拉取到 `%s`，完成后会直接进入新会话待命。", strings.TrimSpace(record.GitRepoURL), finalPath)
	return []control.UIEvent{
		{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:  "git_import_starting",
				Title: "正在导入 Git 工作区",
				Text:  noticeText,
			},
		},
		{
			Kind:             control.UIEventDaemonCommand,
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
	}
}

func targetPickerFirstBlockingMessage(messages []control.FeishuTargetPickerMessage) string {
	for _, message := range messages {
		if message.Level == control.FeishuTargetPickerMessageDanger && strings.TrimSpace(message.Text) != "" {
			return strings.TrimSpace(message.Text)
		}
	}
	return ""
}
