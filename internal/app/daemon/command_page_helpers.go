package daemon

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func commandPageEvent(surfaceID string, view control.FeishuCommandPageView) control.UIEvent {
	return control.UIEvent{
		Kind:             control.UIEventFeishuDirectCommandCatalog,
		SurfaceSessionID: strings.TrimSpace(surfaceID),
		FeishuCommandView: &control.FeishuCommandView{
			Page: &view,
		},
	}
}

func commandPageEvents(surfaceID string, view control.FeishuCommandPageView) []control.UIEvent {
	return []control.UIEvent{commandPageEvent(surfaceID, view)}
}

func commandPageViewFromCatalog(commandID string, catalog *control.FeishuDirectCommandCatalog, breadcrumbs []control.CommandCatalogBreadcrumb, relatedButtons []control.CommandCatalogButton) control.FeishuCommandPageView {
	if catalog == nil {
		return control.FeishuCommandPageView{CommandID: strings.TrimSpace(commandID)}
	}
	return control.FeishuCommandPageView{
		CommandID:       strings.TrimSpace(commandID),
		Title:           strings.TrimSpace(catalog.Title),
		MessageID:       strings.TrimSpace(catalog.MessageID),
		TrackingKey:     strings.TrimSpace(catalog.TrackingKey),
		ThemeKey:        strings.TrimSpace(catalog.ThemeKey),
		Patchable:       catalog.Patchable,
		Breadcrumbs:     append([]control.CommandCatalogBreadcrumb(nil), breadcrumbs...),
		SummarySections: append([]control.FeishuCardTextSection(nil), catalog.SummarySections...),
		Interactive:     catalog.Interactive,
		DisplayStyle:    catalog.DisplayStyle,
		Sections:        append([]control.CommandCatalogSection(nil), catalog.Sections...),
		RelatedButtons:  append([]control.CommandCatalogButton(nil), relatedButtons...),
	}
}

func commandArgumentText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	idx := strings.IndexAny(text, " \t")
	if idx < 0 || idx+1 >= len(text) {
		return ""
	}
	return strings.TrimSpace(text[idx+1:])
}
