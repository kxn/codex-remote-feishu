package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/gitworkspace"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const targetPickerGitImportOutputTailLines = 3

func (s *Service) respondLocalRequest(surface *state.SurfaceConsoleRecord, _ *state.RequestPromptRecord, _ control.Action) []control.UIEvent {
	return notice(surface, "request_unsupported", "这张本地交互卡片已经失效，请重新发送最新命令。")
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
	flow := s.activeOwnerCardFlow(surface)
	record := s.activeTargetPicker(surface)
	if flow == nil || flow.Kind != ownerCardFlowKindTargetPicker || record == nil || strings.TrimSpace(record.PickerID) != strings.TrimSpace(pickerID) {
		return targetPickerGitImportFlowStale(surface, workspaceKey)
	}
	record.GitFinalPath = workspaceKey
	events := s.enterTargetPickerNewThread(surface, workspaceKey)
	filtered := targetPickerFilteredFollowupEvents(events)
	if targetPickerNewThreadReady(surface, workspaceKey) {
		return s.finishTargetPickerWithStage(surface, flow, record, control.FeishuTargetPickerStageSucceeded, "已进入新会话待命", targetPickerGitImportSuccessText(workspaceKey), false, filtered)
	}
	if surface.PendingHeadless != nil && surface.PendingHeadless.PrepareNewThread &&
		normalizeWorkspaceClaimKey(surface.PendingHeadless.ThreadCWD) == workspaceKey {
		processing := s.startTargetPickerProcessing(surface, flow, record, targetPickerPendingGitImport, workspaceKey, "", "正在接入工作区", targetPickerGitImportPostCloneProcessingText(strings.TrimSpace(record.GitRepoURL), workspaceKey))
		return append(processing, filtered...)
	}
	reason := strings.TrimSpace(firstNonEmpty(targetPickerFirstNoticeText(events), fmt.Sprintf("仓库已拉取到 `%s`，但接入工作区失败。目录已保留，你可以稍后通过“添加工作区 / 本地目录”继续接入。", workspaceKey)))
	return s.finishTargetPickerWithStage(surface, flow, record, control.FeishuTargetPickerStageFailed, "导入失败", targetPickerGitImportPostCloneFailureText(workspaceKey, reason), false, filtered)
}

func (s *Service) FailTargetPickerGitImport(surfaceSessionID, pickerID string, importErr *gitworkspace.ImportError) []control.UIEvent {
	surface := s.root.Surfaces[surfaceSessionID]
	if surface == nil {
		return nil
	}
	if importErr == nil {
		return notice(surface, "git_import_clone_failed", "Git 仓库导入失败，请稍后重试。")
	}
	flow := s.activeOwnerCardFlow(surface)
	record := s.activeTargetPicker(surface)
	if flow == nil || flow.Kind != ownerCardFlowKindTargetPicker || record == nil || strings.TrimSpace(record.PickerID) != strings.TrimSpace(pickerID) {
		return notice(surface, string(importErr.Code), targetPickerGitImportErrorText(importErr))
	}
	if destination := strings.TrimSpace(importErr.DestinationPath); destination != "" {
		record.GitFinalPath = normalizeWorkspaceClaimKey(destination)
	}
	return s.finishTargetPickerWithStage(surface, flow, record, control.FeishuTargetPickerStageFailed, "导入失败", targetPickerGitImportCloneFailureText(importErr), false, nil)
}

func (s *Service) cancelTargetPickerGitImport(surface *state.SurfaceConsoleRecord, record *activeTargetPickerRecord) []control.UIEvent {
	if surface == nil || record == nil {
		return nil
	}
	events := []control.UIEvent{{
		Kind:             control.UIEventDaemonCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		DaemonCommand: &control.DaemonCommand{
			Kind:             control.DaemonCommandGitWorkspaceImportCancel,
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
	events = append(events, control.UIEvent{
		Kind:             control.UIEventDaemonCommand,
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

func targetPickerGitImportFlowStale(surface *state.SurfaceConsoleRecord, workspaceKey string) []control.UIEvent {
	return notice(surface, "git_import_flow_stale", fmt.Sprintf("仓库已拉取到 `%s`，但原始选择流程已经失效。目录会保留，你可以稍后通过“添加工作区 / 本地目录”继续接入。", workspaceKey))
}

func targetPickerGitImportCloneProcessingText(repoURL, finalPath string) string {
	return targetPickerGitImportStatusText(repoURL, finalPath,
		[]string{
			"- [x] 校验参数",
			"- [>] 克隆仓库",
			"- [ ] 接入工作区",
			"- [ ] 准备会话",
		},
		[]string{"正在执行 `git clone`，请稍候。"},
		"执行中。普通输入已暂停，请等待完成或取消；如需查看状态，可继续使用 `/status`。",
	)
}

func targetPickerGitImportPostCloneProcessingText(repoURL, finalPath string) string {
	return targetPickerGitImportStatusText(repoURL, finalPath,
		[]string{
			"- [x] 校验参数",
			"- [x] 克隆仓库",
			"- [>] 接入工作区",
			"- [ ] 准备会话",
		},
		[]string{"仓库已拉取完成，正在接入工作区并准备新会话。"},
		"执行中。普通输入已暂停，请等待完成或取消；如需查看状态，可继续使用 `/status`。",
	)
}

func targetPickerGitImportSuccessText(workspaceKey string) string {
	parts := []string{}
	if strings.TrimSpace(workspaceKey) != "" {
		parts = append(parts, "**工作区**\n`"+strings.TrimSpace(workspaceKey)+"`")
	}
	parts = append(parts,
		"**会话**\n新会话",
		"**结果**\n仓库已导入完成，下一条文本会直接在这个新工作区/会话里开始执行。",
	)
	return strings.Join(parts, "\n\n")
}

func targetPickerGitImportCloneFailureText(importErr *gitworkspace.ImportError) string {
	if importErr == nil {
		return "**失败原因**\nGit 仓库导入失败，请稍后重试。"
	}
	repoURL := strings.TrimSpace(importErr.RepoURL)
	finalPath := normalizeWorkspaceClaimKey(importErr.DestinationPath)
	parts := []string{targetPickerGitImportObjectBlock(repoURL, finalPath)}
	parts = append(parts,
		"**停在阶段**\n克隆仓库",
		"**失败原因**\n"+targetPickerGitImportErrorText(importErr),
		"**最近输出**\n"+targetPickerGitImportOutputBlock(importErr.Stderr),
		"**下一步**\n"+targetPickerGitImportNextStep(importErr),
	)
	return joinNonEmptyMarkdown(parts...)
}

func targetPickerGitImportPostCloneFailureText(workspaceKey, reason string) string {
	parts := []string{}
	if object := targetPickerGitImportObjectBlock("", workspaceKey); object != "" {
		parts = append(parts, object)
	}
	reason = strings.TrimSpace(firstNonEmpty(reason, "仓库已拉取完成，但后续工作区接入失败。"))
	if workspaceKey != "" && !strings.Contains(reason, "目录已保留") {
		reason += " 目录已保留。"
	}
	parts = append(parts,
		"**停在阶段**\n接入工作区 / 准备会话",
		"**失败原因**\n"+reason,
		"**下一步**\n稍后可通过“添加工作区 / 本地目录”继续接入，或重新发起一次 Git 导入。",
	)
	return joinNonEmptyMarkdown(parts...)
}

func targetPickerGitImportCancelledText(workspaceKey string) string {
	parts := []string{}
	if object := targetPickerGitImportObjectBlock("", workspaceKey); object != "" {
		parts = append(parts, object)
	}
	parts = append(parts,
		"**结果**\n当前业务流已停止。",
		"**提示**\n如本地已经产生部分目录残留，可按需手动处理。",
	)
	return joinNonEmptyMarkdown(parts...)
}

func targetPickerGitImportStatusText(repoURL, finalPath string, stageLines, outputLines []string, footer string) string {
	parts := []string{}
	if object := targetPickerGitImportObjectBlock(repoURL, finalPath); object != "" {
		parts = append(parts, object)
	}
	if len(stageLines) != 0 {
		parts = append(parts, "**阶段**\n"+strings.Join(stageLines, "\n"))
	}
	if len(outputLines) != 0 {
		bulletLines := make([]string, 0, len(outputLines))
		for _, line := range outputLines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			bulletLines = append(bulletLines, "- "+line)
		}
		if len(bulletLines) != 0 {
			parts = append(parts, "**最近输出**\n"+strings.Join(bulletLines, "\n"))
		}
	}
	if footer = strings.TrimSpace(footer); footer != "" {
		parts = append(parts, footer)
	}
	return joinNonEmptyMarkdown(parts...)
}

func targetPickerGitImportObjectBlock(repoURL, finalPath string) string {
	repoURL = strings.TrimSpace(repoURL)
	finalPath = strings.TrimSpace(finalPath)
	switch {
	case repoURL != "" && finalPath != "":
		return "**对象**\n`" + repoURL + "`\n-> `" + finalPath + "`"
	case finalPath != "":
		return "**对象**\n`" + finalPath + "`"
	case repoURL != "":
		return "**对象**\n`" + repoURL + "`"
	default:
		return ""
	}
}

func targetPickerGitImportOutputBlock(stderr string) string {
	lines := targetPickerGitImportTailLines(stderr, targetPickerGitImportOutputTailLines)
	if len(lines) == 0 {
		return "- 未返回更多输出。"
	}
	for i := range lines {
		lines[i] = "- " + lines[i]
	}
	return strings.Join(lines, "\n")
}

func targetPickerGitImportTailLines(text string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	raw := strings.Split(strings.ReplaceAll(strings.TrimSpace(text), "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, truncateTargetPickerGitImportLine(line, 120))
	}
	if len(lines) <= limit {
		return lines
	}
	return lines[len(lines)-limit:]
}

func truncateTargetPickerGitImportLine(line string, limit int) string {
	line = strings.TrimSpace(line)
	if limit <= 0 || len([]rune(line)) <= limit {
		return line
	}
	runes := []rune(line)
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}

func targetPickerGitImportNextStep(importErr *gitworkspace.ImportError) string {
	if importErr == nil {
		return "检查 Git URL、权限或网络后，再重新发起一次导入。"
	}
	switch importErr.Code {
	case gitworkspace.ImportErrorGitMissing:
		return "先在当前机器安装 `git`，或改用“本地目录”接入已有目录。"
	case gitworkspace.ImportErrorInvalidURL:
		return "检查 Git URL 是否有效，再重新发起一次导入。"
	case gitworkspace.ImportErrorInvalidDirectoryName:
		return "把本地目录名改成不含路径分隔符的普通目录名后重试。"
	case gitworkspace.ImportErrorDestinationExists:
		return "更换落地目录或本地目录名后重试，避免覆盖已有目录。"
	case gitworkspace.ImportErrorRefNotFound:
		return "检查分支或标签名是否存在后再重试。"
	case gitworkspace.ImportErrorAuthFailed:
		return "检查仓库权限、Git 凭据或网络后，再重新发起一次导入。"
	default:
		return "检查 Git URL、目标目录、权限或网络后，再重新发起一次导入。"
	}
}

func targetPickerGitImportErrorText(importErr *gitworkspace.ImportError) string {
	if importErr == nil {
		return "Git 仓库导入失败，请稍后重试。"
	}
	switch importErr.Code {
	case gitworkspace.ImportErrorGitMissing:
		return "当前机器未检测到 `git`，暂时不能直接从 Git URL 导入。"
	case gitworkspace.ImportErrorInvalidURL:
		return "Git 仓库地址无效，请检查地址格式后重试。"
	case gitworkspace.ImportErrorInvalidDirectoryName:
		return "目标目录名无效，请改成不含路径分隔符的普通目录名。"
	case gitworkspace.ImportErrorDestinationExists:
		return "目标目录已经存在，请换一个父目录或目录名后重试。"
	case gitworkspace.ImportErrorRefNotFound:
		return "指定的分支或标签不存在，请检查后重试。"
	case gitworkspace.ImportErrorAuthFailed:
		return "无法访问这个仓库，请确认当前机器上的 Git 凭据或仓库权限后重试。"
	default:
		return "Git 仓库导入失败，请稍后重试。"
	}
}

func joinNonEmptyMarkdown(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		filtered = append(filtered, part)
	}
	return strings.Join(filtered, "\n\n")
}
