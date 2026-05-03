package cronruntime

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func intervalMinutesForLabel(label string) (int, bool) {
	label = strings.TrimSpace(label)
	for _, item := range IntervalChoices {
		if item.Label == label {
			return item.Minutes, true
		}
	}
	return 0, false
}

func commandCatalogSummarySections(lines ...string) []control.FeishuCardTextSection {
	section := commandCatalogTextSection("", lines...)
	if section.Label == "" && len(section.Lines) == 0 {
		return nil
	}
	return []control.FeishuCardTextSection{section}
}

func commandCatalogTextSection(label string, lines ...string) control.FeishuCardTextSection {
	return control.FeishuCardTextSection{
		Label: strings.TrimSpace(label),
		Lines: append([]string(nil), lines...),
	}.Normalized()
}

func runCommandButton(label, commandText, style string, disabled bool) control.CommandCatalogButton {
	return control.FeishuLocalPageCommandButton(label, commandText, style, disabled)
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

func openURLButton(label, openURL, style string, disabled bool) control.CommandCatalogButton {
	return control.CommandCatalogButton{
		Label:    strings.TrimSpace(label),
		Kind:     control.CommandCatalogButtonOpenURL,
		OpenURL:  strings.TrimSpace(openURL),
		Style:    strings.TrimSpace(style),
		Disabled: disabled,
	}
}
