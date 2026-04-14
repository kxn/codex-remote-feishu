package feishu

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func permissionsRequestPromptElements(prompt control.FeishuDirectRequestPrompt, daemonLifecycleID string) []map[string]any {
	options := prompt.Options
	if len(options) == 0 {
		options = []control.RequestPromptOption{
			{OptionID: "accept", Label: "允许本次", Style: "primary"},
			{OptionID: "acceptForSession", Label: "本会话允许", Style: "default"},
			{OptionID: "decline", Label: "拒绝", Style: "default"},
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
	elements := make([]map[string]any, 0, 2)
	if row := cardButtonGroupElement(actions); len(row) != 0 {
		elements = append(elements, row)
	}
	elements = append(elements, map[string]any{
		"tag":     "markdown",
		"content": "你可以选择仅授权当前这一次，或在当前会话内持续授权。",
	})
	return elements
}

func mcpElicitationPromptElements(prompt control.FeishuDirectRequestPrompt, daemonLifecycleID string) []map[string]any {
	if len(prompt.Questions) == 0 {
		return mcpElicitationChoiceElements(prompt, daemonLifecycleID)
	}
	elements := make([]map[string]any, 0, len(prompt.Questions)*3+4)
	if progress := mcpElicitationProgressMarkdown(prompt); progress != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": progress,
		})
	}
	for index, question := range prompt.Questions {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": requestPromptQuestionMarkdown(index, question),
		})
		if question.DirectResponse && len(question.Options) != 0 {
			actions := make([]map[string]any, 0, len(question.Options))
			for _, option := range question.Options {
				button := requestUserInputOptionButton(prompt, question, option, daemonLifecycleID)
				if len(button) == 0 {
					continue
				}
				actions = append(actions, button)
			}
			if row := cardButtonGroupElement(actions); len(row) != 0 {
				elements = append(elements, row)
			}
		}
	}
	if requestPromptNeedsForm(prompt) {
		if form := requestPromptFormElement(prompt, daemonLifecycleID); len(form) != 0 {
			elements = append(elements, form)
		}
	} else if row := requestPromptSubmitActionRow(prompt, daemonLifecycleID); len(row) != 0 {
		elements = append(elements, row)
	}
	if row := mcpElicitationTerminalActionRow(prompt, daemonLifecycleID); len(row) != 0 {
		elements = append(elements, row)
	}
	elements = append(elements, map[string]any{
		"tag":     "markdown",
		"content": mcpElicitationQuestionHint(prompt),
	})
	return elements
}

func mcpElicitationChoiceElements(prompt control.FeishuDirectRequestPrompt, daemonLifecycleID string) []map[string]any {
	options := prompt.Options
	if len(options) == 0 {
		options = []control.RequestPromptOption{
			{OptionID: "accept", Label: "继续", Style: "primary"},
			{OptionID: "decline", Label: "拒绝", Style: "default"},
			{OptionID: "cancel", Label: "取消", Style: "default"},
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
	elements := make([]map[string]any, 0, 2)
	if row := cardButtonGroupElement(actions); len(row) != 0 {
		elements = append(elements, row)
	}
	elements = append(elements, map[string]any{
		"tag":     "markdown",
		"content": "如果需要先完成外部页面操作，请完成后再点击“继续”；如果不打算继续，可直接拒绝或取消。",
	})
	return elements
}

func mcpElicitationTerminalActionRow(prompt control.FeishuDirectRequestPrompt, daemonLifecycleID string) map[string]any {
	return cardButtonGroupElement([]map[string]any{
		cardCallbackButtonElement("拒绝", "default", stampActionValue(map[string]any{
			cardActionPayloadKeyKind:            cardActionKindRequestRespond,
			cardActionPayloadKeyRequestID:       prompt.RequestID,
			cardActionPayloadKeyRequestType:     strings.TrimSpace(prompt.RequestType),
			cardActionPayloadKeyRequestOptionID: "decline",
			cardActionPayloadKeyRequestRevision: prompt.RequestRevision,
		}, daemonLifecycleID), false, ""),
		cardCallbackButtonElement("取消", "default", stampActionValue(map[string]any{
			cardActionPayloadKeyKind:            cardActionKindRequestRespond,
			cardActionPayloadKeyRequestID:       prompt.RequestID,
			cardActionPayloadKeyRequestType:     strings.TrimSpace(prompt.RequestType),
			cardActionPayloadKeyRequestOptionID: "cancel",
			cardActionPayloadKeyRequestRevision: prompt.RequestRevision,
		}, daemonLifecycleID), false, ""),
	})
}

func mcpElicitationProgressMarkdown(prompt control.FeishuDirectRequestPrompt) string {
	if len(prompt.Questions) == 0 {
		return ""
	}
	answered := 0
	for _, question := range prompt.Questions {
		if question.Answered {
			answered++
		}
	}
	return fmt.Sprintf("**填写进度** %d/%d", answered, len(prompt.Questions))
}

func mcpElicitationQuestionHint(prompt control.FeishuDirectRequestPrompt) string {
	hasDirect := false
	for _, question := range prompt.Questions {
		if question.DirectResponse && len(question.Options) != 0 {
			hasDirect = true
			break
		}
	}
	if hasDirect && requestPromptNeedsForm(prompt) {
		return "可先点击按钮填写单选字段；如果需要补充文字或 JSON，请在下方表单提交。确认无误后点击“提交并继续”。"
	}
	if hasDirect {
		return "可先点击按钮完成字段选择；确认无误后点击“提交并继续”。"
	}
	return "填写完成后点击“提交并继续”；如果不想继续这次 MCP 请求，可直接拒绝或取消。"
}
