package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func targetPickerViewStageLabel(record *activeTargetPickerRecord, mode control.FeishuTargetPickerMode, source control.FeishuTargetPickerSourceKind) string {
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
		switch mode {
		case control.FeishuTargetPickerModeAddWorkspace:
			switch source {
			case control.FeishuTargetPickerSourceGitURL:
				return "模式/来源/Git"
			case control.FeishuTargetPickerSourceLocalDirectory:
				return "模式/来源/目录"
			default:
				return "模式/来源"
			}
		default:
			return "模式/目标"
		}
	}
}

func targetPickerViewQuestion(record *activeTargetPickerRecord, mode control.FeishuTargetPickerMode, source control.FeishuTargetPickerSourceKind) string {
	if record == nil {
		return ""
	}
	if record.Stage != control.FeishuTargetPickerStageEditing {
		return strings.TrimSpace(record.StatusTitle)
	}
	switch mode {
	case control.FeishuTargetPickerModeAddWorkspace:
		switch source {
		case control.FeishuTargetPickerSourceGitURL:
			return "克隆哪个仓库，到哪里？"
		case control.FeishuTargetPickerSourceLocalDirectory:
			return "要接入哪个本地目录？"
		default:
			return "新工作区从哪里来？"
		}
	default:
		return "切到哪个工作区 / 会话？"
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
