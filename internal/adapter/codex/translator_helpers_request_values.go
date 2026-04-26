package codex

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func extractRequestMapList(source any) []map[string]any {
	if source == nil {
		return nil
	}
	var rawItems []any
	switch typed := source.(type) {
	case []any:
		rawItems = typed
	case []map[string]any:
		rawItems = make([]any, 0, len(typed))
		for _, item := range typed {
			rawItems = append(rawItems, item)
		}
	default:
		return nil
	}
	items := make([]map[string]any, 0, len(rawItems))
	for _, raw := range rawItems {
		record, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		items = append(items, cloneMap(record))
	}
	return items
}

func requestOptionsFromMaps(source []map[string]any) []agentproto.RequestOption {
	if len(source) == 0 {
		return nil
	}
	options := make([]agentproto.RequestOption, 0, len(source))
	for _, option := range source {
		optionID := strings.TrimSpace(firstNonEmptyString(
			lookupStringFromAny(option["id"]),
			lookupStringFromAny(option["optionId"]),
		))
		if optionID == "" {
			continue
		}
		options = append(options, agentproto.RequestOption{
			OptionID: optionID,
			Label:    strings.TrimSpace(lookupStringFromAny(option["label"])),
			Style:    strings.TrimSpace(lookupStringFromAny(option["style"])),
		})
	}
	return options
}

func requestOptionsToMaps(source []agentproto.RequestOption) []map[string]any {
	if len(source) == 0 {
		return nil
	}
	options := make([]map[string]any, 0, len(source))
	for _, option := range source {
		record := map[string]any{"id": strings.TrimSpace(option.OptionID)}
		if label := strings.TrimSpace(option.Label); label != "" {
			record["label"] = label
		}
		if style := strings.TrimSpace(option.Style); style != "" {
			record["style"] = style
		}
		options = append(options, record)
	}
	return options
}

func requestQuestionsFromMaps(source []map[string]any) []agentproto.RequestQuestion {
	if len(source) == 0 {
		return nil
	}
	questions := make([]agentproto.RequestQuestion, 0, len(source))
	for _, question := range source {
		questionID := strings.TrimSpace(firstNonEmptyString(
			lookupStringFromAny(question["id"]),
			lookupStringFromAny(question["questionId"]),
		))
		if questionID == "" {
			continue
		}
		questions = append(questions, agentproto.RequestQuestion{
			ID:             questionID,
			Header:         strings.TrimSpace(lookupStringFromAny(question["header"])),
			Question:       strings.TrimSpace(lookupStringFromAny(question["question"])),
			AllowOther:     lookupBoolFromAny(question["isOther"]),
			Secret:         lookupBoolFromAny(question["isSecret"]),
			Options:        requestQuestionOptionsFromMaps(extractRequestUserInputQuestionOptions(question)),
			Placeholder:    strings.TrimSpace(lookupStringFromAny(question["placeholder"])),
			DefaultValue:   strings.TrimSpace(lookupStringFromAny(question["defaultValue"])),
			DirectResponse: lookupBoolFromAny(question["directResponse"]),
		})
	}
	return questions
}

func requestQuestionsToMaps(source []agentproto.RequestQuestion) []map[string]any {
	if len(source) == 0 {
		return nil
	}
	questions := make([]map[string]any, 0, len(source))
	for _, question := range source {
		record := map[string]any{"id": strings.TrimSpace(question.ID)}
		if value := strings.TrimSpace(question.Header); value != "" {
			record["header"] = value
		}
		if value := strings.TrimSpace(question.Question); value != "" {
			record["question"] = value
		}
		if question.AllowOther {
			record["isOther"] = true
		}
		if question.Secret {
			record["isSecret"] = true
		}
		if value := strings.TrimSpace(question.Placeholder); value != "" {
			record["placeholder"] = value
		}
		if value := strings.TrimSpace(question.DefaultValue); value != "" {
			record["defaultValue"] = value
		}
		if question.DirectResponse {
			record["directResponse"] = true
		}
		if options := requestQuestionOptionsToMaps(question.Options); len(options) != 0 {
			record["options"] = options
		}
		questions = append(questions, record)
	}
	return questions
}

func requestQuestionOptionsFromMaps(source []map[string]any) []agentproto.RequestQuestionOption {
	if len(source) == 0 {
		return nil
	}
	options := make([]agentproto.RequestQuestionOption, 0, len(source))
	for _, option := range source {
		label := strings.TrimSpace(lookupStringFromAny(option["label"]))
		if label == "" {
			continue
		}
		options = append(options, agentproto.RequestQuestionOption{
			Label:       label,
			Description: strings.TrimSpace(lookupStringFromAny(option["description"])),
		})
	}
	return options
}

func requestQuestionOptionsToMaps(source []agentproto.RequestQuestionOption) []map[string]any {
	if len(source) == 0 {
		return nil
	}
	options := make([]map[string]any, 0, len(source))
	for _, option := range source {
		record := map[string]any{"label": strings.TrimSpace(option.Label)}
		if description := strings.TrimSpace(option.Description); description != "" {
			record["description"] = description
		}
		options = append(options, record)
	}
	return options
}

func extractRequestUserInputQuestions(request, params map[string]any) []map[string]any {
	source := firstNonNil(
		request["questions"],
		request["items"],
		params["questions"],
		params["items"],
	)
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
		if lookupBoolFromAny(record["directResponse"]) {
			question["directResponse"] = true
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
		request["availableDecisions"],
		params["availableDecisions"],
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
	case []string:
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
		record := map[string]any{}
		switch typed := raw.(type) {
		case map[string]any:
			record = typed
		case string:
			record = map[string]any{"id": typed}
		default:
			continue
		}
		optionID := control.NormalizeRequestOptionID(firstNonEmptyString(
			lookupStringFromAny(record["id"]),
			lookupStringFromAny(record["optionId"]),
			lookupStringFromAny(record["decision"]),
			lookupStringFromAny(record["value"]),
			lookupStringFromAny(record["action"]),
		))
		if optionID == "" && len(record) == 1 {
			for key := range record {
				optionID = strings.TrimSpace(key)
			}
		}
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
