package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func targetPickerBodySections(
	page control.FeishuTargetPickerPage,
	workspaceLabel, workspaceMeta, sessionLabel, sessionMeta string,
	localDirectoryPath, gitRepoURL, gitParentDir, gitFinalPath, worktreeBranchName, worktreeFinalPath string,
) []control.FeishuCardTextSection {
	sections := make([]control.FeishuCardTextSection, 0, 6)
	if page == control.FeishuTargetPickerPageLocalDirectory {
		return nil
	}
	if page == control.FeishuTargetPickerPageWorktree {
		if section, ok := targetPickerSummarySection("基准工作区", workspaceLabel, workspaceMeta); ok {
			sections = append(sections, section)
		}
		if section, ok := targetPickerSummarySection("新分支", worktreeBranchName, ""); ok {
			sections = append(sections, section)
		}
		if section, ok := targetPickerSummarySection("目标路径", worktreeFinalPath, ""); ok {
			sections = append(sections, section)
		}
		return cloneFeishuCardSections(sections)
	}
	if section, ok := targetPickerSummarySection("工作区", workspaceLabel, workspaceMeta); ok {
		sections = append(sections, section)
	}
	if section, ok := targetPickerSummarySection("会话", sessionLabel, sessionMeta); ok {
		sections = append(sections, section)
	}
	if section, ok := targetPickerSummarySection("目录", localDirectoryPath, ""); ok {
		sections = append(sections, section)
	}
	if section, ok := targetPickerSummarySection("仓库", gitRepoURL, ""); ok {
		sections = append(sections, section)
	}
	if section, ok := targetPickerSummarySection("落地目录", gitParentDir, ""); ok {
		sections = append(sections, section)
	}
	if section, ok := targetPickerSummarySection("目标路径", gitFinalPath, ""); ok {
		sections = append(sections, section)
	}
	return cloneFeishuCardSections(sections)
}

func targetPickerStatusNoticeSections(record *activeTargetPickerRecord) []control.FeishuCardTextSection {
	if record == nil {
		return nil
	}
	sections := make([]control.FeishuCardTextSection, 0, len(record.StatusSections)+2)
	if text := strings.TrimSpace(record.StatusText); text != "" {
		label := strings.TrimSpace(record.StatusTitle)
		if label == "" {
			switch record.Stage {
			case control.FeishuTargetPickerStageFailed:
				label = "错误"
			case control.FeishuTargetPickerStageCancelled:
				label = "说明"
			default:
				label = "当前状态"
			}
		}
		sections = append(sections, control.FeishuCardTextSection{
			Label: label,
			Lines: []string{text},
		})
	}
	sections = append(sections, cloneFeishuCardSections(record.StatusSections)...)
	if footer := strings.TrimSpace(record.StatusFooter); footer != "" {
		sections = append(sections, control.FeishuCardTextSection{
			Label: "下一步",
			Lines: []string{footer},
		})
	}
	return cloneFeishuCardSections(sections)
}

func targetPickerStageSealed(stage control.FeishuTargetPickerStage) bool {
	switch stage {
	case control.FeishuTargetPickerStageSucceeded, control.FeishuTargetPickerStageFailed, control.FeishuTargetPickerStageCancelled:
		return true
	default:
		return false
	}
}

func targetPickerSummarySection(label, primary, secondary string) (control.FeishuCardTextSection, bool) {
	lines := make([]string, 0, 2)
	if primary = strings.TrimSpace(primary); primary != "" {
		lines = append(lines, primary)
	}
	if secondary = strings.TrimSpace(secondary); secondary != "" {
		lines = append(lines, secondary)
	}
	if len(lines) == 0 {
		return control.FeishuCardTextSection{}, false
	}
	return control.FeishuCardTextSection{
		Label: strings.TrimSpace(label),
		Lines: lines,
	}, true
}
