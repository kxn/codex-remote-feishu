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
	case control.TargetPickerRequestSourceWorktree:
		return "从 Worktree 新建工作区"
	case control.TargetPickerRequestSourceUse:
		return "切换工作区与会话"
	default:
		return "切换工作区与会话"
	}
}

func targetPickerWorkspaceMetaText(entry workspaceSelectionEntry, metaByKey map[string]string) string {
	if len(metaByKey) == 0 {
		return ""
	}
	return strings.TrimSpace(metaByKey[normalizeWorkspaceClaimKey(entry.workspaceKey)])
}

func targetPickerLockedWorkspaceSummary(entries []workspaceSelectionEntry, workspaceKey string) (string, string) {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if workspaceKey == "" {
		return "", ""
	}
	metaByKey := targetPickerWorkspaceMetaByKey(entries)
	for _, entry := range entries {
		if normalizeWorkspaceClaimKey(entry.workspaceKey) != workspaceKey {
			continue
		}
		label := strings.TrimSpace(firstNonEmpty(entry.label, workspaceSelectionLabel(workspaceKey)))
		return label, targetPickerWorkspaceMetaText(entry, metaByKey)
	}
	return workspaceSelectionLabel(workspaceKey), ""
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

func targetPickerHasWorkspaceOption(options []control.FeishuTargetPickerWorkspaceOption, value string) bool {
	for _, option := range options {
		if option.Value == value {
			return true
		}
	}
	return false
}

func targetPickerDefaultWorkspaceSelection(options []control.FeishuTargetPickerWorkspaceOption) string {
	for _, option := range options {
		if strings.TrimSpace(option.Value) == "" || option.Synthetic {
			continue
		}
		return option.Value
	}
	for _, option := range options {
		if strings.TrimSpace(option.Value) != "" {
			return option.Value
		}
	}
	return ""
}

func targetPickerWorkspaceOptionIndex(options []control.FeishuTargetPickerWorkspaceOption, value string) int {
	for i, option := range options {
		if option.Value == value {
			return i
		}
	}
	return -1
}

func targetPickerWorkspaceValueAtCursor(options []control.FeishuTargetPickerWorkspaceOption, cursor int) string {
	if len(options) == 0 {
		return ""
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(options) {
		cursor = len(options) - 1
	}
	return normalizeTargetPickerWorkspaceSelection(options[cursor].Value)
}

func normalizeTargetPickerWorkspaceSelection(value string) string {
	return normalizeWorkspaceClaimKey(value)
}

func targetPickerHasSessionOption(options []control.FeishuTargetPickerSessionOption, value string) bool {
	for _, option := range options {
		if option.Value == value {
			return true
		}
	}
	return false
}

func targetPickerOnlyNewThreadSessionOption(options []control.FeishuTargetPickerSessionOption) bool {
	if len(options) != 1 {
		return false
	}
	return options[0].Kind == control.FeishuTargetPickerSessionNewThread && options[0].Value == targetPickerNewThreadValue
}

func targetPickerShouldAutoSelectNewThread(options []control.FeishuTargetPickerSessionOption, allowNewThread bool) bool {
	return allowNewThread && targetPickerOnlyNewThreadSessionOption(options)
}

func targetPickerSelectedWorkspaceSummary(options []control.FeishuTargetPickerWorkspaceOption, value string) (string, string) {
	for _, option := range options {
		if option.Value == value {
			return option.Label, option.MetaText
		}
	}
	return "", ""
}

func targetPickerSelectedSessionSummary(options []control.FeishuTargetPickerSessionOption, value string) (string, string) {
	for _, option := range options {
		if option.Value == value {
			return option.Label, option.MetaText
		}
	}
	return "", ""
}

func targetPickerThreadValue(threadID string) string {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return ""
	}
	return targetPickerThreadPrefix + threadID
}

func targetPickerRequiresExistingWorkspace(source control.TargetPickerRequestSource) bool {
	switch source {
	case control.TargetPickerRequestSourceList,
		control.TargetPickerRequestSourceUse,
		control.TargetPickerRequestSourceUseAll,
		control.TargetPickerRequestSourceWorkspace:
		return true
	default:
		return false
	}
}

func targetPickerRequiresWorkspaceSelection(source control.TargetPickerRequestSource) bool {
	if targetPickerRequiresExistingWorkspace(source) {
		return true
	}
	return source == control.TargetPickerRequestSourceWorktree
}

func targetPickerUsesSessionSelection(source control.TargetPickerRequestSource) bool {
	return targetPickerRequiresExistingWorkspace(source)
}

func targetPickerAllowsNewThread(source control.TargetPickerRequestSource, allowNewThread bool) bool {
	if !allowNewThread {
		return false
	}
	return targetPickerUsesSessionSelection(source)
}

func targetPickerSourceDefaultsToAllowNewThread(source control.TargetPickerRequestSource) bool {
	switch source {
	case control.TargetPickerRequestSourceUse,
		control.TargetPickerRequestSourceUseAll,
		control.TargetPickerRequestSourceWorkspace:
		return true
	default:
		return false
	}
}

func targetPickerSessionOptionIndex(options []control.FeishuTargetPickerSessionOption, value string) int {
	for i, option := range options {
		if option.Value == value {
			return i
		}
	}
	return -1
}

func normalizeTargetPickerDropdownCursor(cursor int, optionCount int) int {
	if optionCount <= 0 {
		return 0
	}
	if cursor < 0 {
		return 0
	}
	if cursor >= optionCount {
		return optionCount - 1
	}
	return cursor
}

func targetPickerWorkspaceOptions(entries []workspaceSelectionEntry) []control.FeishuTargetPickerWorkspaceOption {
	if len(entries) == 0 {
		return nil
	}
	metaByKey := targetPickerWorkspaceMetaByKey(entries)
	options := make([]control.FeishuTargetPickerWorkspaceOption, 0, len(entries))
	for _, entry := range entries {
		label := strings.TrimSpace(entry.label)
		options = append(options, control.FeishuTargetPickerWorkspaceOption{
			Value:           entry.workspaceKey,
			Label:           label,
			MetaText:        targetPickerWorkspaceMetaText(entry, metaByKey),
			RecoverableOnly: entry.recoverableOnly,
		})
	}
	return options
}
