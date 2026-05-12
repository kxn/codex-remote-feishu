package projector

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/texttags"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func selectionPromptElements(prompt selectionRenderModel, daemonLifecycleID string) []map[string]any {
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
			if block := cardPlainTextBlockElement(line); len(block) != 0 {
				elements = append(elements, block)
			}
			continue
		}
		if block := cardPlainTextBlockElement(line); len(block) != 0 {
			elements = append(elements, block)
		}
		if len(button) != 0 {
			elements = append(elements, button)
		}
	}
	if hint := strings.TrimSpace(prompt.Hint); hint != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": texttags.RenderSystemInlineTags(hint),
		})
	}
	if footer := selectionPromptPaginationFooter(prompt, daemonLifecycleID); len(footer) != 0 {
		elements = append(elements, footer)
	}
	return elements
}

func attachInstanceSelectionPromptElements(prompt selectionRenderModel, daemonLifecycleID string) []map[string]any {
	return buildAttachSelectionPromptElements(prompt, daemonLifecycleID, "当前实例", nil)
}

func attachWorkspaceSelectionPromptElements(prompt selectionRenderModel, daemonLifecycleID string) []map[string]any {
	return buildAttachSelectionPromptElements(prompt, daemonLifecycleID, "当前工作区", func(option control.SelectionOption) bool {
		switch strings.TrimSpace(option.ActionKind) {
		case cardActionKindShowAllWorkspaces, cardActionKindShowRecentWorkspaces:
			return true
		default:
			return false
		}
	})
}

func buildAttachSelectionPromptElements(
	prompt selectionRenderModel,
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
		if block := cardPlainTextBlockElement(text); len(block) != 0 {
			elements = append(elements, block)
		}
	}

	elements = appendAttachSelectionSection(elements, prompt, daemonLifecycleID, currentHeading, current)
	elements = appendAttachSelectionSection(elements, prompt, daemonLifecycleID, "可接管", available)
	elements = appendAttachSelectionSection(elements, prompt, daemonLifecycleID, "其他状态", unavailable)
	elements = appendAttachSelectionSection(elements, prompt, daemonLifecycleID, "更多", more)

	if hint := strings.TrimSpace(prompt.Hint); hint != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": texttags.RenderSystemInlineTags(hint),
		})
	}
	if len(elements) == 0 {
		return nil
	}
	return elements
}

func appendAttachSelectionSection(
	elements []map[string]any,
	prompt selectionRenderModel,
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
			if block := cardPlainTextBlockElement(meta); len(block) != 0 {
				elements = append(elements, block)
			}
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
			line := fmt.Sprintf("%d. %s - 工作目录 %s%s", option.Index, option.Label, strings.TrimSpace(parts[0]), current)
			if len(parts) > 1 {
				line += "\n" + strings.Join(parts[1:], "\n")
			}
			return line
		}
	case control.SelectionPromptAttachWorkspace:
		if option.Subtitle != "" {
			parts := strings.Split(option.Subtitle, "\n")
			line := fmt.Sprintf("%d. %s - 工作区 %s%s", option.Index, option.Label, strings.TrimSpace(parts[0]), current)
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
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
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
				line += "\n" + strings.TrimSpace(parts[0])
			}
			if len(parts) > 1 {
				line += "\n" + strings.Join(parts[1:], "\n")
			}
			return line
		}
	}
	return fmt.Sprintf("%d. %s%s", option.Index, option.Label, current)
}

func selectionOptionButton(prompt selectionRenderModel, option control.SelectionOption, daemonLifecycleID string) map[string]any {
	text := selectionOptionButtonText(prompt, option)
	value := map[string]any{}
	switch strings.TrimSpace(option.ActionKind) {
	case cardActionKindShowScopedThreads:
		value = actionPayloadThreadNavigation(cardActionKindShowScopedThreads, prompt.ViewMode, prompt.Page)
	case cardActionKindShowAllWorkspaces:
		value = actionPayloadNavigationPage(cardActionKindShowAllWorkspaces, prompt.Page)
	case cardActionKindShowRecentWorkspaces:
		value = actionPayloadNavigationPage(cardActionKindShowRecentWorkspaces, prompt.Page)
	case cardActionKindShowAllThreadWorkspaces:
		value = actionPayloadNavigationPage(cardActionKindShowAllThreadWorkspaces, prompt.Page)
	case cardActionKindShowRecentThreadWorkspaces:
		value = actionPayloadNavigationPage(cardActionKindShowRecentThreadWorkspaces, prompt.Page)
	case cardActionKindShowWorkspaceThreads:
		value = actionPayloadWorkspaceThreads(strings.TrimSpace(option.OptionID), 1, prompt.Page)
	case cardActionKindShowThreads:
		value = actionPayloadThreadNavigation(cardActionKindShowThreads, prompt.ViewMode, prompt.Page)
	case cardActionKindShowAllThreads:
		value = actionPayloadThreadNavigation(cardActionKindShowAllThreads, prompt.ViewMode, prompt.Page)
	}
	switch prompt.Kind {
	case control.SelectionPromptAttachInstance:
		if len(value) == 0 {
			if text == "选择" {
				text = "接管"
			}
			value = actionPayloadAttachInstance(option.OptionID)
		}
	case control.SelectionPromptAttachWorkspace:
		if len(value) == 0 {
			if text == "选择" {
				text = "接管"
			}
			value = actionPayloadAttachWorkspace(option.OptionID)
		}
	case control.SelectionPromptUseThread:
		if len(value) == 0 {
			value = actionPayloadUseThread(option.OptionID, option.AllowCrossWorkspace)
		}
	case control.SelectionPromptKickThread:
		if strings.TrimSpace(option.OptionID) == "cancel" {
			value = actionPayloadNavigation(cardActionKindKickThreadCancel)
		} else {
			value = actionPayloadKickThreadConfirm(option.OptionID)
		}
	}
	if len(value) == 0 {
		value = actionPayloadUseThread(option.OptionID, false)
	}
	value = actionPayloadWithCatalog(value, prompt.CatalogFamilyID, prompt.CatalogVariantID, string(prompt.CatalogBackend))
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

func selectionPromptPaginationFooter(prompt selectionRenderModel, daemonLifecycleID string) map[string]any {
	if prompt.TotalPages <= 1 {
		return nil
	}
	buttons := []map[string]any{}
	if prompt.Page > 1 {
		if button := selectionPromptPageButton(prompt, daemonLifecycleID, prompt.Page-1, true); len(button) != 0 {
			buttons = append(buttons, button)
		}
	}
	if prompt.Page < prompt.TotalPages {
		if button := selectionPromptPageButton(prompt, daemonLifecycleID, prompt.Page+1, false); len(button) != 0 {
			buttons = append(buttons, button)
		}
	}
	return cardButtonGroupElement(buttons)
}

func selectionPromptPageButton(prompt selectionRenderModel, daemonLifecycleID string, page int, previous bool) map[string]any {
	label := "下一页"
	if previous {
		label = "上一页"
	}
	var value map[string]any
	switch prompt.Kind {
	case control.SelectionPromptAttachWorkspace:
		value = actionPayloadNavigationPage(cardActionKindShowAllWorkspaces, page)
	case control.SelectionPromptUseThread:
		switch strings.TrimSpace(prompt.ViewMode) {
		case string(control.FeishuThreadSelectionNormalWorkspaceView):
			value = actionPayloadWorkspaceThreads(prompt.ContextKey, page, prompt.ReturnPage)
		case string(control.FeishuThreadSelectionNormalGlobalAll), string(control.FeishuThreadSelectionNormalGlobalRecent):
			value = actionPayloadNavigationPage(cardActionKindShowAllThreadWorkspaces, page)
		case string(control.FeishuThreadSelectionNormalScopedAll):
			value = actionPayloadThreadNavigation(cardActionKindShowScopedThreads, prompt.ViewMode, page)
		case string(control.FeishuThreadSelectionNormalScopedRecent), string(control.FeishuThreadSelectionVSCodeRecent):
			value = actionPayloadThreadNavigation(cardActionKindShowThreads, prompt.ViewMode, page)
		case string(control.FeishuThreadSelectionVSCodeAll), string(control.FeishuThreadSelectionVSCodeScopedAll):
			value = actionPayloadThreadNavigation(cardActionKindShowScopedThreads, prompt.ViewMode, page)
		default:
			value = actionPayloadThreadNavigation(cardActionKindShowThreads, prompt.ViewMode, page)
		}
	default:
		return nil
	}
	value = actionPayloadWithCatalog(value, prompt.CatalogFamilyID, prompt.CatalogVariantID, string(prompt.CatalogBackend))
	stampActionValue(value, daemonLifecycleID)
	return cardCallbackButtonElement(label, "default", value, false, "fill")
}

func selectionOptionButtonText(prompt selectionRenderModel, option control.SelectionOption) string {
	text := strings.TrimSpace(option.ButtonLabel)
	switch strings.TrimSpace(option.ActionKind) {
	case cardActionKindShowAllWorkspaces:
		base := firstNonEmpty(strings.TrimSpace(option.ButtonLabel), strings.TrimSpace(option.Label), "全部工作区")
		return "查看全部 · " + base
	case cardActionKindShowRecentWorkspaces:
		base := firstNonEmpty(strings.TrimSpace(option.ButtonLabel), strings.TrimSpace(option.Label), "最近工作区")
		return "返回 · " + base
	case cardActionKindShowAllThreadWorkspaces:
		base := firstNonEmpty(strings.TrimSpace(option.ButtonLabel), strings.TrimSpace(option.Label), "全部工作区")
		return "查看全部 · " + base
	case cardActionKindShowRecentThreadWorkspaces:
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
		if strings.TrimSpace(option.ActionKind) == cardActionKindShowWorkspaceThreads {
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
	if strings.TrimSpace(option.ActionKind) == cardActionKindShowScopedThreads {
		base := firstNonEmpty(strings.TrimSpace(option.ButtonLabel), strings.TrimSpace(option.Label), "全部会话")
		return "查看全部 · " + base
	}
	if strings.TrimSpace(option.ActionKind) == cardActionKindShowThreads {
		base := firstNonEmpty(strings.TrimSpace(option.ButtonLabel), strings.TrimSpace(option.Label), "最近会话")
		return "返回 · " + base
	}
	if strings.TrimSpace(option.ActionKind) == cardActionKindShowAllThreads {
		base := firstNonEmpty(strings.TrimSpace(option.ButtonLabel), strings.TrimSpace(option.Label), "全部会话")
		return "返回 · " + base
	}
	if strings.TrimSpace(option.ActionKind) == cardActionKindShowWorkspaceThreads {
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
