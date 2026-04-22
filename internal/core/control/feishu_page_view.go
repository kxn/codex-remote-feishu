package control

import "strings"

// FeishuPageView is the generic page-card DTO used by menu/config/root pages.
// It intentionally avoids command-specific implicit defaults.
type FeishuPageView struct {
	PageID          string
	CommandID       string
	Title           string
	MessageID       string
	TrackingKey     string
	ThemeKey        string
	Patchable       bool
	Breadcrumbs     []CommandCatalogBreadcrumb
	SummarySections []FeishuCardTextSection
	BodySections    []FeishuCardTextSection
	NoticeSections  []FeishuCardTextSection
	StatusKind      string
	StatusText      string
	Interactive     bool
	Sealed          bool
	DisplayStyle    CommandCatalogDisplayStyle
	Sections        []CommandCatalogSection
	RelatedButtons  []CommandCatalogButton
}

func NormalizeFeishuPageView(view FeishuPageView) FeishuPageView {
	commandID := strings.TrimSpace(view.CommandID)
	def, _ := FeishuCommandDefinitionByID(commandID)
	title := strings.TrimSpace(view.Title)
	if title == "" {
		title = strings.TrimSpace(def.Title)
	}
	displayStyle := view.DisplayStyle
	if displayStyle == "" {
		displayStyle = CommandCatalogDisplayCompactButtons
	}
	breadcrumbs := cloneCommandBreadcrumbs(view.Breadcrumbs)
	if len(breadcrumbs) == 0 && strings.TrimSpace(def.GroupID) != "" {
		breadcrumbs = FeishuCommandBreadcrumbs(def.GroupID, title)
	}
	bodySections := BuildFeishuPageBodySections(view)
	noticeSections := BuildFeishuPageNoticeSections(view)
	sections := cloneCommandCatalogSections(view.Sections)
	relatedButtons := cloneCommandCatalogButtons(view.RelatedButtons)
	interactive := view.Interactive
	if view.Sealed {
		interactive = false
		relatedButtons = nil
	} else if len(relatedButtons) == 0 && strings.TrimSpace(def.GroupID) != "" {
		relatedButtons = FeishuCommandBackButtons(def.GroupID)
	}
	return FeishuPageView{
		PageID:          strings.TrimSpace(view.PageID),
		CommandID:       commandID,
		Title:           title,
		MessageID:       strings.TrimSpace(view.MessageID),
		TrackingKey:     strings.TrimSpace(view.TrackingKey),
		ThemeKey:        strings.TrimSpace(view.ThemeKey),
		Patchable:       view.Patchable,
		Breadcrumbs:     breadcrumbs,
		SummarySections: cloneNormalizedFeishuCardSections(bodySections),
		BodySections:    bodySections,
		NoticeSections:  noticeSections,
		StatusKind:      "",
		StatusText:      "",
		Interactive:     interactive,
		Sealed:          view.Sealed,
		DisplayStyle:    displayStyle,
		Sections:        sections,
		RelatedButtons:  relatedButtons,
	}
}

func BuildFeishuPageBodySections(view FeishuPageView) []FeishuCardTextSection {
	return cloneNormalizedFeishuCardSections(firstNonEmptyFeishuCardSections(view.BodySections, view.SummarySections))
}

func BuildFeishuPageNoticeSections(view FeishuPageView) []FeishuCardTextSection {
	sections := make([]FeishuCardTextSection, 0, len(view.NoticeSections)+1)
	if feedback, ok := pageFeedbackSection(view.StatusKind, view.StatusText); ok {
		sections = append(sections, feedback)
	}
	sections = append(sections, cloneNormalizedFeishuCardSections(view.NoticeSections)...)
	if len(sections) == 0 {
		return nil
	}
	return sections
}

func pageFeedbackSection(statusKind, statusText string) (FeishuCardTextSection, bool) {
	text := normalizeCommandFeedbackText(statusText)
	if text == "" {
		return FeishuCardTextSection{}, false
	}
	label := "状态"
	switch strings.TrimSpace(statusKind) {
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

func FeishuPageViewFromCommandPageView(view FeishuPageView) FeishuPageView {
	return NormalizeFeishuPageView(FeishuPageView{
		PageID:          strings.TrimSpace(view.CommandID),
		CommandID:       strings.TrimSpace(view.CommandID),
		Title:           strings.TrimSpace(view.Title),
		MessageID:       strings.TrimSpace(view.MessageID),
		TrackingKey:     strings.TrimSpace(view.TrackingKey),
		ThemeKey:        strings.TrimSpace(view.ThemeKey),
		Patchable:       view.Patchable,
		Breadcrumbs:     cloneCommandBreadcrumbs(view.Breadcrumbs),
		SummarySections: cloneNormalizedFeishuCardSections(view.SummarySections),
		BodySections:    cloneNormalizedFeishuCardSections(view.BodySections),
		NoticeSections:  cloneNormalizedFeishuCardSections(view.NoticeSections),
		StatusKind:      strings.TrimSpace(view.StatusKind),
		StatusText:      strings.TrimSpace(view.StatusText),
		Interactive:     view.Interactive,
		Sealed:          view.Sealed,
		DisplayStyle:    view.DisplayStyle,
		Sections:        cloneCommandCatalogSections(view.Sections),
		RelatedButtons:  cloneCommandCatalogButtons(view.RelatedButtons),
	})
}
