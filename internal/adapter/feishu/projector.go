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
	GatewayID        string
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
			CardBody:         event.Notice.Text,
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
				title = "在线实例"
			case control.SelectionPromptUseThread:
				title = "会话列表"
			case control.SelectionPromptNewInstance:
				title = "选择要恢复的会话"
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
			GatewayID:        event.GatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			CardTitle:        title,
			CardBody:         requestPromptBody(*event.RequestPrompt),
			CardThemeKey:     cardThemeApproval,
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
		return projectBlock(event.GatewayID, event.SurfaceSessionID, chatID, *event.Block, event.FileChangeSummary)
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

func projectBlock(gatewayID, surfaceSessionID, chatID string, block render.Block, summary *control.FileChangeSummary) []Operation {
	if !block.Final {
		return []Operation{{
			Kind:             OperationSendText,
			GatewayID:        gatewayID,
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
	elements := finalBlockExtraElements(summary)
	return []Operation{{
		Kind:             OperationSendCard,
		GatewayID:        gatewayID,
		SurfaceSessionID: surfaceSessionID,
		ChatID:           chatID,
		CardTitle:        title,
		CardBody:         body,
		CardThemeKey:     cardThemeFinal,
		CardElements:     elements,
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
			parts := strings.Split(option.Subtitle, "\n")
			line := fmt.Sprintf("%d. %s - 工作目录 `%s`%s", option.Index, option.Label, parts[0], current)
			if len(parts) > 1 {
				line += "\n" + strings.Join(parts[1:], "\n")
			}
			return line
		}
	default:
		if option.Subtitle != "" {
			parts := strings.Split(option.Subtitle, "\n")
			line := fmt.Sprintf("%d. %s%s", option.Index, option.Label, current)
			if len(parts) > 0 && parts[0] != "" {
				line += "\n`" + parts[0] + "`"
			}
			if len(parts) > 1 {
				line += "\n" + strings.Join(parts[1:], "\n")
			}
			return line
		}
	}
	return fmt.Sprintf("%d. %s%s", option.Index, option.Label, current)
}

func selectionOptionButton(prompt control.SelectionPrompt, option control.SelectionOption) map[string]any {
	text := strings.TrimSpace(option.ButtonLabel)
	if text == "" {
		text = "选择"
	}
	value := map[string]any{}
	switch prompt.Kind {
	case control.SelectionPromptAttachInstance:
		if text == "选择" {
			text = "接管"
		}
		value = map[string]any{
			"kind":        "attach_instance",
			"instance_id": strings.TrimSpace(option.OptionID),
		}
	case control.SelectionPromptUseThread:
		if text == "选择" {
			text = "切换"
		}
		value = map[string]any{
			"kind":      "use_thread",
			"thread_id": strings.TrimSpace(option.OptionID),
		}
	case control.SelectionPromptNewInstance:
		if text == "选择" {
			text = "恢复"
		}
		value = map[string]any{
			"kind":      "resume_headless_thread",
			"thread_id": strings.TrimSpace(option.OptionID),
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
		"value":    value,
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

func finalBlockExtraElements(summary *control.FileChangeSummary) []map[string]any {
	if summary == nil || summary.FileCount == 0 || len(summary.Files) == 0 {
		return nil
	}
	elements := []map[string]any{{
		"tag": "markdown",
		"content": fmt.Sprintf(
			"**本次修改** %d 个文件  %s",
			summary.FileCount,
			formatFileChangeCountsMarkdown(summary.AddedLines, summary.RemovedLines),
		),
	}}
	labels := fileChangeDisplayLabels(summary.Files)
	limit := len(summary.Files)
	if limit > maxEmbeddedFileSummaryRows {
		limit = maxEmbeddedFileSummaryRows
	}
	for index := 0; index < limit; index++ {
		elements = append(elements, map[string]any{
			"tag": "markdown",
			"content": fmt.Sprintf(
				"%d. %s\n%s",
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
	return elements
}

func formatFileChangePath(file control.FileChangeSummaryEntry, labels map[string]string) string {
	path := strings.TrimSpace(file.Path)
	movePath := strings.TrimSpace(file.MovePath)
	switch {
	case path != "" && movePath != "":
		return fmt.Sprintf("`%s` -> `%s`", fileChangeDisplayLabel(path, labels), fileChangeDisplayLabel(movePath, labels))
	case path != "":
		return fmt.Sprintf("`%s`", fileChangeDisplayLabel(path, labels))
	case movePath != "":
		return fmt.Sprintf("`%s`", fileChangeDisplayLabel(movePath, labels))
	default:
		return "`(unknown)`"
	}
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
	if snapshot.Attachment.InstanceID == "" {
		lines = append(lines, snapshotField("已接管", "无"))
	} else {
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
			lines = append(lines, snapshotField("实例 PID", fmt.Sprintf("`%d`", snapshot.Attachment.PID)))
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
			lines = append(lines, snapshotField("工作目录", fmt.Sprintf("`%s`", snapshot.NextPrompt.CWD)))
		}
		lines = append(lines, snapshotField("模型", fmt.Sprintf("`%s`（%s）", displaySnapshotValue(snapshot.NextPrompt.EffectiveModel, snapshot.NextPrompt.EffectiveModelSource), snapshotConfigSourceLabel(snapshot.NextPrompt.EffectiveModelSource))))
		lines = append(lines, snapshotField("推理强度", fmt.Sprintf("`%s`（%s）", displaySnapshotValue(snapshot.NextPrompt.EffectiveReasoningEffort, snapshot.NextPrompt.EffectiveReasoningEffortSource), snapshotConfigSourceLabel(snapshot.NextPrompt.EffectiveReasoningEffortSource))))
		lines = append(lines, snapshotField("执行权限", fmt.Sprintf("`%s`（%s）", agentproto.DisplayAccessModeShort(snapshot.NextPrompt.EffectiveAccessMode), snapshotConfigSourceLabel(snapshot.NextPrompt.EffectiveAccessModeSource))))
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
			lines = append(lines, snapshotField("飞书临时覆盖", "无"))
		} else {
			lines = append(lines, snapshotField("飞书临时覆盖", strings.Join(overrideParts, "，")))
		}
		lines = append(lines, snapshotField("底层真实配置", fmt.Sprintf("模型 `%s`（%s）；推理 `%s`（%s）",
			displaySnapshotValue(snapshot.NextPrompt.BaseModel, snapshot.NextPrompt.BaseModelSource),
			snapshotConfigSourceLabel(snapshot.NextPrompt.BaseModelSource),
			displaySnapshotValue(snapshot.NextPrompt.BaseReasoningEffort, snapshot.NextPrompt.BaseReasoningEffortSource),
			snapshotConfigSourceLabel(snapshot.NextPrompt.BaseReasoningEffortSource),
		)))
	}
	if snapshot.PendingHeadless.InstanceID != "" {
		lines = append(lines, "")
		lines = append(lines, "**Headless 创建中：**")
		if snapshot.PendingHeadless.ThreadTitle != "" {
			lines = append(lines, fmt.Sprintf("- %s", snapshotField("目标会话", snapshot.PendingHeadless.ThreadTitle)))
		}
		if snapshot.PendingHeadless.ThreadCWD != "" {
			lines = append(lines, fmt.Sprintf("- %s", snapshotField("启动目录", fmt.Sprintf("`%s`", snapshot.PendingHeadless.ThreadCWD))))
		}
		if snapshot.PendingHeadless.PID > 0 {
			lines = append(lines, fmt.Sprintf("- %s", snapshotField("进程 PID", fmt.Sprintf("`%d`", snapshot.PendingHeadless.PID))))
		}
		if !snapshot.PendingHeadless.ExpiresAt.IsZero() {
			lines = append(lines, fmt.Sprintf("- %s", snapshotField("启动超时", fmt.Sprintf("`%s`", snapshot.PendingHeadless.ExpiresAt.Format("2006-01-02 15:04:05 MST")))))
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func snapshotField(label, value string) string {
	return fmt.Sprintf("**%s：** %s", label, value)
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
