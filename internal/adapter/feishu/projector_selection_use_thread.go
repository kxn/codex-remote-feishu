package feishu

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
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
		case cardActionKindShowScopedThreads, cardActionKindShowThreads:
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
		case cardActionKindShowScopedThreads:
			continue
		case cardActionKindShowThreads, cardActionKindShowAllThreads, cardActionKindShowAllThreadWorkspaces, cardActionKindShowRecentThreadWorkspaces:
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
	if useThreadExpandedWorkspaceIndex(prompt, singleWorkspaceView, moreOptions) {
		return useThreadWorkspaceIndexElements(prompt, daemonLifecycleID, currentOptions, groups, moreOptions)
	}

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

func useThreadExpandedWorkspaceIndex(prompt control.SelectionPrompt, singleWorkspaceView bool, moreOptions []control.SelectionOption) bool {
	if singleWorkspaceView {
		return false
	}
	if strings.TrimSpace(prompt.Title) != "全部会话" {
		return false
	}
	if strings.TrimSpace(prompt.ContextTitle) == "" {
		return false
	}
	for _, option := range moreOptions {
		if strings.TrimSpace(option.ActionKind) == cardActionKindShowRecentThreadWorkspaces {
			return true
		}
	}
	return false
}

func useThreadWorkspaceIndexElements(prompt control.SelectionPrompt, daemonLifecycleID string, currentOptions []control.SelectionOption, groups []useThreadWorkspaceGroup, moreOptions []control.SelectionOption) []map[string]any {
	elements := make([]map[string]any, 0, len(groups)+len(moreOptions)+8)

	if len(currentOptions) > 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**当前会话**",
		})
		for _, option := range currentOptions {
			elements = append(elements, useThreadActionElement(prompt, option, daemonLifecycleID))
		}
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
	if contextKey := strings.TrimSpace(prompt.ContextKey); contextKey != "" {
		if button := cardButtonGroupElement([]map[string]any{workspaceThreadsButton("查看当前工作区全部会话", contextKey, daemonLifecycleID)}); len(button) != 0 {
			elements = append(elements, button)
		}
	}

	if len(groups) > 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**其他工作区**",
		})
	}
	for _, group := range groups {
		label := strings.TrimSpace(group.Label)
		if label == "" {
			label = strings.TrimSpace(group.Key)
		}
		availableCount := 0
		for _, option := range group.Options {
			if !option.Disabled {
				availableCount++
			}
		}
		if availableCount == 0 {
			elements = append(elements, cardCallbackButtonElement("不可恢复 · "+label, "default", nil, true, "fill"))
			continue
		}
		buttonLabel := "查看全部 · " + label
		if availableCount > 1 {
			buttonLabel = fmt.Sprintf("查看全部 · %s (%d)", label, availableCount)
		}
		elements = append(elements, workspaceThreadsButton(buttonLabel, group.Key, daemonLifecycleID))
	}

	if len(moreOptions) > 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**更多**",
		})
		for _, option := range moreOptions {
			elements = append(elements, useThreadActionElement(prompt, option, daemonLifecycleID))
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
		cardActionPayloadKeyKind:         cardActionKindShowWorkspaceThreads,
		cardActionPayloadKeyWorkspaceKey: strings.TrimSpace(workspaceKey),
	}, daemonLifecycleID)
	return cardCallbackButtonElement(label, "default", value, false, "fill")
}

func useThreadSelectionOptionGroup(option control.SelectionOption) useThreadOptionGroup {
	switch strings.TrimSpace(option.ActionKind) {
	case cardActionKindShowScopedThreads, cardActionKindShowThreads, cardActionKindShowAllThreads, cardActionKindShowAllThreadWorkspaces, cardActionKindShowRecentThreadWorkspaces:
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
