package feishu

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

type OperationKind string

const (
	OperationSendText         OperationKind = "send_text"
	OperationSendCard         OperationKind = "send_card"
	OperationUpdateCard       OperationKind = "update_card"
	OperationSendImage        OperationKind = "send_image"
	OperationDeleteMessage    OperationKind = "delete_message"
	OperationAddReaction      OperationKind = "add_reaction"
	OperationRemoveReaction   OperationKind = "remove_reaction"
	OperationSetTimeSensitive OperationKind = "set_time_sensitive"
)

type Operation struct {
	Kind             OperationKind
	GatewayID        string
	SurfaceSessionID string
	ReceiveID        string
	ReceiveIDType    string
	ChatID           string
	MessageID        string
	ReplyToMessageID string
	EmojiType        string
	TimeSensitive    bool
	Text             string
	ImagePath        string
	ImageBase64      string
	CardTitle        string
	CardBody         string
	CardThemeKey     string
	CardElements     []map[string]any
	CardUpdateMulti  bool
	cardEnvelope     cardEnvelopeVersion
	card             *cardDocument
}

const (
	emojiQueuePending = "OneSecond"
	emojiThinking     = "THINKING"
	emojiSteered      = "THUMBSUP"
	emojiDiscarded    = "ThumbsDown"
)

const (
	cardThemeInfo     = "info"
	cardThemePlan     = "plan"
	cardThemeSuccess  = "success"
	cardThemeError    = "error"
	cardThemeFinal    = "final"
	cardThemeApproval = "approval"
)

const maxEmbeddedFileSummaryRows = 6

type Projector struct {
	readGitWorktree func(string) *gitWorktreeSummary
	snapshotBinary  string
}

func NewProjector() *Projector {
	return &Projector{readGitWorktree: inspectGitWorktreeSummary}
}

func (p *Projector) SetSnapshotBinary(value string) {
	if p == nil {
		return
	}
	p.snapshotBinary = strings.TrimSpace(value)
}

func (p *Projector) ProjectPreviewSupplements(gatewayID, surfaceSessionID, chatID, replyToMessageID string, supplements []PreviewSupplement) []Operation {
	if len(supplements) == 0 {
		return nil
	}
	var ops []Operation
	for _, supplement := range supplements {
		op, ok := projectPreviewSupplement(gatewayID, surfaceSessionID, chatID, replyToMessageID, supplement)
		if ok {
			ops = append(ops, op)
		}
	}
	return ops
}

func (p *Projector) Project(chatID string, event control.UIEvent) []Operation {
	switch event.Kind {
	case control.UIEventSnapshot:
		if event.Snapshot == nil {
			return nil
		}
		body := p.formatSnapshot(*event.Snapshot)
		return []Operation{{
			Kind:             OperationSendCard,
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        "当前状态",
			CardBody:         body,
			CardThemeKey:     cardThemeInfo,
			cardEnvelope:     cardEnvelopeV2,
			card:             legacyCardDocument("当前状态", body, cardThemeInfo, nil),
		}}
	case control.UIEventNotice:
		if event.Notice == nil {
			return nil
		}
		title := strings.TrimSpace(event.Notice.Title)
		if title == "" {
			title = "系统提示"
		}
		return []Operation{{
			Kind:             OperationSendCard,
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        title,
			CardBody:         projectNoticeBody(*event.Notice),
			CardThemeKey:     noticeThemeKey(*event.Notice),
			cardEnvelope:     cardEnvelopeV2,
			card:             legacyCardDocument(title, projectNoticeBody(*event.Notice), noticeThemeKey(*event.Notice), nil),
		}}
	case control.UIEventPlanUpdated:
		if event.PlanUpdate == nil {
			return nil
		}
		title := "当前计划"
		body := planUpdateBody(*event.PlanUpdate)
		elements := planUpdateElements(*event.PlanUpdate)
		return []Operation{{
			Kind:             OperationSendCard,
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			ReplyToMessageID: event.SourceMessageID,
			CardTitle:        title,
			CardBody:         body,
			CardThemeKey:     cardThemePlan,
			CardElements:     elements,
			cardEnvelope:     cardEnvelopeV2,
			card:             rawCardDocument(title, body, cardThemePlan, elements),
		}}
	case control.UIEventFeishuDirectSelectionPrompt:
		var prompt *control.FeishuDirectSelectionPrompt
		switch {
		case event.FeishuSelectionView != nil:
			projected, ok := FeishuDirectSelectionPromptFromView(*event.FeishuSelectionView, event.FeishuSelectionContext)
			if !ok {
				return nil
			}
			prompt = &projected
		case event.FeishuDirectSelectionPrompt != nil:
			prompt = event.FeishuDirectSelectionPrompt
		default:
			return nil
		}
		title := strings.TrimSpace(prompt.Title)
		if title == "" {
			title = "请选择"
			switch prompt.Kind {
			case control.SelectionPromptAttachInstance:
				title = "在线 VS Code 实例"
			case control.SelectionPromptAttachWorkspace:
				title = "工作区列表"
			case control.SelectionPromptUseThread:
				title = "会话列表"
			case control.SelectionPromptKickThread:
				title = "强踢当前会话？"
			}
		}
		elements := selectionPromptElements(*prompt, event.DaemonLifecycleID)
		return []Operation{{
			Kind:             OperationSendCard,
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        title,
			CardBody:         "",
			CardThemeKey:     cardThemeInfo,
			CardElements:     elements,
			cardEnvelope:     cardEnvelopeV2,
			card:             rawCardDocument(title, "", cardThemeInfo, elements),
		}}
	case control.UIEventFeishuDirectCommandCatalog:
		var catalog *control.FeishuDirectCommandCatalog
		switch {
		case event.FeishuCommandView != nil:
			projected, ok := FeishuDirectCommandCatalogFromView(*event.FeishuCommandView, event.FeishuCommandContext)
			if !ok {
				return nil
			}
			catalog = &projected
		case event.FeishuDirectCommandCatalog != nil:
			catalog = event.FeishuDirectCommandCatalog
		default:
			return nil
		}
		title := strings.TrimSpace(catalog.Title)
		if title == "" {
			title = "命令菜单"
		}
		body := commandCatalogBody(*catalog)
		elements := commandCatalogElements(*catalog, event.DaemonLifecycleID)
		operation := Operation{
			Kind:             OperationSendCard,
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        title,
			CardBody:         body,
			CardThemeKey:     cardThemeInfo,
			CardElements:     elements,
		}
		operation.cardEnvelope = cardEnvelopeV2
		operation.card = rawCardDocument(title, body, cardThemeInfo, elements)
		return []Operation{operation}
	case control.UIEventFeishuDirectRequestPrompt:
		if event.FeishuDirectRequestPrompt == nil {
			return nil
		}
		title := strings.TrimSpace(event.FeishuDirectRequestPrompt.Title)
		if title == "" {
			title = "需要确认"
		}
		body := requestPromptBody(*event.FeishuDirectRequestPrompt)
		elements := requestPromptElements(*event.FeishuDirectRequestPrompt, event.DaemonLifecycleID)
		return []Operation{{
			Kind:             OperationSendCard,
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        title,
			CardBody:         body,
			CardThemeKey:     cardThemeApproval,
			CardElements:     elements,
			cardEnvelope:     cardEnvelopeV2,
			card:             rawCardDocument(title, body, cardThemeApproval, elements),
		}}
	case control.UIEventFeishuPathPicker:
		if event.FeishuPathPickerView == nil {
			return nil
		}
		view := *event.FeishuPathPickerView
		title := strings.TrimSpace(view.Title)
		if title == "" {
			title = "选择路径"
		}
		elements := pathPickerElements(view, event.DaemonLifecycleID)
		return []Operation{{
			Kind:             OperationSendCard,
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        title,
			CardBody:         "",
			CardThemeKey:     cardThemeInfo,
			CardElements:     elements,
			cardEnvelope:     cardEnvelopeV2,
			card:             rawCardDocument(title, "", cardThemeInfo, elements),
		}}
	case control.UIEventFeishuTargetPicker:
		if event.FeishuTargetPickerView == nil {
			return nil
		}
		view := *event.FeishuTargetPickerView
		title := strings.TrimSpace(view.Title)
		if title == "" {
			title = "选择工作区与会话"
		}
		elements := targetPickerElements(view, event.DaemonLifecycleID)
		return []Operation{{
			Kind:             OperationSendCard,
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        title,
			CardBody:         "",
			CardThemeKey:     cardThemeInfo,
			CardElements:     elements,
			cardEnvelope:     cardEnvelopeV2,
			card:             rawCardDocument(title, "", cardThemeInfo, elements),
		}}
	case control.UIEventPendingInput:
		if event.PendingInput == nil {
			return nil
		}
		var ops []Operation
		if event.PendingInput.QueueOn {
			ops = append(ops, Operation{
				Kind:             OperationAddReaction,
				GatewayID:        event.GatewayID,
				SurfaceSessionID: event.SurfaceSessionID,
				ChatID:           chatID,
				MessageID:        event.PendingInput.SourceMessageID,
				EmojiType:        emojiQueuePending,
			})
		}
		if event.PendingInput.QueueOff {
			ops = append(ops, Operation{
				Kind:             OperationRemoveReaction,
				GatewayID:        event.GatewayID,
				SurfaceSessionID: event.SurfaceSessionID,
				ChatID:           chatID,
				MessageID:        event.PendingInput.SourceMessageID,
				EmojiType:        emojiQueuePending,
			})
		}
		if event.PendingInput.TypingOn {
			ops = append(ops, Operation{
				Kind:             OperationAddReaction,
				GatewayID:        event.GatewayID,
				SurfaceSessionID: event.SurfaceSessionID,
				ChatID:           chatID,
				MessageID:        event.PendingInput.SourceMessageID,
				EmojiType:        emojiThinking,
			})
		}
		if event.PendingInput.TypingOff {
			ops = append(ops, Operation{
				Kind:             OperationRemoveReaction,
				GatewayID:        event.GatewayID,
				SurfaceSessionID: event.SurfaceSessionID,
				ChatID:           chatID,
				MessageID:        event.PendingInput.SourceMessageID,
				EmojiType:        emojiThinking,
			})
		}
		if event.PendingInput.ThumbsUp {
			ops = append(ops, Operation{
				Kind:             OperationAddReaction,
				GatewayID:        event.GatewayID,
				SurfaceSessionID: event.SurfaceSessionID,
				ChatID:           chatID,
				MessageID:        event.PendingInput.SourceMessageID,
				EmojiType:        emojiSteered,
			})
		}
		if event.PendingInput.ThumbsDown {
			ops = append(ops, Operation{
				Kind:             OperationAddReaction,
				GatewayID:        event.GatewayID,
				SurfaceSessionID: event.SurfaceSessionID,
				ChatID:           chatID,
				MessageID:        event.PendingInput.SourceMessageID,
				EmojiType:        emojiDiscarded,
			})
		}
		return ops
	case control.UIEventBlockCommitted:
		if event.Block == nil {
			return nil
		}
		return p.projectBlock(event.GatewayID, event.SurfaceSessionID, chatID, event.SourceMessageID, event.SourceMessagePreview, *event.Block, event.FileChangeSummary, event.FinalTurnSummary)
	case control.UIEventImageOutput:
		if event.ImageOutput == nil {
			return nil
		}
		if strings.TrimSpace(event.ImageOutput.SavedPath) == "" && strings.TrimSpace(event.ImageOutput.ImageBase64) == "" {
			return nil
		}
		return []Operation{{
			Kind:             OperationSendImage,
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			ReplyToMessageID: event.SourceMessageID,
			ImagePath:        strings.TrimSpace(event.ImageOutput.SavedPath),
			ImageBase64:      strings.TrimSpace(event.ImageOutput.ImageBase64),
		}}
	case control.UIEventExecCommandProgress:
		if event.ExecCommandProgress == nil {
			return nil
		}
		return p.projectExecCommandProgress(chatID, event)
	case control.UIEventThreadSelectionChange:
		if event.ThreadSelection == nil {
			return nil
		}
		body := projectThreadSelectionChangeBody(*event.ThreadSelection)
		return []Operation{{
			Kind:             OperationSendCard,
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        "系统提示",
			CardBody:         body,
			CardThemeKey:     cardThemeInfo,
			cardEnvelope:     cardEnvelopeV2,
			card:             legacyCardDocument("系统提示", body, cardThemeInfo, nil),
		}}
	default:
		return nil
	}
}

func projectThreadSelectionChangeBody(selection control.ThreadSelectionChanged) string {
	if strings.TrimSpace(selection.RouteMode) == "new_thread_ready" {
		return "已准备新建会话。\n\n当前还没有实际会话 ID；下一条文本会作为首条消息创建新会话。"
	}
	lines := []string{fmt.Sprintf("当前输入目标已切换到：%s", selection.Title)}
	if first := strings.TrimSpace(selection.FirstUserMessage); first != "" {
		lines = append(lines, "", "会话起点：", first)
	}
	if lastUser := strings.TrimSpace(selection.LastUserMessage); lastUser != "" {
		lines = append(lines, "", "最近用户：", lastUser)
	}
	if lastAssistant := strings.TrimSpace(selection.LastAssistantMessage); lastAssistant != "" {
		lines = append(lines, "", "最近回复：", lastAssistant)
	} else if preview := strings.TrimSpace(selection.Preview); preview != "" {
		lines = append(lines, "", "最近回复：", preview)
	}
	return strings.Join(lines, "\n")
}

func (p *Projector) projectBlock(gatewayID, surfaceSessionID, chatID, sourceMessageID, sourceMessagePreview string, block render.Block, summary *control.FileChangeSummary, finalSummary *control.FinalTurnSummary) []Operation {
	if !block.Final {
		return []Operation{{
			Kind:             OperationSendText,
			GatewayID:        gatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			Text:             block.Text,
		}}
	}
	body := block.Text
	if block.Kind == render.BlockAssistantCode {
		body = fenced(block.Language, block.Text)
	} else if block.Kind == render.BlockAssistantMarkdown {
		body = renderSystemInlineTags(block.Text)
	}
	elements := p.finalBlockExtraElements(summary, finalSummary)
	return []Operation{{
		Kind:             OperationSendCard,
		GatewayID:        gatewayID,
		SurfaceSessionID: surfaceSessionID,
		ChatID:           chatID,
		ReplyToMessageID: sourceMessageID,
		CardTitle:        finalCardTitle(sourceMessagePreview),
		CardBody:         body,
		CardThemeKey:     cardThemeFinal,
		CardElements:     elements,
		cardEnvelope:     cardEnvelopeV2,
		card:             legacyCardDocument(finalCardTitle(sourceMessagePreview), body, cardThemeFinal, elements),
	}}
}

func projectPreviewSupplement(gatewayID, surfaceSessionID, chatID, replyToMessageID string, supplement PreviewSupplement) (Operation, bool) {
	switch strings.TrimSpace(supplement.Kind) {
	case "card":
		title, _ := supplement.Data["title"].(string)
		body, _ := supplement.Data["body"].(string)
		theme, _ := supplement.Data["theme"].(string)
		elements, _ := supplement.Data["elements"].([]map[string]any)
		if strings.TrimSpace(title) == "" && strings.TrimSpace(body) == "" && len(elements) == 0 {
			return Operation{}, false
		}
		if strings.TrimSpace(title) == "" {
			title = "补充信息"
		}
		if strings.TrimSpace(theme) == "" {
			theme = cardThemeInfo
		}
		return Operation{
			Kind:             OperationSendCard,
			GatewayID:        gatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ReplyToMessageID: replyToMessageID,
			CardTitle:        title,
			CardBody:         body,
			CardThemeKey:     theme,
			CardElements:     elements,
			cardEnvelope:     cardEnvelopeV2,
			card:             rawCardDocument(title, body, theme, elements),
		}, true
	default:
		return Operation{}, false
	}
}

func finalCardTitle(sourceMessagePreview string) string {
	const baseTitle = "✅ 最后答复"
	preview := truncateFinalTitlePreview(sourceMessagePreview)
	if preview == "" {
		return baseTitle
	}
	return baseTitle + "：" + preview
}

func truncateFinalTitlePreview(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if text == "" {
		return ""
	}
	if shouldUseWordBasedTitlePreview(text) {
		return truncateFinalTitleWords(text, 10)
	}
	return truncateFinalTitleCharacters(text, 10)
}

func shouldUseWordBasedTitlePreview(text string) bool {
	hasHan := false
	hasLatin := false
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			hasHan = true
		case unicode.In(r, unicode.Latin):
			hasLatin = true
		}
		if hasHan {
			return false
		}
	}
	return hasLatin
}

func truncateFinalTitleWords(text string, limit int) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}
	if len(words) <= limit {
		return strings.Join(words, " ")
	}
	return strings.Join(words[:limit], " ") + "..."
}

func truncateFinalTitleCharacters(text string, limit int) string {
	var out strings.Builder
	count := 0
	truncated := false
	for _, r := range text {
		if unicode.IsSpace(r) {
			if out.Len() == 0 {
				continue
			}
			last := []rune(out.String())
			if len(last) > 0 && unicode.IsSpace(last[len(last)-1]) {
				continue
			}
			out.WriteRune(' ')
			continue
		}
		if count >= limit {
			truncated = true
			break
		}
		out.WriteRune(r)
		count++
	}
	preview := strings.TrimSpace(out.String())
	if preview == "" {
		return ""
	}
	if truncated {
		return preview + "..."
	}
	return preview
}

func fenced(language, text string) string {
	if language == "" {
		language = "text"
	}
	return "```" + language + "\n" + text + "\n```"
}

func (p *Projector) finalBlockExtraElements(summary *control.FileChangeSummary, finalSummary *control.FinalTurnSummary) []map[string]any {
	var elements []map[string]any
	if summary != nil && summary.FileCount > 0 && len(summary.Files) > 0 {
		elements = append(elements, map[string]any{
			"tag": "markdown",
			"content": fmt.Sprintf(
				"**本次修改** %d 个文件  %s",
				summary.FileCount,
				formatFileChangeCountsMarkdown(summary.AddedLines, summary.RemovedLines),
			),
		})
		labels := fileChangeDisplayLabels(summary.Files)
		limit := len(summary.Files)
		if limit > maxEmbeddedFileSummaryRows {
			limit = maxEmbeddedFileSummaryRows
		}
		for index := 0; index < limit; index++ {
			elements = append(elements, map[string]any{
				"tag": "markdown",
				"content": fmt.Sprintf(
					"%d. %s  %s",
					index+1,
					formatFileChangePath(summary.Files[index], labels),
					formatFileChangeCountsMarkdown(summary.Files[index].AddedLines, summary.Files[index].RemovedLines),
				),
			})
		}
		if remaining := len(summary.Files) - limit; remaining > 0 {
			elements = append(elements, map[string]any{
				"tag":     "markdown",
				"content": fmt.Sprintf("另有 %d 个文件未展开。", remaining),
			})
		}
	}
	if line := formatFinalTurnSummaryLine(finalSummary); line != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": line,
		})
	}
	if line := p.formatFinalWorktreeSummaryLine(finalSummary); line != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": line,
		})
	}
	if len(elements) == 0 {
		return nil
	}
	return elements
}

func formatFinalTurnSummaryLine(summary *control.FinalTurnSummary) string {
	if summary == nil || summary.Elapsed <= 0 {
		return ""
	}
	parts := []string{fmt.Sprintf("**本轮用时** %s", formatElapsedDuration(summary.Elapsed))}
	if usage := summary.Usage; usage != nil {
		parts = append(parts, fmt.Sprintf("**本轮累计** %s", formatTokenUsageSummary(usage, false)))
	}
	if usage := summary.ThreadUsage; usage != nil {
		parts = append(parts, fmt.Sprintf("**线程累计** %s", formatTokenUsageSummary(usage, true)))
	}
	contextInputTokens := 0
	if usage := summary.ThreadUsage; usage != nil {
		contextInputTokens = usage.InputTokens
	} else if usage := summary.Usage; usage != nil {
		contextInputTokens = usage.InputTokens
	}
	if contextLeft := formatApproxContextLeftSummary(contextInputTokens, summary.ModelContextWindow); contextLeft != "" {
		parts = append(parts, fmt.Sprintf("**上下文剩余(估算)** %s", contextLeft))
	}
	return strings.Join(parts, "  ")
}

func formatTokenUsageSummary(usage *control.FinalTurnUsage, compact bool) string {
	if usage == nil {
		return ""
	}
	return strings.Join([]string{
		fmt.Sprintf("输入 %s", formatTokenUsageValue(usage.InputTokens, compact)),
		fmt.Sprintf("缓存 %s", formatCachedUsageSummary(usage.CachedInputTokens, usage.InputTokens, compact)),
		fmt.Sprintf("输出 %s", formatTokenUsageValue(usage.OutputTokens, compact)),
		fmt.Sprintf("推理 %s", formatTokenUsageValue(usage.ReasoningOutputTokens, compact)),
	}, "  ")
}

func formatCachedUsageSummary(cachedInput, input int, compact bool) string {
	value := formatTokenUsageValue(cachedInput, compact)
	if input <= 0 {
		return value
	}
	return fmt.Sprintf("%s (%.1f%%)", value, float64(cachedInput)*100/float64(input))
}

func formatTokenUsageValue(value int, compact bool) string {
	if !compact {
		return fmt.Sprintf("%d", value)
	}
	type unit struct {
		suffix string
		base   float64
	}
	units := []unit{
		{suffix: "B", base: 1_000_000_000},
		{suffix: "M", base: 1_000_000},
		{suffix: "K", base: 1_000},
	}
	floatValue := float64(value)
	for _, item := range units {
		if value >= int(item.base) {
			return fmt.Sprintf("%.1f%s", floatValue/item.base, item.suffix)
		}
	}
	return fmt.Sprintf("%d", value)
}

func formatApproxContextLeftSummary(input int, modelContextWindow *int) string {
	if modelContextWindow == nil || *modelContextWindow <= 0 {
		return ""
	}
	if input <= 0 {
		return "100.0%"
	}
	left := 100 * (1 - float64(input)/float64(*modelContextWindow))
	if left < 0 {
		left = 0
	}
	if left > 100 {
		left = 100
	}
	return fmt.Sprintf("%.1f%%", left)
}

func formatElapsedDuration(value time.Duration) string {
	if value <= 0 {
		return "0秒"
	}
	if value < time.Second {
		return "<1秒"
	}
	totalSeconds := int(value.Round(time.Second) / time.Second)
	if totalSeconds < 60 {
		return fmt.Sprintf("%d秒", totalSeconds)
	}
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	var b strings.Builder
	if hours > 0 {
		b.WriteString(fmt.Sprintf("%d小时", hours))
	}
	if minutes > 0 {
		b.WriteString(fmt.Sprintf("%d分钟", minutes))
	}
	if seconds > 0 || b.Len() == 0 {
		b.WriteString(fmt.Sprintf("%d秒", seconds))
	}
	return b.String()
}

func formatFileChangePath(file control.FileChangeSummaryEntry, labels map[string]string) string {
	path := strings.TrimSpace(file.Path)
	movePath := strings.TrimSpace(file.MovePath)
	switch {
	case path != "" && movePath != "":
		return fmt.Sprintf("%s → %s", formatNeutralTextTag(fileChangeDisplayLabel(path, labels)), formatNeutralTextTag(fileChangeDisplayLabel(movePath, labels)))
	case path != "":
		return formatNeutralTextTag(fileChangeDisplayLabel(path, labels))
	case movePath != "":
		return formatNeutralTextTag(fileChangeDisplayLabel(movePath, labels))
	default:
		return formatNeutralTextTag("(unknown)")
	}
}

func projectNoticeBody(notice control.Notice) string {
	if strings.HasPrefix(strings.TrimSpace(notice.Title), "链路错误") {
		return renderSystemInlineTags(notice.Text)
	}
	switch notice.Code {
	case "debug_error", "surface_override_usage", "surface_access_usage", "message_recall_too_late":
		return renderSystemInlineTags(notice.Text)
	default:
		return notice.Text
	}
}

func renderSystemInlineTags(text string) string {
	if !strings.Contains(text, "`") {
		return text
	}
	lines := strings.Split(text, "\n")
	inFence := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.Contains(line, "`") {
			continue
		}
		lines[i] = renderInlineTagsInLine(line)
	}
	return strings.Join(lines, "\n")
}

func renderInlineTagsInLine(line string) string {
	var out strings.Builder
	for len(line) > 0 {
		start := strings.IndexByte(line, '`')
		if start < 0 {
			out.WriteString(line)
			break
		}
		out.WriteString(line[:start])
		line = line[start+1:]
		end := strings.IndexByte(line, '`')
		if end < 0 {
			out.WriteByte('`')
			out.WriteString(line)
			break
		}
		token := strings.TrimSpace(line[:end])
		if token == "" {
			out.WriteString("``")
		} else {
			out.WriteString(formatInlineCodeTextTag(token))
		}
		line = line[end+1:]
	}
	return out.String()
}

func fileChangeDisplayLabels(files []control.FileChangeSummaryEntry) map[string]string {
	paths := make([]string, 0, len(files)*2)
	for _, file := range files {
		if path := normalizeFileSummaryPath(file.Path); path != "" {
			paths = append(paths, path)
		}
		if movePath := normalizeFileSummaryPath(file.MovePath); movePath != "" {
			paths = append(paths, movePath)
		}
	}
	return shortestUniquePathSuffixes(paths)
}

func fileChangeDisplayLabel(path string, labels map[string]string) string {
	normalized := normalizeFileSummaryPath(path)
	if normalized == "" {
		return ""
	}
	if label := strings.TrimSpace(labels[normalized]); label != "" {
		return label
	}
	return clampDisplayPath(normalized)
}

func shortestUniquePathSuffixes(paths []string) map[string]string {
	unique := uniqueNormalizedPaths(paths)
	if len(unique) == 0 {
		return map[string]string{}
	}
	resolved := make(map[string]string, len(unique))
	maxDepth := 0
	for _, path := range unique {
		if depth := len(strings.Split(path, "/")); depth > maxDepth {
			maxDepth = depth
		}
	}
	for depth := 1; depth <= maxDepth; depth++ {
		counts := map[string]int{}
		for _, path := range unique {
			if resolved[path] != "" {
				continue
			}
			suffix := pathSuffix(path, depth)
			counts[suffix]++
		}
		for _, path := range unique {
			if resolved[path] != "" {
				continue
			}
			suffix := pathSuffix(path, depth)
			if counts[suffix] == 1 {
				resolved[path] = clampDisplayPath(suffix)
			}
		}
	}
	for _, path := range unique {
		if resolved[path] == "" {
			resolved[path] = clampDisplayPath(path)
		}
	}
	return resolved
}

func uniqueNormalizedPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		normalized := normalizeFileSummaryPath(path)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	return out
}

func normalizeFileSummaryPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.ReplaceAll(path, "\\", "/")
	path = strings.Trim(path, "/")
	return path
}

func pathSuffix(path string, depth int) string {
	parts := strings.Split(normalizeFileSummaryPath(path), "/")
	if depth >= len(parts) {
		return strings.Join(parts, "/")
	}
	return strings.Join(parts[len(parts)-depth:], "/")
}

func clampDisplayPath(path string) string {
	path = normalizeFileSummaryPath(path)
	const maxLen = 36
	if len(path) <= maxLen {
		return path
	}
	if idx := strings.Index(path, "/"); idx >= 0 {
		tail := path
		if len(tail) > maxLen-4 {
			tail = tail[len(tail)-(maxLen-4):]
		}
		return ".../" + strings.TrimLeft(tail, "/")
	}
	return path[len(path)-maxLen:]
}

func formatFileChangeCountsMarkdown(added, removed int) string {
	return fmt.Sprintf("<font color='green'>+%d</font> <font color='red'>-%d</font>", added, removed)
}
