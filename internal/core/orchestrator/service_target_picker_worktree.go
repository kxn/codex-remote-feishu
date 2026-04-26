package orchestrator

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/workspaceimport"
)

type targetPickerWorktreeState struct {
	FinalPath  string
	CanConfirm bool
	Messages   []control.FeishuTargetPickerMessage
}

func (s *Service) filterGitWorkspaceSelectionEntries(entries []workspaceSelectionEntry) []workspaceSelectionEntry {
	if len(entries) == 0 {
		return nil
	}
	filtered := make([]workspaceSelectionEntry, 0, len(entries))
	for _, entry := range entries {
		workspaceKey := normalizeWorkspaceClaimKey(entry.workspaceKey)
		if workspaceKey == "" {
			continue
		}
		info, err := gitmeta.InspectWorkspace(workspaceKey, gitmeta.InspectOptions{})
		if err != nil || !info.InRepo() {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func (s *Service) buildTargetPickerWorktreeState(record *activeTargetPickerRecord) targetPickerWorktreeState {
	worktreeState := targetPickerWorktreeState{}
	if record == nil {
		return worktreeState
	}
	baseWorkspaceKey := normalizeTargetPickerWorkspaceSelection(record.SelectedWorkspaceKey)
	branchName := strings.TrimSpace(record.WorktreeBranchName)
	directoryName := strings.TrimSpace(record.WorktreeDirectoryName)
	if !s.config.GitAvailable {
		worktreeState.Messages = append(worktreeState.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  "当前机器未检测到 `git`，暂时不能创建 worktree 工作区。",
		})
		return worktreeState
	}
	if baseWorkspaceKey == "" {
		return worktreeState
	}
	info, err := gitmeta.InspectWorkspace(baseWorkspaceKey, gitmeta.InspectOptions{})
	if err != nil {
		worktreeState.Messages = append(worktreeState.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  "无法读取基准工作区的 Git 信息，请稍后重试。",
		})
		return worktreeState
	}
	if !info.InRepo() {
		worktreeState.Messages = append(worktreeState.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  "当前选择的工作区不是 Git 工作区，不能从它创建 worktree。",
		})
		return worktreeState
	}
	if branchName == "" {
		finalPath, message := targetPickerWorktreePreviewWithoutBranch(baseWorkspaceKey, directoryName)
		if finalPath != "" {
			worktreeState.FinalPath = normalizeWorkspaceClaimKey(finalPath)
		} else if message == "" {
			return worktreeState
		}
		if message != "" {
			worktreeState.Messages = append(worktreeState.Messages, control.FeishuTargetPickerMessage{
				Level: control.FeishuTargetPickerMessageDanger,
				Text:  message,
			})
		}
		return worktreeState
	}
	preview, err := gitmeta.PreviewWorktree(gitmeta.WorktreeCreateRequest{
		BaseWorkspacePath: baseWorkspaceKey,
		BranchName:        branchName,
		DirectoryName:     directoryName,
	})
	if err == nil {
		worktreeState.FinalPath = normalizeWorkspaceClaimKey(preview.DestinationPath)
		worktreeState.CanConfirm = true
		return worktreeState
	}
	var worktreeErr *gitmeta.WorktreeCreateError
	if !targetPickerErrorAsWorktree(err, &worktreeErr) || worktreeErr == nil {
		worktreeState.Messages = append(worktreeState.Messages, control.FeishuTargetPickerMessage{
			Level: control.FeishuTargetPickerMessageDanger,
			Text:  "无法预检查最终路径，请重新确认基准工作区、分支名和目录名。",
		})
		return worktreeState
	}
	if destination := strings.TrimSpace(worktreeErr.DestinationPath); destination != "" {
		worktreeState.FinalPath = normalizeWorkspaceClaimKey(destination)
	}
	worktreeState.Messages = append(worktreeState.Messages, control.FeishuTargetPickerMessage{
		Level: control.FeishuTargetPickerMessageDanger,
		Text:  targetPickerWorktreePreviewErrorText(worktreeErr),
	})
	return worktreeState
}

func targetPickerWorktreePreviewWithoutBranch(baseWorkspaceKey, directoryName string) (string, string) {
	baseWorkspaceKey = normalizeWorkspaceClaimKey(baseWorkspaceKey)
	directoryName = strings.TrimSpace(directoryName)
	if baseWorkspaceKey == "" || directoryName == "" {
		return "", ""
	}
	if err := workspaceimport.ValidateDirectoryName(directoryName); err != nil {
		return "", "本地目录名无效，请改成不含路径分隔符的普通目录名。"
	}
	finalPath := filepath.Join(filepath.Dir(baseWorkspaceKey), directoryName)
	if _, err := os.Stat(finalPath); err == nil {
		return finalPath, fmt.Sprintf("目标目录已存在：%s。请更换目录名或基准工作区。", normalizeWorkspaceClaimKey(finalPath))
	} else if !os.IsNotExist(err) {
		return "", "无法预检查最终路径，请重新确认基准工作区和目录名。"
	}
	return finalPath, ""
}

func (s *Service) confirmTargetPickerWorktree(surface *state.SurfaceConsoleRecord, flow *activeOwnerCardFlowRecord, record *activeTargetPickerRecord, view control.FeishuTargetPickerView) []eventcontract.Event {
	if surface == nil || record == nil {
		return nil
	}
	worktreeState := s.buildTargetPickerWorktreeState(record)
	if !worktreeState.CanConfirm {
		if message := targetPickerWorktreeValidationMessage(record, worktreeState.Messages); message != "" &&
			!targetPickerHasBlockingMessage(view.SourceMessages, message) {
			view.SourceMessages = append([]control.FeishuTargetPickerMessage{{
				Level: control.FeishuTargetPickerMessageDanger,
				Text:  message,
			}}, view.SourceMessages...)
		}
		return []eventcontract.Event{s.targetPickerViewEvent(surface, view, false)}
	}
	finalPath := strings.TrimSpace(worktreeState.FinalPath)
	record.WorktreeFinalPath = finalPath
	status := targetPickerWorktreeCreateProcessingStatus(view.SelectedWorkspaceLabel, strings.TrimSpace(record.WorktreeBranchName), finalPath)
	processing := s.startTargetPickerProcessingWithSections(
		surface,
		flow,
		record,
		targetPickerPendingWorktreeCreate,
		finalPath,
		"",
		"正在创建 Worktree 工作区",
		"",
		status.Sections,
		status.Footer,
	)
	return append(processing,
		eventcontract.Event{
			Kind:             eventcontract.KindDaemonCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			DaemonCommand: &control.DaemonCommand{
				Kind:             control.DaemonCommandGitWorkspaceWorktreeCreate,
				SurfaceSessionID: surface.SurfaceSessionID,
				PickerID:         strings.TrimSpace(record.PickerID),
				WorkspaceKey:     normalizeTargetPickerWorkspaceSelection(record.SelectedWorkspaceKey),
				BranchName:       strings.TrimSpace(record.WorktreeBranchName),
				DirectoryName:    strings.TrimSpace(record.WorktreeDirectoryName),
			},
		},
	)
}

func (s *Service) CompleteTargetPickerWorktreeCreate(surfaceSessionID, pickerID, workspaceKey string) []eventcontract.Event {
	surface := s.root.Surfaces[surfaceSessionID]
	if surface == nil {
		return nil
	}
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if workspaceKey == "" {
		return notice(surface, "worktree_create_failed", "worktree 已创建完成，但解析本地工作区目录失败。")
	}
	flow := s.activeOwnerCardFlow(surface)
	record := s.activeTargetPicker(surface)
	if flow == nil || flow.Kind != ownerCardFlowKindTargetPicker || record == nil || strings.TrimSpace(record.PickerID) != strings.TrimSpace(pickerID) {
		return notice(surface, "worktree_create_flow_stale", fmt.Sprintf("worktree 已创建到 `%s`，但原始选择流程已经失效。目录会保留，你可以稍后通过“从目录新建”继续接入。", workspaceKey))
	}
	record.WorktreeFinalPath = workspaceKey
	events := s.enterTargetPickerNewThread(surface, workspaceKey)
	filtered := targetPickerFilteredFollowupEvents(events)
	if targetPickerNewThreadReady(surface, workspaceKey) {
		status := targetPickerWorktreeCreateSuccessStatus(workspaceKey)
		return s.finishTargetPickerWithStageAndSections(surface, flow, record, control.FeishuTargetPickerStageSucceeded, "已进入新会话待命", "", status.Sections, status.Footer, false, filtered)
	}
	if surface.PendingHeadless != nil && surface.PendingHeadless.PrepareNewThread &&
		normalizeWorkspaceClaimKey(surface.PendingHeadless.ThreadCWD) == workspaceKey {
		status := targetPickerWorktreeCreatePostCreateProcessingStatus(strings.TrimSpace(record.WorktreeBranchName), workspaceKey)
		processing := s.startTargetPickerProcessingWithSections(surface, flow, record, targetPickerPendingWorktreeCreate, workspaceKey, "", "正在接入工作区", "", status.Sections, status.Footer)
		return append(processing, filtered...)
	}
	reason := strings.TrimSpace(firstNonEmpty(targetPickerFirstNoticeText(events), fmt.Sprintf("worktree 已创建到 `%s`，但接入工作区失败。目录已保留，你可以稍后通过“从目录新建”继续接入。", workspaceKey)))
	status := targetPickerWorktreeCreatePostCreateFailureStatus(workspaceKey, reason)
	return s.finishTargetPickerWithStageAndSections(surface, flow, record, control.FeishuTargetPickerStageFailed, "创建失败", "", status.Sections, status.Footer, false, filtered)
}

func (s *Service) FailTargetPickerWorktreeCreate(surfaceSessionID, pickerID string, createErr *gitmeta.WorktreeCreateError) []eventcontract.Event {
	surface := s.root.Surfaces[surfaceSessionID]
	if surface == nil {
		return nil
	}
	if createErr == nil {
		return notice(surface, "worktree_create_failed", "worktree 创建失败，请稍后重试。")
	}
	flow := s.activeOwnerCardFlow(surface)
	record := s.activeTargetPicker(surface)
	if flow == nil || flow.Kind != ownerCardFlowKindTargetPicker || record == nil || strings.TrimSpace(record.PickerID) != strings.TrimSpace(pickerID) {
		return notice(surface, string(createErr.Code), targetPickerWorktreeErrorText(createErr))
	}
	if destination := strings.TrimSpace(createErr.DestinationPath); destination != "" {
		record.WorktreeFinalPath = normalizeWorkspaceClaimKey(destination)
	}
	status := targetPickerWorktreeCreateFailureStatus(createErr)
	return s.finishTargetPickerWithStageAndSections(surface, flow, record, control.FeishuTargetPickerStageFailed, "创建失败", "", status.Sections, status.Footer, false, nil)
}

func (s *Service) cancelTargetPickerWorktreeCreate(surface *state.SurfaceConsoleRecord, record *activeTargetPickerRecord) []eventcontract.Event {
	if surface == nil || record == nil {
		return nil
	}
	events := []eventcontract.Event{{
		Kind:             eventcontract.KindDaemonCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		DaemonCommand: &control.DaemonCommand{
			Kind:             control.DaemonCommandGitWorkspaceWorktreeCancel,
			SurfaceSessionID: surface.SurfaceSessionID,
			PickerID:         strings.TrimSpace(record.PickerID),
		},
	}}
	pending := surface.PendingHeadless
	if pending == nil || !pending.PrepareNewThread || normalizeWorkspaceClaimKey(pending.ThreadCWD) != normalizeWorkspaceClaimKey(record.PendingWorkspaceKey) {
		return events
	}
	surface.PendingHeadless = nil
	events = append(events, s.finalizeDetachedSurface(surface)...)
	events = append(events, eventcontract.Event{
		Kind:             eventcontract.KindDaemonCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		DaemonCommand: &control.DaemonCommand{
			Kind:             control.DaemonCommandKillHeadless,
			SurfaceSessionID: surface.SurfaceSessionID,
			InstanceID:       pending.InstanceID,
			ThreadID:         pending.ThreadID,
			ThreadTitle:      pending.ThreadTitle,
			ThreadCWD:        pending.ThreadCWD,
		},
	})
	return events
}

func targetPickerWorktreeValidationMessage(record *activeTargetPickerRecord, messages []control.FeishuTargetPickerMessage) string {
	if message := targetPickerFirstBlockingMessage(messages); message != "" {
		return message
	}
	missingBaseWorkspace := normalizeTargetPickerWorkspaceSelection(record.SelectedWorkspaceKey) == ""
	missingBranch := strings.TrimSpace(record.WorktreeBranchName) == ""
	switch {
	case missingBaseWorkspace && missingBranch:
		return "请先选择基准工作区并填写新分支名。"
	case missingBaseWorkspace:
		return "请先选择基准工作区。"
	case missingBranch:
		return "请先填写新分支名。"
	default:
		return "当前 worktree 配置还不能执行，请先修正阻塞项。"
	}
}

func targetPickerWorktreePreviewErrorText(err *gitmeta.WorktreeCreateError) string {
	if err == nil {
		return "无法预检查最终路径，请重新确认基准工作区、分支名和目录名。"
	}
	switch err.Code {
	case gitmeta.WorktreeCreateErrorBaseWorkspaceNotGit:
		return "当前选择的工作区不是 Git 工作区，不能从它创建 worktree。"
	case gitmeta.WorktreeCreateErrorInvalidBranchName:
		return "新分支名无效，请使用 Git 允许的分支名。"
	case gitmeta.WorktreeCreateErrorBranchExists:
		return "这个分支已经存在，请换一个新的分支名。"
	case gitmeta.WorktreeCreateErrorInvalidDirectoryName:
		return "本地目录名无效，请改成不含路径分隔符的普通目录名。"
	case gitmeta.WorktreeCreateErrorDestinationExists:
		return fmt.Sprintf("目标目录已存在：%s。请更换目录名或基准工作区。", normalizeWorkspaceClaimKey(err.DestinationPath))
	default:
		return "无法预检查最终路径，请重新确认基准工作区、分支名和目录名。"
	}
}

func targetPickerWorktreeErrorText(err *gitmeta.WorktreeCreateError) string {
	if err == nil {
		return "worktree 创建失败，请稍后重试。"
	}
	switch err.Code {
	case gitmeta.WorktreeCreateErrorGitMissing:
		return "当前机器未检测到 `git`，暂时不能创建 worktree 工作区。"
	case gitmeta.WorktreeCreateErrorBaseWorkspaceNotGit:
		return "当前选择的工作区不是 Git 工作区，不能从它创建 worktree。"
	case gitmeta.WorktreeCreateErrorInvalidBranchName:
		return "新分支名无效，请检查后重试。"
	case gitmeta.WorktreeCreateErrorBranchExists:
		return "这个分支已经存在，请换一个新的分支名后重试。"
	case gitmeta.WorktreeCreateErrorInvalidDirectoryName:
		return "本地目录名无效，请改成不含路径分隔符的普通目录名。"
	case gitmeta.WorktreeCreateErrorDestinationExists:
		return "目标目录已经存在，请换一个目录名或基准工作区后重试。"
	default:
		return "worktree 创建失败，请稍后重试。"
	}
}

func targetPickerWorktreeCreateSuccessStatus(workspaceKey string) feishuCardStatusPayload {
	sections := []control.FeishuCardTextSection{}
	if strings.TrimSpace(workspaceKey) != "" {
		sections = append(sections, control.FeishuCardTextSection{Label: "工作区", Lines: []string{strings.TrimSpace(workspaceKey)}})
	}
	sections = append(sections,
		control.FeishuCardTextSection{Label: "会话", Lines: []string{"新会话"}},
		control.FeishuCardTextSection{Label: "结果", Lines: []string{"worktree 工作区已创建完成，下一条文本会直接在这个新工作区/会话里开始执行。"}},
	)
	return feishuCardStatusPayload{Sections: sections}
}

func targetPickerWorktreeCreateFailureStatus(createErr *gitmeta.WorktreeCreateError) feishuCardStatusPayload {
	if createErr == nil {
		return feishuCardStatusPayload{
			Sections: []control.FeishuCardTextSection{{Label: "失败原因", Lines: []string{"worktree 创建失败，请稍后重试。"}}},
		}
	}
	sections := targetPickerGitImportObjectSections("", normalizeWorkspaceClaimKey(createErr.DestinationPath))
	sections = append(sections,
		control.FeishuCardTextSection{Label: "停在阶段", Lines: []string{"创建 worktree"}},
		control.FeishuCardTextSection{Label: "失败原因", Lines: []string{targetPickerWorktreeErrorText(createErr)}},
		control.FeishuCardTextSection{Label: "最近输出", Lines: targetPickerGitImportOutputLines(createErr.Stderr)},
		control.FeishuCardTextSection{Label: "下一步", Lines: []string{"请检查基准工作区、分支名和本地目录名后重试。"}},
	)
	return feishuCardStatusPayload{Sections: sections}
}

func targetPickerWorktreeCreatePostCreateProcessingStatus(branchName, workspaceKey string) feishuCardStatusPayload {
	sections := targetPickerGitImportObjectSections("", workspaceKey)
	if strings.TrimSpace(branchName) != "" {
		sections = append(sections, control.FeishuCardTextSection{Label: "新分支", Lines: []string{strings.TrimSpace(branchName)}})
	}
	sections = append(sections,
		control.FeishuCardTextSection{Label: "当前阶段", Lines: []string{
			"✅ 校验参数",
			"✅ 创建 worktree",
			"🔄 接入工作区",
			"⚪ 准备新会话",
		}},
		control.FeishuCardTextSection{Label: "最近输出", Lines: []string{"worktree 已创建完成，正在接入工作区并准备新会话。"}},
	)
	return feishuCardStatusPayload{Sections: sections}
}

func targetPickerWorktreeCreatePostCreateFailureStatus(workspaceKey, reason string) feishuCardStatusPayload {
	sections := targetPickerGitImportObjectSections("", workspaceKey)
	reason = strings.TrimSpace(firstNonEmpty(reason, "worktree 已创建完成，但后续工作区接入失败。"))
	if workspaceKey != "" && !strings.Contains(reason, "目录已保留") {
		reason += " 目录已保留。"
	}
	sections = append(sections,
		control.FeishuCardTextSection{Label: "停在阶段", Lines: []string{"接入工作区 / 准备会话"}},
		control.FeishuCardTextSection{Label: "失败原因", Lines: []string{reason}},
		control.FeishuCardTextSection{Label: "下一步", Lines: []string{"稍后可通过“从目录新建”继续接入，或重新发起一次 worktree 创建。"}},
	)
	return feishuCardStatusPayload{Sections: sections}
}

func targetPickerWorktreeCreateCancelledStatus(workspaceKey string) feishuCardStatusPayload {
	sections := targetPickerGitImportObjectSections("", workspaceKey)
	sections = append(sections,
		control.FeishuCardTextSection{Label: "结果", Lines: []string{"当前业务流已停止。"}},
		control.FeishuCardTextSection{Label: "提示", Lines: []string{"如果本地已经产生部分目录残留，可按需手动处理。"}},
	)
	return feishuCardStatusPayload{Sections: sections}
}

func targetPickerErrorAsWorktree(err error, target **gitmeta.WorktreeCreateError) bool {
	if err == nil || target == nil {
		return false
	}
	return errors.As(err, target)
}
