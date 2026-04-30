package control

import "strings"

func FeishuPageViewFromView(view FeishuCatalogView, productMode, menuStage string) (FeishuPageView, bool) {
	return FeishuPageViewFromViewContext(view, legacyCatalogContext(productMode, menuStage))
}

func FeishuPageViewFromViewContext(view FeishuCatalogView, ctx CatalogContext) (FeishuPageView, bool) {
	switch {
	case view.Menu != nil:
		return BuildFeishuCommandMenuPageViewForContext(*view.Menu, ctx), true
	case view.Config != nil:
		return BuildFeishuCommandConfigPageView(*view.Config), true
	case view.Page != nil:
		return NormalizeFeishuPageView(*view.Page), true
	default:
		return FeishuPageView{}, false
	}
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
		Kind:        CommandCatalogButtonAction,
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

func cloneCommandBreadcrumbs(source []CommandCatalogBreadcrumb) []CommandCatalogBreadcrumb {
	if len(source) == 0 {
		return nil
	}
	return append([]CommandCatalogBreadcrumb(nil), source...)
}

func cloneCommandCatalogButtons(source []CommandCatalogButton) []CommandCatalogButton {
	if len(source) == 0 {
		return nil
	}
	out := make([]CommandCatalogButton, 0, len(source))
	for _, button := range source {
		cloned := button
		cloned.Label = strings.TrimSpace(button.Label)
		cloned.CommandText = strings.TrimSpace(button.CommandText)
		cloned.CommandID = strings.TrimSpace(button.CommandID)
		cloned.CatalogFamilyID = strings.TrimSpace(button.CatalogFamilyID)
		cloned.CatalogVariantID = strings.TrimSpace(button.CatalogVariantID)
		if len(button.CallbackValue) != 0 {
			cloned.CallbackValue = cloneActionPayload(button.CallbackValue)
		}
		cloned.Style = strings.TrimSpace(button.Style)
		out = append(out, cloned)
	}
	return out
}

func cloneCommandCatalogSections(source []CommandCatalogSection) []CommandCatalogSection {
	if len(source) == 0 {
		return nil
	}
	out := make([]CommandCatalogSection, 0, len(source))
	for _, section := range source {
		cloned := CommandCatalogSection{
			Title:   strings.TrimSpace(section.Title),
			Entries: make([]CommandCatalogEntry, 0, len(section.Entries)),
		}
		for _, entry := range section.Entries {
			cloned.Entries = append(cloned.Entries, CommandCatalogEntry{
				Title:       strings.TrimSpace(entry.Title),
				Commands:    append([]string(nil), entry.Commands...),
				Description: strings.TrimSpace(entry.Description),
				Examples:    append([]string(nil), entry.Examples...),
				Buttons:     cloneCommandCatalogButtons(entry.Buttons),
				Form:        cloneCommandCatalogForm(entry.Form),
			})
		}
		out = append(out, cloned)
	}
	return out
}

func cloneCommandCatalogForm(form *CommandCatalogForm) *CommandCatalogForm {
	if form == nil {
		return nil
	}
	cloned := *form
	cloned.CommandID = strings.TrimSpace(form.CommandID)
	cloned.CommandText = strings.TrimSpace(form.CommandText)
	cloned.CatalogFamilyID = strings.TrimSpace(form.CatalogFamilyID)
	cloned.CatalogVariantID = strings.TrimSpace(form.CatalogVariantID)
	cloned.Field = CommandCatalogFormField{
		Name:         strings.TrimSpace(form.Field.Name),
		Kind:         form.Field.Kind,
		Label:        strings.TrimSpace(form.Field.Label),
		Placeholder:  strings.TrimSpace(form.Field.Placeholder),
		DefaultValue: strings.TrimSpace(form.Field.DefaultValue),
		Options:      append([]CommandCatalogFormFieldOption(nil), form.Field.Options...),
	}
	return &cloned
}

func cloneActionPayload(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]any, len(value))
	for key, current := range value {
		out[key] = current
	}
	return out
}
