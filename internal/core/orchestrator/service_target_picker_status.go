package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func targetPickerDefaultPage(source control.TargetPickerRequestSource) control.FeishuTargetPickerPage {
	switch source {
	case control.TargetPickerRequestSourceList,
		control.TargetPickerRequestSourceUse,
		control.TargetPickerRequestSourceUseAll,
		control.TargetPickerRequestSourceWorkspace:
		return control.FeishuTargetPickerPageMode
	default:
		if targetPickerSupportsAddWorkspace(source) {
			return control.FeishuTargetPickerPageMode
		}
		return control.FeishuTargetPickerPageTarget
	}
}

func targetPickerNormalizePage(page control.FeishuTargetPickerPage, source control.TargetPickerRequestSource, mode control.FeishuTargetPickerMode, sourceKind control.FeishuTargetPickerSourceKind) control.FeishuTargetPickerPage {
	addSupported := targetPickerSupportsAddWorkspace(source)
	if page == "" {
		page = targetPickerDefaultPage(source)
	}
	switch page {
	case control.FeishuTargetPickerPageMode:
		if addSupported {
			return page
		}
		return control.FeishuTargetPickerPageTarget
	case control.FeishuTargetPickerPageTarget:
		if mode == control.FeishuTargetPickerModeAddWorkspace {
			return targetPickerNormalizePage(control.FeishuTargetPickerPageSource, source, mode, sourceKind)
		}
		return page
	case control.FeishuTargetPickerPageSource:
		if !addSupported {
			return control.FeishuTargetPickerPageTarget
		}
		if mode != control.FeishuTargetPickerModeAddWorkspace {
			return control.FeishuTargetPickerPageTarget
		}
		return page
	case control.FeishuTargetPickerPageLocalDirectory:
		if mode != control.FeishuTargetPickerModeAddWorkspace {
			return control.FeishuTargetPickerPageTarget
		}
		if sourceKind != control.FeishuTargetPickerSourceLocalDirectory {
			return control.FeishuTargetPickerPageSource
		}
		return page
	case control.FeishuTargetPickerPageGit:
		if mode != control.FeishuTargetPickerModeAddWorkspace {
			return control.FeishuTargetPickerPageTarget
		}
		if sourceKind != control.FeishuTargetPickerSourceGitURL {
			return control.FeishuTargetPickerPageSource
		}
		return page
	default:
		if mode == control.FeishuTargetPickerModeAddWorkspace {
			return control.FeishuTargetPickerPageSource
		}
		return control.FeishuTargetPickerPageTarget
	}
}

func targetPickerAdvancePage(page control.FeishuTargetPickerPage, mode control.FeishuTargetPickerMode, sourceKind control.FeishuTargetPickerSourceKind) control.FeishuTargetPickerPage {
	switch page {
	case control.FeishuTargetPickerPageMode:
		if mode == control.FeishuTargetPickerModeAddWorkspace {
			return control.FeishuTargetPickerPageSource
		}
		return control.FeishuTargetPickerPageTarget
	case control.FeishuTargetPickerPageSource:
		if sourceKind == control.FeishuTargetPickerSourceGitURL {
			return control.FeishuTargetPickerPageGit
		}
		return control.FeishuTargetPickerPageLocalDirectory
	default:
		return page
	}
}

func targetPickerPreviousPage(page control.FeishuTargetPickerPage, source control.TargetPickerRequestSource) control.FeishuTargetPickerPage {
	switch page {
	case control.FeishuTargetPickerPageTarget:
		if targetPickerSupportsAddWorkspace(source) {
			return control.FeishuTargetPickerPageMode
		}
		return control.FeishuTargetPickerPageTarget
	case control.FeishuTargetPickerPageSource:
		return control.FeishuTargetPickerPageMode
	case control.FeishuTargetPickerPageLocalDirectory, control.FeishuTargetPickerPageGit:
		return control.FeishuTargetPickerPageSource
	default:
		return page
	}
}

func targetPickerCanGoBack(page control.FeishuTargetPickerPage, source control.TargetPickerRequestSource) bool {
	return targetPickerPreviousPage(page, source) != page
}

func targetPickerViewStageLabel(record *activeTargetPickerRecord, page control.FeishuTargetPickerPage, mode control.FeishuTargetPickerMode, source control.FeishuTargetPickerSourceKind) string {
	if record == nil {
		return ""
	}
	switch record.Stage {
	case control.FeishuTargetPickerStageProcessing:
		switch record.PendingKind {
		case targetPickerPendingGitImport:
			return "Git/处理中"
		case targetPickerPendingUseThread:
			return "目标/处理中"
		case targetPickerPendingNewThread:
			if mode == control.FeishuTargetPickerModeAddWorkspace && source == control.FeishuTargetPickerSourceLocalDirectory {
				return "目录/处理中"
			}
			return "目标/处理中"
		default:
			return "处理中"
		}
	case control.FeishuTargetPickerStageSucceeded:
		return "已完成"
	case control.FeishuTargetPickerStageFailed:
		return "失败"
	case control.FeishuTargetPickerStageCancelled:
		return "已取消"
	default:
		switch page {
		case control.FeishuTargetPickerPageMode:
			return "模式"
		case control.FeishuTargetPickerPageTarget:
			return "模式/目标"
		case control.FeishuTargetPickerPageSource:
			return "模式/来源"
		case control.FeishuTargetPickerPageLocalDirectory:
			return "模式/来源/目录"
		case control.FeishuTargetPickerPageGit:
			return "模式/来源/Git"
		default:
			if mode == control.FeishuTargetPickerModeAddWorkspace {
				if source == control.FeishuTargetPickerSourceGitURL {
					return "模式/来源/Git"
				}
				return "模式/来源"
			}
			return "模式/目标"
		}
	}
}

func targetPickerViewQuestion(record *activeTargetPickerRecord, page control.FeishuTargetPickerPage) string {
	if record == nil {
		return ""
	}
	if record.Stage != control.FeishuTargetPickerStageEditing {
		return strings.TrimSpace(record.StatusTitle)
	}
	switch page {
	case control.FeishuTargetPickerPageMode:
		return "这次要做什么？"
	case control.FeishuTargetPickerPageTarget:
		return "切到哪个工作区 / 会话？"
	case control.FeishuTargetPickerPageSource:
		return "新工作区从哪里来？"
	case control.FeishuTargetPickerPageLocalDirectory:
		return "要接入哪个本地目录？"
	case control.FeishuTargetPickerPageGit:
		return "克隆哪个仓库，到哪里？"
	default:
		return "这次要做什么？"
	}
}

func targetPickerSwitchProcessingStatus(workspaceLabel, sessionLabel string) feishuCardStatusPayload {
	lines := make([]string, 0, 2)
	if workspaceLabel = strings.TrimSpace(workspaceLabel); workspaceLabel != "" {
		lines = append(lines, "工作区："+workspaceLabel)
	}
	if sessionLabel = strings.TrimSpace(sessionLabel); sessionLabel != "" {
		lines = append(lines, "会话："+sessionLabel)
	}
	sections := []control.FeishuCardTextSection{}
	if len(lines) != 0 {
		sections = append(sections, control.FeishuCardTextSection{Label: "对象", Lines: lines})
	}
	sections = append(sections, control.FeishuCardTextSection{
		Label: "当前阶段",
		Lines: []string{
			"🔄 切换目标",
			"⚪ 完成",
		},
	})
	return feishuCardStatusPayload{Sections: sections}
}

func targetPickerLocalDirectoryProcessingStatus(path string) feishuCardStatusPayload {
	sections := []control.FeishuCardTextSection{}
	if path = strings.TrimSpace(path); path != "" {
		sections = append(sections, control.FeishuCardTextSection{Label: "目录", Lines: []string{path}})
	}
	sections = append(sections, control.FeishuCardTextSection{
		Label: "当前阶段",
		Lines: []string{
			"✅ 校验目录",
			"🔄 接入工作区",
			"⚪ 准备新会话",
		},
	})
	return feishuCardStatusPayload{Sections: sections}
}
