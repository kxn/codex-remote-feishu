package gateway

import (
	"strings"

	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func parseTargetPickerCardAction(
	env RoutingEnv,
	value map[string]any,
	event *larkcallback.CardActionTriggerEvent,
	meta *control.ActionInboundMeta,
	surfaceSessionID, chatID, operatorID, messageID string,
) (control.Action, bool) {
	switch actionPayloadKind(value) {
	case cardActionKindTargetPickerSelectMode:
		pickerID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyPickerID))
		targetValue := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyTargetValue))
		if pickerID == "" || targetValue == "" {
			return control.Action{}, false
		}
		return control.Action{
			Kind:              control.ActionTargetPickerSelectMode,
			GatewayID:         strings.TrimSpace(env.GatewayID),
			SurfaceSessionID:  surfaceSessionID,
			ChatID:            chatID,
			ActorUserID:       operatorID,
			MessageID:         messageID,
			PickerID:          pickerID,
			TargetPickerValue: targetValue,
			Inbound:           meta,
		}, true
	case cardActionKindTargetPickerSelectSource:
		pickerID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyPickerID))
		targetValue := selectStaticFormValue(event.Event.Action.FormValue, cardTargetPickerSourceFieldName)
		if targetValue == "" {
			targetValue = pathPickerSelectedEntryName(event, cardTargetPickerSourceFieldName)
		}
		if targetValue == "" {
			targetValue = strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyTargetValue))
		}
		if pickerID == "" || targetValue == "" {
			return control.Action{}, false
		}
		return control.Action{
			Kind:              control.ActionTargetPickerSelectSource,
			GatewayID:         strings.TrimSpace(env.GatewayID),
			SurfaceSessionID:  surfaceSessionID,
			ChatID:            chatID,
			ActorUserID:       operatorID,
			MessageID:         messageID,
			PickerID:          pickerID,
			TargetPickerValue: targetValue,
			Inbound:           meta,
		}, true
	case cardActionKindTargetPickerSelectWorkspace:
		pickerID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyPickerID))
		workspaceKey := selectStaticFormValue(event.Event.Action.FormValue, cardTargetPickerWorkspaceFieldName)
		if workspaceKey == "" {
			workspaceKey = pathPickerSelectedEntryName(event, cardTargetPickerWorkspaceFieldName)
		}
		if pickerID == "" || workspaceKey == "" {
			return control.Action{}, false
		}
		return control.Action{
			Kind:             control.ActionTargetPickerSelectWorkspace,
			GatewayID:        strings.TrimSpace(env.GatewayID),
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			PickerID:         pickerID,
			WorkspaceKey:     workspaceKey,
			Inbound:          meta,
		}, true
	case cardActionKindTargetPickerSelectSession:
		pickerID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyPickerID))
		targetValue := selectStaticFormValue(event.Event.Action.FormValue, cardTargetPickerSessionFieldName)
		if targetValue == "" {
			targetValue = pathPickerSelectedEntryName(event, cardTargetPickerSessionFieldName)
		}
		if pickerID == "" || targetValue == "" {
			return control.Action{}, false
		}
		return control.Action{
			Kind:              control.ActionTargetPickerSelectSession,
			GatewayID:         strings.TrimSpace(env.GatewayID),
			SurfaceSessionID:  surfaceSessionID,
			ChatID:            chatID,
			ActorUserID:       operatorID,
			MessageID:         messageID,
			PickerID:          pickerID,
			TargetPickerValue: targetValue,
			Inbound:           meta,
		}, true
	case cardActionKindTargetPickerPage:
		pickerID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyPickerID))
		fieldName := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyFieldName))
		if pickerID == "" || fieldName == "" {
			return control.Action{}, false
		}
		return control.Action{
			Kind:             control.ActionTargetPickerPage,
			GatewayID:        strings.TrimSpace(env.GatewayID),
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			PickerID:         pickerID,
			FieldName:        fieldName,
			Cursor:           intMapValue(value, cardActionPayloadKeyCursor),
			Inbound:          meta,
		}, true
	case cardActionKindTargetPickerOpenPathPicker:
		pickerID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyPickerID))
		targetValue := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyTargetValue))
		if pickerID == "" || targetValue == "" {
			return control.Action{}, false
		}
		return control.Action{
			Kind:              control.ActionTargetPickerOpenPathPicker,
			GatewayID:         strings.TrimSpace(env.GatewayID),
			SurfaceSessionID:  surfaceSessionID,
			ChatID:            chatID,
			ActorUserID:       operatorID,
			MessageID:         messageID,
			PickerID:          pickerID,
			TargetPickerValue: targetValue,
			RequestAnswers:    targetPickerDraftAnswersFromFormValue(event.Event.Action.FormValue),
			Inbound:           meta,
		}, true
	case cardActionKindTargetPickerBack:
		pickerID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyPickerID))
		if pickerID == "" {
			return control.Action{}, false
		}
		return control.Action{
			Kind:             control.ActionTargetPickerBack,
			GatewayID:        strings.TrimSpace(env.GatewayID),
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			PickerID:         pickerID,
			RequestAnswers:   targetPickerDraftAnswersFromFormValue(event.Event.Action.FormValue),
			Inbound:          meta,
		}, true
	case cardActionKindTargetPickerCancel:
		pickerID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyPickerID))
		if pickerID == "" {
			return control.Action{}, false
		}
		return control.Action{
			Kind:             control.ActionTargetPickerCancel,
			GatewayID:        strings.TrimSpace(env.GatewayID),
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			PickerID:         pickerID,
			RequestAnswers:   targetPickerDraftAnswersFromFormValue(event.Event.Action.FormValue),
			Inbound:          meta,
		}, true
	case cardActionKindTargetPickerConfirm:
		pickerID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyPickerID))
		if pickerID == "" {
			return control.Action{}, false
		}
		return control.Action{
			Kind:              control.ActionTargetPickerConfirm,
			GatewayID:         strings.TrimSpace(env.GatewayID),
			SurfaceSessionID:  surfaceSessionID,
			ChatID:            chatID,
			ActorUserID:       operatorID,
			MessageID:         messageID,
			PickerID:          pickerID,
			WorkspaceKey:      selectStaticFormValue(event.Event.Action.FormValue, cardTargetPickerWorkspaceFieldName),
			TargetPickerValue: selectStaticFormValue(event.Event.Action.FormValue, cardTargetPickerSessionFieldName),
			RequestAnswers:    targetPickerDraftAnswersFromFormValue(event.Event.Action.FormValue),
			Inbound:           meta,
		}, true
	default:
		return control.Action{}, false
	}
}
