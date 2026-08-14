package cronruntime

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

func intervalMinutesForLabel(label string) (int, bool) {
	label = strings.TrimSpace(label)
	for _, item := range IntervalChoices {
		if item.Label == label {
			return item.Minutes, true
		}
	}
	return 0, false
}

func callbackActionButton(label, commandID string, actionKind control.ActionKind, actionArg, style string, disabled bool) control.CommandCatalogButton {
	return control.CommandCatalogButton{
		Label:         strings.TrimSpace(label),
		Kind:          control.CommandCatalogButtonCallbackAction,
		CommandText:   control.BuildFeishuActionText(actionKind, actionArg),
		CommandID:     strings.TrimSpace(commandID),
		CallbackValue: frontstagecontract.ActionPayloadPageLocalAction(string(actionKind), actionArg),
		Style:         strings.TrimSpace(style),
		Disabled:      disabled,
	}
}
