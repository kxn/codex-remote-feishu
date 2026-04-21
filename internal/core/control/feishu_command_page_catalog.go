package control

import "strings"

func BuildFeishuCommandPageCatalog(view FeishuCommandPageView) FeishuDirectCommandCatalog {
	def, _ := FeishuCommandDefinitionByID(strings.TrimSpace(view.CommandID))
	title := strings.TrimSpace(view.Title)
	if title == "" {
		title = strings.TrimSpace(def.Title)
	}
	displayStyle := view.DisplayStyle
	if displayStyle == "" {
		displayStyle = CommandCatalogDisplayCompactButtons
	}
	breadcrumbs := append([]CommandCatalogBreadcrumb(nil), view.Breadcrumbs...)
	if len(breadcrumbs) == 0 && strings.TrimSpace(def.GroupID) != "" {
		breadcrumbs = FeishuCommandBreadcrumbs(def.GroupID, title)
	}
	bodySections := BuildFeishuCommandPageBodySections(view)
	noticeSections := BuildFeishuCommandPageNoticeSections(view)
	sections := append([]CommandCatalogSection(nil), view.Sections...)
	relatedButtons := append([]CommandCatalogButton(nil), view.RelatedButtons...)
	interactive := view.Interactive
	if view.Sealed {
		interactive = false
		relatedButtons = nil
	}
	if len(relatedButtons) == 0 && strings.TrimSpace(def.GroupID) != "" {
		relatedButtons = FeishuCommandBackButtons(def.GroupID)
	}
	if view.Sealed {
		relatedButtons = nil
	}
	return FeishuDirectCommandCatalog{
		Title:           title,
		MessageID:       strings.TrimSpace(view.MessageID),
		TrackingKey:     strings.TrimSpace(view.TrackingKey),
		ThemeKey:        strings.TrimSpace(view.ThemeKey),
		Patchable:       view.Patchable,
		SummarySections: cloneNormalizedFeishuCardSections(bodySections),
		BodySections:    bodySections,
		NoticeSections:  noticeSections,
		Interactive:     interactive,
		Sealed:          view.Sealed,
		DisplayStyle:    displayStyle,
		Breadcrumbs:     breadcrumbs,
		Sections:        sections,
		RelatedButtons:  relatedButtons,
	}
}

func FeishuCommandPageViewFromCatalog(commandID string, catalog FeishuDirectCommandCatalog, breadcrumbs []CommandCatalogBreadcrumb, relatedButtons []CommandCatalogButton) FeishuCommandPageView {
	view := FeishuCommandPageView{
		CommandID:       strings.TrimSpace(commandID),
		Title:           strings.TrimSpace(catalog.Title),
		MessageID:       strings.TrimSpace(catalog.MessageID),
		TrackingKey:     strings.TrimSpace(catalog.TrackingKey),
		ThemeKey:        strings.TrimSpace(catalog.ThemeKey),
		Patchable:       catalog.Patchable,
		Breadcrumbs:     append([]CommandCatalogBreadcrumb(nil), breadcrumbs...),
		SummarySections: cloneNormalizedFeishuCardSections(firstNonEmptyFeishuCardSections(catalog.BodySections, catalog.SummarySections)),
		BodySections:    cloneNormalizedFeishuCardSections(firstNonEmptyFeishuCardSections(catalog.BodySections, catalog.SummarySections)),
		NoticeSections:  cloneNormalizedFeishuCardSections(catalog.NoticeSections),
		Interactive:     catalog.Interactive,
		Sealed:          catalog.Sealed,
		DisplayStyle:    catalog.DisplayStyle,
		Sections:        append([]CommandCatalogSection(nil), catalog.Sections...),
		RelatedButtons:  append([]CommandCatalogButton(nil), relatedButtons...),
	}
	if view.DisplayStyle == "" {
		view.DisplayStyle = CommandCatalogDisplayDefault
	}
	if len(view.BodySections) == 0 {
		lines := splitFeishuCommandPageSummaryLines(catalog.Summary)
		if len(lines) != 0 {
			view.BodySections = []FeishuCardTextSection{{Lines: lines}}
			view.SummarySections = cloneNormalizedFeishuCardSections(view.BodySections)
		}
	}
	if view.CommandID == "" {
		view.CommandID = strings.TrimSpace(commandID)
	}
	return view
}

func BuildFeishuCommandPageSummarySections(view FeishuCommandPageView) []FeishuCardTextSection {
	return BuildFeishuCommandPageBodySections(view)
}

func BuildFeishuCommandPageBodySections(view FeishuCommandPageView) []FeishuCardTextSection {
	return cloneNormalizedFeishuCardSections(firstNonEmptyFeishuCardSections(view.BodySections, view.SummarySections))
}

func BuildFeishuCommandPageNoticeSections(view FeishuCommandPageView) []FeishuCardTextSection {
	sections := make([]FeishuCardTextSection, 0, len(view.NoticeSections)+1)
	if feedback, ok := commandPageFeedbackSection(view); ok {
		sections = append(sections, feedback)
	}
	sections = append(sections, cloneNormalizedFeishuCardSections(view.NoticeSections)...)
	if len(sections) == 0 {
		return nil
	}
	return sections
}

func commandPageFeedbackSection(view FeishuCommandPageView) (FeishuCardTextSection, bool) {
	text := normalizeCommandFeedbackText(view.StatusText)
	if text == "" {
		return FeishuCardTextSection{}, false
	}
	label := "状态"
	switch strings.TrimSpace(view.StatusKind) {
	case "error":
		label = "错误"
	case "info":
		label = "说明"
	}
	return FeishuCardTextSection{
		Label: label,
		Lines: []string{text},
	}, true
}

func FeishuCommandBreadcrumbsForCommand(commandID string, extraLabels ...string) []CommandCatalogBreadcrumb {
	def, ok := FeishuCommandDefinitionByID(strings.TrimSpace(commandID))
	if !ok {
		return nil
	}
	breadcrumbs := FeishuCommandBreadcrumbs(def.GroupID, def.Title)
	for _, label := range extraLabels {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		breadcrumbs = append(breadcrumbs, CommandCatalogBreadcrumb{Label: label})
	}
	return breadcrumbs
}

func FeishuCommandBackToRootButtons(commandID string) []CommandCatalogButton {
	def, ok := FeishuCommandDefinitionByID(strings.TrimSpace(commandID))
	if !ok {
		return nil
	}
	command := strings.TrimSpace(def.CanonicalSlash)
	if command == "" {
		return nil
	}
	return []CommandCatalogButton{{
		Label:       "返回" + strings.TrimSpace(def.Title),
		Kind:        CommandCatalogButtonRunCommand,
		CommandText: command,
	}}
}

func splitFeishuCommandPageSummaryLines(text string) []string {
	lines := make([]string, 0, 4)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func cloneNormalizedFeishuCardSections(source []FeishuCardTextSection) []FeishuCardTextSection {
	if len(source) == 0 {
		return nil
	}
	out := make([]FeishuCardTextSection, 0, len(source))
	for _, section := range source {
		normalized := section.Normalized()
		if normalized.Label == "" && len(normalized.Lines) == 0 {
			continue
		}
		clonedLines := append([]string(nil), normalized.Lines...)
		out = append(out, FeishuCardTextSection{
			Label: normalized.Label,
			Lines: clonedLines,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func firstNonEmptyFeishuCardSections(values ...[]FeishuCardTextSection) []FeishuCardTextSection {
	for _, value := range values {
		if len(value) != 0 {
			return value
		}
	}
	return nil
}
