package projector

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

const useThreadWorkspacePreviewLimit = 2

func useThreadSelectionPromptElements(prompt selectionRenderModel, daemonLifecycleID string) []map[string]any {
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
			if block := cardPlainTextBlockElement(selectionOptionBody(prompt.Kind, option)); len(block) != 0 {
				elements = append(elements, block)
			}
		}
	}
	if hint := strings.TrimSpace(prompt.Hint); hint != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": renderSystemInlineTags(hint),
		})
	}
	if footer := useThreadPromptFooter(prompt, daemonLifecycleID); len(footer) != 0 {
		elements = append(elements, footer)
	}
	return elements
}

func useThreadPromptUsesVSCodeInstanceLayout(prompt selectionRenderModel) bool {
	return strings.TrimSpace(prompt.Layout) == "vscode_instance_threads"
}

func useThreadVSCodeInstanceElements(prompt selectionRenderModel, daemonLifecycleID string) []map[string]any {
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
		if block := cardPlainTextBlockElement(text); len(block) != 0 {
			elements = append(elements, block)
		}
	}

	if len(current) > 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**当前会话**",
		})
		for _, option := range current {
			elements = append(elements, useThreadActionElement(prompt, option, daemonLifecycleID))
			if meta := strings.TrimSpace(firstNonEmpty(option.MetaText, selectionOptionBody(prompt.Kind, option))); meta != "" {
				if block := cardPlainTextBlockElement(meta); len(block) != 0 {
					elements = append(elements, block)
				}
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
			if block := cardPlainTextBlockElement(fmt.Sprintf("%d. %s", index+1, meta)); len(block) != 0 {
				elements = append(elements, block)
			}
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
					if block := cardPlainTextBlockElement(meta); len(block) != 0 {
						elements = append(elements, block)
					}
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
					if block := cardPlainTextBlockElement(meta); len(block) != 0 {
						elements = append(elements, block)
					}
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
				if block := cardPlainTextBlockElement(meta); len(block) != 0 {
					elements = append(elements, block)
				}
			}
		}
	}

	if hint := strings.TrimSpace(prompt.Hint); hint != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": renderSystemInlineTags(hint),
		})
	}
	if footer := useThreadPromptFooter(prompt, daemonLifecycleID); len(footer) != 0 {
		elements = append(elements, footer)
	}
	return elements
}

type useThreadWorkspaceGroup struct {
	Key     string
	Label   string
	AgeText string
	Options []control.SelectionOption
}

func useThreadPromptUsesWorkspaceGrouping(prompt selectionRenderModel) bool {
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

func useThreadWorkspaceGroupedElements(prompt selectionRenderModel, daemonLifecycleID string) []map[string]any {
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
				if block := cardPlainTextBlockElement(meta); len(block) != 0 {
					elements = append(elements, block)
				}
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
			if block := cardPlainTextBlockElement(text); len(block) != 0 {
				elements = append(elements, block)
			}
		}
		if contextKey := strings.TrimSpace(prompt.ContextKey); contextKey != "" {
			label := "查看当前工作区全部会话"
			if button := cardButtonGroupElement([]map[string]any{workspaceThreadsButton(label, contextKey, prompt.Page, daemonLifecycleID)}); len(button) != 0 {
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
			if block := cardPlainTextBlockElement(header); len(block) != 0 {
				elements = append(elements, block)
			}
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
				if block := cardPlainTextBlockElement(unavailableReason); len(block) != 0 {
					elements = append(elements, block)
				}
			}
			continue
		}
		visible := available
		if !singleWorkspaceView && len(visible) > useThreadWorkspacePreviewLimit {
			visible = visible[:useThreadWorkspacePreviewLimit]
		}
		for index, option := range visible {
			meta := strings.TrimSpace(firstNonEmpty(option.MetaText, "时间未知"))
			if block := cardPlainTextBlockElement(fmt.Sprintf("%d. %s", index+1, meta)); len(block) != 0 {
				elements = append(elements, block)
			}
			elements = append(elements, useThreadActionElement(prompt, option, daemonLifecycleID))
		}
		if !singleWorkspaceView && len(available) > useThreadWorkspacePreviewLimit {
			label := "展开 " + firstNonEmpty(strings.TrimSpace(group.Label), strings.TrimSpace(group.Key))
			if button := cardButtonGroupElement([]map[string]any{workspaceThreadsButton(label, group.Key, prompt.Page, daemonLifecycleID)}); len(button) != 0 {
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
				if block := cardPlainTextBlockElement(meta); len(block) != 0 {
					elements = append(elements, block)
				}
			}
		}
	}

	if hint := strings.TrimSpace(prompt.Hint); hint != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": renderSystemInlineTags(hint),
		})
	}
	if footer := useThreadPromptFooter(prompt, daemonLifecycleID); len(footer) != 0 {
		elements = append(elements, footer)
	}
	return elements
}

func useThreadExpandedWorkspaceIndex(prompt selectionRenderModel, singleWorkspaceView bool, moreOptions []control.SelectionOption) bool {
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

func useThreadWorkspaceIndexElements(prompt selectionRenderModel, daemonLifecycleID string, currentOptions []control.SelectionOption, groups []useThreadWorkspaceGroup, moreOptions []control.SelectionOption) []map[string]any {
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
		if block := cardPlainTextBlockElement(text); len(block) != 0 {
			elements = append(elements, block)
		}
	}
	if contextKey := strings.TrimSpace(prompt.ContextKey); contextKey != "" {
		if button := cardButtonGroupElement([]map[string]any{workspaceThreadsButton("查看当前工作区全部会话", contextKey, prompt.Page, daemonLifecycleID)}); len(button) != 0 {
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
		elements = append(elements, workspaceThreadsButton(buttonLabel, group.Key, prompt.Page, daemonLifecycleID))
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
	if footer := useThreadPromptFooter(prompt, daemonLifecycleID); len(footer) != 0 {
		elements = append(elements, footer)
	}
	return elements
}

func useThreadActionElement(prompt selectionRenderModel, option control.SelectionOption, daemonLifecycleID string) map[string]any {
	return selectionOptionButton(prompt, option, daemonLifecycleID)
}

func workspaceThreadsButton(label, workspaceKey string, returnPage int, daemonLifecycleID string) map[string]any {
	value := stampActionValue(actionPayloadWorkspaceThreads(workspaceKey, 1, returnPage), daemonLifecycleID)
	return cardCallbackButtonElement(label, "default", value, false, "fill")
}

func useThreadPromptFooter(prompt selectionRenderModel, daemonLifecycleID string) map[string]any {
	buttons := []map[string]any{}
	if strings.TrimSpace(prompt.ViewMode) == string(control.FeishuThreadSelectionNormalWorkspaceView) {
		if prompt.ReturnPage > 0 {
			backValue := stampActionValue(actionPayloadNavigationPage(cardActionKindShowAllThreadWorkspaces, prompt.ReturnPage), daemonLifecycleID)
			buttons = append(buttons, cardCallbackButtonElement("返回分组", "default", backValue, false, "fill"))
		}
	}
	if prompt.TotalPages > 1 {
		if prompt.Page > 1 {
			if button := useThreadPageButton(prompt, daemonLifecycleID, prompt.Page-1, true); len(button) != 0 {
				buttons = append(buttons, button)
			}
		}
		if prompt.Page < prompt.TotalPages {
			if button := useThreadPageButton(prompt, daemonLifecycleID, prompt.Page+1, false); len(button) != 0 {
				buttons = append(buttons, button)
			}
		}
	}
	return cardButtonGroupElement(buttons)
}

func useThreadPageButton(prompt selectionRenderModel, daemonLifecycleID string, page int, previous bool) map[string]any {
	label := "下一页"
	if previous {
		label = "上一页"
	}
	var value map[string]any
	switch strings.TrimSpace(prompt.ViewMode) {
	case string(control.FeishuThreadSelectionNormalWorkspaceView):
		value = actionPayloadWorkspaceThreads(prompt.ContextKey, page, prompt.ReturnPage)
	case string(control.FeishuThreadSelectionNormalGlobalAll), string(control.FeishuThreadSelectionNormalGlobalRecent):
		value = actionPayloadNavigationPage(cardActionKindShowAllThreadWorkspaces, page)
	case string(control.FeishuThreadSelectionNormalScopedAll):
		value = actionPayloadThreadNavigation(cardActionKindShowScopedThreads, prompt.ViewMode, page)
	case string(control.FeishuThreadSelectionVSCodeAll), string(control.FeishuThreadSelectionVSCodeScopedAll):
		value = actionPayloadThreadNavigation(cardActionKindShowScopedThreads, prompt.ViewMode, page)
	default:
		value = actionPayloadThreadNavigation(cardActionKindShowThreads, prompt.ViewMode, page)
	}
	stampActionValue(value, daemonLifecycleID)
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
