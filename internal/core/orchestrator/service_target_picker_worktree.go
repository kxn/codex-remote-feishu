package orchestrator

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

type targetPickerWorktreeState struct {
	FinalPath  string
	CanConfirm bool
	Messages   []control.FeishuTargetPickerMessage
}

func (s *Service) buildTargetPickerWorktreeState(record *activeTargetPickerRecord) targetPickerWorktreeState {
	worktreeState := targetPickerWorktreeState{}
	if record == nil {
		return worktreeState
	}
	baseWorkspaceKey := normalizeTargetPickerWorkspaceSelection(record.SelectedWorkspaceKey)
	if baseWorkspaceKey == "" {
		return worktreeState
	}
	preview, err := gitmeta.PreviewWorktree(gitmeta.WorktreeCreateRequest{
		BaseWorkspacePath: baseWorkspaceKey,
		BranchName:        strings.TrimSpace(record.WorktreeBranchName),
		DirectoryName:     strings.TrimSpace(record.WorktreeDirectoryName),
	})
	if err == nil {
		if strings.TrimSpace(preview.DestinationPath) != "" {
			worktreeState.FinalPath = normalizeWorkspaceClaimKey(preview.DestinationPath)
		}
		worktreeState.CanConfirm = preview.CanConfirm
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
		Text:  gitmeta.WorktreeCreateErrorText(worktreeErr),
	})
	return worktreeState
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
	if blocked := s.preflightFeishuRoomWorkspaceChange(surface, finalPath); blocked != nil {
		message := strings.TrimSpace(firstNoticeText(blocked))
		if message == "" {
			message = "当前群 workspace 暂时不能切换，请稍后重试。"
		}
		if !targetPickerHasBlockingMessage(view.SourceMessages, message) {
			view.SourceMessages = append([]control.FeishuTargetPickerMessage{{
				Level: control.FeishuTargetPickerMessageDanger,
				Text:  message,
			}}, view.SourceMessages...)
		}
		return []eventcontract.Event{s.targetPickerViewEvent(surface, view, false)}
	}
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
	pendingText := s.takePendingTextInput(surface)
	events := s.enterTargetPickerNewThread(surface, workspaceKey)
	filtered := filterPickerFollowupEvents(events)
	if targetPickerNewThreadReady(surface, workspaceKey) {
		status := targetPickerWorktreeCreateSuccessStatus(workspaceKey)
		result := s.finishTargetPickerWithStageAndSections(surface, flow, record, control.FeishuTargetPickerStageSucceeded, "已进入新会话待命", "", status.Sections, status.Footer, false, filtered)
		return append(result, s.replayPendingTextInput(surface, pendingText)...)
	}
	restorePendingTextInput(surface, pendingText)
	if surface.PendingHeadless != nil && surface.PendingHeadless.PrepareNewThread &&
		pendingHeadlessWorkspaceClaimKey(surface.PendingHeadless) == workspaceKey {
		status := targetPickerWorktreeCreatePostCreateProcessingStatus(strings.TrimSpace(record.WorktreeBranchName), workspaceKey)
		processing := s.startTargetPickerProcessingWithSections(surface, flow, record, targetPickerPendingWorktreeCreate, workspaceKey, "", "正在接入工作区", "", status.Sections, status.Footer)
		return append(processing, filtered...)
	}
	reason := strings.TrimSpace(xutil.FirstNonEmpty(firstNoticeText(events), fmt.Sprintf("worktree 已创建到 `%s`，但接入工作区失败。目录已保留，你可以稍后通过“从目录新建”继续接入。", workspaceKey)))
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
		return notice(surface, string(createErr.Code), gitmeta.WorktreeCreateErrorText(createErr))
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
	if pending == nil || !pending.PrepareNewThread || pendingHeadlessWorkspaceClaimKey(pending) != normalizeWorkspaceClaimKey(record.PendingWorkspaceKey) {
		return events
	}
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
			WorkspaceKey:     pending.WorkspaceKey,
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
		control.FeishuCardTextSection{Label: "失败原因", Lines: []string{gitmeta.WorktreeCreateErrorText(createErr)}},
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
	reason = strings.TrimSpace(xutil.FirstNonEmpty(reason, "worktree 已创建完成，但后续工作区接入失败。"))
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
