package feishu

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

type OperationKind string

const (
	OperationSendText       OperationKind = "send_text"
	OperationSendCard       OperationKind = "send_card"
	OperationAddReaction    OperationKind = "add_reaction"
	OperationRemoveReaction OperationKind = "remove_reaction"
)

type Operation struct {
	Kind             OperationKind
	SurfaceSessionID string
	ReceiveID        string
	ReceiveIDType    string
	ChatID           string
	MessageID        string
	EmojiType        string
	Text             string
	CardTitle        string
	CardBody         string
	CardThemeKey     string
	CardElements     []map[string]any
}

const (
	emojiQueuePending = "OneSecond"
	emojiThinking     = "THINKING"
	emojiDiscarded    = "ThumbsDown"
)

type Projector struct{}

func NewProjector() *Projector {
	return &Projector{}
}

func (p *Projector) Project(chatID string, event control.UIEvent) []Operation {
	switch event.Kind {
	case control.UIEventSnapshot:
		if event.Snapshot == nil {
			return nil
		}
		return []Operation{{
			Kind:             OperationSendCard,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        "当前状态",
			CardBody:         formatSnapshot(*event.Snapshot),
			CardThemeKey:     "system",
		}}
	case control.UIEventNotice:
		if event.Notice == nil {
			return nil
		}
		title := strings.TrimSpace(event.Notice.Title)
		if title == "" {
			title = "系统提示"
		}
		themeKey := strings.TrimSpace(event.Notice.ThemeKey)
		if themeKey == "" {
			themeKey = "system"
		}
		return []Operation{{
			Kind:             OperationSendCard,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        title,
			CardBody:         event.Notice.Text,
			CardThemeKey:     themeKey,
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
				title = "在线实例"
			case control.SelectionPromptUseThread:
				title = "会话列表"
			case control.SelectionPromptNewInstance:
				title = "选择要恢复的会话"
			}
		}
		return []Operation{{
			Kind:             OperationSendCard,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        title,
			CardBody:         "",
			CardThemeKey:     "system",
			CardElements:     selectionPromptElements(*event.SelectionPrompt),
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
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        title,
			CardBody:         requestPromptBody(*event.RequestPrompt),
			CardThemeKey:     chooseThemeKey(event.RequestPrompt.ThreadID, "approval"),
			CardElements:     requestPromptElements(*event.RequestPrompt),
		}}
	case control.UIEventPendingInput:
		if event.PendingInput == nil {
			return nil
		}
		var ops []Operation
		if event.PendingInput.QueueOn {
			ops = append(ops, Operation{
				Kind:             OperationAddReaction,
				SurfaceSessionID: event.SurfaceSessionID,
				ChatID:           chatID,
				MessageID:        event.PendingInput.SourceMessageID,
				EmojiType:        emojiQueuePending,
			})
		}
		if event.PendingInput.QueueOff {
			ops = append(ops, Operation{
				Kind:             OperationRemoveReaction,
				SurfaceSessionID: event.SurfaceSessionID,
				ChatID:           chatID,
				MessageID:        event.PendingInput.SourceMessageID,
				EmojiType:        emojiQueuePending,
			})
		}
		if event.PendingInput.TypingOn {
			ops = append(ops, Operation{
				Kind:             OperationAddReaction,
				SurfaceSessionID: event.SurfaceSessionID,
				ChatID:           chatID,
				MessageID:        event.PendingInput.SourceMessageID,
				EmojiType:        emojiThinking,
			})
		}
		if event.PendingInput.TypingOff {
			ops = append(ops, Operation{
				Kind:             OperationRemoveReaction,
				SurfaceSessionID: event.SurfaceSessionID,
				ChatID:           chatID,
				MessageID:        event.PendingInput.SourceMessageID,
				EmojiType:        emojiThinking,
			})
		}
		if event.PendingInput.ThumbsDown {
			ops = append(ops, Operation{
				Kind:             OperationAddReaction,
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
		return projectBlock(event.SurfaceSessionID, chatID, *event.Block)
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
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        "系统提示",
			CardBody:         body,
			CardThemeKey:     chooseThemeKey(event.ThreadSelection.ThreadID, "system"),
		}}
	default:
		return nil
	}
}

func projectBlock(surfaceSessionID, chatID string, block render.Block) []Operation {
	if !block.Final {
		return []Operation{{
			Kind:             OperationSendText,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			Text:             block.Text,
		}}
	}
	titlePrefix := "过程信息"
	if block.Final {
		titlePrefix = "最终回复"
	}
	title := titlePrefix
	if block.ThreadTitle != "" {
		title += " · " + block.ThreadTitle
	}
	body := block.Text
	if block.Kind == render.BlockAssistantCode {
		body = fenced(block.Language, block.Text)
	}
	return []Operation{{
		Kind:             OperationSendCard,
		SurfaceSessionID: surfaceSessionID,
		ChatID:           chatID,
		CardTitle:        title,
		CardBody:         body,
		CardThemeKey:     chooseThemeKey(block.ThemeKey, block.ThreadID),
	}}
}

func fenced(language, text string) string {
	if language == "" {
		language = "text"
	}
	return "```" + language + "\n" + text + "\n```"
}

func selectionPromptElements(prompt control.SelectionPrompt) []map[string]any {
	if len(prompt.Options) == 0 {
		return nil
	}
	elements := make([]map[string]any, 0, len(prompt.Options)*2+1)
	for _, option := range prompt.Options {
		line := selectionOptionBody(prompt.Kind, option)
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": line,
		})
		elements = append(elements, map[string]any{
			"tag": "action",
			"actions": []map[string]any{
				selectionOptionButton(prompt, option),
			},
		})
	}
	if hint := strings.TrimSpace(prompt.Hint); hint != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": hint,
		})
	}
	return elements
}

func selectionOptionBody(kind control.SelectionPromptKind, option control.SelectionOption) string {
	current := ""
	if option.IsCurrent {
		current = " [当前]"
	}
	switch kind {
	case control.SelectionPromptAttachInstance:
		if option.Subtitle != "" {
			return fmt.Sprintf("%d. %s - 工作目录 `%s`%s", option.Index, option.Label, option.Subtitle, current)
		}
	default:
		if option.Subtitle != "" {
			return fmt.Sprintf("%d. %s%s\n`%s`", option.Index, option.Label, current, option.Subtitle)
		}
	}
	return fmt.Sprintf("%d. %s%s", option.Index, option.Label, current)
}

func selectionOptionButton(prompt control.SelectionPrompt, option control.SelectionOption) map[string]any {
	text := "选择"
	switch prompt.Kind {
	case control.SelectionPromptAttachInstance:
		text = "接管"
	case control.SelectionPromptUseThread:
		text = "切换"
	case control.SelectionPromptNewInstance:
		text = "恢复"
	}
	disabled := option.Disabled
	buttonType := "default"
	if option.IsCurrent {
		text = "当前"
		disabled = true
	} else {
		buttonType = "primary"
	}
	return map[string]any{
		"tag":  "button",
		"type": buttonType,
		"text": map[string]any{
			"tag":     "plain_text",
			"content": text,
		},
		"disabled": disabled,
		"value": map[string]any{
			"kind":      "prompt_select",
			"prompt_id": prompt.PromptID,
			"option_id": option.OptionID,
		},
	}
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

func requestPromptElements(prompt control.RequestPrompt) []map[string]any {
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
		button := requestPromptButton(prompt, option)
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

func requestPromptButton(prompt control.RequestPrompt, option control.RequestPromptOption) map[string]any {
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
		"value": map[string]any{
			"kind":              "request_respond",
			"request_id":        prompt.RequestID,
			"request_type":      strings.TrimSpace(prompt.RequestType),
			"request_option_id": strings.TrimSpace(option.OptionID),
		},
	}
}

func requestPromptContainsOption(options []control.RequestPromptOption, optionID string) bool {
	for _, option := range options {
		if strings.TrimSpace(option.OptionID) == optionID {
			return true
		}
	}
	return false
}

func formatSnapshot(snapshot control.Snapshot) string {
	lines := []string{}
	if snapshot.Attachment.InstanceID == "" {
		lines = append(lines, "当前未接管任何实例。")
	} else {
		lines = append(lines, fmt.Sprintf("已接管：%s", formatInstanceLabel(snapshot.Attachment.DisplayName, snapshot.Attachment.Source, snapshot.Attachment.Managed)))
		switch {
		case snapshot.Attachment.SelectedThreadTitle != "":
			lines = append(lines, fmt.Sprintf("当前输入目标：%s", snapshot.Attachment.SelectedThreadTitle))
			if short := shortenThreadID(snapshot.Attachment.SelectedThreadID); short != "" {
				lines = append(lines, fmt.Sprintf("会话 ID：%s", short))
			}
		case snapshot.Attachment.SelectedThreadID != "":
			lines = append(lines, fmt.Sprintf("当前输入目标：%s", snapshot.Attachment.SelectedThreadID))
		default:
			lines = append(lines, "当前输入目标：未绑定会话")
		}
		if preview := strings.TrimSpace(snapshot.Attachment.SelectedThreadPreview); preview != "" {
			lines = append(lines, fmt.Sprintf("最近信息：%s", preview))
		}
		if snapshot.Attachment.PID > 0 {
			lines = append(lines, fmt.Sprintf("实例 PID：`%d`", snapshot.Attachment.PID))
		}
		lines = append(lines, fmt.Sprintf("路由模式：%s", snapshot.Attachment.RouteMode))
		lines = append(lines, "")
		lines = append(lines, "如果现在从飞书发送一条消息：")
		target := "新建会话"
		switch {
		case snapshot.NextPrompt.ThreadTitle != "":
			target = snapshot.NextPrompt.ThreadTitle
		case snapshot.NextPrompt.ThreadID != "":
			target = snapshot.NextPrompt.ThreadID
		}
		lines = append(lines, fmt.Sprintf("目标：%s", target))
		if snapshot.NextPrompt.CWD != "" {
			lines = append(lines, fmt.Sprintf("工作目录：`%s`", snapshot.NextPrompt.CWD))
		}
		lines = append(lines, fmt.Sprintf("模型：`%s`（%s）", displaySnapshotValue(snapshot.NextPrompt.EffectiveModel, snapshot.NextPrompt.EffectiveModelSource), snapshotConfigSourceLabel(snapshot.NextPrompt.EffectiveModelSource)))
		lines = append(lines, fmt.Sprintf("推理强度：`%s`（%s）", displaySnapshotValue(snapshot.NextPrompt.EffectiveReasoningEffort, snapshot.NextPrompt.EffectiveReasoningEffortSource), snapshotConfigSourceLabel(snapshot.NextPrompt.EffectiveReasoningEffortSource)))
		lines = append(lines, fmt.Sprintf("执行权限：`%s`（%s）", agentproto.DisplayAccessModeShort(snapshot.NextPrompt.EffectiveAccessMode), snapshotConfigSourceLabel(snapshot.NextPrompt.EffectiveAccessModeSource)))
		overrideParts := []string{}
		if snapshot.NextPrompt.OverrideModel != "" {
			overrideParts = append(overrideParts, "模型 `"+snapshot.NextPrompt.OverrideModel+"`")
		}
		if snapshot.NextPrompt.OverrideReasoningEffort != "" {
			overrideParts = append(overrideParts, "推理 `"+snapshot.NextPrompt.OverrideReasoningEffort+"`")
		}
		if snapshot.NextPrompt.OverrideAccessMode != "" {
			overrideParts = append(overrideParts, "权限 `"+agentproto.DisplayAccessModeShort(snapshot.NextPrompt.OverrideAccessMode)+"`")
		}
		if len(overrideParts) == 0 {
			lines = append(lines, "飞书临时覆盖：无")
		} else {
			lines = append(lines, "飞书临时覆盖："+strings.Join(overrideParts, "，"))
		}
		lines = append(lines, fmt.Sprintf("底层真实配置：模型 `%s`（%s）；推理 `%s`（%s）",
			displaySnapshotValue(snapshot.NextPrompt.BaseModel, snapshot.NextPrompt.BaseModelSource),
			snapshotConfigSourceLabel(snapshot.NextPrompt.BaseModelSource),
			displaySnapshotValue(snapshot.NextPrompt.BaseReasoningEffort, snapshot.NextPrompt.BaseReasoningEffortSource),
			snapshotConfigSourceLabel(snapshot.NextPrompt.BaseReasoningEffortSource),
		))
	}
	if snapshot.PendingHeadless.InstanceID != "" {
		lines = append(lines, "")
		lines = append(lines, "Headless 创建中：")
		if snapshot.PendingHeadless.ThreadTitle != "" {
			lines = append(lines, fmt.Sprintf("- 目标会话：%s", snapshot.PendingHeadless.ThreadTitle))
		}
		if snapshot.PendingHeadless.ThreadCWD != "" {
			lines = append(lines, fmt.Sprintf("- 启动目录：`%s`", snapshot.PendingHeadless.ThreadCWD))
		}
		if snapshot.PendingHeadless.PID > 0 {
			lines = append(lines, fmt.Sprintf("- 进程 PID：`%d`", snapshot.PendingHeadless.PID))
		}
		if !snapshot.PendingHeadless.ExpiresAt.IsZero() {
			lines = append(lines, fmt.Sprintf("- 启动超时：`%s`", snapshot.PendingHeadless.ExpiresAt.Format("2006-01-02 15:04:05 MST")))
		}
	}
	if len(snapshot.Instances) > 0 {
		lines = append(lines, "")
		lines = append(lines, "在线实例：")
		for _, instance := range snapshot.Instances {
			if !instance.Online {
				continue
			}
			line := fmt.Sprintf("- %s - 工作目录 `%s`", formatInstanceLabel(instance.DisplayName, instance.Source, instance.Managed), instance.WorkspaceRoot)
			if instance.PID > 0 {
				line += fmt.Sprintf(" · PID `%d`", instance.PID)
			}
			lines = append(lines, line)
		}
	}
	if len(snapshot.Threads) > 0 {
		lines = append(lines, "")
		lines = append(lines, "已知会话：")
		for _, thread := range snapshot.Threads {
			flags := []string{}
			if thread.IsSelected {
				flags = append(flags, "当前")
			}
			if thread.IsObservedFocused {
				flags = append(flags, "VS Code")
			}
			suffix := ""
			if len(flags) > 0 {
				suffix = " [" + strings.Join(flags, ", ") + "]"
			}
			title := thread.DisplayTitle
			if title == "" {
				title = thread.Name
			}
			if title == "" {
				title = thread.ThreadID
			}
			line := fmt.Sprintf("- %s%s", title, suffix)
			if short := shortenThreadID(thread.ThreadID); short != "" && !strings.Contains(title, short) {
				line += fmt.Sprintf(" (ID %s)", short)
			}
			if preview := strings.TrimSpace(thread.Preview); preview != "" {
				line += fmt.Sprintf("\n  %s", preview)
			}
			lines = append(lines, line)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func displaySnapshotValue(value, source string) string {
	if strings.TrimSpace(value) == "" {
		return "未知"
	}
	return value
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

func formatInstanceLabel(displayName, source string, managed bool) string {
	label := strings.TrimSpace(displayName)
	if label == "" {
		label = "未知实例"
	}
	if strings.EqualFold(strings.TrimSpace(source), "headless") {
		if managed {
			return label + " (Headless)"
		}
		return label + " (Headless, unmanaged)"
	}
	return label
}

func chooseThemeKey(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "system"
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
