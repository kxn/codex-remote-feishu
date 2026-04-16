package orchestrator

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	targetPickerGitImportLocalRequestKind       = "target_picker_git_import"
	targetPickerGitImportPathPickerConsumerKind = "target_picker_git_import"

	targetPickerGitImportFieldRepoURL       = "repo_url"
	targetPickerGitImportFieldBranchOrTag   = "branch_or_tag"
	targetPickerGitImportFieldDirectoryName = "directory_name"

	targetPickerGitImportMetaPickerID      = "picker_id"
	targetPickerGitImportMetaRepoURL       = "repo_url"
	targetPickerGitImportMetaBranchOrTag   = "branch_or_tag"
	targetPickerGitImportMetaDirectoryName = "directory_name"
)

type targetPickerGitImportPathPickerConsumer struct{}

func (s *Service) nextLocalRequestToken(prefix string) string {
	s.nextLocalRequestID++
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "local-request"
	}
	return fmt.Sprintf("%s-%d", prefix, s.nextLocalRequestID)
}

func (s *Service) openTargetPickerGitImportPrompt(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface == nil {
		return nil
	}
	if surface.PendingRequests == nil {
		surface.PendingRequests = map[string]*state.RequestPromptRecord{}
	}
	pickerID := ""
	if surface.ActiveTargetPicker != nil {
		pickerID = strings.TrimSpace(surface.ActiveTargetPicker.PickerID)
	}
	requestID := s.nextLocalRequestToken("git-import")
	record := &state.RequestPromptRecord{
		RequestID:   requestID,
		RequestType: "request_user_input",
		Title:       "填写 Git 仓库信息",
		Body:        "先填写仓库地址。下一步会让你选择把仓库克隆到哪个本地父目录下。",
		Options: []state.RequestPromptOptionRecord{
			{OptionID: "cancel", Label: "取消", Style: "default"},
		},
		Questions: []state.RequestPromptQuestionRecord{
			{
				ID:             targetPickerGitImportFieldRepoURL,
				Header:         "Git URL",
				Question:       "填写要导入的 Git 仓库地址",
				Placeholder:    "https://github.com/org/repo.git 或 git@github.com:org/repo.git",
				DirectResponse: false,
			},
			{
				ID:             targetPickerGitImportFieldBranchOrTag,
				Header:         "分支或标签（可选）",
				Question:       "如需固定到特定分支或标签，可在这里填写",
				Placeholder:    "例如 release/1.5 或 v1.5.0",
				Optional:       true,
				DirectResponse: false,
			},
			{
				ID:             targetPickerGitImportFieldDirectoryName,
				Header:         "目录名（可选）",
				Question:       "如不填写，将根据仓库地址自动推导目录名",
				Placeholder:    "例如 codex-remote-feishu",
				Optional:       true,
				DirectResponse: false,
			},
		},
		LocalKind: targetPickerGitImportLocalRequestKind,
		LocalMeta: map[string]string{
			targetPickerGitImportMetaPickerID: pickerID,
		},
		CardRevision: 1,
		CreatedAt:    s.now(),
	}
	surface.PendingRequests[requestID] = record
	return []control.UIEvent{s.requestPromptEvent(surface, record, "")}
}

func (s *Service) respondLocalRequest(surface *state.SurfaceConsoleRecord, request *state.RequestPromptRecord, action control.Action) []control.UIEvent {
	if surface == nil || request == nil {
		return nil
	}
	switch strings.TrimSpace(request.LocalKind) {
	case targetPickerGitImportLocalRequestKind:
		return s.respondTargetPickerGitImportRequest(surface, request, action)
	default:
		return notice(surface, "request_unsupported", "当前本地交互请求暂不支持继续处理。")
	}
}

func (s *Service) respondTargetPickerGitImportRequest(surface *state.SurfaceConsoleRecord, request *state.RequestPromptRecord, action control.Action) []control.UIEvent {
	optionID := control.NormalizeRequestOptionID(action.RequestOptionID)
	if optionID == "cancel" {
		delete(surface.PendingRequests, request.RequestID)
		return notice(surface, "git_import_cancelled", "已取消 Git 仓库导入。当前工作目标保持不变。")
	}
	_, complete, missingLabels, errText := buildRequestUserInputResponse(request, action.RequestAnswers, false)
	if errText != "" {
		return notice(surface, "request_invalid", errText)
	}
	if !complete {
		if len(action.RequestAnswers) == 0 {
			if len(missingLabels) != 0 {
				return notice(surface, "request_invalid", fmt.Sprintf("问题“%s”还没有填写答案。", missingLabels[0]))
			}
			return notice(surface, "request_invalid", "当前没有可提交的答案。")
		}
		bumpRequestCardRevision(request)
		if len(missingLabels) == 0 {
			return s.requestPromptRefreshWithNotice(surface, request, "request_saved", "已记录当前答案，请继续补充其他内容后再提交。")
		}
		return s.requestPromptRefreshWithNotice(surface, request, "request_saved", fmt.Sprintf("已记录当前答案。还差 %d 个必填项。", len(missingLabels)))
	}

	repoURL := strings.TrimSpace(request.DraftAnswers[targetPickerGitImportFieldRepoURL])
	if repoURL == "" {
		return notice(surface, "git_import_invalid_url", "Git URL 不能为空，请填写后再继续。")
	}
	delete(surface.PendingRequests, request.RequestID)

	initialPath := s.surfaceCurrentWorkspaceKey(surface)
	if strings.TrimSpace(initialPath) == "" {
		initialPath = string(filepath.Separator)
	}
	meta := cloneStringMap(request.LocalMeta)
	meta[targetPickerGitImportMetaRepoURL] = repoURL
	if branchOrTag := strings.TrimSpace(request.DraftAnswers[targetPickerGitImportFieldBranchOrTag]); branchOrTag != "" {
		meta[targetPickerGitImportMetaBranchOrTag] = branchOrTag
	}
	if directoryName := strings.TrimSpace(request.DraftAnswers[targetPickerGitImportFieldDirectoryName]); directoryName != "" {
		meta[targetPickerGitImportMetaDirectoryName] = directoryName
	}
	return s.openPathPicker(surface, surface.ActorUserID, control.PathPickerRequest{
		Mode:         control.PathPickerModeDirectory,
		Title:        "选择仓库落地父目录",
		RootPath:     string(filepath.Separator),
		InitialPath:  initialPath,
		Hint:         "请选择一个父目录。仓库会在这个目录下创建新的子目录，完成后直接进入新会话待命。",
		ConfirmLabel: "克隆到这里",
		CancelLabel:  "取消",
		ConsumerKind: targetPickerGitImportPathPickerConsumerKind,
		ConsumerMeta: meta,
	})
}

func (targetPickerGitImportPathPickerConsumer) PathPickerConfirmed(_ *Service, surface *state.SurfaceConsoleRecord, result control.PathPickerResult) []control.UIEvent {
	if surface == nil {
		return nil
	}
	repoURL := strings.TrimSpace(result.ConsumerMeta[targetPickerGitImportMetaRepoURL])
	if repoURL == "" {
		return notice(surface, "git_import_invalid_url", "Git 仓库地址缺失，请重新开始导入流程。")
	}
	noticeText := fmt.Sprintf("正在把 `%s` 拉取到 `%s` 下，完成后会直接进入新会话待命。", repoURL, result.SelectedPath)
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
				PickerID:         strings.TrimSpace(result.ConsumerMeta[targetPickerGitImportMetaPickerID]),
				LocalPath:        strings.TrimSpace(result.SelectedPath),
				RepoURL:          repoURL,
				RefName:          strings.TrimSpace(result.ConsumerMeta[targetPickerGitImportMetaBranchOrTag]),
				DirectoryName:    strings.TrimSpace(result.ConsumerMeta[targetPickerGitImportMetaDirectoryName]),
			},
		},
	}
}

func (targetPickerGitImportPathPickerConsumer) PathPickerCancelled(_ *Service, surface *state.SurfaceConsoleRecord, _ control.PathPickerResult) []control.UIEvent {
	return notice(surface, "git_import_cancelled", "已取消 Git 仓库导入。当前工作目标保持不变。")
}

func (s *Service) CompleteTargetPickerGitImport(surfaceSessionID, pickerID, workspaceKey string) []control.UIEvent {
	surface := s.root.Surfaces[surfaceSessionID]
	if surface == nil {
		return nil
	}
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if workspaceKey == "" {
		return notice(surface, "git_import_clone_failed", "仓库已拉取完成，但解析本地工作区目录失败。")
	}
	if surface.ActiveTargetPicker == nil || strings.TrimSpace(surface.ActiveTargetPicker.PickerID) != strings.TrimSpace(pickerID) {
		return notice(surface, "git_import_flow_stale", fmt.Sprintf("仓库已拉取到 `%s`，但原始选择流程已经失效。目录会保留，你可以稍后通过“添加工作区 / 本地目录”继续接入。", workspaceKey))
	}
	events := s.enterTargetPickerNewThread(surface, workspaceKey)
	if targetPickerNewThreadSucceeded(surface, workspaceKey) {
		clearSurfaceTargetPicker(surface)
		return events
	}
	return append(events, notice(surface, "git_import_workspace_attach_failed", fmt.Sprintf("仓库已拉取到 `%s`，但接入工作区失败。目录已保留，你可以稍后通过“添加工作区 / 本地目录”继续接入。", workspaceKey))...)
}
