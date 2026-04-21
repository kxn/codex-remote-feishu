package control

import "strings"

func normalizeFeishuCommandProductMode(productMode string) string {
	switch strings.ToLower(strings.TrimSpace(productMode)) {
	case "vscode":
		return "vscode"
	default:
		return "normal"
	}
}

// FeishuCommandDefinitionForDisplay projects a canonical command definition into
// the user-facing help/menu shape for the current surface mode.
func FeishuCommandDefinitionForDisplay(def FeishuCommandDefinition, productMode string, interactive bool, menuStage string) (FeishuCommandDefinition, bool) {
	if interactive {
		if !def.ShowInMenu || !FeishuCommandVisibleInMenuStage(def.ID, menuStage) {
			return FeishuCommandDefinition{}, false
		}
	} else if !def.ShowInHelp {
		return FeishuCommandDefinition{}, false
	}

	projected := cloneFeishuCommandDefinition(def)
	if normalizeFeishuCommandProductMode(productMode) != "normal" {
		return projected, true
	}

	switch strings.TrimSpace(projected.ID) {
	case FeishuCommandUse, FeishuCommandUseAll:
		return FeishuCommandDefinition{}, false
	case FeishuCommandVSCodeMigrate:
		return FeishuCommandDefinition{}, false
	}
	return projected, true
}

func BuildFeishuCommandDisplayPageView(title, summary string, interactive bool, productMode, menuStage string) FeishuCommandPageView {
	sections := make([]CommandCatalogSection, 0, len(feishuCommandGroups))
	for _, group := range feishuCommandGroups {
		defs := FeishuCommandDefinitionsForGroup(group.ID)
		entries := make([]CommandCatalogEntry, 0, len(defs))
		for _, def := range defs {
			def, ok := FeishuCommandDefinitionForDisplay(def, productMode, interactive, menuStage)
			if !ok || def.GroupID != group.ID {
				continue
			}
			if interactive {
				entries = append(entries, buildFeishuCommandCatalogEntry(def, catalogButtonLabel(def)))
				continue
			}
			entries = append(entries, buildFeishuCommandCatalogEntry(def, ""))
		}
		if len(entries) == 0 {
			continue
		}
		sections = append(sections, CommandCatalogSection{
			Title:   group.Title,
			Entries: entries,
		})
	}
	view := FeishuCommandPageView{
		Title:       title,
		Interactive: interactive,
		Sections:    sections,
	}
	if lines := splitFeishuCommandPageSummaryLines(summary); len(lines) != 0 {
		view.SummarySections = []FeishuCardTextSection{{Lines: lines}}
	}
	return NormalizeFeishuCommandPageView(view)
}
