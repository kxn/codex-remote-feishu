package control

import "strings"

// CommandCatalogTextSection builds a normalized Feishu card text section.
// It consolidates the commandCatalogTextSection copies previously living in
// app/daemon and app/cronruntime.
func CommandCatalogTextSection(label string, lines ...string) FeishuCardTextSection {
	section := FeishuCardTextSection{
		Label: strings.TrimSpace(label),
		Lines: append([]string(nil), lines...),
	}
	return section.Normalized()
}

// CommandCatalogSummarySections builds a single summary section, or nil when
// the section would be empty. It consolidates the commandCatalogSummarySections
// copies previously living in app/daemon and app/cronruntime.
func CommandCatalogSummarySections(lines ...string) []FeishuCardTextSection {
	section := CommandCatalogTextSection("", lines...)
	if section.Label == "" && len(section.Lines) == 0 {
		return nil
	}
	return []FeishuCardTextSection{section}
}

// OpenURLButton builds a command catalog button that opens a URL. It
// consolidates the openURLButton copies previously living in app/daemon and
// app/cronruntime.
func OpenURLButton(label, openURL, style string, disabled bool) CommandCatalogButton {
	return CommandCatalogButton{
		Label:    strings.TrimSpace(label),
		Kind:     CommandCatalogButtonOpenURL,
		OpenURL:  strings.TrimSpace(openURL),
		Style:    strings.TrimSpace(style),
		Disabled: disabled,
	}
}
