package codex

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func extractRequestPayload(message map[string]any) map[string]any {
	request := lookupMap(message, "params", "request")
	if len(request) > 0 {
		return request
	}
	request = lookupMap(message, "params", "serverRequest")
	if len(request) > 0 {
		return request
	}
	return map[string]any{}
}

func extractRequestID(message map[string]any, request map[string]any) string {
	return firstNonEmptyString(
		lookupStringFromAny(request["id"]),
		lookupString(message, "params", "requestId"),
		lookupString(message, "params", "id"),
	)
}

func extractRequestThreadID(message map[string]any, request map[string]any) string {
	return firstNonEmptyString(
		lookupString(message, "params", "thread", "id"),
		lookupString(message, "params", "threadId"),
		lookupString(request, "thread", "id"),
		lookupStringFromAny(request["threadId"]),
	)
}

func extractRequestTurnID(message map[string]any, request map[string]any) string {
	return firstNonEmptyString(
		lookupString(message, "params", "turn", "id"),
		lookupString(message, "params", "turnId"),
		lookupString(request, "turn", "id"),
		lookupStringFromAny(request["turnId"]),
	)
}

func extractRequestType(request, params map[string]any) string {
	switch raw := strings.ToLower(strings.TrimSpace(extractRawRequestType(request, params))); {
	case raw == "", raw == "approval", raw == "confirm", raw == "confirmation":
		return "approval"
	case strings.HasPrefix(raw, "approval"):
		return "approval"
	case strings.HasPrefix(raw, "confirm"):
		return "approval"
	default:
		return raw
	}
}

func extractRawRequestType(request, params map[string]any) string {
	return strings.TrimSpace(firstNonEmptyString(
		lookupStringFromAny(request["type"]),
		lookupStringFromAny(request["requestType"]),
		lookupStringFromAny(request["kind"]),
		lookupStringFromAny(params["type"]),
		lookupStringFromAny(params["requestType"]),
		lookupStringFromAny(params["kind"]),
	))
}

func extractRequestMetadata(requestType string, request, params map[string]any) map[string]any {
	metadata := map[string]any{}
	if requestType != "" {
		metadata["requestType"] = requestType
	}
	if rawType := extractRawRequestType(request, params); rawType != "" {
		metadata["requestKind"] = strings.ToLower(strings.TrimSpace(rawType))
	}
	title := firstNonEmptyString(
		lookupStringFromAny(request["title"]),
		lookupStringFromAny(request["name"]),
		lookupStringFromAny(params["title"]),
	)
	if title == "" {
		switch requestType {
		case "", "approval":
			title = "需要确认"
		default:
			title = "需要处理请求"
		}
	}
	if title != "" {
		metadata["title"] = title
	}
	body := firstNonEmptyString(
		lookupStringFromAny(request["message"]),
		lookupStringFromAny(request["description"]),
		lookupStringFromAny(request["body"]),
		lookupStringFromAny(request["prompt"]),
		lookupStringFromAny(request["reason"]),
		lookupStringFromAny(params["message"]),
		lookupStringFromAny(params["description"]),
		lookupStringFromAny(params["body"]),
	)
	command := extractRequestCommand(request, params)
	if command != "" {
		if body != "" {
			body += "\n\n"
		}
		body += "```text\n" + command + "\n```"
	}
	if body != "" {
		metadata["body"] = body
	}
	acceptLabel := firstNonEmptyString(
		lookupStringFromAny(request["acceptLabel"]),
		lookupStringFromAny(request["approveLabel"]),
		lookupStringFromAny(request["allowLabel"]),
		lookupStringFromAny(request["confirmLabel"]),
		lookupStringFromAny(params["acceptLabel"]),
	)
	if acceptLabel != "" {
		metadata["acceptLabel"] = acceptLabel
	}
	declineLabel := firstNonEmptyString(
		lookupStringFromAny(request["declineLabel"]),
		lookupStringFromAny(request["denyLabel"]),
		lookupStringFromAny(request["rejectLabel"]),
		lookupStringFromAny(params["declineLabel"]),
	)
	if declineLabel != "" {
		metadata["declineLabel"] = declineLabel
	}
	if options := extractRequestOptions(request, params); len(options) != 0 {
		metadata["options"] = options
	}
	return metadata
}

func extractRequestUserInputMetadata(message map[string]any) map[string]any {
	params := lookupMap(message, "params")
	metadata := map[string]any{
		"requestType": "request_user_input",
	}
	if itemID := firstNonEmptyString(
		lookupStringFromAny(params["itemId"]),
		lookupString(params, "item", "id"),
	); itemID != "" {
		metadata["itemId"] = itemID
	}
	title := firstNonEmptyString(
		lookupStringFromAny(params["title"]),
		lookupStringFromAny(params["header"]),
	)
	if title == "" {
		title = "需要补充输入"
	}
	metadata["title"] = title
	if body := firstNonEmptyString(
		lookupStringFromAny(params["message"]),
		lookupStringFromAny(params["body"]),
		lookupStringFromAny(params["description"]),
	); body != "" {
		metadata["body"] = body
	}
	if questions := extractRequestUserInputQuestions(params); len(questions) != 0 {
		metadata["questions"] = questions
	}
	return metadata
}

func extractResolvedRequestMetadata(requestType string, request, params map[string]any) map[string]any {
	metadata := map[string]any{}
	if requestType != "" {
		metadata["requestType"] = requestType
	}
	decision := firstNonEmptyString(
		lookupString(params, "result", "decision"),
		lookupString(params, "response", "decision"),
		lookupStringFromAny(params["decision"]),
		lookupString(request, "result", "decision"),
		lookupString(request, "response", "decision"),
		lookupStringFromAny(request["decision"]),
	)
	if decision != "" {
		metadata["decision"] = decision
	}
	return metadata
}

func extractRequestCommand(request, params map[string]any) string {
	command := firstNonEmptyString(
		lookupStringFromAny(request["command"]),
		lookupString(request, "command", "command"),
		lookupString(request, "command", "text"),
		lookupStringFromAny(params["command"]),
		lookupString(params, "command", "command"),
		lookupString(params, "command", "text"),
	)
	return strings.TrimSpace(command)
}

func extractRequestUserInputQuestions(params map[string]any) []map[string]any {
	source := firstNonNil(params["questions"], params["items"])
	if source == nil {
		return nil
	}
	var rawQuestions []any
	switch typed := source.(type) {
	case []any:
		rawQuestions = typed
	case []map[string]any:
		rawQuestions = make([]any, 0, len(typed))
		for _, item := range typed {
			rawQuestions = append(rawQuestions, item)
		}
	default:
		return nil
	}
	questions := make([]map[string]any, 0, len(rawQuestions))
	for _, raw := range rawQuestions {
		record, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		questionID := firstNonEmptyString(
			lookupStringFromAny(record["id"]),
			lookupStringFromAny(record["questionId"]),
		)
		if questionID == "" {
			continue
		}
		question := map[string]any{"id": questionID}
		if header := firstNonEmptyString(
			lookupStringFromAny(record["header"]),
			lookupStringFromAny(record["title"]),
		); header != "" {
			question["header"] = header
		}
		if prompt := firstNonEmptyString(
			lookupStringFromAny(record["question"]),
			lookupStringFromAny(record["label"]),
			lookupStringFromAny(record["prompt"]),
		); prompt != "" {
			question["question"] = prompt
		}
		if lookupBoolFromAny(record["isOther"]) {
			question["isOther"] = true
		}
		if lookupBoolFromAny(record["isSecret"]) {
			question["isSecret"] = true
		}
		if placeholder := firstNonEmptyString(
			lookupStringFromAny(record["placeholder"]),
			lookupStringFromAny(record["inputPlaceholder"]),
		); placeholder != "" {
			question["placeholder"] = placeholder
		}
		if defaultValue := firstNonEmptyString(lookupStringFromAny(record["defaultValue"])); defaultValue != "" {
			question["defaultValue"] = defaultValue
		}
		if options := extractRequestUserInputQuestionOptions(record); len(options) != 0 {
			question["options"] = options
		}
		questions = append(questions, question)
	}
	return questions
}

func extractRequestUserInputQuestionOptions(question map[string]any) []map[string]any {
	source := firstNonNil(question["options"], question["choices"])
	if source == nil {
		return nil
	}
	var rawOptions []any
	switch typed := source.(type) {
	case []any:
		rawOptions = typed
	case []map[string]any:
		rawOptions = make([]any, 0, len(typed))
		for _, item := range typed {
			rawOptions = append(rawOptions, item)
		}
	default:
		return nil
	}
	options := make([]map[string]any, 0, len(rawOptions))
	for _, raw := range rawOptions {
		record, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		label := firstNonEmptyString(
			lookupStringFromAny(record["label"]),
			lookupStringFromAny(record["title"]),
			lookupStringFromAny(record["text"]),
		)
		if label == "" {
			continue
		}
		option := map[string]any{"label": label}
		if description := firstNonEmptyString(
			lookupStringFromAny(record["description"]),
			lookupStringFromAny(record["subtitle"]),
		); description != "" {
			option["description"] = description
		}
		options = append(options, option)
	}
	return options
}

func extractRequestOptions(request, params map[string]any) []map[string]any {
	source := firstNonNil(
		request["options"],
		request["choices"],
		params["options"],
		params["choices"],
	)
	if source == nil {
		return nil
	}
	var rawOptions []any
	switch typed := source.(type) {
	case []any:
		rawOptions = typed
	case []map[string]any:
		rawOptions = make([]any, 0, len(typed))
		for _, item := range typed {
			rawOptions = append(rawOptions, item)
		}
	default:
		return nil
	}
	if len(rawOptions) == 0 {
		return nil
	}
	options := make([]map[string]any, 0, len(rawOptions))
	for _, raw := range rawOptions {
		record, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		optionID := control.NormalizeRequestOptionID(firstNonEmptyString(
			lookupStringFromAny(record["id"]),
			lookupStringFromAny(record["optionId"]),
			lookupStringFromAny(record["decision"]),
			lookupStringFromAny(record["value"]),
			lookupStringFromAny(record["action"]),
		))
		if optionID == "" {
			continue
		}
		option := map[string]any{"id": optionID}
		label := firstNonEmptyString(
			lookupStringFromAny(record["label"]),
			lookupStringFromAny(record["title"]),
			lookupStringFromAny(record["text"]),
			lookupStringFromAny(record["name"]),
		)
		if label != "" {
			option["label"] = label
		}
		style := firstNonEmptyString(
			lookupStringFromAny(record["style"]),
			lookupStringFromAny(record["appearance"]),
			lookupStringFromAny(record["variant"]),
		)
		if style != "" {
			option["style"] = style
		}
		options = append(options, option)
	}
	return options
}
