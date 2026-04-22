package projector

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func SelectionViewStructuredProjection(
	view control.FeishuSelectionView,
	ctx *control.FeishuUISelectionContext,
	daemonLifecycleID string,
) (string, []map[string]any, bool) {
	switch {
	case view.Instance != nil && view.PromptKind == control.SelectionPromptAttachInstance:
		return firstNonEmpty(selectionViewStructuredTitle(ctx), "在线 VS Code 实例"),
			instanceSelectionViewElements(*view.Instance, ctx, daemonLifecycleID),
			true
	case view.Thread != nil && view.PromptKind == control.SelectionPromptUseThread && selectionViewUsesDirectThreadPicker(view.Thread.Mode):
		return firstNonEmpty(selectionViewStructuredTitle(ctx), "选择会话"),
			threadSelectionDropdownElements(*view.Thread, ctx, daemonLifecycleID),
			true
	default:
		return selectionViewPromptProjection(view, ctx, daemonLifecycleID)
	}
}

func selectionViewPromptProjection(
	view control.FeishuSelectionView,
	ctx *control.FeishuUISelectionContext,
	daemonLifecycleID string,
) (string, []map[string]any, bool) {
	prompt, ok := FeishuDirectSelectionPromptFromView(view, ctx)
	if !ok {
		return "", nil, false
	}
	title := strings.TrimSpace(prompt.Title)
	if title == "" {
		title = selectionPromptDefaultTitle(prompt.Kind)
	}
	return title, selectionPromptElements(prompt, daemonLifecycleID), true
}

func selectionPromptDefaultTitle(kind control.SelectionPromptKind) string {
	switch kind {
	case control.SelectionPromptAttachInstance:
		return "在线 VS Code 实例"
	case control.SelectionPromptAttachWorkspace:
		return "工作区列表"
	case control.SelectionPromptUseThread:
		return "会话列表"
	case control.SelectionPromptKickThread:
		return "强踢当前会话？"
	default:
		return "请选择"
	}
}

func selectionViewStructuredTitle(ctx *control.FeishuUISelectionContext) string {
	if ctx == nil {
		return ""
	}
	return strings.TrimSpace(ctx.Title)
}

func selectionViewStructuredContextElements(ctx *control.FeishuUISelectionContext) []map[string]any {
	if ctx == nil {
		return nil
	}
	elements := make([]map[string]any, 0, 2)
	if title := strings.TrimSpace(ctx.ContextTitle); title != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**" + title + "**",
		})
	}
	if text := strings.TrimSpace(ctx.ContextText); text != "" {
		if block := cardPlainTextBlockElement(text); len(block) != 0 {
			elements = append(elements, block)
		}
	}
	return elements
}

func instanceSelectionViewElements(view control.FeishuInstanceSelectionView, ctx *control.FeishuUISelectionContext, daemonLifecycleID string) []map[string]any {
	available := make([]control.FeishuInstanceSelectionEntry, 0, len(view.Entries))
	unavailable := make([]control.FeishuInstanceSelectionEntry, 0, len(view.Entries))
	for _, entry := range view.Entries {
		if entry.Disabled {
			unavailable = append(unavailable, entry)
			continue
		}
		available = append(available, entry)
	}
	elements := make([]map[string]any, 0, len(view.Entries)*2+4)
	elements = append(elements, selectionViewStructuredContextElements(ctx)...)
	elements = appendInstanceSelectionSection(elements, "可接管", available, daemonLifecycleID)
	elements = appendInstanceSelectionSection(elements, "其他状态", unavailable, daemonLifecycleID)
	return elements
}

func appendInstanceSelectionSection(
	elements []map[string]any,
	title string,
	entries []control.FeishuInstanceSelectionEntry,
	daemonLifecycleID string,
) []map[string]any {
	if len(entries) == 0 {
		return elements
	}
	elements = append(elements, map[string]any{
		"tag":     "markdown",
		"content": "**" + strings.TrimSpace(title) + "**",
	})
	for _, entry := range entries {
		button := cardCallbackButtonElement(
			instanceSelectionEntryButtonLabel(entry),
			"default",
			stampActionValue(actionPayloadAttachInstance(entry.InstanceID), daemonLifecycleID),
			entry.Disabled,
			"fill",
		)
		if group := cardButtonGroupElement([]map[string]any{button}); len(group) != 0 {
			elements = append(elements, group)
		}
		if meta := strings.TrimSpace(entry.MetaText); meta != "" {
			if block := cardPlainTextBlockElement(meta); len(block) != 0 {
				elements = append(elements, block)
			}
		}
	}
	return elements
}

func instanceSelectionEntryButtonLabel(entry control.FeishuInstanceSelectionEntry) string {
	summary := firstNonEmpty(strings.TrimSpace(entry.Label), strings.TrimSpace(entry.InstanceID), "实例")
	switch {
	case entry.Disabled:
		return "不可接管 · " + summary
	case strings.TrimSpace(entry.ButtonLabel) == "切换":
		return "切换 · " + summary
	default:
		return "接管 · " + summary
	}
}

func selectionViewUsesDirectThreadPicker(mode control.FeishuThreadSelectionViewMode) bool {
	switch mode {
	case control.FeishuThreadSelectionVSCodeRecent,
		control.FeishuThreadSelectionVSCodeAll,
		control.FeishuThreadSelectionVSCodeScopedAll:
		return true
	default:
		return false
	}
}

func threadSelectionDropdownElements(view control.FeishuThreadSelectionView, ctx *control.FeishuUISelectionContext, daemonLifecycleID string) []map[string]any {
	elements := make([]map[string]any, 0, len(view.Entries)+6)
	elements = append(elements, selectionViewStructuredContextElements(ctx)...)

	options := make([]map[string]any, 0, len(view.Entries))
	initialOption := ""
	hiddenCount := 0
	allowCrossWorkspace := false
	for _, entry := range view.Entries {
		if entry.Disabled && !entry.Current {
			hiddenCount++
			continue
		}
		value := strings.TrimSpace(entry.ThreadID)
		label := strings.TrimSpace(firstNonEmpty(entry.Summary, entry.ThreadID))
		if value == "" || label == "" {
			continue
		}
		options = append(options, map[string]any{
			"text":  cardPlainText(label),
			"value": value,
		})
		if entry.Current {
			initialOption = value
		}
		if entry.AllowCrossWorkspace {
			allowCrossWorkspace = true
		}
	}

	if len(options) == 0 {
		if hiddenCount != 0 {
			if block := cardPlainTextBlockElement("当前没有可切换的会话。"); len(block) != 0 {
				elements = append(elements, block)
			}
		}
		return elements
	}

	elements = append(elements, map[string]any{
		"tag":     "markdown",
		"content": "**会话**",
	})
	elements = append(elements, pathPickerSelectStaticElement(
		cardSelectionThreadFieldName,
		"选择会话",
		stampActionValue(actionPayloadUseThreadField(cardSelectionThreadFieldName, allowCrossWorkspace), daemonLifecycleID),
		options,
		initialOption,
	))
	if hiddenCount != 0 {
		if block := cardPlainTextBlockElement("已省略当前不可切换的会话。"); len(block) != 0 {
			elements = append(elements, block)
		}
	}
	return elements
}
