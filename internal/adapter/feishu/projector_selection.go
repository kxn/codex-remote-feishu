package feishu

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func selectionPromptElements(prompt control.SelectionPrompt, daemonLifecycleID string) []map[string]any {
	if prompt.Kind == control.SelectionPromptUseThread {
		return useThreadSelectionPromptElements(prompt, daemonLifecycleID)
	}
	if prompt.Kind == control.SelectionPromptAttachInstance {
		return attachInstanceSelectionPromptElements(prompt, daemonLifecycleID)
	}
	if prompt.Kind == control.SelectionPromptAttachWorkspace {
		return attachWorkspaceSelectionPromptElements(prompt, daemonLifecycleID)
	}
	if len(prompt.Options) == 0 {
		return nil
	}
	elements := make([]map[string]any, 0, len(prompt.Options)*2+1)
	for _, option := range prompt.Options {
		button := cardButtonGroupElement([]map[string]any{selectionOptionButton(prompt, option, daemonLifecycleID)})
		line := selectionOptionBody(prompt.Kind, option)
		if prompt.Kind == control.SelectionPromptUseThread {
			if len(button) != 0 {
				elements = append(elements, button)
			}
			if line != "" {
				elements = append(elements, map[string]any{
					"tag":     "markdown",
					"content": line,
				})
			}
			continue
		}
		if line != "" {
			elements = append(elements, map[string]any{
				"tag":     "markdown",
				"content": line,
			})
		}
		if len(button) != 0 {
			elements = append(elements, button)
		}
	}
	if hint := strings.TrimSpace(prompt.Hint); hint != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": renderSystemInlineTags(hint),
		})
	}
	return elements
}

func attachInstanceSelectionPromptElements(prompt control.SelectionPrompt, daemonLifecycleID string) []map[string]any {
	return buildAttachSelectionPromptElements(prompt, daemonLifecycleID, "当前实例", nil)
}

func attachWorkspaceSelectionPromptElements(prompt control.SelectionPrompt, daemonLifecycleID string) []map[string]any {
	return buildAttachSelectionPromptElements(prompt, daemonLifecycleID, "当前工作区", func(option control.SelectionOption) bool {
		switch strings.TrimSpace(option.ActionKind) {
		case "show_all_workspaces", "show_recent_workspaces":
			return true
		default:
			return false
		}
	})
}

func buildAttachSelectionPromptElements(
	prompt control.SelectionPrompt,
	daemonLifecycleID string,
	currentHeading string,
	isMoreOption func(control.SelectionOption) bool,
) []map[string]any {
	available := make([]control.SelectionOption, 0, len(prompt.Options))
	unavailable := make([]control.SelectionOption, 0, len(prompt.Options))
	current := make([]control.SelectionOption, 0, 1)
	more := make([]control.SelectionOption, 0, 1)
	for _, option := range prompt.Options {
		if isMoreOption != nil && isMoreOption(option) {
			more = append(more, option)
			continue
		}
		switch {
		case option.IsCurrent:
			current = append(current, option)
		case option.Disabled:
			unavailable = append(unavailable, option)
		default:
			available = append(available, option)
		}
	}

	capacity := len(prompt.Options)*2 + 4
	if strings.TrimSpace(prompt.ContextTitle) != "" || strings.TrimSpace(prompt.ContextText) != "" {
		capacity += 2
	}
	if len(current) > 0 {
		capacity += len(current) * 2
	}
	elements := make([]map[string]any, 0, capacity)

	if title := strings.TrimSpace(prompt.ContextTitle); title != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**" + title + "**",
		})
	}
	if text := strings.TrimSpace(prompt.ContextText); text != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": renderSystemInlineTags(text),
		})
	}

	elements = appendAttachSelectionSection(elements, prompt, daemonLifecycleID, currentHeading, current)
	elements = appendAttachSelectionSection(elements, prompt, daemonLifecycleID, "可接管", available)
	elements = appendAttachSelectionSection(elements, prompt, daemonLifecycleID, "其他状态", unavailable)
	elements = appendAttachSelectionSection(elements, prompt, daemonLifecycleID, "更多", more)

	if hint := strings.TrimSpace(prompt.Hint); hint != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": renderSystemInlineTags(hint),
		})
	}
	if len(elements) == 0 {
		return nil
	}
	return elements
}

func appendAttachSelectionSection(
	elements []map[string]any,
	prompt control.SelectionPrompt,
	daemonLifecycleID string,
	title string,
	options []control.SelectionOption,
) []map[string]any {
	if len(options) == 0 {
		return elements
	}
	elements = append(elements, map[string]any{
		"tag":     "markdown",
		"content": "**" + title + "**",
	})
	for _, option := range options {
		if button := cardButtonGroupElement([]map[string]any{selectionOptionButton(prompt, option, daemonLifecycleID)}); len(button) != 0 {
			elements = append(elements, button)
		}
		if meta := strings.TrimSpace(firstNonEmpty(option.MetaText, selectionOptionBody(prompt.Kind, option))); meta != "" {
			elements = append(elements, map[string]any{
				"tag":     "markdown",
				"content": renderSystemInlineTags(meta),
			})
		}
	}
	return elements
}

type useThreadOptionGroup string

const (
	useThreadOptionGroupCurrent     useThreadOptionGroup = "current"
	useThreadOptionGroupTakeover    useThreadOptionGroup = "takeover"
	useThreadOptionGroupUnavailable useThreadOptionGroup = "unavailable"
	useThreadOptionGroupMore        useThreadOptionGroup = "more"
)

func selectionOptionBody(kind control.SelectionPromptKind, option control.SelectionOption) string {
	current := ""
	if option.IsCurrent {
		current = " [当前]"
	}
	switch kind {
	case control.SelectionPromptAttachInstance:
		if option.Subtitle != "" {
			parts := strings.Split(option.Subtitle, "\n")
			line := fmt.Sprintf("%d. %s - 工作目录 %s%s", option.Index, option.Label, formatNeutralTextTag(parts[0]), current)
			if len(parts) > 1 {
				line += "\n" + strings.Join(parts[1:], "\n")
			}
			return line
		}
	case control.SelectionPromptAttachWorkspace:
		if option.Subtitle != "" {
			parts := strings.Split(option.Subtitle, "\n")
			line := fmt.Sprintf("%d. %s - 工作区 %s%s", option.Index, option.Label, formatNeutralTextTag(parts[0]), current)
			if len(parts) > 1 {
				line += "\n" + strings.Join(parts[1:], "\n")
			}
			return line
		}
	case control.SelectionPromptUseThread:
		if option.Subtitle == "" {
			return ""
		}
		parts := strings.Split(option.Subtitle, "\n")
		lines := make([]string, 0, len(parts))
		for i, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if i == 0 && strings.HasPrefix(part, "/") {
				lines = append(lines, formatNeutralTextTag(part))
				continue
			}
			lines = append(lines, part)
		}
		return strings.Join(lines, "\n")
	default:
		if option.Subtitle != "" {
			parts := strings.Split(option.Subtitle, "\n")
			line := fmt.Sprintf("%d. %s%s", option.Index, option.Label, current)
			if len(parts) > 0 && parts[0] != "" {
				line += "\n" + formatNeutralTextTag(parts[0])
			}
			if len(parts) > 1 {
				line += "\n" + strings.Join(parts[1:], "\n")
			}
			return line
		}
	}
	return fmt.Sprintf("%d. %s%s", option.Index, option.Label, current)
}

func selectionOptionButton(prompt control.SelectionPrompt, option control.SelectionOption, daemonLifecycleID string) map[string]any {
	text := selectionOptionButtonText(prompt, option)
	value := map[string]any{}
	switch strings.TrimSpace(option.ActionKind) {
	case "show_scoped_threads":
		value = map[string]any{"kind": "show_scoped_threads"}
	case "show_all_workspaces":
		value = map[string]any{"kind": "show_all_workspaces"}
	case "show_recent_workspaces":
		value = map[string]any{"kind": "show_recent_workspaces"}
	case "show_all_thread_workspaces":
		value = map[string]any{"kind": "show_all_thread_workspaces"}
	case "show_recent_thread_workspaces":
		value = map[string]any{"kind": "show_recent_thread_workspaces"}
	case "show_workspace_threads":
		value = map[string]any{"kind": "show_workspace_threads", "workspace_key": strings.TrimSpace(option.OptionID)}
	case "show_threads":
		value = map[string]any{"kind": "show_threads"}
	case "show_all_threads":
		value = map[string]any{"kind": "show_all_threads"}
	}
	switch prompt.Kind {
	case control.SelectionPromptAttachInstance:
		if len(value) == 0 {
			if text == "选择" {
				text = "接管"
			}
			value = map[string]any{
				"kind":        "attach_instance",
				"instance_id": strings.TrimSpace(option.OptionID),
			}
		}
	case control.SelectionPromptAttachWorkspace:
		if len(value) == 0 {
			if text == "选择" {
				text = "接管"
			}
			value = map[string]any{
				"kind":          "attach_workspace",
				"workspace_key": strings.TrimSpace(option.OptionID),
			}
		}
	case control.SelectionPromptUseThread:
		if len(value) == 0 {
			value = map[string]any{
				"kind":                  "use_thread",
				"thread_id":             strings.TrimSpace(option.OptionID),
				"allow_cross_workspace": option.AllowCrossWorkspace,
			}
		}
	case control.SelectionPromptKickThread:
		if strings.TrimSpace(option.OptionID) == "cancel" {
			value = map[string]any{"kind": "kick_thread_cancel"}
		} else {
			value = map[string]any{
				"kind":      "kick_thread_confirm",
				"thread_id": strings.TrimSpace(option.OptionID),
			}
		}
	}
	if len(value) == 0 {
		value = map[string]any{
			"kind":      "use_thread",
			"thread_id": strings.TrimSpace(option.OptionID),
		}
	}
	stampActionValue(value, daemonLifecycleID)
	disabled := option.Disabled
	buttonType := "default"
	if option.IsCurrent {
		disabled = true
		if prompt.Kind != control.SelectionPromptUseThread {
			text = "当前"
		}
	} else {
		buttonType = "primary"
	}
	width := ""
	if prompt.Kind == control.SelectionPromptUseThread || prompt.Kind == control.SelectionPromptAttachWorkspace || prompt.Kind == control.SelectionPromptAttachInstance {
		width = "fill"
	}
	button := cardCallbackButtonElement(text, buttonType, value, disabled, width)
	return button
}

func selectionOptionButtonText(prompt control.SelectionPrompt, option control.SelectionOption) string {
	text := strings.TrimSpace(option.ButtonLabel)
	switch strings.TrimSpace(option.ActionKind) {
	case "show_all_workspaces":
		base := firstNonEmpty(strings.TrimSpace(option.ButtonLabel), strings.TrimSpace(option.Label), "全部工作区")
		return "查看全部 · " + base
	case "show_recent_workspaces":
		base := firstNonEmpty(strings.TrimSpace(option.ButtonLabel), strings.TrimSpace(option.Label), "最近工作区")
		return "返回 · " + base
	case "show_all_thread_workspaces":
		base := firstNonEmpty(strings.TrimSpace(option.ButtonLabel), strings.TrimSpace(option.Label), "全部工作区")
		return "查看全部 · " + base
	case "show_recent_thread_workspaces":
		base := firstNonEmpty(strings.TrimSpace(option.ButtonLabel), strings.TrimSpace(option.Label), "最近工作区")
		return "返回 · " + base
	}
	if prompt.Kind == control.SelectionPromptAttachInstance {
		summary := firstNonEmpty(strings.TrimSpace(option.Label), text, "实例")
		switch {
		case option.IsCurrent:
			return "当前 · " + summary
		case option.Disabled:
			return "不可接管 · " + summary
		case text == "切换":
			return "切换 · " + summary
		default:
			return "接管 · " + summary
		}
	}
	if prompt.Kind == control.SelectionPromptAttachWorkspace {
		summary := firstNonEmpty(strings.TrimSpace(option.Label), text, "工作区")
		if strings.TrimSpace(option.ActionKind) == "show_workspace_threads" {
			switch {
			case option.IsCurrent:
				return "当前 · " + summary
			case option.Disabled:
				return "不可恢复 · " + summary
			default:
				return "恢复 · " + summary
			}
		}
		switch {
		case option.IsCurrent:
			return "当前 · " + summary
		case option.Disabled:
			return "不可接管 · " + summary
		case text == "切换":
			return "切换 · " + summary
		default:
			return "接管 · " + summary
		}
	}
	if prompt.Kind != control.SelectionPromptUseThread {
		if text == "" {
			return "选择"
		}
		return text
	}
	if strings.TrimSpace(option.ActionKind) == "show_scoped_threads" {
		base := firstNonEmpty(strings.TrimSpace(option.ButtonLabel), strings.TrimSpace(option.Label), "全部会话")
		return "查看全部 · " + base
	}
	if strings.TrimSpace(option.ActionKind) == "show_threads" {
		base := firstNonEmpty(strings.TrimSpace(option.ButtonLabel), strings.TrimSpace(option.Label), "最近会话")
		return "返回 · " + base
	}
	if strings.TrimSpace(option.ActionKind) == "show_all_threads" {
		base := firstNonEmpty(strings.TrimSpace(option.ButtonLabel), strings.TrimSpace(option.Label), "全部会话")
		return "返回 · " + base
	}
	if strings.TrimSpace(option.ActionKind) == "show_workspace_threads" {
		base := firstNonEmpty(strings.TrimSpace(option.ButtonLabel), strings.TrimSpace(option.Label), "工作区全部会话")
		return "查看全部 · " + base
	}
	summary := firstNonEmpty(strings.TrimSpace(option.Label), strings.TrimSpace(option.ButtonLabel), "未命名会话")
	switch {
	case option.IsCurrent:
		return "当前 · " + summary
	case option.Disabled:
		return "不可接管 · " + summary
	default:
		return "接管 · " + summary
	}
}
