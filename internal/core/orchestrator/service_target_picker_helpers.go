package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func targetPickerTitle(source control.TargetPickerRequestSource) string {
	switch source {
	case control.TargetPickerRequestSourceDir:
		return "从目录新建工作区"
	case control.TargetPickerRequestSourceGit:
		return "从 GIT URL 新建工作区"
	case control.TargetPickerRequestSourceUse:
		return "切换工作会话"
	default:
		return "切换工作会话"
	}
}

func targetPickerWorkspaceMetaText(entry workspaceSelectionEntry, disambiguateWithPath bool) string {
	_ = entry
	_ = disambiguateWithPath
	return ""
}

func parseTargetPickerSessionValue(value string) (control.FeishuTargetPickerSessionKind, string) {
	value = strings.TrimSpace(value)
	switch {
	case value == targetPickerNewThreadValue:
		return control.FeishuTargetPickerSessionNewThread, ""
	case strings.HasPrefix(value, targetPickerThreadPrefix):
		return control.FeishuTargetPickerSessionThread, strings.TrimPrefix(value, targetPickerThreadPrefix)
	default:
		return "", ""
	}
}

func targetPickerSessionMetaText(source control.TargetPickerRequestSource, value string) string {
	if targetPickerRequiresExistingWorkspace(source) {
		return ""
	}
	return strings.TrimSpace(value)
}
