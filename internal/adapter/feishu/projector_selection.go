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
	available := make([]control.SelectionOption, 0, len(prompt.Options))
	unavailable := make([]control.SelectionOption, 0, len(prompt.Options))
	current := make([]control.SelectionOption, 0, 1)
	for _, option := range prompt.Options {
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

	if len(current) > 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**当前实例**",
		})
		for _, option := range current {
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
	}

	if len(available) > 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**可接管**",
		})
		for _, option := range available {
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
	}

	if len(unavailable) > 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**其他状态**",
		})
		for _, option := range unavailable {
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
	}

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

func attachWorkspaceSelectionPromptElements(prompt control.SelectionPrompt, daemonLifecycleID string) []map[string]any {
	available := make([]control.SelectionOption, 0, len(prompt.Options))
	unavailable := make([]control.SelectionOption, 0, len(prompt.Options))
	current := make([]control.SelectionOption, 0, 1)
	more := make([]control.SelectionOption, 0, 1)
	for _, option := range prompt.Options {
		switch strings.TrimSpace(option.ActionKind) {
		case "show_all_workspaces", "show_recent_workspaces":
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

	if len(current) > 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**当前工作区**",
		})
		for _, option := range current {
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
	}

	if len(available) > 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**可接管**",
		})
		for _, option := range available {
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
	}

	if len(unavailable) > 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**其他状态**",
		})
		for _, option := range unavailable {
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
	}

	if len(more) > 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**更多**",
		})
		for _, option := range more {
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
	}

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

type useThreadOptionGroup string

const (
	useThreadOptionGroupCurrent     useThreadOptionGroup = "current"
	useThreadOptionGroupTakeover    useThreadOptionGroup = "takeover"
	useThreadOptionGroupUnavailable useThreadOptionGroup = "unavailable"
	useThreadOptionGroupMore        useThreadOptionGroup = "more"
)

func useThreadSelectionPromptElements(prompt control.SelectionPrompt, daemonLifecycleID string) []map[string]any {
	if useThreadPromptUsesVSCodeInstanceLayout(prompt) {
		return useThreadVSCodeInstanceElements(prompt, daemonLifecycleID)
	}
	if useThreadPromptUsesWorkspaceGrouping(prompt) {
		return useThreadWorkspaceGroupedElements(prompt, daemonLifecycleID)
	}
	grouped := map[useThreadOptionGroup][]control.SelectionOption{
		useThreadOptionGroupCurrent:     {},
		useThreadOptionGroupTakeover:    {},
		useThreadOptionGroupUnavailable: {},
		useThreadOptionGroupMore:        {},
	}
	for _, option := range prompt.Options {
		group := useThreadSelectionOptionGroup(option)
		grouped[group] = append(grouped[group], option)
	}
	order := []useThreadOptionGroup{
		useThreadOptionGroupCurrent,
		useThreadOptionGroupTakeover,
		useThreadOptionGroupUnavailable,
		useThreadOptionGroupMore,
	}
	elements := make([]map[string]any, 0, len(prompt.Options)*3+4)
	for _, group := range order {
		options := grouped[group]
		if len(options) == 0 {
			continue
		}
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**" + useThreadSelectionGroupTitle(group) + "**",
		})
		for _, option := range options {
			if button := cardButtonGroupElement([]map[string]any{selectionOptionButton(prompt, option, daemonLifecycleID)}); len(button) != 0 {
				elements = append(elements, button)
			}
			if line := selectionOptionBody(prompt.Kind, option); line != "" {
				elements = append(elements, map[string]any{
					"tag":     "markdown",
					"content": line,
				})
			}
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

func useThreadPromptUsesVSCodeInstanceLayout(prompt control.SelectionPrompt) bool {
	return strings.TrimSpace(prompt.Layout) == "vscode_instance_threads"
}

func useThreadVSCodeInstanceElements(prompt control.SelectionPrompt, daemonLifecycleID string) []map[string]any {
	elements := make([]map[string]any, 0, len(prompt.Options)*3+8)
	isFullView := strings.TrimSpace(prompt.Title) == "当前实例全部会话"

	current := make([]control.SelectionOption, 0, 1)
	remaining := make([]control.SelectionOption, 0, len(prompt.Options))
	more := make([]control.SelectionOption, 0, 1)
	available := make([]control.SelectionOption, 0, len(prompt.Options))
	unavailable := make([]control.SelectionOption, 0, len(prompt.Options))
	for _, option := range prompt.Options {
		switch strings.TrimSpace(option.ActionKind) {
		case "show_scoped_threads", "show_threads":
			more = append(more, option)
			continue
		}
		if option.IsCurrent {
			current = append(current, option)
			continue
		}
		remaining = append(remaining, option)
		if option.Disabled {
			unavailable = append(unavailable, option)
			continue
		}
		available = append(available, option)
	}

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

	if len(current) > 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**当前会话**",
		})
		for _, option := range current {
			elements = append(elements, useThreadActionElement(prompt, option, daemonLifecycleID))
			if meta := strings.TrimSpace(firstNonEmpty(option.MetaText, selectionOptionBody(prompt.Kind, option))); meta != "" {
				elements = append(elements, map[string]any{
					"tag":     "markdown",
					"content": renderSystemInlineTags(meta),
				})
			}
		}
	}

	if isFullView {
		if len(remaining) > 0 {
			elements = append(elements, map[string]any{
				"tag":     "markdown",
				"content": "**全部会话**",
			})
		}
		for index, option := range remaining {
			meta := strings.TrimSpace(firstNonEmpty(option.MetaText, "时间未知"))
			elements = append(elements, map[string]any{
				"tag":     "markdown",
				"content": fmt.Sprintf("%d. %s", index+1, renderSystemInlineTags(meta)),
			})
			elements = append(elements, useThreadActionElement(prompt, option, daemonLifecycleID))
		}
	} else {
		if len(available) > 0 {
			elements = append(elements, map[string]any{
				"tag":     "markdown",
				"content": "**可接管**",
			})
			for _, option := range available {
				elements = append(elements, useThreadActionElement(prompt, option, daemonLifecycleID))
				if meta := strings.TrimSpace(firstNonEmpty(option.MetaText, selectionOptionBody(prompt.Kind, option))); meta != "" {
					elements = append(elements, map[string]any{
						"tag":     "markdown",
						"content": renderSystemInlineTags(meta),
					})
				}
			}
		}
		if len(unavailable) > 0 {
			elements = append(elements, map[string]any{
				"tag":     "markdown",
				"content": "**其他状态**",
			})
			for _, option := range unavailable {
				elements = append(elements, useThreadActionElement(prompt, option, daemonLifecycleID))
				if meta := strings.TrimSpace(firstNonEmpty(option.MetaText, selectionOptionBody(prompt.Kind, option))); meta != "" {
					elements = append(elements, map[string]any{
						"tag":     "markdown",
						"content": renderSystemInlineTags(meta),
					})
				}
			}
		}
	}

	if len(more) > 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**更多**",
		})
		for _, option := range more {
			elements = append(elements, useThreadActionElement(prompt, option, daemonLifecycleID))
			if meta := strings.TrimSpace(firstNonEmpty(option.MetaText, selectionOptionBody(prompt.Kind, option))); meta != "" {
				elements = append(elements, map[string]any{
					"tag":     "markdown",
					"content": renderSystemInlineTags(meta),
				})
			}
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

type useThreadWorkspaceGroup struct {
	Key     string
	Label   string
	AgeText string
	Options []control.SelectionOption
}

func useThreadPromptUsesWorkspaceGrouping(prompt control.SelectionPrompt) bool {
	if strings.TrimSpace(prompt.Layout) != "workspace_grouped_useall" {
		return false
	}
	for _, option := range prompt.Options {
		if strings.TrimSpace(option.GroupKey) != "" {
			return true
		}
	}
	return false
}

func useThreadWorkspaceGroupedElements(prompt control.SelectionPrompt, daemonLifecycleID string) []map[string]any {
	elements := make([]map[string]any, 0, len(prompt.Options)*3+8)
	currentOptions := make([]control.SelectionOption, 0, 1)
	moreOptions := make([]control.SelectionOption, 0, 1)
	groups := make([]useThreadWorkspaceGroup, 0)
	groupIndex := map[string]int{}
	for _, option := range prompt.Options {
		switch strings.TrimSpace(option.ActionKind) {
		case "show_scoped_threads":
			continue
		case "show_threads", "show_all_threads", "show_all_thread_workspaces", "show_recent_thread_workspaces":
			moreOptions = append(moreOptions, option)
			continue
		}
		if option.IsCurrent {
			currentOptions = append(currentOptions, option)
			continue
		}
		groupKey := strings.TrimSpace(option.GroupKey)
		if groupKey == "" || groupKey == strings.TrimSpace(prompt.ContextKey) {
			continue
		}
		position, ok := groupIndex[groupKey]
		if !ok {
			position = len(groups)
			groupIndex[groupKey] = position
			groups = append(groups, useThreadWorkspaceGroup{
				Key:     groupKey,
				Label:   firstNonEmpty(strings.TrimSpace(option.GroupLabel), groupKey),
				AgeText: strings.TrimSpace(option.AgeText),
				Options: []control.SelectionOption{},
			})
		}
		groups[position].Options = append(groups[position].Options, option)
	}
	singleWorkspaceView := strings.TrimSpace(prompt.Title) != "全部会话" && strings.TrimSpace(prompt.ContextTitle) == ""

	if len(currentOptions) > 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**当前会话**",
		})
		for _, option := range currentOptions {
			elements = append(elements, useThreadActionElement(prompt, option, daemonLifecycleID))
			if meta := strings.TrimSpace(firstNonEmpty(option.MetaText, selectionOptionBody(prompt.Kind, option))); meta != "" {
				elements = append(elements, map[string]any{
					"tag":     "markdown",
					"content": meta,
				})
			}
		}
	}

	if !singleWorkspaceView {
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
		if contextKey := strings.TrimSpace(prompt.ContextKey); contextKey != "" {
			label := "查看当前工作区全部会话"
			if button := cardButtonGroupElement([]map[string]any{workspaceThreadsButton(label, contextKey, daemonLifecycleID)}); len(button) != 0 {
				elements = append(elements, button)
			}
		}
	}

	for _, group := range groups {
		if !singleWorkspaceView {
			header := strings.TrimSpace(group.Label)
			if header == "" {
				header = strings.TrimSpace(group.Key)
			}
			if age := strings.TrimSpace(group.AgeText); age != "" {
				header += " · " + age
			}
			elements = append(elements, map[string]any{
				"tag":     "markdown",
				"content": "**" + header + "**",
			})
		}
		available := make([]control.SelectionOption, 0, len(group.Options))
		var unavailableReason string
		for _, option := range group.Options {
			if option.Disabled {
				if unavailableReason == "" {
					unavailableReason = strings.TrimSpace(firstNonEmpty(option.MetaText, selectionOptionBody(prompt.Kind, option)))
				}
				continue
			}
			available = append(available, option)
		}
		if len(available) == 0 {
			if unavailableReason != "" {
				elements = append(elements, map[string]any{
					"tag":     "markdown",
					"content": renderSystemInlineTags(unavailableReason),
				})
			}
			continue
		}
		visible := available
		if !singleWorkspaceView && len(visible) > 5 {
			visible = visible[:5]
		}
		for index, option := range visible {
			meta := strings.TrimSpace(firstNonEmpty(option.MetaText, "时间未知"))
			elements = append(elements, map[string]any{
				"tag":     "markdown",
				"content": fmt.Sprintf("%d. %s", index+1, renderSystemInlineTags(meta)),
			})
			elements = append(elements, useThreadActionElement(prompt, option, daemonLifecycleID))
		}
		if !singleWorkspaceView && len(available) > 5 {
			label := "查看" + firstNonEmpty(strings.TrimSpace(group.Label), strings.TrimSpace(group.Key)) + "全部会话"
			if button := cardButtonGroupElement([]map[string]any{workspaceThreadsButton(label, group.Key, daemonLifecycleID)}); len(button) != 0 {
				elements = append(elements, button)
			}
		}
	}

	if len(moreOptions) > 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**更多**",
		})
		for _, option := range moreOptions {
			elements = append(elements, useThreadActionElement(prompt, option, daemonLifecycleID))
			if meta := strings.TrimSpace(firstNonEmpty(option.MetaText, selectionOptionBody(prompt.Kind, option))); meta != "" {
				elements = append(elements, map[string]any{
					"tag":     "markdown",
					"content": renderSystemInlineTags(meta),
				})
			}
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

func useThreadActionElement(prompt control.SelectionPrompt, option control.SelectionOption, daemonLifecycleID string) map[string]any {
	return selectionOptionButton(prompt, option, daemonLifecycleID)
}

func workspaceThreadsButton(label, workspaceKey, daemonLifecycleID string) map[string]any {
	value := stampActionValue(map[string]any{
		"kind":          "show_workspace_threads",
		"workspace_key": strings.TrimSpace(workspaceKey),
	}, daemonLifecycleID)
	return cardCallbackButtonElement(label, "default", value, false, "fill")
}

func useThreadSelectionOptionGroup(option control.SelectionOption) useThreadOptionGroup {
	switch strings.TrimSpace(option.ActionKind) {
	case "show_scoped_threads", "show_threads", "show_all_threads", "show_all_thread_workspaces", "show_recent_thread_workspaces":
		return useThreadOptionGroupMore
	}
	if option.IsCurrent {
		return useThreadOptionGroupCurrent
	}
	if option.Disabled {
		return useThreadOptionGroupUnavailable
	}
	return useThreadOptionGroupTakeover
}

func useThreadSelectionGroupTitle(group useThreadOptionGroup) string {
	switch group {
	case useThreadOptionGroupCurrent:
		return "当前会话"
	case useThreadOptionGroupTakeover:
		return "可接管"
	case useThreadOptionGroupUnavailable:
		return "其他状态"
	case useThreadOptionGroupMore:
		return "更多"
	default:
		return "会话"
	}
}

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
