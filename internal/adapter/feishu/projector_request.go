package feishu

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

const (
	requestUserInputSubmitOptionID                      = "submit"
	requestUserInputConfirmSubmitWithUnansweredOptionID = "confirm_submit_with_unanswered"
	requestUserInputCancelSubmitWithUnansweredOptionID  = "cancel_submit_with_unanswered"
)

func requestPromptBody(prompt control.FeishuDirectRequestPrompt) string {
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

func requestPromptElements(prompt control.FeishuDirectRequestPrompt, daemonLifecycleID string) []map[string]any {
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

func requestUserInputPromptElements(prompt control.FeishuDirectRequestPrompt, daemonLifecycleID string) []map[string]any {
	elements := make([]map[string]any, 0, len(prompt.Questions)*3+2)
	if progress := requestPromptProgressMarkdown(prompt); progress != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": progress,
		})
	}
	if prompt.SubmitWithUnansweredConfirmPending {
		if markdown := requestPromptSubmitConfirmMarkdown(prompt); markdown != "" {
			elements = append(elements, map[string]any{
				"tag":     "markdown",
				"content": markdown,
			})
		}
		if row := requestPromptSubmitConfirmActionRow(prompt, daemonLifecycleID); len(row) != 0 {
			elements = append(elements, row)
		}
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
			if len(actions) != 0 {
				if group := cardButtonGroupElement(actions); len(group) != 0 {
					elements = append(elements, group)
				}
			}
		}
	}
	if requestPromptNeedsForm(prompt) {
		if form := requestPromptFormElement(prompt, daemonLifecycleID); len(form) != 0 {
			elements = append(elements, form)
		}
	} else if !prompt.SubmitWithUnansweredConfirmPending {
		if row := requestPromptSubmitActionRow(prompt, daemonLifecycleID); len(row) != 0 {
			elements = append(elements, row)
		}
	}
	elements = append(elements, map[string]any{
		"tag":     "markdown",
		"content": requestPromptQuestionHint(prompt),
	})
	return elements
}

func requestPromptButton(prompt control.FeishuDirectRequestPrompt, option control.RequestPromptOption, daemonLifecycleID string) map[string]any {
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
		cardActionPayloadKeyRequestRevision: prompt.RequestRevision,
	}, daemonLifecycleID), false, "")
}

func requestUserInputOptionButton(prompt control.FeishuDirectRequestPrompt, question control.RequestPromptQuestion, option control.RequestPromptQuestionOption, daemonLifecycleID string) map[string]any {
	label := strings.TrimSpace(option.Label)
	if label == "" {
		return nil
	}
	buttonType := "primary"
	selectedAnswer := strings.TrimSpace(question.DefaultValue)
	if selectedAnswer != "" && !strings.EqualFold(selectedAnswer, label) {
		buttonType = "default"
	}
	return cardCallbackButtonElement(label, buttonType, stampActionValue(map[string]any{
		cardActionPayloadKeyKind:            cardActionKindRequestRespond,
		cardActionPayloadKeyRequestID:       prompt.RequestID,
		cardActionPayloadKeyRequestType:     strings.TrimSpace(prompt.RequestType),
		cardActionPayloadKeyRequestRevision: prompt.RequestRevision,
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
	lines := make([]string, 0, 8)
	title := firstNonEmpty(strings.TrimSpace(question.Header), strings.TrimSpace(question.Question))
	if title != "" {
		lines = append(lines, fmt.Sprintf("**问题 %d** %s", index+1, title))
	}
	if question.Answered {
		lines = append(lines, "状态：已回答")
	} else {
		lines = append(lines, "状态：待回答")
	}
	if question.Header != "" && strings.TrimSpace(question.Question) != "" && strings.TrimSpace(question.Question) != strings.TrimSpace(question.Header) {
		lines = append(lines, strings.TrimSpace(question.Question))
	}
	if value := strings.TrimSpace(question.DefaultValue); value != "" {
		lines = append(lines, "当前答案："+value)
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

func requestPromptProgressMarkdown(prompt control.FeishuDirectRequestPrompt) string {
	if len(prompt.Questions) == 0 {
		return ""
	}
	answered := 0
	for _, question := range prompt.Questions {
		if question.Answered {
			answered++
		}
	}
	return fmt.Sprintf("**回答进度** %d/%d", answered, len(prompt.Questions))
}

func requestPromptNeedsForm(prompt control.FeishuDirectRequestPrompt) bool {
	for _, question := range prompt.Questions {
		if requestPromptQuestionNeedsFormInput(question) {
			return true
		}
	}
	return false
}

func requestPromptQuestionNeedsFormInput(question control.RequestPromptQuestion) bool {
	return len(question.Options) == 0 || question.AllowOther || !question.DirectResponse
}

func requestPromptFormElement(prompt control.FeishuDirectRequestPrompt, daemonLifecycleID string) map[string]any {
	elements := make([]map[string]any, 0, len(prompt.Questions)+2)
	for _, question := range prompt.Questions {
		if !requestPromptQuestionNeedsFormInput(question) {
			continue
		}
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
	if len(elements) == 0 {
		return nil
	}
	elements = append(elements, cardFormSubmitButtonElement("提交答案", stampActionValue(map[string]any{
		cardActionPayloadKeyKind:            cardActionKindSubmitRequestForm,
		cardActionPayloadKeyRequestID:       prompt.RequestID,
		cardActionPayloadKeyRequestType:     strings.TrimSpace(prompt.RequestType),
		cardActionPayloadKeyRequestOptionID: requestUserInputSubmitOptionID,
		cardActionPayloadKeyRequestRevision: prompt.RequestRevision,
	}, daemonLifecycleID)))
	return map[string]any{
		"tag":      "form",
		"name":     "request_form_" + strings.TrimSpace(prompt.RequestID),
		"elements": elements,
	}
}

func requestPromptSubmitActionRow(prompt control.FeishuDirectRequestPrompt, daemonLifecycleID string) map[string]any {
	if len(prompt.Questions) == 0 {
		return nil
	}
	return cardButtonGroupElement([]map[string]any{
		cardCallbackButtonElement("提交答案", "primary", stampActionValue(map[string]any{
			cardActionPayloadKeyKind:            cardActionKindRequestRespond,
			cardActionPayloadKeyRequestID:       prompt.RequestID,
			cardActionPayloadKeyRequestType:     strings.TrimSpace(prompt.RequestType),
			cardActionPayloadKeyRequestOptionID: requestUserInputSubmitOptionID,
		}, daemonLifecycleID), false, ""),
	})
}

func requestPromptSubmitConfirmActionRow(prompt control.FeishuDirectRequestPrompt, daemonLifecycleID string) map[string]any {
	return cardButtonGroupElement([]map[string]any{
		cardCallbackButtonElement("继续补答", "default", stampActionValue(map[string]any{
			cardActionPayloadKeyKind:            cardActionKindRequestRespond,
			cardActionPayloadKeyRequestID:       prompt.RequestID,
			cardActionPayloadKeyRequestType:     strings.TrimSpace(prompt.RequestType),
			cardActionPayloadKeyRequestOptionID: requestUserInputCancelSubmitWithUnansweredOptionID,
			cardActionPayloadKeyRequestRevision: prompt.RequestRevision,
		}, daemonLifecycleID), false, ""),
		cardCallbackButtonElement("确认提交已有答案", "primary", stampActionValue(map[string]any{
			cardActionPayloadKeyKind:            cardActionKindRequestRespond,
			cardActionPayloadKeyRequestID:       prompt.RequestID,
			cardActionPayloadKeyRequestType:     strings.TrimSpace(prompt.RequestType),
			cardActionPayloadKeyRequestOptionID: requestUserInputConfirmSubmitWithUnansweredOptionID,
			cardActionPayloadKeyRequestRevision: prompt.RequestRevision,
		}, daemonLifecycleID), false, ""),
	})
}

func requestPromptSubmitConfirmMarkdown(prompt control.FeishuDirectRequestPrompt) string {
	missing := len(prompt.SubmitWithUnansweredMissingLabels)
	switch {
	case missing <= 0:
		return "仍有未答题。是否提交已有答案？"
	case missing == 1:
		return fmt.Sprintf("仍有 1 个问题未回答：%s。是否提交已有答案？", prompt.SubmitWithUnansweredMissingLabels[0])
	default:
		return fmt.Sprintf("仍有 %d 个问题未回答。是否提交已有答案？", missing)
	}
}

func requestPromptQuestionHint(prompt control.FeishuDirectRequestPrompt) string {
	if prompt.SubmitWithUnansweredConfirmPending {
		return "你可以继续补答，也可以确认提交已有答案（未回答的问题将按留空提交）。"
	}
	hasDirect := false
	for _, question := range prompt.Questions {
		if question.DirectResponse && len(question.Options) != 0 {
			hasDirect = true
			break
		}
	}
	if hasDirect && requestPromptNeedsForm(prompt) {
		return "可直接点击按钮回答单选题；如果需要补充文字或填写其他答案，请在下方表单里提交。若仍有未答题，会先进入确认提交步骤。"
	}
	if hasDirect {
		return "点击按钮可逐题作答；准备结束时请点击“提交答案”。若仍有未答题，会先进入确认提交步骤。"
	}
	return "填写后点击“提交答案”；若仍有未答题，会先进入确认提交步骤。"
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
