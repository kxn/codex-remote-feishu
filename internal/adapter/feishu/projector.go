package feishu

import (
	"context"
	"fmt"
	"html"
	"os/exec"
	"strconv"
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
	OperationSendImage        OperationKind = "send_image"
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
	cardThemeSuccess  = "success"
	cardThemeError    = "error"
	cardThemeFinal    = "final"
	cardThemeApproval = "approval"
)

const maxEmbeddedFileSummaryRows = 6
const maxEmbeddedWorktreePaths = 3

type gitWorktreeSummary struct {
	Dirty bool
	Files []string
}

type Projector struct {
	readGitWorktree func(string) *gitWorktreeSummary
}

func NewProjector() *Projector {
	return &Projector{readGitWorktree: inspectGitWorktreeSummary}
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
		return []Operation{{
			Kind:             OperationSendCard,
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        "当前状态",
			CardBody:         formatSnapshot(*event.Snapshot),
			CardThemeKey:     cardThemeInfo,
			cardEnvelope:     cardEnvelopeV2,
			card:             legacyCardDocument("当前状态", formatSnapshot(*event.Snapshot), cardThemeInfo, nil),
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
	case control.UIEventSelectionPrompt:
		if event.SelectionPrompt == nil {
			return nil
		}
		title := strings.TrimSpace(event.SelectionPrompt.Title)
		if title == "" {
			title = "请选择"
			switch event.SelectionPrompt.Kind {
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
		elements := selectionPromptElements(*event.SelectionPrompt, event.DaemonLifecycleID)
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
	case control.UIEventCommandCatalog:
		if event.CommandCatalog == nil {
			return nil
		}
		title := strings.TrimSpace(event.CommandCatalog.Title)
		if title == "" {
			title = "命令菜单"
		}
		body := commandCatalogBody(*event.CommandCatalog)
		elements := commandCatalogElements(*event.CommandCatalog, event.DaemonLifecycleID)
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
	case control.UIEventRequestPrompt:
		if event.RequestPrompt == nil {
			return nil
		}
		title := strings.TrimSpace(event.RequestPrompt.Title)
		if title == "" {
			title = "需要确认"
		}
		body := requestPromptBody(*event.RequestPrompt)
		elements := requestPromptElements(*event.RequestPrompt, event.DaemonLifecycleID)
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
	body := fmt.Sprintf("当前输入目标已切换到：%s", selection.Title)
	if short := shortenThreadID(selection.ThreadID); short != "" {
		body += "\n\n会话 ID：" + short
	}
	if preview := strings.TrimSpace(selection.Preview); preview != "" {
		body += "\n\n最近信息：\n" + preview
	}
	return body
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
	const baseTitle = "最后答复"
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
		total := usage.TotalTokens
		if total <= 0 {
			total = usage.InputTokens + usage.OutputTokens
		}
		if total > 0 {
			parts = append(parts, fmt.Sprintf("**Token** %d", total))
		}
	}
	return strings.Join(parts, "  ")
}

func (p *Projector) formatFinalWorktreeSummaryLine(summary *control.FinalTurnSummary) string {
	if summary == nil || summary.Elapsed <= 0 {
		return ""
	}
	cwd := strings.TrimSpace(summary.ThreadCWD)
	if cwd == "" || p == nil || p.readGitWorktree == nil {
		return ""
	}
	worktree := p.readGitWorktree(cwd)
	if worktree == nil {
		return ""
	}
	if !worktree.Dirty {
		return "**工作区** " + formatNeutralTextTag("干净")
	}
	labels := shortestUniquePathSuffixes(worktree.Files)
	limit := len(worktree.Files)
	if limit > maxEmbeddedWorktreePaths {
		limit = maxEmbeddedWorktreePaths
	}
	parts := []string{"**工作区**", formatNeutralTextTag("有改动")}
	for index := 0; index < limit; index++ {
		parts = append(parts, formatNeutralTextTag(fileChangeDisplayLabel(worktree.Files[index], labels)))
	}
	return strings.Join(parts, " ")
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

func inspectGitWorktreeSummary(cwd string) *gitWorktreeSummary {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return nil
	}
	output, ok := runGitInspector(cwd, "status", "--porcelain", "--untracked-files=all")
	if !ok {
		return nil
	}
	files := parseGitStatusPaths(output)
	return &gitWorktreeSummary{
		Dirty: len(files) > 0,
		Files: files,
	}
}

func runGitInspector(cwd string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(output)), true
}

func parseGitStatusPaths(output string) []string {
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	seen := map[string]bool{}
	files := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		line = strings.TrimRight(line, "\r")
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if idx := strings.LastIndex(path, " -> "); idx >= 0 {
			path = strings.TrimSpace(path[idx+4:])
		}
		path = normalizeFileSummaryPath(parseGitStatusPath(path))
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		files = append(files, path)
	}
	return files
}

func parseGitStatusPath(path string) string {
	path = strings.TrimSpace(path)
	if len(path) >= 2 && strings.HasPrefix(path, "\"") && strings.HasSuffix(path, "\"") {
		if unquoted, err := strconv.Unquote(path); err == nil {
			return unquoted
		}
	}
	return path
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

func formatNeutralTextTag(text string) string {
	return "<text_tag color='neutral'>" + html.EscapeString(strings.TrimSpace(text)) + "</text_tag>"
}

func formatCommandTextTag(text string) string {
	text = html.EscapeString(strings.TrimSpace(text))
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = restoreLiteralAmpersands(text)
	return "<text_tag color='neutral'>" + text + "</text_tag>"
}

func formatInlineCodeTextTag(text string) string {
	trimmed := strings.TrimSpace(text)
	escaped := html.EscapeString(trimmed)
	escaped = strings.ReplaceAll(escaped, "&lt;", "<")
	escaped = strings.ReplaceAll(escaped, "&gt;", ">")
	escaped = restoreLiteralAmpersands(escaped)
	escaped = strings.ReplaceAll(escaped, "&#34;", "\"")
	escaped = strings.ReplaceAll(escaped, "&#39;", "'")
	return "<text_tag color='neutral'>" + escaped + "</text_tag>"
}

func restoreLiteralAmpersands(text string) string {
	if !strings.Contains(text, "&amp;") {
		return text
	}
	var out strings.Builder
	out.Grow(len(text))
	for len(text) > 0 {
		if strings.HasPrefix(text, "&amp;") {
			suffix := text[len("&amp;"):]
			if startsHTMLLikeEntity(suffix) {
				out.WriteString("&amp;")
			} else {
				out.WriteByte('&')
			}
			text = suffix
			continue
		}
		out.WriteByte(text[0])
		text = text[1:]
	}
	return out.String()
}

func startsHTMLLikeEntity(text string) bool {
	if text == "" {
		return false
	}
	limit := strings.IndexByte(text, ';')
	if limit <= 0 {
		return false
	}
	token := text[:limit]
	if token == "" {
		return false
	}
	if token[0] == '#' {
		if len(token) == 1 {
			return false
		}
		for i := 1; i < len(token); i++ {
			ch := token[i]
			if i == 1 && (ch == 'x' || ch == 'X') {
				continue
			}
			if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F') {
				continue
			}
			return false
		}
		return true
	}
	for i := 0; i < len(token); i++ {
		ch := token[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (i > 0 && ch >= '0' && ch <= '9') {
			continue
		}
		return false
	}
	return true
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
