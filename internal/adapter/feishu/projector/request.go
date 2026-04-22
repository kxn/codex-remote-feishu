package projector

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

const (
	requestControlCancelTurn          = "cancel_turn"
	requestControlSkipOption          = "skip_optional"
	requestPromptStepPreviousOptionID = "step_previous"
	requestPromptStepNextOptionID     = "step_next"
)

func requestPromptSections(prompt control.FeishuRequestView) []control.FeishuCardTextSection {
	sections := make([]control.FeishuCardTextSection, 0, len(prompt.Sections)+1)
	if threadTitle := strings.TrimSpace(prompt.ThreadTitle); threadTitle != "" {
		sections = append(sections, control.FeishuCardTextSection{
			Lines: []string{"当前会话：" + threadTitle},
		})
	}
	for _, section := range prompt.Sections {
		if normalized := section.Normalized(); normalized.Label != "" || len(normalized.Lines) != 0 {
			sections = append(sections, normalized)
		}
	}
	return sections
}

func RequestPromptElements(prompt control.FeishuRequestView, daemonLifecycleID string) []map[string]any {
	elements := appendCardTextSections(nil, requestPromptSections(prompt))
	switch normalizeRequestPromptType(prompt.RequestType) {
	case "request_user_input":
		if len(prompt.Questions) != 0 {
			return append(elements, requestUserInputPromptElements(prompt, daemonLifecycleID)...)
		}
	case "permissions_request_approval":
		return append(elements, permissionsRequestPromptElements(prompt, daemonLifecycleID)...)
	case "mcp_server_elicitation":
		return append(elements, mcpElicitationPromptElements(prompt, daemonLifecycleID)...)
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
	if group := cardButtonGroupElement(actions); len(group) != 0 {
		elements = append(elements, group)
	}
	elements = append(elements, map[string]any{
		"tag":     "markdown",
		"content": hint,
	})
	return elements
}

func requestUserInputPromptElements(prompt control.FeishuRequestView, daemonLifecycleID string) []map[string]any {
	elements := make([]map[string]any, 0, 12)
	if progress := requestPromptProgressMarkdown(prompt); progress != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": progress,
		})
	}
	elements = appendCurrentRequestQuestionElements(elements, prompt, daemonLifecycleID)
	if requestPromptNeedsForm(prompt) && !prompt.Sealed {
		if form := requestPromptFormElement(prompt, daemonLifecycleID); len(form) != 0 {
			elements = append(elements, form)
		}
	}
	if retry := requestPromptRetryActionElement(prompt, daemonLifecycleID); len(retry) != 0 {
		elements = append(elements, retry)
	}
	if skip := requestPromptSkipOptionalElement(prompt, daemonLifecycleID); len(skip) != 0 {
		elements = append(elements, skip)
	}
	if status := requestPromptStatusMarkdown(prompt); status != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": status,
		})
	}
	if footer := requestPromptCancelFooterElements(prompt, daemonLifecycleID); len(footer) != 0 {
		elements = append(elements, footer...)
	}
	return elements
}

func appendCurrentRequestQuestionElements(elements []map[string]any, prompt control.FeishuRequestView, daemonLifecycleID string) []map[string]any {
	question, index, ok := requestPromptCurrentQuestion(prompt)
	if !ok {
		return elements
	}
	if section, ok := requestPromptQuestionSection(index, len(prompt.Questions), question); ok {
		elements = appendCardTextSections(elements, []control.FeishuCardTextSection{section})
	}
	if prompt.Sealed || !question.DirectResponse || len(question.Options) == 0 {
		return elements
	}
	actions := make([]map[string]any, 0, len(question.Options))
	for _, option := range question.Options {
		button := requestUserInputOptionButton(prompt, question, option, daemonLifecycleID)
		if len(button) == 0 {
			continue
		}
		actions = append(actions, button)
	}
	return append(elements, requestPromptVerticalButtons(actions)...)
}

func requestPromptButton(prompt control.FeishuRequestView, option control.RequestPromptOption, daemonLifecycleID string) map[string]any {
	label := strings.TrimSpace(option.Label)
	if label == "" {
		return nil
	}
	buttonType := strings.TrimSpace(option.Style)
	if buttonType == "" {
		buttonType = "default"
	}
	return cardCallbackButtonElement(label, buttonType, stampActionValue(actionPayloadRequestRespond(prompt.RequestID, prompt.RequestType, option.OptionID, nil, prompt.RequestRevision), daemonLifecycleID), false, "")
}

func requestUserInputOptionButton(prompt control.FeishuRequestView, question control.RequestPromptQuestion, option control.RequestPromptQuestionOption, daemonLifecycleID string) map[string]any {
	label := strings.TrimSpace(option.Label)
	if label == "" {
		return nil
	}
	buttonType := "primary"
	selectedAnswer := strings.TrimSpace(question.DefaultValue)
	if selectedAnswer != "" && !strings.EqualFold(selectedAnswer, label) {
		buttonType = "default"
	}
	return cardCallbackButtonElement(label, buttonType, stampActionValue(actionPayloadRequestRespond(prompt.RequestID, prompt.RequestType, "", map[string]any{
		strings.TrimSpace(question.ID): []any{label},
	}, prompt.RequestRevision), daemonLifecycleID), false, "fill")
}

func requestPromptContainsOption(options []control.RequestPromptOption, optionID string) bool {
	for _, option := range options {
		if strings.TrimSpace(option.OptionID) == optionID {
			return true
		}
	}
	return false
}

func requestPromptQuestionSection(index, total int, question control.RequestPromptQuestion) (control.FeishuCardTextSection, bool) {
	lines := make([]string, 0, 12)
	title := firstNonEmpty(strings.TrimSpace(question.Header), strings.TrimSpace(question.Question))
	if title != "" {
		lines = append(lines, "标题："+title)
	}
	switch {
	case question.Answered:
		lines = append(lines, "状态：已回答")
	case question.Skipped:
		lines = append(lines, "状态：已跳过")
	default:
		lines = append(lines, "状态：待回答")
	}
	if question.Optional {
		lines = append(lines, "该题可跳过。")
	}
	if question.Header != "" && strings.TrimSpace(question.Question) != "" && strings.TrimSpace(question.Question) != strings.TrimSpace(question.Header) {
		lines = append(lines, "")
		lines = append(lines, "说明：")
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
	section := control.FeishuCardTextSection{
		Label: requestPromptQuestionLabel(index, total),
		Lines: lines,
	}.Normalized()
	if section.Label == "" && len(section.Lines) == 0 {
		return control.FeishuCardTextSection{}, false
	}
	return section, true
}

func requestPromptProgressMarkdown(prompt control.FeishuRequestView) string {
	if len(prompt.Questions) == 0 {
		return ""
	}
	completed := 0
	for _, question := range prompt.Questions {
		if question.Answered || question.Skipped {
			completed++
		}
	}
	return fmt.Sprintf("**回答进度** %d/%d · 当前第 %d 题", completed, len(prompt.Questions), normalizedRequestPromptCurrentQuestionIndex(prompt)+1)
}

func requestPromptQuestionLabel(index, total int) string {
	if total <= 0 {
		return fmt.Sprintf("问题 %d", index+1)
	}
	return fmt.Sprintf("问题 %d/%d", index+1, total)
}

func normalizedRequestPromptCurrentQuestionIndex(prompt control.FeishuRequestView) int {
	if len(prompt.Questions) == 0 {
		return 0
	}
	if prompt.CurrentQuestionIndex < 0 {
		return 0
	}
	if prompt.CurrentQuestionIndex >= len(prompt.Questions) {
		return len(prompt.Questions) - 1
	}
	return prompt.CurrentQuestionIndex
}

func requestPromptCurrentQuestion(prompt control.FeishuRequestView) (control.RequestPromptQuestion, int, bool) {
	if len(prompt.Questions) == 0 {
		return control.RequestPromptQuestion{}, 0, false
	}
	index := normalizedRequestPromptCurrentQuestionIndex(prompt)
	return prompt.Questions[index], index, true
}

func requestPromptNeedsForm(prompt control.FeishuRequestView) bool {
	question, _, ok := requestPromptCurrentQuestion(prompt)
	if !ok || prompt.Sealed {
		return false
	}
	return requestPromptQuestionNeedsFormInput(question)
}

func requestPromptQuestionNeedsFormInput(question control.RequestPromptQuestion) bool {
	return len(question.Options) == 0 || question.AllowOther || !question.DirectResponse
}

func requestPromptFormElement(prompt control.FeishuRequestView, daemonLifecycleID string) map[string]any {
	question, _, ok := requestPromptCurrentQuestion(prompt)
	if !ok || !requestPromptQuestionNeedsFormInput(question) {
		return nil
	}
	name := strings.TrimSpace(question.ID)
	if name == "" {
		return nil
	}
	input := map[string]any{
		"tag":  "input",
		"name": name,
	}
	label := firstNonEmpty(strings.TrimSpace(question.Header), strings.TrimSpace(question.Question), name)
	input["label"] = cardPlainText(label)
	input["label_position"] = "left"
	if placeholder := strings.TrimSpace(question.Placeholder); placeholder != "" {
		input["placeholder"] = cardPlainText(placeholder)
	}
	if value := strings.TrimSpace(question.DefaultValue); value != "" {
		input["default_value"] = value
	}
	submit := cardFormSubmitButtonElement("提交", stampActionValue(map[string]any{
		cardActionPayloadKeyKind:            cardActionKindSubmitRequestForm,
		cardActionPayloadKeyRequestID:       prompt.RequestID,
		cardActionPayloadKeyRequestType:     strings.TrimSpace(prompt.RequestType),
		cardActionPayloadKeyFieldName:       name,
		cardActionPayloadKeyRequestRevision: prompt.RequestRevision,
	}, daemonLifecycleID))
	if len(submit) == 0 {
		return nil
	}
	submit["name"] = "submit_request_" + name
	return map[string]any{
		"tag":  "form",
		"name": "request_form_" + strings.TrimSpace(prompt.RequestID) + "_" + name,
		"elements": []map[string]any{{
			"tag":                "column_set",
			"horizontal_spacing": "small",
			"columns": []map[string]any{
				{
					"tag":            "column",
					"width":          "weighted",
					"weight":         5,
					"vertical_align": "center",
					"elements":       []map[string]any{input},
				},
				{
					"tag":            "column",
					"width":          "auto",
					"vertical_align": "center",
					"elements":       []map[string]any{submit},
				},
			},
		}},
	}
}

func requestPromptRetryActionElement(prompt control.FeishuRequestView, daemonLifecycleID string) map[string]any {
	if prompt.Sealed || !requestPromptQuestionsComplete(prompt) {
		return nil
	}
	return cardCallbackButtonElement("重新提交", "primary", stampActionValue(actionPayloadRequestRespond(prompt.RequestID, prompt.RequestType, "", nil, prompt.RequestRevision), daemonLifecycleID), false, "fill")
}

func requestPromptSkipOptionalElement(prompt control.FeishuRequestView, daemonLifecycleID string) map[string]any {
	question, _, ok := requestPromptCurrentQuestion(prompt)
	if !ok || prompt.Sealed || !question.Optional || question.Answered || question.Skipped {
		return nil
	}
	return cardCallbackButtonElement("跳过", "default", stampActionValue(actionPayloadRequestControl(prompt.RequestID, prompt.RequestType, requestControlSkipOption, question.ID, prompt.RequestRevision), daemonLifecycleID), false, "fill")
}

func requestPromptStatusMarkdown(prompt control.FeishuRequestView) string {
	if strings.TrimSpace(prompt.StatusText) == "" {
		return ""
	}
	return strings.TrimSpace(prompt.StatusText)
}

func requestPromptCancelFooterElements(prompt control.FeishuRequestView, daemonLifecycleID string) []map[string]any {
	if prompt.Sealed {
		return nil
	}
	requestType := normalizeRequestPromptType(prompt.RequestType)
	if requestType == "request_user_input" {
		return []map[string]any{
			cardDividerElement(),
			cardCallbackButtonElement("取消", "default", stampActionValue(actionPayloadRequestControl(prompt.RequestID, prompt.RequestType, requestControlCancelTurn, "", prompt.RequestRevision), daemonLifecycleID), false, "fill"),
		}
	}
	return nil
}

func requestPromptVerticalButtons(buttons []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(buttons))
	for _, button := range buttons {
		if len(button) == 0 {
			continue
		}
		cloned := cloneCardMap(button)
		if strings.TrimSpace(cardStringValue(cloned["width"])) == "" {
			cloned["width"] = "fill"
		}
		out = append(out, cloned)
	}
	return out
}

func requestPromptQuestionsComplete(prompt control.FeishuRequestView) bool {
	if len(prompt.Questions) == 0 {
		return false
	}
	for _, question := range prompt.Questions {
		if !question.Answered && !question.Skipped {
			return false
		}
	}
	return true
}

func stampActionValue(value map[string]any, daemonLifecycleID string) map[string]any {
	return actionPayloadWithLifecycle(value, daemonLifecycleID)
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
