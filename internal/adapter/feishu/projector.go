package feishu

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	projectorpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/projector"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
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
	Kind                 OperationKind
	GatewayID            string
	SurfaceSessionID     string
	ReceiveID            string
	ReceiveIDType        string
	ChatID               string
	MessageID            string
	ReplyToMessageID     string
	EmojiType            string
	TimeSensitive        bool
	Text                 string
	AttentionText        string
	AttentionUserID      string
	ImagePath            string
	ImageBase64          string
	CardTitle            string
	CardTitleTag         string
	CardSubtitle         string
	CardSubtitleTag      string
	CardBody             string
	CardThemeKey         string
	CardElements         []map[string]any
	CardUpdateMulti      bool
	ProgressCardStartSeq int
	ProgressCardEndSeq   int
	cardEnvelope         cardEnvelopeVersion
	card                 *cardDocument
	finalSourceBody      string
}

func (operation Operation) FinalSourceBody() string {
	return operation.finalSourceBody
}

const (
	emojiQueuePending = "OneSecond"
	emojiThinking     = "THINKING"
	emojiSteered      = "THUMBSUP"
	emojiDiscarded    = "ThumbsDown"
)

const (
	cardThemeInfo     = "info"
	cardThemeProgress = "progress"
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
	menuHomeVersion string
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

func (p *Projector) SetMenuHomeVersion(value string) {
	if p == nil {
		return
	}
	p.menuHomeVersion = strings.TrimSpace(value)
}

func (p *Projector) ProjectEvent(chatID string, event eventcontract.Event) []Operation {
	event = event.Normalized()
	return applyAttentionToOperations(p.projectEventBase(chatID, event), event.Meta.Attention)
}

func (p *Projector) projectEventBase(chatID string, event eventcontract.Event) []Operation {
	switch payload := event.CanonicalPayload().(type) {
	case eventcontract.SnapshotPayload:
		elements := p.projectSnapshotElements(payload.Snapshot)
		return []Operation{{
			Kind:             OperationSendCard,
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        "当前状态",
			CardBody:         "",
			CardElements:     elements,
			CardThemeKey:     cardThemeInfo,
			cardEnvelope:     cardEnvelopeV2,
			card:             rawCardDocument("当前状态", "", cardThemeInfo, elements),
		}}
	case eventcontract.NoticePayload:
		title := strings.TrimSpace(payload.Notice.Title)
		if title == "" {
			title = "系统提示"
		}
		body, elements := projectorpkg.ProjectNoticeContent(payload.Notice)
		theme := noticeThemeKey(payload.Notice)
		operation := Operation{
			Kind:             OperationSendCard,
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        title,
			CardBody:         body,
			CardThemeKey:     theme,
			CardElements:     elements,
			cardEnvelope:     cardEnvelopeV2,
			card:             rawCardDocument(title, body, theme, elements),
		}
		return []Operation{applyTemporarySessionHeaderToOperation(applyReplyLaneToNewOperation(event, operation), payload.Notice.TemporarySessionLabel)}
	case eventcontract.PlanUpdatePayload:
		title := "当前计划"
		elements := projectorpkg.PlanUpdateElements(payload.PlanUpdate)
		operation := Operation{
			Kind:             OperationSendCard,
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        title,
			CardBody:         "",
			CardThemeKey:     cardThemePlan,
			CardElements:     elements,
			cardEnvelope:     cardEnvelopeV2,
			card:             rawCardDocument(title, "", cardThemePlan, elements),
		}
		return []Operation{applyTemporarySessionHeaderToOperation(applyReplyLaneToNewOperation(event, operation), payload.PlanUpdate.TemporarySessionLabel)}
	case eventcontract.SelectionPayload:
		title, elements, ok := projectorpkg.SelectionViewStructuredProjection(payload.View, payload.Context, firstNonEmpty(event.DaemonLifecycleID, event.Meta.DaemonLifecycleID))
		if !ok {
			return nil
		}
		operation := Operation{
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
		}
		return []Operation{applyReplyLaneToNewOperation(event, operation)}
	case eventcontract.PagePayload:
		pageView := control.NormalizeFeishuPageView(payload.View)
		title := strings.TrimSpace(pageView.Title)
		if title == "" {
			title = "页面"
		}
		body := projectorpkg.PageBody(pageView)
		elements := projectorpkg.PageElementsWithOptions(
			pageView,
			firstNonEmpty(event.DaemonLifecycleID, event.Meta.DaemonLifecycleID),
			projectorpkg.PageRenderOptions{MenuHomeVersion: p.menuHomeVersion},
		)
		theme := firstNonEmpty(strings.TrimSpace(pageView.ThemeKey), cardThemeInfo)
		operation := Operation{
			Kind:             OperationSendCard,
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			MessageID:        strings.TrimSpace(pageView.MessageID),
			CardTitle:        title,
			CardBody:         body,
			CardThemeKey:     theme,
			CardElements:     elements,
			CardUpdateMulti:  pageView.Patchable,
		}
		if operation.MessageID != "" {
			operation.Kind = OperationUpdateCard
		}
		operation.cardEnvelope = cardEnvelopeV2
		operation.card = rawCardDocument(title, body, theme, elements)
		if operation.Kind == OperationSendCard {
			operation = applyReplyLaneToNewOperation(event, operation)
		}
		return []Operation{applyTemporarySessionHeaderToOperation(operation, pageView.TemporarySessionLabel)}
	case eventcontract.RequestPayload:
		title := strings.TrimSpace(payload.View.Title)
		if title == "" {
			title = "需要确认"
		}
		elements := projectorpkg.RequestPromptElements(payload.View, firstNonEmpty(event.DaemonLifecycleID, event.Meta.DaemonLifecycleID))
		operation := Operation{
			Kind:             OperationSendCard,
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        title,
			CardBody:         "",
			CardThemeKey:     cardThemeApproval,
			CardElements:     elements,
			cardEnvelope:     cardEnvelopeV2,
			card:             rawCardDocument(title, "", cardThemeApproval, elements),
		}
		return []Operation{applyTemporarySessionHeaderToOperation(applyReplyLaneToNewOperation(event, operation), payload.View.TemporarySessionLabel)}
	case eventcontract.TimelineTextPayload:
		text := strings.TrimSpace(payload.TimelineText.Text)
		if text == "" {
			return nil
		}
		replyToMessageID := strings.TrimSpace(firstNonEmpty(payload.TimelineText.ReplyToMessageID, event.SourceMessageID, event.Meta.SourceMessageID))
		return []Operation{{
			Kind:             OperationSendText,
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			ReplyToMessageID: replyToMessageID,
			Text:             text,
		}}
	case eventcontract.PathPickerPayload:
		view := payload.View
		title := strings.TrimSpace(view.Title)
		if title == "" {
			title = "选择路径"
		}
		elements := projectorpkg.PathPickerElements(view, firstNonEmpty(event.DaemonLifecycleID, event.Meta.DaemonLifecycleID))
		operation := Operation{
			Kind:             OperationSendCard,
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        title,
			CardBody:         "",
			CardThemeKey:     cardThemeInfo,
			CardUpdateMulti:  true,
			CardElements:     elements,
			cardEnvelope:     cardEnvelopeV2,
			card:             rawCardDocument(title, "", cardThemeInfo, elements),
		}
		if messageID := strings.TrimSpace(view.MessageID); messageID != "" {
			operation.Kind = OperationUpdateCard
			operation.MessageID = messageID
			operation.ReplyToMessageID = ""
		}
		if operation.Kind == OperationSendCard {
			operation = applyReplyLaneToNewOperation(event, operation)
		}
		return []Operation{operation}
	case eventcontract.TargetPickerPayload:
		view := payload.View
		title := strings.TrimSpace(view.Title)
		if title == "" {
			title = "选择工作区与会话"
		}
		elements := projectorpkg.TargetPickerElements(view, firstNonEmpty(event.DaemonLifecycleID, event.Meta.DaemonLifecycleID))
		theme := projectorpkg.TargetPickerTheme(view)
		operation := Operation{
			Kind:             OperationSendCard,
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        title,
			CardBody:         "",
			CardThemeKey:     theme,
			CardUpdateMulti:  true,
			CardElements:     elements,
			cardEnvelope:     cardEnvelopeV2,
			card:             rawCardDocument(title, "", theme, elements),
		}
		if messageID := strings.TrimSpace(view.MessageID); messageID != "" {
			operation.Kind = OperationUpdateCard
			operation.MessageID = messageID
			operation.ReplyToMessageID = ""
		}
		if operation.Kind == OperationSendCard {
			operation = applyReplyLaneToNewOperation(event, operation)
		}
		return []Operation{operation}
	case eventcontract.ThreadHistoryPayload:
		return p.projectThreadHistory(chatID, event, payload.View)
	case eventcontract.PendingInputPayload:
		var ops []Operation
		if payload.State.QueueOn {
			ops = append(ops, Operation{
				Kind:             OperationAddReaction,
				GatewayID:        event.GatewayID,
				SurfaceSessionID: event.SurfaceSessionID,
				ChatID:           chatID,
				MessageID:        payload.State.SourceMessageID,
				EmojiType:        emojiQueuePending,
			})
		}
		if payload.State.QueueOff {
			ops = append(ops, Operation{
				Kind:             OperationRemoveReaction,
				GatewayID:        event.GatewayID,
				SurfaceSessionID: event.SurfaceSessionID,
				ChatID:           chatID,
				MessageID:        payload.State.SourceMessageID,
				EmojiType:        emojiQueuePending,
			})
		}
		if payload.State.TypingOn {
			ops = append(ops, Operation{
				Kind:             OperationAddReaction,
				GatewayID:        event.GatewayID,
				SurfaceSessionID: event.SurfaceSessionID,
				ChatID:           chatID,
				MessageID:        payload.State.SourceMessageID,
				EmojiType:        emojiThinking,
			})
		}
		if payload.State.TypingOff {
			ops = append(ops, Operation{
				Kind:             OperationRemoveReaction,
				GatewayID:        event.GatewayID,
				SurfaceSessionID: event.SurfaceSessionID,
				ChatID:           chatID,
				MessageID:        payload.State.SourceMessageID,
				EmojiType:        emojiThinking,
			})
		}
		if payload.State.ThumbsUp {
			ops = append(ops, Operation{
				Kind:             OperationAddReaction,
				GatewayID:        event.GatewayID,
				SurfaceSessionID: event.SurfaceSessionID,
				ChatID:           chatID,
				MessageID:        payload.State.SourceMessageID,
				EmojiType:        emojiSteered,
			})
		}
		if payload.State.ThumbsDown {
			ops = append(ops, Operation{
				Kind:             OperationAddReaction,
				GatewayID:        event.GatewayID,
				SurfaceSessionID: event.SurfaceSessionID,
				ChatID:           chatID,
				MessageID:        payload.State.SourceMessageID,
				EmojiType:        emojiDiscarded,
			})
		}
		return ops
	case eventcontract.BlockCommittedPayload:
		return p.projectBlock(
			event.GatewayID,
			event.SurfaceSessionID,
			chatID,
			firstNonEmpty(event.SourceMessageID, event.Meta.SourceMessageID),
			firstNonEmpty(event.SourceMessagePreview, event.Meta.SourceMessagePreview),
			payload.Block,
			payload.FileChangeSummary,
			payload.TurnDiffPreview,
			payload.FinalTurnSummary,
		)
	case eventcontract.ImageOutputPayload:
		if strings.TrimSpace(payload.ImageOutput.SavedPath) == "" && strings.TrimSpace(payload.ImageOutput.ImageBase64) == "" {
			return nil
		}
		operation := Operation{
			Kind:             OperationSendImage,
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			ImagePath:        strings.TrimSpace(payload.ImageOutput.SavedPath),
			ImageBase64:      strings.TrimSpace(payload.ImageOutput.ImageBase64),
		}
		return []Operation{applyReplyLaneToNewOperation(event, operation)}
	case eventcontract.ExecCommandProgressPayload:
		return p.projectExecCommandProgress(chatID, event, payload.Progress)
	default:
		return nil
	}
}

func (p *Projector) projectBlock(gatewayID, surfaceSessionID, chatID, sourceMessageID, sourceMessagePreview string, block render.Block, summary *control.FileChangeSummary, turnDiffPreview *control.TurnDiffPreview, finalSummary *control.FinalTurnSummary) []Operation {
	if !block.Final {
		return []Operation{{
			Kind:             OperationSendText,
			GatewayID:        gatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ReplyToMessageID: sourceMessageID,
			Text:             block.Text,
		}}
	}
	body := block.Text
	if block.Kind == render.BlockAssistantCode {
		body = fenced(block.Language, block.Text)
	}
	elements := p.finalBlockExtraElements(summary, turnDiffPreview, finalSummary)
	title := finalCardTitle(sourceMessagePreview)
	return projectFinalReplyCards(gatewayID, surfaceSessionID, chatID, sourceMessageID, title, temporarySessionHeaderSubtitle(block.TemporarySessionLabel), body, elements)
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

func projectFinalReplyCards(gatewayID, surfaceSessionID, chatID, sourceMessageID, title, subtitle, rawBody string, primaryElements []map[string]any) []Operation {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "✅ 最后答复"
	}
	chunks := splitFinalReplyBodies(rawBody, title, subtitle, primaryElements)
	if len(chunks) == 0 {
		chunks = []finalReplyChunk{newFinalReplyChunk(title, subtitle, rawBody, primaryElements)}
	}
	ops := make([]Operation, 0, len(chunks))
	for i, chunk := range chunks {
		op := Operation{
			Kind:             OperationSendCard,
			GatewayID:        gatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ReplyToMessageID: sourceMessageID,
			CardTitle:        chunk.title,
			CardSubtitle:     chunk.subtitle,
			CardSubtitleTag:  cardTextTagLarkMarkdown,
			CardBody:         chunk.renderedBody,
			CardThemeKey:     cardThemeFinal,
			CardElements:     chunk.elements,
			cardEnvelope:     cardEnvelopeV2,
			card:             finalReplyCardDocument(chunk.title, chunk.subtitle, chunk.renderedBody, cardThemeFinal, chunk.elements),
		}
		if i == 0 {
			op.finalSourceBody = chunk.sourceBody
		}
		ops = append(ops, op)
	}
	return ops
}

func replyToMessageIDForEvent(event eventcontract.Event) string {
	return strings.TrimSpace(firstNonEmpty(
		event.SourceMessageID,
		event.Meta.SourceMessageID,
	))
}

func applyReplyLaneToNewOperation(event eventcontract.Event, operation Operation) Operation {
	if operation.Kind != OperationSendCard && operation.Kind != OperationSendText && operation.Kind != OperationSendImage {
		return operation
	}
	if event.Meta.MessageDelivery.FirstSendLane != eventcontract.MessageLaneReplyThread {
		return operation
	}
	operation.ReplyToMessageID = replyToMessageIDForEvent(event)
	return operation
}

func applyAttentionToOperations(operations []Operation, attention eventcontract.AttentionAnnotation) []Operation {
	attention = attention.Normalized()
	if len(operations) == 0 || attention.Empty() {
		return operations
	}
	for i := range operations {
		switch operations[i].Kind {
		case OperationSendCard, OperationUpdateCard, OperationSendText:
			operations[i].AttentionText = attention.Text
			operations[i].AttentionUserID = attention.MentionUserID
			return operations
		}
	}
	return operations
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

func (p *Projector) finalBlockExtraElements(summary *control.FileChangeSummary, turnDiffPreview *control.TurnDiffPreview, finalSummary *control.FinalTurnSummary) []map[string]any {
	var elements []map[string]any
	if summary != nil && summary.FileCount > 0 && len(summary.Files) > 0 {
		summaryLine := fmt.Sprintf(
			"**本次修改** %d 个文件  %s",
			summary.FileCount,
			formatFileChangeCountsMarkdown(summary.AddedLines, summary.RemovedLines),
		)
		if url := projectorTurnDiffPreviewURL(turnDiffPreview); url != "" {
			summaryLine += "  [查看](" + url + ")"
		}
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": summaryLine,
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

func projectorTurnDiffPreviewURL(preview *control.TurnDiffPreview) string {
	if preview == nil {
		return ""
	}
	return strings.TrimSpace(preview.URL)
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
	contextInputTokens := (*int)(nil)
	if summary.ContextInputTokens != nil {
		value := *summary.ContextInputTokens
		contextInputTokens = &value
	} else if usage := summary.Usage; usage != nil {
		value := usage.InputTokens
		contextInputTokens = &value
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

func formatApproxContextLeftSummary(input *int, modelContextWindow *int) string {
	if modelContextWindow == nil || *modelContextWindow <= 0 {
		return ""
	}
	if input == nil {
		return ""
	}
	if *input <= 0 {
		return "100.0%"
	}
	left := 100 * (1 - float64(*input)/float64(*modelContextWindow))
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
