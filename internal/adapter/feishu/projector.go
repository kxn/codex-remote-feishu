package feishu

import (
	"fmt"
	"html"
	"strings"
	"time"
	"unicode"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

type OperationKind string

const (
	OperationSendText       OperationKind = "send_text"
	OperationSendCard       OperationKind = "send_card"
	OperationSendImage      OperationKind = "send_image"
	OperationAddReaction    OperationKind = "add_reaction"
	OperationRemoveReaction OperationKind = "remove_reaction"
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
	Text             string
	ImagePath        string
	ImageBase64      string
	CardTitle        string
	CardBody         string
	CardThemeKey     string
	CardElements     []map[string]any
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

type Projector struct{}

func NewProjector() *Projector {
	return &Projector{}
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
		return []Operation{{
			Kind:             OperationSendCard,
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        title,
			CardBody:         "",
			CardThemeKey:     cardThemeInfo,
			CardElements:     selectionPromptElements(*event.SelectionPrompt, event.DaemonLifecycleID),
		}}
	case control.UIEventCommandCatalog:
		if event.CommandCatalog == nil {
			return nil
		}
		title := strings.TrimSpace(event.CommandCatalog.Title)
		if title == "" {
			title = "命令菜单"
		}
		return []Operation{{
			Kind:             OperationSendCard,
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        title,
			CardBody:         commandCatalogBody(*event.CommandCatalog),
			CardThemeKey:     cardThemeInfo,
			CardElements:     commandCatalogElements(*event.CommandCatalog, event.DaemonLifecycleID),
		}}
	case control.UIEventRequestPrompt:
		if event.RequestPrompt == nil {
			return nil
		}
		title := strings.TrimSpace(event.RequestPrompt.Title)
		if title == "" {
			title = "需要确认"
		}
		return []Operation{{
			Kind:             OperationSendCard,
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        title,
			CardBody:         requestPromptBody(*event.RequestPrompt),
			CardThemeKey:     cardThemeApproval,
			CardElements:     requestPromptElements(*event.RequestPrompt, event.DaemonLifecycleID),
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
		return projectBlock(event.GatewayID, event.SurfaceSessionID, chatID, event.SourceMessageID, event.SourceMessagePreview, *event.Block, event.FileChangeSummary, event.FinalTurnSummary)
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
		body := fmt.Sprintf("当前输入目标已切换到：%s", event.ThreadSelection.Title)
		if short := shortenThreadID(event.ThreadSelection.ThreadID); short != "" {
			body += "\n\n会话 ID：" + short
		}
		if preview := strings.TrimSpace(event.ThreadSelection.Preview); preview != "" {
			body += "\n\n最近信息：\n" + preview
		}
		return []Operation{{
			Kind:             OperationSendCard,
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        "系统提示",
			CardBody:         body,
			CardThemeKey:     cardThemeInfo,
		}}
	default:
		return nil
	}
}

func projectBlock(gatewayID, surfaceSessionID, chatID, sourceMessageID, sourceMessagePreview string, block render.Block, summary *control.FileChangeSummary, finalSummary *control.FinalTurnSummary) []Operation {
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
	elements := finalBlockExtraElements(summary, finalSummary)
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
		button := map[string]any{
			"tag": "action",
			"actions": []map[string]any{
				selectionOptionButton(prompt, option, daemonLifecycleID),
			},
		}
		line := selectionOptionBody(prompt.Kind, option)
		if prompt.Kind == control.SelectionPromptUseThread {
			elements = append(elements, button)
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
		elements = append(elements, button)
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
			elements = append(elements, map[string]any{
				"tag": "action",
				"actions": []map[string]any{
					selectionOptionButton(prompt, option, daemonLifecycleID),
				},
			})
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
			elements = append(elements, map[string]any{
				"tag": "action",
				"actions": []map[string]any{
					selectionOptionButton(prompt, option, daemonLifecycleID),
				},
			})
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
			elements = append(elements, map[string]any{
				"tag": "action",
				"actions": []map[string]any{
					selectionOptionButton(prompt, option, daemonLifecycleID),
				},
			})
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
			"content": "**当前工作区**",
		})
		for _, option := range current {
			elements = append(elements, map[string]any{
				"tag": "action",
				"actions": []map[string]any{
					selectionOptionButton(prompt, option, daemonLifecycleID),
				},
			})
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
			elements = append(elements, map[string]any{
				"tag": "action",
				"actions": []map[string]any{
					selectionOptionButton(prompt, option, daemonLifecycleID),
				},
			})
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
			elements = append(elements, map[string]any{
				"tag": "action",
				"actions": []map[string]any{
					selectionOptionButton(prompt, option, daemonLifecycleID),
				},
			})
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
			elements = append(elements, map[string]any{
				"tag": "action",
				"actions": []map[string]any{
					selectionOptionButton(prompt, option, daemonLifecycleID),
				},
			})
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
		if strings.TrimSpace(option.ActionKind) == "show_scoped_threads" {
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
	groups := make([]useThreadWorkspaceGroup, 0)
	groupIndex := map[string]int{}
	for _, option := range prompt.Options {
		if strings.TrimSpace(option.ActionKind) == "show_scoped_threads" {
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
			elements = append(elements, map[string]any{
				"tag": "action",
				"actions": []map[string]any{
					workspaceThreadsButton(label, contextKey, daemonLifecycleID),
				},
			})
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
			elements = append(elements, map[string]any{
				"tag": "action",
				"actions": []map[string]any{
					workspaceThreadsButton(label, group.Key, daemonLifecycleID),
				},
			})
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
	return map[string]any{
		"tag": "action",
		"actions": []map[string]any{
			selectionOptionButton(prompt, option, daemonLifecycleID),
		},
	}
}

func workspaceThreadsButton(label, workspaceKey, daemonLifecycleID string) map[string]any {
	value := stampActionValue(map[string]any{
		"kind":          "show_workspace_threads",
		"workspace_key": strings.TrimSpace(workspaceKey),
	}, daemonLifecycleID)
	return map[string]any{
		"tag":  "button",
		"type": "default",
		"text": map[string]any{
			"tag":     "plain_text",
			"content": strings.TrimSpace(label),
		},
		"value": value,
		"width": "fill",
	}
}

func useThreadSelectionOptionGroup(option control.SelectionOption) useThreadOptionGroup {
	if strings.TrimSpace(option.ActionKind) == "show_scoped_threads" {
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
	if strings.TrimSpace(option.ActionKind) == "show_scoped_threads" {
		value = map[string]any{"kind": "show_scoped_threads"}
	} else if strings.TrimSpace(option.ActionKind) == "show_workspace_threads" {
		value = map[string]any{"kind": "show_workspace_threads", "workspace_key": strings.TrimSpace(option.OptionID)}
	}
	switch prompt.Kind {
	case control.SelectionPromptAttachInstance:
		if text == "选择" {
			text = "接管"
		}
		value = map[string]any{
			"kind":        "attach_instance",
			"instance_id": strings.TrimSpace(option.OptionID),
		}
	case control.SelectionPromptAttachWorkspace:
		if text == "选择" {
			text = "接管"
		}
		value = map[string]any{
			"kind":          "attach_workspace",
			"workspace_key": strings.TrimSpace(option.OptionID),
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
	button := map[string]any{
		"tag":  "button",
		"type": buttonType,
		"text": map[string]any{
			"tag":     "plain_text",
			"content": text,
		},
		"disabled": disabled,
		"value":    value,
	}
	if prompt.Kind == control.SelectionPromptUseThread || prompt.Kind == control.SelectionPromptAttachWorkspace || prompt.Kind == control.SelectionPromptAttachInstance {
		button["width"] = "fill"
	}
	return button
}

func selectionOptionButtonText(prompt control.SelectionPrompt, option control.SelectionOption) string {
	text := strings.TrimSpace(option.ButtonLabel)
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

func commandCatalogBody(catalog control.CommandCatalog) string {
	return renderSystemInlineTags(strings.TrimSpace(catalog.Summary))
}

func commandCatalogElements(catalog control.CommandCatalog, daemonLifecycleID string) []map[string]any {
	elements := make([]map[string]any, 0, len(catalog.Sections)*3+2)
	if breadcrumb := commandCatalogBreadcrumbMarkdown(catalog.Breadcrumbs); breadcrumb != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": breadcrumb,
		})
	}
	for _, section := range catalog.Sections {
		title := strings.TrimSpace(section.Title)
		if title != "" {
			elements = append(elements, map[string]any{
				"tag":     "markdown",
				"content": "**" + title + "**",
			})
		}
		for _, entry := range section.Entries {
			if catalog.DisplayStyle == control.CommandCatalogDisplayCompactButtons && catalog.Interactive && len(entry.Buttons) > 0 {
				elements = append(elements, commandCatalogCompactButtonElements(entry.Buttons, daemonLifecycleID)...)
				if entry.Form == nil {
					continue
				}
			}
			if markdown := commandCatalogEntryMarkdown(entry); markdown != "" {
				elements = append(elements, map[string]any{
					"tag":     "markdown",
					"content": markdown,
				})
			}
			if catalog.Interactive && len(entry.Buttons) > 0 {
				elements = append(elements, map[string]any{
					"tag":     "action",
					"actions": commandCatalogButtons(entry.Buttons, daemonLifecycleID),
				})
			}
			if catalog.Interactive && entry.Form != nil {
				elements = append(elements, commandCatalogFormElement(*entry.Form, daemonLifecycleID))
			}
		}
	}
	if len(catalog.RelatedButtons) > 0 {
		elements = append(elements, map[string]any{
			"tag":     "action",
			"actions": commandCatalogButtons(catalog.RelatedButtons, daemonLifecycleID),
		})
	}
	return elements
}

func commandCatalogFormElement(form control.CommandCatalogForm, daemonLifecycleID string) map[string]any {
	field := form.Field
	input := map[string]any{
		"tag":  "input",
		"name": strings.TrimSpace(field.Name),
	}
	if label := strings.TrimSpace(field.Label); label != "" {
		input["label"] = map[string]any{
			"tag":     "plain_text",
			"content": label,
		}
		input["label_position"] = "left"
	}
	if placeholder := strings.TrimSpace(field.Placeholder); placeholder != "" {
		input["placeholder"] = map[string]any{
			"tag":     "plain_text",
			"content": placeholder,
		}
	}
	if value := strings.TrimSpace(field.DefaultValue); value != "" {
		input["default_value"] = value
	}
	submitValue := stampActionValue(map[string]any{
		"kind":       "submit_command_form",
		"command_id": strings.TrimSpace(form.CommandID),
		"command":    strings.TrimSpace(form.CommandText),
		"field_name": strings.TrimSpace(field.Name),
	}, daemonLifecycleID)
	formName := strings.TrimSpace(form.CommandID)
	if formName == "" {
		formName = "command_form"
	} else {
		formName = "command_form_" + formName
	}
	return map[string]any{
		"tag":  "form",
		"name": formName,
		"elements": []map[string]any{
			input,
			{
				"tag":         "button",
				"type":        "primary",
				"action_type": "form_submit",
				"name":        "submit",
				"text": map[string]any{
					"tag":     "plain_text",
					"content": firstNonEmpty(strings.TrimSpace(form.SubmitLabel), "执行"),
				},
				"value": submitValue,
			},
		},
	}
}

func commandCatalogCompactButtonElements(buttons []control.CommandCatalogButton, daemonLifecycleID string) []map[string]any {
	elements := make([]map[string]any, 0, len(buttons))
	for _, button := range buttons {
		actions := commandCatalogButtonsWithDefault([]control.CommandCatalogButton{button}, daemonLifecycleID, "default")
		if len(actions) == 0 {
			continue
		}
		elements = append(elements, map[string]any{
			"tag":     "action",
			"actions": actions,
		})
	}
	return elements
}

func commandCatalogEntryMarkdown(entry control.CommandCatalogEntry) string {
	parts := []string{}
	if title := strings.TrimSpace(entry.Title); title != "" {
		parts = append(parts, "**"+title+"**")
	}
	if commands := formatCommandTags(entry.Commands); commands != "" {
		parts = append(parts, commands)
	}
	if desc := strings.TrimSpace(entry.Description); desc != "" {
		parts = append(parts, desc)
	}
	line := strings.Join(parts, " ")
	if examples := formatCommandExamples(entry.Examples); examples != "" {
		if line == "" {
			return "例如：" + examples
		}
		return line + "\n例如：" + examples
	}
	return line
}

func formatCommandTags(commands []string) string {
	tags := make([]string, 0, len(commands))
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		tags = append(tags, formatCommandTextTag(command))
	}
	return strings.Join(tags, " / ")
}

func formatCommandExamples(examples []string) string {
	tags := make([]string, 0, len(examples))
	for _, example := range examples {
		example = strings.TrimSpace(example)
		if example == "" {
			continue
		}
		tags = append(tags, formatCommandTextTag(example))
	}
	return strings.Join(tags, "，")
}

func commandCatalogBreadcrumbMarkdown(items []control.CommandCatalogBreadcrumb) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		label := strings.TrimSpace(item.Label)
		if label == "" {
			continue
		}
		parts = append(parts, label)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " / ")
}

func commandCatalogButtons(buttons []control.CommandCatalogButton, daemonLifecycleID string) []map[string]any {
	return commandCatalogButtonsWithDefault(buttons, daemonLifecycleID, "")
}

func commandCatalogButtonsWithDefault(buttons []control.CommandCatalogButton, daemonLifecycleID, defaultTypeOverride string) []map[string]any {
	actions := make([]map[string]any, 0, len(buttons))
	defaultType := "default"
	if defaultTypeOverride != "" {
		defaultType = defaultTypeOverride
	} else if len(buttons) == 1 {
		defaultType = "primary"
	}
	for _, button := range buttons {
		label := strings.TrimSpace(button.Label)
		payload := map[string]any{}
		switch button.Kind {
		case "", control.CommandCatalogButtonRunCommand:
			commandText := strings.TrimSpace(button.CommandText)
			if commandText == "" {
				continue
			}
			if label == "" {
				label = commandText
			}
			payload = map[string]any{
				"kind":         "run_command",
				"command_text": commandText,
			}
		case control.CommandCatalogButtonStartCommandCapture:
			commandID := strings.TrimSpace(button.CommandID)
			if commandID == "" {
				continue
			}
			payload = map[string]any{
				"kind":       "start_command_capture",
				"command_id": commandID,
			}
		case control.CommandCatalogButtonCancelCommandCapture:
			commandID := strings.TrimSpace(button.CommandID)
			if commandID == "" {
				continue
			}
			payload = map[string]any{
				"kind":       "cancel_command_capture",
				"command_id": commandID,
			}
		default:
			continue
		}
		if label == "" {
			continue
		}
		buttonType := defaultType
		if style := strings.TrimSpace(button.Style); style != "" {
			buttonType = style
		}
		actions = append(actions, map[string]any{
			"tag":  "button",
			"type": buttonType,
			"text": map[string]any{
				"tag":     "plain_text",
				"content": label,
			},
			"disabled": button.Disabled,
			"value":    stampActionValue(payload, daemonLifecycleID),
		})
	}
	return actions
}

func requestPromptBody(prompt control.RequestPrompt) string {
	lines := []string{}
	if prompt.ThreadTitle != "" {
		lines = append(lines, "当前会话："+prompt.ThreadTitle)
	}
	body := strings.TrimSpace(prompt.Body)
	if body == "" {
		body = "本地 Codex 正在等待你的确认。"
	}
	if body != "" {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, body)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func requestPromptElements(prompt control.RequestPrompt, daemonLifecycleID string) []map[string]any {
	options := prompt.Options
	if len(options) == 0 {
		options = []control.RequestPromptOption{
			{OptionID: "accept", Label: "允许一次", Style: "primary"},
			{OptionID: "decline", Label: "拒绝", Style: "default"},
			{OptionID: "captureFeedback", Label: "告诉 Codex 怎么改", Style: "default"},
		}
	}
	actions := make([]map[string]any, 0, len(options))
	for _, option := range options {
		button := requestPromptButton(prompt, option, daemonLifecycleID)
		if len(button) == 0 {
			continue
		}
		actions = append(actions, button)
	}
	hint := "这个确认只影响当前这一次请求。"
	if requestPromptContainsOption(options, "captureFeedback") {
		hint = "如果想拒绝并补充处理意见，请点击“告诉 Codex 怎么改”后再发送下一条文字。"
	}
	return []map[string]any{
		{
			"tag":     "action",
			"actions": actions,
		},
		{
			"tag":     "markdown",
			"content": hint,
		},
	}
}

func requestPromptButton(prompt control.RequestPrompt, option control.RequestPromptOption, daemonLifecycleID string) map[string]any {
	label := strings.TrimSpace(option.Label)
	if label == "" {
		return nil
	}
	buttonType := strings.TrimSpace(option.Style)
	if buttonType == "" {
		buttonType = "default"
	}
	return map[string]any{
		"tag":  "button",
		"type": buttonType,
		"text": map[string]any{
			"tag":     "plain_text",
			"content": label,
		},
		"value": stampActionValue(map[string]any{
			"kind":              "request_respond",
			"request_id":        prompt.RequestID,
			"request_type":      strings.TrimSpace(prompt.RequestType),
			"request_option_id": strings.TrimSpace(option.OptionID),
		}, daemonLifecycleID),
	}
}

func stampActionValue(value map[string]any, daemonLifecycleID string) map[string]any {
	if len(value) == 0 {
		return value
	}
	if strings.TrimSpace(daemonLifecycleID) == "" {
		return value
	}
	value["daemon_lifecycle_id"] = strings.TrimSpace(daemonLifecycleID)
	return value
}

func requestPromptContainsOption(options []control.RequestPromptOption, optionID string) bool {
	for _, option := range options {
		if strings.TrimSpace(option.OptionID) == optionID {
			return true
		}
	}
	return false
}

func finalBlockExtraElements(summary *control.FileChangeSummary, finalSummary *control.FinalTurnSummary) []map[string]any {
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

func formatNeutralTextTag(text string) string {
	return "<text_tag color='neutral'>" + html.EscapeString(strings.TrimSpace(text)) + "</text_tag>"
}

func formatCommandTextTag(text string) string {
	text = html.EscapeString(strings.TrimSpace(text))
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	return "<text_tag color='neutral'>" + text + "</text_tag>"
}

func formatInlineCodeTextTag(text string) string {
	trimmed := strings.TrimSpace(text)
	escaped := html.EscapeString(trimmed)
	escaped = strings.ReplaceAll(escaped, "&lt;", "<")
	escaped = strings.ReplaceAll(escaped, "&gt;", ">")
	return "<text_tag color='neutral'>" + escaped + "</text_tag>"
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

func formatSnapshot(snapshot control.Snapshot) string {
	lines := []string{}
	lines = append(lines, snapshotField("当前模式", formatNeutralTextTag(displaySnapshotMode(snapshot.ProductMode))))
	if strings.TrimSpace(snapshot.WorkspaceKey) != "" {
		lines = append(lines, snapshotField("当前 workspace", formatNeutralTextTag(snapshot.WorkspaceKey)))
	}
	if snapshot.Attachment.InstanceID == "" {
		lines = append(lines, snapshotField("接管对象类型", "无"))
		lines = append(lines, snapshotField("已接管", "无"))
	} else {
		lines = append(lines, snapshotField("接管对象类型", formatNeutralTextTag(displayAttachmentObjectType(snapshot.Attachment.ObjectType))))
		lines = append(lines, snapshotField("已接管", formatInstanceLabel(snapshot.Attachment.DisplayName, snapshot.Attachment.Source, snapshot.Attachment.Managed)))
		if snapshot.Attachment.Abandoning {
			lines = append(lines, snapshotField("状态", "正在断开，等待当前 turn 收尾"))
		}
		switch {
		case snapshot.Attachment.SelectedThreadTitle != "":
			lines = append(lines, snapshotField("当前输入目标", snapshot.Attachment.SelectedThreadTitle))
			if short := shortenThreadID(snapshot.Attachment.SelectedThreadID); short != "" {
				lines = append(lines, snapshotField("会话 ID", short))
			}
		case snapshot.Attachment.SelectedThreadID != "":
			lines = append(lines, snapshotField("当前输入目标", snapshot.Attachment.SelectedThreadID))
		case snapshot.Attachment.RouteMode == "new_thread_ready":
			lines = append(lines, snapshotField("当前输入目标", "新建会话（等待首条消息）"))
		case snapshot.Attachment.RouteMode == "follow_local":
			lines = append(lines, snapshotField("当前输入目标", "跟随当前 VS Code（等待中）"))
		default:
			lines = append(lines, snapshotField("当前输入目标", "未绑定会话"))
		}
		if preview := strings.TrimSpace(snapshot.Attachment.SelectedThreadPreview); preview != "" {
			lines = append(lines, snapshotField("最近信息", preview))
		}
		if dispatch := snapshotDispatchText(snapshot.Dispatch); dispatch != "" {
			lines = append(lines, snapshotField("执行状态", dispatch))
		}
		if gate := snapshotGateText(snapshot.Gate); gate != "" {
			lines = append(lines, snapshotField("输入门禁", gate))
		}
		if snapshot.Attachment.PID > 0 {
			lines = append(lines, snapshotField("实例 PID", formatNeutralTextTag(fmt.Sprintf("%d", snapshot.Attachment.PID))))
		}
		lines = append(lines, "")
		lines = append(lines, "**如果现在从飞书发送一条消息：**")
		target := "未就绪"
		switch {
		case snapshot.NextPrompt.ThreadTitle != "":
			target = snapshot.NextPrompt.ThreadTitle
		case snapshot.NextPrompt.ThreadID != "":
			target = snapshot.NextPrompt.ThreadID
		case snapshot.NextPrompt.CreateThread:
			target = "新建会话"
		case snapshot.Attachment.RouteMode == "new_thread_ready":
			target = "新建会话"
		case snapshot.Attachment.RouteMode == "follow_local":
			target = "跟随当前 VS Code（等待中）"
		}
		lines = append(lines, snapshotField("目标", target))
		if snapshot.NextPrompt.CWD != "" {
			lines = append(lines, snapshotField("工作目录", formatNeutralTextTag(snapshot.NextPrompt.CWD)))
		}
		lines = append(lines, snapshotField("模型", fmt.Sprintf("%s（%s）", formatNeutralTextTag(displaySnapshotValue(snapshot.NextPrompt.EffectiveModel, snapshot.NextPrompt.EffectiveModelSource)), snapshotConfigSourceLabel(snapshot.NextPrompt.EffectiveModelSource))))
		lines = append(lines, snapshotField("推理强度", fmt.Sprintf("%s（%s）", formatNeutralTextTag(displaySnapshotValue(snapshot.NextPrompt.EffectiveReasoningEffort, snapshot.NextPrompt.EffectiveReasoningEffortSource)), snapshotConfigSourceLabel(snapshot.NextPrompt.EffectiveReasoningEffortSource))))
		lines = append(lines, snapshotField("执行权限", fmt.Sprintf("%s（%s）", formatNeutralTextTag(agentproto.DisplayAccessModeShort(snapshot.NextPrompt.EffectiveAccessMode)), snapshotConfigSourceLabel(snapshot.NextPrompt.EffectiveAccessModeSource))))
		overrideParts := []string{}
		if snapshot.NextPrompt.OverrideModel != "" {
			overrideParts = append(overrideParts, "模型 "+formatNeutralTextTag(snapshot.NextPrompt.OverrideModel))
		}
		if snapshot.NextPrompt.OverrideReasoningEffort != "" {
			overrideParts = append(overrideParts, "推理 "+formatNeutralTextTag(snapshot.NextPrompt.OverrideReasoningEffort))
		}
		if snapshot.NextPrompt.OverrideAccessMode != "" {
			overrideParts = append(overrideParts, "权限 "+formatNeutralTextTag(agentproto.DisplayAccessModeShort(snapshot.NextPrompt.OverrideAccessMode)))
		}
		if len(overrideParts) == 0 {
			lines = append(lines, snapshotField("飞书临时覆盖", "无"))
		} else {
			lines = append(lines, snapshotField("飞书临时覆盖", strings.Join(overrideParts, "，")))
		}
		lines = append(lines, snapshotField("底层真实配置", fmt.Sprintf("模型 %s（%s）；推理 %s（%s）",
			formatNeutralTextTag(displaySnapshotValue(snapshot.NextPrompt.BaseModel, snapshot.NextPrompt.BaseModelSource)),
			snapshotConfigSourceLabel(snapshot.NextPrompt.BaseModelSource),
			formatNeutralTextTag(displaySnapshotValue(snapshot.NextPrompt.BaseReasoningEffort, snapshot.NextPrompt.BaseReasoningEffortSource)),
			snapshotConfigSourceLabel(snapshot.NextPrompt.BaseReasoningEffortSource),
		)))
	}
	if autoContinue := snapshotAutoContinueText(snapshot.AutoContinue); autoContinue != "" {
		lines = append(lines, snapshotField("自动继续", autoContinue))
	}
	if snapshot.PendingHeadless.InstanceID != "" {
		lines = append(lines, "")
		lines = append(lines, "**后台恢复中：**")
		if snapshot.PendingHeadless.ThreadTitle != "" {
			lines = append(lines, fmt.Sprintf("- %s", snapshotField("目标会话", snapshot.PendingHeadless.ThreadTitle)))
		}
		if snapshot.PendingHeadless.ThreadCWD != "" {
			lines = append(lines, fmt.Sprintf("- %s", snapshotField("启动目录", formatNeutralTextTag(snapshot.PendingHeadless.ThreadCWD))))
		}
		if snapshot.PendingHeadless.PID > 0 {
			lines = append(lines, fmt.Sprintf("- %s", snapshotField("进程 PID", formatNeutralTextTag(fmt.Sprintf("%d", snapshot.PendingHeadless.PID)))))
		}
		if !snapshot.PendingHeadless.ExpiresAt.IsZero() {
			lines = append(lines, fmt.Sprintf("- %s", snapshotField("启动超时", formatNeutralTextTag(snapshot.PendingHeadless.ExpiresAt.Format("2006-01-02 15:04:05 MST")))))
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func snapshotField(label, value string) string {
	return fmt.Sprintf("**%s：** %s", label, value)
}

func displaySnapshotMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "vscode", "vs-code", "vs_code":
		return "vscode"
	default:
		return "normal"
	}
}

func displaySnapshotValue(value, source string) string {
	if strings.TrimSpace(value) == "" {
		return "未知"
	}
	return value
}

func snapshotGateText(summary control.GateSummary) string {
	switch summary.Kind {
	case "request_capture":
		return "正在等待一条文字处理意见；下一条文本不会发到当前会话"
	case "pending_request":
		if summary.PendingRequestCount > 1 {
			return fmt.Sprintf("有 %d 个待确认请求；普通文本和图片会先被拦住", summary.PendingRequestCount)
		}
		return "有 1 个待确认请求；普通文本和图片会先被拦住"
	default:
		return ""
	}
}

func snapshotAutoContinueText(summary control.AutoContinueSummary) string {
	stateText := "关闭"
	if summary.Enabled {
		stateText = "开启"
	}
	parts := []string{stateText}
	if summary.ConsecutiveCount > 0 {
		parts = append(parts, fmt.Sprintf("连续 %d 次", summary.ConsecutiveCount))
	}
	if summary.PendingReason != "" {
		label := summary.PendingReason
		switch summary.PendingReason {
		case "incomplete_stop":
			label = "等待继续未完成任务"
		case "retryable_failure":
			label = "等待重试可恢复失败"
		}
		parts = append(parts, label)
	}
	if !summary.PendingDueAt.IsZero() {
		parts = append(parts, "计划于 "+formatNeutralTextTag(summary.PendingDueAt.Format("2006-01-02 15:04:05 MST")))
	}
	return strings.Join(parts, "，")
}

func snapshotDispatchText(summary control.DispatchSummary) string {
	if !summary.InstanceOnline && summary.DispatchMode == "" && summary.ActiveItemStatus == "" && summary.QueuedCount == 0 {
		return ""
	}
	if !summary.InstanceOnline {
		if summary.QueuedCount > 0 {
			return fmt.Sprintf("实例离线，已保留接管关系；%d 条排队消息会在恢复后继续", summary.QueuedCount)
		}
		return "实例离线，已保留接管关系，等待恢复"
	}
	switch summary.DispatchMode {
	case "paused_for_local":
		if summary.QueuedCount > 0 {
			return fmt.Sprintf("本地 VS Code 占用中；%d 条飞书消息继续排队", summary.QueuedCount)
		}
		return "本地 VS Code 占用中；新的飞书消息会先排队"
	case "handoff_wait":
		if summary.QueuedCount > 0 {
			return fmt.Sprintf("等待本地 turn handoff；%d 条排队消息稍后继续派发", summary.QueuedCount)
		}
		return "等待本地 turn handoff；稍后自动恢复远端派发"
	}
	switch summary.ActiveItemStatus {
	case "running":
		if summary.QueuedCount > 0 {
			return fmt.Sprintf("当前 1 条执行中，另有 %d 条排队", summary.QueuedCount)
		}
		return "当前 1 条执行中"
	case "dispatching":
		if summary.QueuedCount > 0 {
			return fmt.Sprintf("当前 1 条派发中，另有 %d 条排队", summary.QueuedCount)
		}
		return "当前 1 条派发中"
	}
	if summary.QueuedCount > 0 {
		return fmt.Sprintf("当前 %d 条排队", summary.QueuedCount)
	}
	return "空闲"
}

func snapshotConfigSourceLabel(source string) string {
	switch source {
	case "thread":
		return "会话配置"
	case "cwd_default":
		return "工作目录默认配置"
	case "surface_override":
		return "飞书临时覆盖"
	case "surface_default":
		return "飞书默认"
	default:
		return "未知"
	}
}

func displayAttachmentObjectType(value string) string {
	switch strings.TrimSpace(value) {
	case "workspace":
		return "工作区"
	case "vscode_instance":
		return "VS Code 实例"
	case "headless_instance":
		return "headless 实例"
	case "instance":
		return "实例"
	default:
		return "未知"
	}
}

func formatInstanceLabel(displayName, source string, managed bool) string {
	label := strings.TrimSpace(displayName)
	if label == "" {
		label = "未知实例"
	}
	if strings.EqualFold(strings.TrimSpace(source), "headless") {
		_ = managed
		return label
	}
	return label
}

func noticeThemeKey(notice control.Notice) string {
	key := strings.ToLower(strings.TrimSpace(notice.ThemeKey))
	switch {
	case key == cardThemeError || strings.Contains(key, "error") || strings.Contains(key, "fail"):
		return cardThemeError
	case key == cardThemeSuccess || key == "normal" || key == "ok":
		return cardThemeSuccess
	case key == cardThemeApproval || strings.Contains(key, "approval"):
		return cardThemeApproval
	case key == cardThemeFinal:
		return cardThemeFinal
	}

	title := strings.TrimSpace(notice.Title)
	code := strings.ToLower(strings.TrimSpace(notice.Code))
	text := strings.TrimSpace(notice.Text)
	if containsAny(title, "错误", "失败", "无法", "拒绝", "离线", "过期", "失效") ||
		containsAny(code, "error", "failed", "rejected", "offline", "expired", "invalid") ||
		containsAny(text, "链路错误", "创建失败", "连接失败") {
		return cardThemeError
	}
	if strings.HasPrefix(title, "已") ||
		containsAny(title, "成功", "就绪", "完成") ||
		containsAny(code, "attached", "detached", "follow", "cleared", "requested") ||
		strings.HasPrefix(text, "已") {
		return cardThemeSuccess
	}
	return cardThemeInfo
}

func containsAny(value string, parts ...string) bool {
	for _, part := range parts {
		if strings.Contains(value, part) {
			return true
		}
	}
	return false
}

func shortenThreadID(threadID string) string {
	parts := strings.Split(threadID, "-")
	if len(parts) >= 2 {
		head := strings.TrimSpace(parts[1])
		tail := strings.TrimSpace(parts[len(parts)-1])
		if len(tail) > 4 {
			tail = tail[len(tail)-4:]
		}
		switch {
		case head == "":
		case tail == "":
			return head
		case head == tail:
			return head
		default:
			return head + "…" + tail
		}
	}
	if len(threadID) <= 10 {
		return threadID
	}
	return threadID[len(threadID)-8:]
}
