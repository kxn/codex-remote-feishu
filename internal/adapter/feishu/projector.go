package feishu

import (
	"fmt"
	"strings"

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
	Kind         OperationKind
	ChatID       string
	MessageID    string
	EmojiType    string
	Text         string
	CardTitle    string
	CardBody     string
	CardThemeKey string
	CardElements []map[string]any
}

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
			Kind:         OperationSendCard,
			ChatID:       chatID,
			CardTitle:    "当前状态",
			CardBody:     formatSnapshot(*event.Snapshot),
			CardThemeKey: "system",
		}}
	case control.UIEventNotice:
		if event.Notice == nil {
			return nil
		}
		return []Operation{{
			Kind:         OperationSendCard,
			ChatID:       chatID,
			CardTitle:    "系统提示",
			CardBody:     event.Notice.Text,
			CardThemeKey: "system",
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
			}
		}
		return []Operation{{
			Kind:         OperationSendCard,
			ChatID:       chatID,
			CardTitle:    title,
			CardBody:     "",
			CardThemeKey: "system",
			CardElements: selectionPromptElements(*event.SelectionPrompt),
		}}
	case control.UIEventPendingInput:
		if event.PendingInput == nil {
			return nil
		}
		var ops []Operation
		if event.PendingInput.TypingOn {
			ops = append(ops, Operation{
				Kind:      OperationAddReaction,
				ChatID:    chatID,
				MessageID: event.PendingInput.SourceMessageID,
				EmojiType: "THINKING",
			})
		}
		if event.PendingInput.TypingOff {
			ops = append(ops, Operation{
				Kind:      OperationRemoveReaction,
				ChatID:    chatID,
				MessageID: event.PendingInput.SourceMessageID,
				EmojiType: "THINKING",
			})
		}
		if event.PendingInput.ThumbsDown {
			ops = append(ops, Operation{
				Kind:      OperationAddReaction,
				ChatID:    chatID,
				MessageID: event.PendingInput.SourceMessageID,
				EmojiType: "THUMBSDOWN",
			})
		}
		return ops
	case control.UIEventBlockCommitted:
		if event.Block == nil {
			return nil
		}
		return projectBlock(chatID, *event.Block)
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
			Kind:         OperationSendCard,
			ChatID:       chatID,
			CardTitle:    "系统提示",
			CardBody:     body,
			CardThemeKey: chooseThemeKey(event.ThreadSelection.ThreadID, "system"),
		}}
	default:
		return nil
	}
}

func projectBlock(chatID string, block render.Block) []Operation {
	if !block.Final {
		return []Operation{{
			Kind:   OperationSendText,
			ChatID: chatID,
			Text:   block.Text,
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
		Kind:         OperationSendCard,
		ChatID:       chatID,
		CardTitle:    title,
		CardBody:     body,
		CardThemeKey: chooseThemeKey(block.ThemeKey, block.ThreadID),
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

func formatSnapshot(snapshot control.Snapshot) string {
	lines := []string{}
	if snapshot.Attachment.InstanceID == "" {
		lines = append(lines, "当前未接管任何实例。")
	} else {
		lines = append(lines, fmt.Sprintf("已接管：%s", snapshot.Attachment.DisplayName))
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
		lines = append(lines, fmt.Sprintf("模型：`%s`（%s）", displaySnapshotValue(snapshot.NextPrompt.EffectiveModel), snapshotConfigSourceLabel(snapshot.NextPrompt.EffectiveModelSource)))
		lines = append(lines, fmt.Sprintf("推理强度：`%s`（%s）", displaySnapshotValue(snapshot.NextPrompt.EffectiveReasoningEffort), snapshotConfigSourceLabel(snapshot.NextPrompt.EffectiveReasoningEffortSource)))
		overrideParts := []string{}
		if snapshot.NextPrompt.OverrideModel != "" {
			overrideParts = append(overrideParts, "模型 `"+snapshot.NextPrompt.OverrideModel+"`")
		}
		if snapshot.NextPrompt.OverrideReasoningEffort != "" {
			overrideParts = append(overrideParts, "推理 `"+snapshot.NextPrompt.OverrideReasoningEffort+"`")
		}
		if len(overrideParts) == 0 {
			lines = append(lines, "飞书临时覆盖：无")
		} else {
			lines = append(lines, "飞书临时覆盖："+strings.Join(overrideParts, "，"))
		}
		lines = append(lines, fmt.Sprintf("底层真实配置：模型 `%s`（%s）；推理 `%s`（%s）",
			displaySnapshotValue(snapshot.NextPrompt.BaseModel),
			snapshotConfigSourceLabel(snapshot.NextPrompt.BaseModelSource),
			displaySnapshotValue(snapshot.NextPrompt.BaseReasoningEffort),
			snapshotConfigSourceLabel(snapshot.NextPrompt.BaseReasoningEffortSource),
		))
	}
	if len(snapshot.Instances) > 0 {
		lines = append(lines, "")
		lines = append(lines, "在线实例：")
		for _, instance := range snapshot.Instances {
			if !instance.Online {
				continue
			}
			line := fmt.Sprintf("- %s - 工作目录 `%s`", instance.DisplayName, instance.WorkspaceRoot)
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

func displaySnapshotValue(value string) string {
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
	default:
		return "未知"
	}
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
