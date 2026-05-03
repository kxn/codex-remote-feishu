package control

import (
	"strings"

	frontstagecontract "github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

func FeishuLocalCardActionPayload(actionKind ActionKind, actionArg string) map[string]any {
	return frontstagecontract.ActionPayloadPageLocalAction(string(actionKind), strings.TrimSpace(actionArg))
}

func FeishuLocalCardCommandPayload(commandText string) (map[string]any, bool) {
	action, ok := ParseFeishuTextActionWithoutCatalog(commandText)
	if !ok {
		return nil, false
	}
	return FeishuLocalCardActionPayload(action.Kind, FeishuActionArgumentText(action.Text)), true
}

func FeishuLocalPageCommandPayload(commandText string) (map[string]any, bool) {
	return FeishuLocalCardCommandPayload(commandText)
}

func FeishuLocalPageCommandButton(label, commandText, style string, disabled bool) CommandCatalogButton {
	button := CommandCatalogButton{
		Label:       strings.TrimSpace(label),
		CommandText: strings.TrimSpace(commandText),
		Style:       strings.TrimSpace(style),
		Disabled:    disabled,
	}
	if payload, ok := FeishuLocalCardCommandPayload(button.CommandText); ok {
		button.Kind = CommandCatalogButtonCallbackAction
		button.CallbackValue = payload
		return button
	}
	button.Kind = CommandCatalogButtonAction
	return button
}
