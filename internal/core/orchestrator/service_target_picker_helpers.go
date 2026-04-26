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
		return "切换工作会话"
	default:
		return "切换工作会话"
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

func targetPickerSupportsAddWorkspace(source control.TargetPickerRequestSource) bool {
	switch source {
	case control.TargetPickerRequestSourceDir,
		control.TargetPickerRequestSourceGit,
		control.TargetPickerRequestSourceWorktree:
		return true
	default:
		return false
	}
}

func targetPickerDefaultMode(source control.TargetPickerRequestSource) control.FeishuTargetPickerMode {
	if targetPickerSupportsAddWorkspace(source) {
		return control.FeishuTargetPickerModeAddWorkspace
	}
	if targetPickerRequiresExistingWorkspace(source) {
		return control.FeishuTargetPickerModeExistingWorkspace
	}
	return control.FeishuTargetPickerModeExistingWorkspace
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

func normalizeTargetPickerMode(value string) control.FeishuTargetPickerMode {
	switch control.FeishuTargetPickerMode(strings.TrimSpace(value)) {
	case control.FeishuTargetPickerModeExistingWorkspace, control.FeishuTargetPickerModeAddWorkspace:
		return control.FeishuTargetPickerMode(strings.TrimSpace(value))
	default:
		return ""
	}
}

func targetPickerModeOptions(addSupported, hasExistingWorkspace bool, selected control.FeishuTargetPickerMode) []control.FeishuTargetPickerModeOption {
	if !addSupported {
		return nil
	}
	return []control.FeishuTargetPickerModeOption{
		{
			Value:             control.FeishuTargetPickerModeExistingWorkspace,
			Label:             "进入已有工作区",
			MetaText:          "切到一个已有工作区里的某个会话",
			Selected:          selected == control.FeishuTargetPickerModeExistingWorkspace,
			Available:         hasExistingWorkspace,
			UnavailableReason: targetPickerModeUnavailableReason(hasExistingWorkspace),
		},
		{
			Value:     control.FeishuTargetPickerModeAddWorkspace,
			Label:     "新建工作区",
			MetaText:  "接入目录或克隆仓库后，直接进入一个新会话",
			Selected:  selected == control.FeishuTargetPickerModeAddWorkspace,
			Available: true,
		},
	}
}

func targetPickerModeAvailable(options []control.FeishuTargetPickerModeOption, value control.FeishuTargetPickerMode) bool {
	for _, option := range options {
		if option.Value == value {
			return targetPickerModeOptionAvailable(option)
		}
	}
	return false
}

func targetPickerModeOptionAvailable(option control.FeishuTargetPickerModeOption) bool {
	return option.Available || strings.TrimSpace(option.UnavailableReason) == ""
}

func targetPickerModeUnavailableReason(hasExistingWorkspace bool) string {
	if hasExistingWorkspace {
		return ""
	}
	return "当前没有已有工作区可进入，请先新建工作区。"
}

func normalizeTargetPickerSourceKind(value string) control.FeishuTargetPickerSourceKind {
	switch control.FeishuTargetPickerSourceKind(strings.TrimSpace(value)) {
	case control.FeishuTargetPickerSourceLocalDirectory,
		control.FeishuTargetPickerSourceGitURL,
		control.FeishuTargetPickerSourceGitWorktree:
		return control.FeishuTargetPickerSourceKind(strings.TrimSpace(value))
	default:
		return ""
	}
}

func (s *Service) targetPickerSourceOptions() []control.FeishuTargetPickerSourceOption {
	options := []control.FeishuTargetPickerSourceOption{{
		Value:     control.FeishuTargetPickerSourceLocalDirectory,
		Label:     "已有目录",
		MetaText:  "接入本机上已经存在的目录，并在完成后进入新会话待命",
		Available: true,
	}}
	gitOption := control.FeishuTargetPickerSourceOption{
		Value:     control.FeishuTargetPickerSourceGitURL,
		Label:     "从 Git URL",
		MetaText:  "填写仓库地址后拉取到本地，并在完成后进入新会话待命",
		Available: s.config.GitAvailable,
	}
	if !gitOption.Available {
		gitOption.MetaText = "需要本机已安装 git 后才能使用"
		gitOption.UnavailableReason = "当前机器未检测到 `git`，暂时不能直接从 Git URL 导入。"
	}
	options = append(options, gitOption)
	return options
}

func targetPickerDefaultSourceKind(source control.TargetPickerRequestSource) control.FeishuTargetPickerSourceKind {
	switch source {
	case control.TargetPickerRequestSourceGit:
		return control.FeishuTargetPickerSourceGitURL
	case control.TargetPickerRequestSourceDir:
		return control.FeishuTargetPickerSourceLocalDirectory
	case control.TargetPickerRequestSourceWorktree:
		return control.FeishuTargetPickerSourceGitWorktree
	default:
		return ""
	}
}

func targetPickerAllowsNewThread(source control.TargetPickerRequestSource, allowNewThread bool) bool {
	if !allowNewThread {
		return false
	}
	return targetPickerUsesSessionSelection(source)
}

func targetPickerHasSourceOption(options []control.FeishuTargetPickerSourceOption, value control.FeishuTargetPickerSourceKind) bool {
	for _, option := range options {
		if option.Value == value {
			return true
		}
	}
	return false
}

func targetPickerDefaultSourceSelection(options []control.FeishuTargetPickerSourceOption) control.FeishuTargetPickerSourceKind {
	for _, option := range options {
		if option.Value != "" {
			return option.Value
		}
	}
	return ""
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

func targetPickerSourceAvailable(options []control.FeishuTargetPickerSourceOption, value control.FeishuTargetPickerSourceKind) bool {
	for _, option := range options {
		if option.Value == value {
			return option.Available
		}
	}
	return false
}

func targetPickerSourceUnavailableReason(options []control.FeishuTargetPickerSourceOption, value control.FeishuTargetPickerSourceKind) string {
	for _, option := range options {
		if option.Value == value {
			return strings.TrimSpace(option.UnavailableReason)
		}
	}
	return ""
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
