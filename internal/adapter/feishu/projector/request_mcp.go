package projector

import (
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

const requestControlCancelRequest = "cancel_request"

func permissionsRequestPromptElements(prompt control.FeishuRequestView, daemonLifecycleID string) []map[string]any {
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

func mcpElicitationPromptElements(prompt control.FeishuRequestView, daemonLifecycleID string) []map[string]any {
	if len(prompt.Questions) == 0 {
		return mcpElicitationChoiceElements(prompt, daemonLifecycleID)
	}
	elements := make([]map[string]any, 0, 12)
	if progress := mcpElicitationProgressMarkdown(prompt); progress != "" {
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
	if footer := mcpElicitationCancelFooterElements(prompt, daemonLifecycleID); len(footer) != 0 {
		elements = append(elements, footer...)
	}
	return elements
}

func mcpElicitationChoiceElements(prompt control.FeishuRequestView, daemonLifecycleID string) []map[string]any {
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

func mcpElicitationCancelFooterElements(prompt control.FeishuRequestView, daemonLifecycleID string) []map[string]any {
	if prompt.Sealed {
		return nil
	}
	return []map[string]any{
		cardDividerElement(),
		cardCallbackButtonElement("取消", "default", stampActionValue(actionPayloadRequestControl(prompt.RequestID, prompt.RequestType, requestControlCancelRequest, "", prompt.RequestRevision), daemonLifecycleID), false, "fill"),
	}
}

func mcpElicitationProgressMarkdown(prompt control.FeishuRequestView) string {
	if len(prompt.Questions) == 0 {
		return ""
	}
	completed := 0
	for _, question := range prompt.Questions {
		if question.Answered || question.Skipped {
			completed++
		}
	}
	return fmt.Sprintf("**填写进度** %d/%d · 当前第 %d 题", completed, len(prompt.Questions), normalizedRequestPromptCurrentQuestionIndex(prompt)+1)
}
