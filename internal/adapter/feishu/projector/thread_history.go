package projector

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/texttags"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func ThreadHistoryTheme(view control.FeishuThreadHistoryView) string {
	if strings.TrimSpace(view.NoticeText) != "" {
		return cardThemeError
	}
	return cardThemeInfo
}

func ThreadHistoryElements(view control.FeishuThreadHistoryView, daemonLifecycleID string) []map[string]any {
	elements := make([]map[string]any, 0, 10)
	if summary := threadHistorySummaryMarkdown(view); summary != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": summary,
		})
	}
	hasBusinessContent := len(elements) != 0
	switch {
	case view.Loading:
		// Keep summary visible and move the loading text into the notice lane.
	case view.Detail != nil:
		elements = append(elements, ThreadHistoryDetailElements(view, daemonLifecycleID)...)
		hasBusinessContent = true
	default:
		elements = append(elements, ThreadHistoryListElements(view, daemonLifecycleID)...)
		hasBusinessContent = true
	}
	if noticeSections := threadHistoryNoticeSections(view); len(noticeSections) != 0 {
		if hasBusinessContent {
			elements = append(elements, cardDividerElement())
		}
		elements = appendCardTextSections(elements, noticeSections)
	}
	return elements
}

func ThreadHistoryListElements(view control.FeishuThreadHistoryView, daemonLifecycleID string) []map[string]any {
	elements := make([]map[string]any, 0, 6)
	if len(view.TurnOptions) == 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": firstNonEmpty(strings.TrimSpace(view.Hint), "这个会话暂时还没有可展示的历史。"),
		})
		return elements
	}
	elements = append(elements, map[string]any{
		"tag":     "markdown",
		"content": "**选择要查看的一轮**",
	})
	payload := actionPayloadThreadHistory(cardActionKindHistoryDetail, view.PickerID, "", 0)
	payload[cardActionPayloadKeyFieldName] = cardThreadHistoryTurnFieldName
	elements = append(elements, pathPickerSelectStaticElement(
		cardThreadHistoryTurnFieldName,
		"选择一轮并查看详情",
		stampActionValue(payload, daemonLifecycleID),
		threadHistoryTurnOptions(view.TurnOptions),
		strings.TrimSpace(view.SelectedTurnID),
	))
	if hint := strings.TrimSpace(view.Hint); hint != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": texttags.RenderSystemInlineTags(hint),
		})
	}
	buttons := make([]map[string]any, 0, 2)
	if view.Page > 0 {
		buttons = append(buttons, cardCallbackButtonElement("上一页", "default", stampActionValue(actionPayloadThreadHistory(cardActionKindHistoryPage, view.PickerID, "", view.Page-1), daemonLifecycleID), false, "fill"))
	}
	if view.Page+1 < view.TotalPages {
		buttons = append(buttons, cardCallbackButtonElement("下一页", "default", stampActionValue(actionPayloadThreadHistory(cardActionKindHistoryPage, view.PickerID, "", view.Page+1), daemonLifecycleID), false, "fill"))
	}
	if group := cardButtonGroupElement(buttons); len(group) != 0 {
		elements = append(elements, group)
	}
	return elements
}

func ThreadHistoryDetailElements(view control.FeishuThreadHistoryView, daemonLifecycleID string) []map[string]any {
	detail := view.Detail
	if detail == nil {
		return nil
	}
	lines := []string{
		fmt.Sprintf("**第 %d 轮**", detail.Ordinal),
		"**状态**\n" + texttags.FormatNeutralTextTag(firstNonEmpty(strings.TrimSpace(detail.Status), "-")),
	}
	if turnID := strings.TrimSpace(detail.TurnID); turnID != "" {
		lines = append(lines, "**turn_id**\n"+texttags.FormatInlineCodeTextTag(turnID))
	}
	if updated := strings.TrimSpace(detail.UpdatedText); updated != "" {
		lines = append(lines, "**更新时间**\n"+texttags.FormatNeutralTextTag(updated))
	}
	elements := []map[string]any{{
		"tag":     "markdown",
		"content": strings.Join(lines, "\n"),
	}}
	elements = appendCardTextSections(elements, threadHistoryDetailSections(detail))
	buttons := make([]map[string]any, 0, 3)
	if detail.PrevTurnID != "" {
		buttons = append(buttons, cardCallbackButtonElement("较新一轮", "default", stampActionValue(actionPayloadThreadHistory(cardActionKindHistoryDetail, view.PickerID, detail.PrevTurnID, 0), daemonLifecycleID), false, "fill"))
	}
	if detail.NextTurnID != "" {
		buttons = append(buttons, cardCallbackButtonElement("较旧一轮", "default", stampActionValue(actionPayloadThreadHistory(cardActionKindHistoryDetail, view.PickerID, detail.NextTurnID, 0), daemonLifecycleID), false, "fill"))
	}
	if group := cardButtonGroupElement(buttons); len(group) != 0 {
		elements = append(elements, group)
	}
	elements = append(elements, cardButtonGroupElement([]map[string]any{
		cardCallbackButtonElement("返回列表", "default", stampActionValue(actionPayloadThreadHistory(cardActionKindHistoryPage, view.PickerID, "", detail.ReturnPage), daemonLifecycleID), false, "fill"),
	}))
	return elements
}

func threadHistorySummaryMarkdown(view control.FeishuThreadHistoryView) string {
	lines := make([]string, 0, 4)
	if label := strings.TrimSpace(view.ThreadLabel); label != "" {
		lines = append(lines, "**当前会话**\n"+texttags.FormatNeutralTextTag(label))
	}
	if view.TurnCount > 0 {
		lines = append(lines, fmt.Sprintf("**总轮数**\n%s", texttags.FormatNeutralTextTag(fmt.Sprintf("%d", view.TurnCount))))
	}
	if view.Detail == nil && view.TotalPages > 0 && view.PageEnd > 0 {
		lines = append(lines, fmt.Sprintf("**当前页**\n%s", texttags.FormatNeutralTextTag(fmt.Sprintf("%d-%d / %d", view.PageStart+1, view.PageEnd, view.TurnCount))))
	}
	if label := strings.TrimSpace(view.CurrentTurnLabel); label != "" {
		lines = append(lines, "**当前进行**\n"+texttags.FormatNeutralTextTag(label))
	}
	return strings.Join(lines, "\n")
}

func threadHistoryDetailSections(detail *control.FeishuThreadHistoryTurnDetail) []control.FeishuCardTextSection {
	if detail == nil {
		return nil
	}
	sections := make([]control.FeishuCardTextSection, 0, 3)
	if text := strings.TrimSpace(detail.ErrorText); text != "" {
		sections = append(sections, control.FeishuCardTextSection{
			Label: "错误",
			Lines: []string{truncateThreadHistoryDetailText(text, 600)},
		})
	}
	sections = append(sections,
		control.FeishuCardTextSection{
			Label: "你的输入",
			Lines: threadHistoryDetailSectionLines(detail.Inputs),
		},
		control.FeishuCardTextSection{
			Label: "已产生的回复",
			Lines: threadHistoryDetailSectionLines(detail.Outputs),
		},
	)
	return sections
}

func threadHistoryDetailSectionLines(items []string) []string {
	if len(items) == 0 {
		return []string{"-"}
	}
	lines := make([]string, 0, len(items))
	for index, item := range items {
		lines = append(lines, fmt.Sprintf("%d. %s", index+1, truncateThreadHistoryDetailText(item, 600)))
	}
	return lines
}

func threadHistoryTurnOptions(options []control.FeishuThreadHistoryTurnOption) []map[string]any {
	result := make([]map[string]any, 0, len(options))
	for _, option := range options {
		value := strings.TrimSpace(option.TurnID)
		if value == "" {
			continue
		}
		result = append(result, map[string]any{
			"text":  cardPlainText(threadHistoryTurnOptionText(option)),
			"value": value,
		})
	}
	return result
}

func threadHistoryTurnOptionText(option control.FeishuThreadHistoryTurnOption) string {
	label := strings.TrimSpace(option.Label)
	meta := strings.TrimSpace(option.MetaText)
	if meta == "" {
		return label
	}
	return label + " · " + meta
}

func truncateThreadHistoryDetailText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 3 {
		limit = 3
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit-3]) + "..."
}

func threadHistoryNoticeSections(view control.FeishuThreadHistoryView) []control.FeishuCardTextSection {
	if len(view.NoticeSections) != 0 {
		return view.NoticeSections
	}
	sections := make([]control.FeishuCardTextSection, 0, 1)
	if text := strings.TrimSpace(view.NoticeText); text != "" {
		sections = append(sections, control.FeishuCardTextSection{
			Label: "错误",
			Lines: []string{text},
		})
	}
	if view.Loading {
		text := firstNonEmpty(strings.TrimSpace(view.LoadingText), "正在读取历史，请稍候...")
		sections = append(sections, control.FeishuCardTextSection{
			Label: "当前状态",
			Lines: []string{text},
		})
	}
	return sections
}
