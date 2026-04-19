package daemon

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func commandCatalogSummarySections(lines ...string) []control.FeishuCardTextSection {
	section := commandCatalogTextSection("", lines...)
	if section.Label == "" && len(section.Lines) == 0 {
		return nil
	}
	return []control.FeishuCardTextSection{section}
}

func commandCatalogTextSection(label string, lines ...string) control.FeishuCardTextSection {
	section := control.FeishuCardTextSection{
		Label: strings.TrimSpace(label),
		Lines: append([]string(nil), lines...),
	}
	return section.Normalized()
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
