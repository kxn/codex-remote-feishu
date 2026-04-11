package feishu

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func requestPromptBody(prompt control.RequestPrompt) string {
	lines := []string{}
	if prompt.ThreadTitle != "" {
		lines = append(lines, "当前会话："+prompt.ThreadTitle)
	}
	body := strings.TrimSpace(prompt.Body)
	if body == "" {
		if prompt.RequestType == "request_user_input" {
			body = "本地 Codex 正在等待你补充参数或说明。"
		} else {
			body = "本地 Codex 正在等待你的确认。"
		}
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
	if normalizeRequestPromptType(prompt.RequestType) == "request_user_input" && len(prompt.Questions) != 0 {
		return requestUserInputPromptElements(prompt, daemonLifecycleID)
	}
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
	elements := make([]map[string]any, 0, 2)
	if group := cardButtonGroupElement(actions); len(group) != 0 {
		elements = append(elements, group)
	}
	elements = append(elements, map[string]any{
		"tag":     "markdown",
		"content": hint,
	})
	return elements
}

func requestUserInputPromptElements(prompt control.RequestPrompt, daemonLifecycleID string) []map[string]any {
	elements := make([]map[string]any, 0, len(prompt.Questions)*3+1)
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
			if len(actions) != 0 {
				if group := cardButtonGroupElement(actions); len(group) != 0 {
					elements = append(elements, group)
				}
			}
		}
	}
	if requestPromptNeedsForm(prompt) {
		elements = append(elements, requestPromptFormElement(prompt, daemonLifecycleID))
	}
	elements = append(elements, map[string]any{
		"tag":     "markdown",
		"content": requestPromptQuestionHint(prompt),
	})
	return elements
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
	return cardCallbackButtonElement(label, buttonType, stampActionValue(map[string]any{
		cardActionPayloadKeyKind:            cardActionKindRequestRespond,
		cardActionPayloadKeyRequestID:       prompt.RequestID,
		cardActionPayloadKeyRequestType:     strings.TrimSpace(prompt.RequestType),
		cardActionPayloadKeyRequestOptionID: strings.TrimSpace(option.OptionID),
	}, daemonLifecycleID), false, "")
}

func requestUserInputOptionButton(prompt control.RequestPrompt, question control.RequestPromptQuestion, option control.RequestPromptQuestionOption, daemonLifecycleID string) map[string]any {
	label := strings.TrimSpace(option.Label)
	if label == "" {
		return nil
	}
	return cardCallbackButtonElement(label, "primary", stampActionValue(map[string]any{
		cardActionPayloadKeyKind:        cardActionKindRequestRespond,
		cardActionPayloadKeyRequestID:   prompt.RequestID,
		cardActionPayloadKeyRequestType: strings.TrimSpace(prompt.RequestType),
		cardActionPayloadKeyRequestAnswers: map[string]any{
			strings.TrimSpace(question.ID): []any{label},
		},
	}, daemonLifecycleID), false, "")
}

func stampActionValue(value map[string]any, daemonLifecycleID string) map[string]any {
	return actionPayloadWithLifecycle(value, daemonLifecycleID)
}

func requestPromptContainsOption(options []control.RequestPromptOption, optionID string) bool {
	for _, option := range options {
		if strings.TrimSpace(option.OptionID) == optionID {
			return true
		}
	}
	return false
}

func requestPromptQuestionMarkdown(index int, question control.RequestPromptQuestion) string {
	lines := make([]string, 0, 6)
	title := firstNonEmpty(strings.TrimSpace(question.Header), strings.TrimSpace(question.Question))
	if title != "" {
		lines = append(lines, fmt.Sprintf("**问题 %d** %s", index+1, title))
	}
	if question.Header != "" && strings.TrimSpace(question.Question) != "" && strings.TrimSpace(question.Question) != strings.TrimSpace(question.Header) {
		lines = append(lines, strings.TrimSpace(question.Question))
	}
	if len(question.Options) != 0 {
		lines = append(lines, "")
		lines = append(lines, "可选项：")
		for _, option := range question.Options {
			line := "- " + strings.TrimSpace(option.Label)
			if description := strings.TrimSpace(option.Description); description != "" {
				line += "：" + description
			}
			lines = append(lines, line)
		}
	}
	if question.AllowOther {
		lines = append(lines, "")
		lines = append(lines, "也可以直接填写其他答案。")
	}
	if question.Secret {
		lines = append(lines, "")
		lines = append(lines, "该答案按私密输入处理，不会在飞书卡片正文中回显。")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func requestPromptNeedsForm(prompt control.RequestPrompt) bool {
	for _, question := range prompt.Questions {
		if len(question.Options) == 0 || question.AllowOther || !question.DirectResponse {
			return true
		}
	}
	return false
}

func requestPromptFormElement(prompt control.RequestPrompt, daemonLifecycleID string) map[string]any {
	elements := make([]map[string]any, 0, len(prompt.Questions)+1)
	for _, question := range prompt.Questions {
		name := strings.TrimSpace(question.ID)
		if name == "" {
			continue
		}
		input := map[string]any{
			"tag":  "input",
			"name": name,
		}
		label := firstNonEmpty(strings.TrimSpace(question.Header), strings.TrimSpace(question.Question), name)
		input["label"] = map[string]any{
			"tag":     "plain_text",
			"content": label,
		}
		input["label_position"] = "left"
		if placeholder := strings.TrimSpace(question.Placeholder); placeholder != "" {
			input["placeholder"] = map[string]any{
				"tag":     "plain_text",
				"content": placeholder,
			}
		}
		if value := strings.TrimSpace(question.DefaultValue); value != "" {
			input["default_value"] = value
		}
		elements = append(elements, input)
	}
	elements = append(elements, cardFormSubmitButtonElement("提交答案", stampActionValue(map[string]any{
		cardActionPayloadKeyKind:        cardActionKindSubmitRequestForm,
		cardActionPayloadKeyRequestID:   prompt.RequestID,
		cardActionPayloadKeyRequestType: strings.TrimSpace(prompt.RequestType),
	}, daemonLifecycleID)))
	return map[string]any{
		"tag":      "form",
		"name":     "request_form_" + strings.TrimSpace(prompt.RequestID),
		"elements": elements,
	}
}

func requestPromptQuestionHint(prompt control.RequestPrompt) string {
	hasDirect := false
	for _, question := range prompt.Questions {
		if question.DirectResponse && len(question.Options) != 0 {
			hasDirect = true
			break
		}
	}
	if hasDirect && requestPromptNeedsForm(prompt) {
		return "可直接点击按钮回答单选题；如果需要补充文字或填写其他答案，请在下方表单里提交。"
	}
	if hasDirect {
		return "点击按钮即可将答案直接回传给当前 turn。"
	}
	return "填写后点击“提交答案”，答案会直接回传给当前 turn。"
}

func normalizeRequestPromptType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch {
	case normalized == "", normalized == "approval":
		return "approval"
	case strings.HasPrefix(normalized, "approval"):
		return "approval"
	default:
		return normalized
	}
}
