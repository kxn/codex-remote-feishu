package selectflow

import (
	"fmt"
	"strings"

	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"

	frontstagecontract "github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

const DefaultPaginationHint = "超出卡片大小，如未找到请翻页。"

type PaginatedSelectFlowDefinition struct {
	FieldName       string
	PayloadValueKey string
	PaginationHint  string
}

var (
	PathPickerDirectoryFlow = PaginatedSelectFlowDefinition{
		FieldName:       frontstagecontract.CardPathPickerDirectorySelectFieldName,
		PayloadValueKey: frontstagecontract.CardActionPayloadKeyEntryName,
		PaginationHint:  DefaultPaginationHint,
	}
	PathPickerFileFlow = PaginatedSelectFlowDefinition{
		FieldName:       frontstagecontract.CardPathPickerFileSelectFieldName,
		PayloadValueKey: frontstagecontract.CardActionPayloadKeyEntryName,
		PaginationHint:  DefaultPaginationHint,
	}
	TargetPickerWorkspaceFlow = PaginatedSelectFlowDefinition{
		FieldName:      frontstagecontract.CardTargetPickerWorkspaceFieldName,
		PaginationHint: DefaultPaginationHint,
	}
	TargetPickerSessionFlow = PaginatedSelectFlowDefinition{
		FieldName:      frontstagecontract.CardTargetPickerSessionFieldName,
		PaginationHint: DefaultPaginationHint,
	}
	ThreadSelectionFlow = PaginatedSelectFlowDefinition{
		FieldName:       frontstagecontract.CardSelectionThreadFieldName,
		PayloadValueKey: frontstagecontract.CardActionPayloadKeyThreadID,
		PaginationHint:  DefaultPaginationHint,
	}
)

func (d PaginatedSelectFlowDefinition) ResolvedFieldName(payload map[string]any) string {
	if fieldName := payloadStringValue(payload, frontstagecontract.CardActionPayloadKeyFieldName); fieldName != "" {
		return fieldName
	}
	return strings.TrimSpace(d.FieldName)
}

func (d PaginatedSelectFlowDefinition) RecoverSelectedValue(payload map[string]any, action *larkcallback.CallBackAction) string {
	return RecoverCallbackValue(payload, action, d.FieldName, d.PayloadValueKey)
}

func RecoverCallbackValue(payload map[string]any, action *larkcallback.CallBackAction, defaultFieldName, payloadValueKey string) string {
	if value := payloadStringValue(payload, payloadValueKey); value != "" {
		return value
	}
	if value := SelectedOptionValue(action); value != "" {
		return value
	}
	fieldName := strings.TrimSpace(defaultFieldName)
	if payloadFieldName := payloadStringValue(payload, frontstagecontract.CardActionPayloadKeyFieldName); payloadFieldName != "" {
		fieldName = payloadFieldName
	}
	if fieldName == "" || action == nil {
		return ""
	}
	return FormValue(action.FormValue, fieldName)
}

func SelectedOptionValue(action *larkcallback.CallBackAction) string {
	if action == nil {
		return ""
	}
	if option := strings.TrimSpace(action.Option); option != "" {
		return option
	}
	for _, option := range action.Options {
		if option = strings.TrimSpace(option); option != "" {
			return option
		}
	}
	return ""
}

func FormValue(values map[string]interface{}, key string) string {
	if len(values) == 0 || strings.TrimSpace(key) == "" {
		return ""
	}
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}
	switch typed := raw.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []string:
		for _, item := range typed {
			if item = strings.TrimSpace(item); item != "" {
				return item
			}
		}
	case []interface{}:
		for _, item := range typed {
			if item == nil {
				continue
			}
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				return text
			}
		}
	default:
		return strings.TrimSpace(fmt.Sprint(raw))
	}
	return ""
}

func payloadStringValue(values map[string]any, key string) string {
	if len(values) == 0 || strings.TrimSpace(key) == "" {
		return ""
	}
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(raw))
}
