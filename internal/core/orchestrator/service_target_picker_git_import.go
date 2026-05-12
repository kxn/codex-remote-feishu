package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/workspaceimport"
)

const targetPickerGitImportOutputTailLines = 3

type feishuCardStatusPayload struct {
	Sections []control.FeishuCardTextSection
	Footer   string
}

func (s *Service) respondLocalRequest(surface *state.SurfaceConsoleRecord, _ *state.RequestPromptRecord, _ control.Action) []eventcontract.Event {
	return notice(surface, "request_unsupported", "这张本地交互卡片已经失效，请重新发送最新命令。")
}

func (s *Service) CompleteTargetPickerGitImport(surfaceSessionID, pickerID, workspaceKey string) []eventcontract.Event {
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
		status := targetPickerGitImportSuccessStatus(workspaceKey)
		return s.finishTargetPickerWithStageAndSections(surface, flow, record, control.FeishuTargetPickerStageSucceeded, "已进入新会话待命", "", status.Sections, status.Footer, false, filtered)
	}
	if surface.PendingHeadless != nil && surface.PendingHeadless.PrepareNewThread &&
		normalizeWorkspaceClaimKey(firstNonEmpty(surface.PendingHeadless.WorkspaceKey, surface.PendingHeadless.ThreadCWD)) == workspaceKey {
		status := targetPickerGitImportPostCloneProcessingStatus(strings.TrimSpace(record.GitRepoURL), workspaceKey)
		processing := s.startTargetPickerProcessingWithSections(surface, flow, record, targetPickerPendingGitImport, workspaceKey, "", "正在接入工作区", "", status.Sections, status.Footer)
		return append(processing, filtered...)
	}
	reason := strings.TrimSpace(firstNonEmpty(targetPickerFirstNoticeText(events), fmt.Sprintf("仓库已拉取到 `%s`，但接入工作区失败。目录已保留，你可以稍后通过“添加工作区 / 本地目录”继续接入。", workspaceKey)))
	status := targetPickerGitImportPostCloneFailureStatus(workspaceKey, reason)
	return s.finishTargetPickerWithStageAndSections(surface, flow, record, control.FeishuTargetPickerStageFailed, "导入失败", "", status.Sections, status.Footer, false, filtered)
}

func (s *Service) FailTargetPickerGitImport(surfaceSessionID, pickerID string, importErr *workspaceimport.ImportError) []eventcontract.Event {
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
	status := targetPickerGitImportCloneFailureStatus(importErr)
	return s.finishTargetPickerWithStageAndSections(surface, flow, record, control.FeishuTargetPickerStageFailed, "导入失败", "", status.Sections, status.Footer, false, nil)
}

func (s *Service) cancelTargetPickerGitImport(surface *state.SurfaceConsoleRecord, record *activeTargetPickerRecord) []eventcontract.Event {
	if surface == nil || record == nil {
		return nil
	}
	events := []eventcontract.Event{{
		Kind:             eventcontract.KindDaemonCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		DaemonCommand: &control.DaemonCommand{
			Kind:             control.DaemonCommandGitWorkspaceImportCancel,
			SurfaceSessionID: surface.SurfaceSessionID,
			PickerID:         strings.TrimSpace(record.PickerID),
		},
	}}
	pending := surface.PendingHeadless
	if pending == nil || !pending.PrepareNewThread || normalizeWorkspaceClaimKey(firstNonEmpty(pending.WorkspaceKey, pending.ThreadCWD)) != normalizeWorkspaceClaimKey(record.PendingWorkspaceKey) {
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

func targetPickerGitImportFlowStale(surface *state.SurfaceConsoleRecord, workspaceKey string) []eventcontract.Event {
	return notice(surface, "git_import_flow_stale", fmt.Sprintf("仓库已拉取到 `%s`，但原始选择流程已经失效。目录会保留，你可以稍后通过“添加工作区 / 本地目录”继续接入。", workspaceKey))
}

func targetPickerGitImportCloneProcessingStatus(repoURL, finalPath string) feishuCardStatusPayload {
	return targetPickerGitImportStatusPayload(repoURL, finalPath,
		[]string{
			"✅ 校验参数",
			"🔄 克隆仓库",
			"⚪ 接入工作区",
			"⚪ 准备新会话",
		},
		[]string{"正在执行 git clone ..."},
		"",
	)
}

func targetPickerGitImportPostCloneProcessingStatus(repoURL, finalPath string) feishuCardStatusPayload {
	return targetPickerGitImportStatusPayload(repoURL, finalPath,
		[]string{
			"✅ 校验参数",
			"✅ 克隆仓库",
			"🔄 接入工作区",
			"⚪ 准备新会话",
		},
		[]string{"仓库已拉取完成，正在接入工作区并准备新会话。"},
		"",
	)
}

func targetPickerGitImportSuccessStatus(workspaceKey string) feishuCardStatusPayload {
	sections := []control.FeishuCardTextSection{}
	if strings.TrimSpace(workspaceKey) != "" {
		sections = append(sections, control.FeishuCardTextSection{Label: "工作区", Lines: []string{strings.TrimSpace(workspaceKey)}})
	}
	sections = append(sections,
		control.FeishuCardTextSection{Label: "会话", Lines: []string{"新会话"}},
		control.FeishuCardTextSection{Label: "结果", Lines: []string{"仓库已导入完成，下一条文本会直接在这个新工作区/会话里开始执行。"}},
	)
	return feishuCardStatusPayload{Sections: sections}
}

func targetPickerGitImportCloneFailureStatus(importErr *workspaceimport.ImportError) feishuCardStatusPayload {
	if importErr == nil {
		return feishuCardStatusPayload{
			Sections: []control.FeishuCardTextSection{{Label: "失败原因", Lines: []string{"Git 仓库导入失败，请稍后重试。"}}},
		}
	}
	repoURL := strings.TrimSpace(importErr.RepoURL)
	finalPath := normalizeWorkspaceClaimKey(importErr.DestinationPath)
	sections := targetPickerGitImportObjectSections(repoURL, finalPath)
	sections = append(sections,
		control.FeishuCardTextSection{Label: "停在阶段", Lines: []string{"克隆仓库"}},
		control.FeishuCardTextSection{Label: "失败原因", Lines: []string{targetPickerGitImportErrorText(importErr)}},
		control.FeishuCardTextSection{Label: "最近输出", Lines: targetPickerGitImportOutputLines(importErr.Stderr)},
		control.FeishuCardTextSection{Label: "下一步", Lines: []string{targetPickerGitImportNextStep(importErr)}},
	)
	return feishuCardStatusPayload{Sections: sections}
}

func targetPickerGitImportPostCloneFailureStatus(workspaceKey, reason string) feishuCardStatusPayload {
	sections := targetPickerGitImportObjectSections("", workspaceKey)
	reason = strings.TrimSpace(firstNonEmpty(reason, "仓库已拉取完成，但后续工作区接入失败。"))
	if workspaceKey != "" && !strings.Contains(reason, "目录已保留") {
		reason += " 目录已保留。"
	}
	sections = append(sections,
		control.FeishuCardTextSection{Label: "停在阶段", Lines: []string{"接入工作区 / 准备会话"}},
		control.FeishuCardTextSection{Label: "失败原因", Lines: []string{reason}},
		control.FeishuCardTextSection{Label: "下一步", Lines: []string{"稍后可通过“添加工作区 / 本地目录”继续接入，或重新发起一次 Git 导入。"}},
	)
	return feishuCardStatusPayload{Sections: sections}
}

func targetPickerGitImportCancelledStatus(workspaceKey string) feishuCardStatusPayload {
	sections := targetPickerGitImportObjectSections("", workspaceKey)
	sections = append(sections,
		control.FeishuCardTextSection{Label: "结果", Lines: []string{"当前业务流已停止。"}},
		control.FeishuCardTextSection{Label: "提示", Lines: []string{"如本地已经产生部分目录残留，可按需手动处理。"}},
	)
	return feishuCardStatusPayload{Sections: sections}
}

func targetPickerGitImportStatusPayload(repoURL, finalPath string, stageLines, outputLines []string, footer string) feishuCardStatusPayload {
	sections := targetPickerGitImportObjectSections(repoURL, finalPath)
	if len(stageLines) != 0 {
		sections = append(sections, control.FeishuCardTextSection{Label: "当前阶段", Lines: stageLines})
	}
	if len(outputLines) != 0 {
		normalized := make([]string, 0, len(outputLines))
		for _, line := range outputLines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			normalized = append(normalized, line)
		}
		sections = append(sections, control.FeishuCardTextSection{Label: "最近输出", Lines: normalized})
	}
	return feishuCardStatusPayload{
		Sections: sections,
		Footer:   strings.TrimSpace(footer),
	}
}

func targetPickerGitImportObjectSections(repoURL, finalPath string) []control.FeishuCardTextSection {
	repoURL = strings.TrimSpace(repoURL)
	finalPath = strings.TrimSpace(finalPath)
	switch {
	case repoURL != "" && finalPath != "":
		return []control.FeishuCardTextSection{{Label: "对象", Lines: []string{repoURL, "-> " + finalPath}}}
	case finalPath != "":
		return []control.FeishuCardTextSection{{Label: "对象", Lines: []string{finalPath}}}
	case repoURL != "":
		return []control.FeishuCardTextSection{{Label: "对象", Lines: []string{repoURL}}}
	default:
		return nil
	}
}

func targetPickerGitImportOutputLines(stderr string) []string {
	lines := targetPickerGitImportTailLines(stderr, targetPickerGitImportOutputTailLines)
	if len(lines) == 0 {
		return []string{"未返回更多输出。"}
	}
	return lines
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

func targetPickerGitImportNextStep(importErr *workspaceimport.ImportError) string {
	if importErr == nil {
		return "检查 Git URL、权限或网络后，再重新发起一次导入。"
	}
	switch importErr.Code {
	case workspaceimport.ImportErrorGitMissing:
		return "先在当前机器安装 `git`，或改用“本地目录”接入已有目录。"
	case workspaceimport.ImportErrorInvalidURL:
		return "检查 Git URL 是否有效，再重新发起一次导入。"
	case workspaceimport.ImportErrorInvalidDirectoryName:
		return "把本地目录名改成不含路径分隔符的普通目录名后重试。"
	case workspaceimport.ImportErrorDestinationExists:
		return "更换落地目录或本地目录名后重试，避免覆盖已有目录。"
	case workspaceimport.ImportErrorRefNotFound:
		return "检查分支或标签名是否存在后再重试。"
	case workspaceimport.ImportErrorAuthFailed:
		return "检查仓库权限、Git 凭据或网络后，再重新发起一次导入。"
	default:
		return "检查 Git URL、目标目录、权限或网络后，再重新发起一次导入。"
	}
}

func targetPickerGitImportErrorText(importErr *workspaceimport.ImportError) string {
	if importErr == nil {
		return "Git 仓库导入失败，请稍后重试。"
	}
	switch importErr.Code {
	case workspaceimport.ImportErrorGitMissing:
		return "当前机器未检测到 `git`，暂时不能直接从 Git URL 导入。"
	case workspaceimport.ImportErrorInvalidURL:
		return "Git 仓库地址无效，请检查地址格式后重试。"
	case workspaceimport.ImportErrorInvalidDirectoryName:
		return "目标目录名无效，请改成不含路径分隔符的普通目录名。"
	case workspaceimport.ImportErrorDestinationExists:
		return "目标目录已经存在，请换一个父目录或目录名后重试。"
	case workspaceimport.ImportErrorRefNotFound:
		return "指定的分支或标签不存在，请检查后重试。"
	case workspaceimport.ImportErrorAuthFailed:
		return "无法访问这个仓库，请确认当前机器上的 Git 凭据或仓库权限后重试。"
	default:
		return "Git 仓库导入失败，请稍后重试。"
	}
}
