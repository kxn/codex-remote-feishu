package control

import (
	"strings"
)

func normalizeFeishuCommandProductMode(productMode string) string {
	switch strings.ToLower(strings.TrimSpace(productMode)) {
	case "claude":
		return "normal"
	case "vscode":
		return "vscode"
	default:
		return "normal"
	}
}

func FeishuCommandDefinitionForDisplayContext(def FeishuCommandDefinition, interactive bool, ctx CatalogContext) (FeishuCommandDefinition, bool) {
	resolved, ok := ResolveFeishuCommandDisplayFamily(strings.TrimSpace(def.ID), interactive, ctx)
	if !ok {
		return FeishuCommandDefinition{}, false
	}
	return resolved.Definition, true
}

func projectFeishuCommandDefinitionForDisplay(def FeishuCommandDefinition, interactive bool, ctx CatalogContext) (FeishuCommandDefinition, bool) {
	ctx = NormalizeCatalogContext(ctx)
	profile := ResolveFeishuCommandDisplayProfileForContext(ctx)
	if !profile.IncludesFamily(def.ID) {
		return FeishuCommandDefinition{}, false
	}
	support, ok := ResolveFeishuCommandSupport(ctx, def.ID)
	if !ok || !support.Visible {
		return FeishuCommandDefinition{}, false
	}
	if interactive {
		if !def.ShowInMenu || !profile.MenuVisibleInStage(def.ID, ctx.MenuStage) {
			return FeishuCommandDefinition{}, false
		}
	} else if !def.ShowInHelp {
		return FeishuCommandDefinition{}, false
	}

	return cloneFeishuCommandDefinition(def), true
}
func BuildFeishuCommandDisplayPageViewForContext(title, summary string, interactive bool, ctx CatalogContext) FeishuPageView {
	sections := make([]CommandCatalogSection, 0, len(feishuCommandGroups))
	for _, group := range feishuCommandGroups {
		resolved := ResolveFeishuCommandDisplayGroup(group.ID, interactive, ctx)
		entries := make([]CommandCatalogEntry, 0, len(resolved))
		for _, current := range resolved {
			def := current.Definition
			if interactive {
				entries = append(entries, buildFeishuCommandCatalogEntryWithCatalog(def, current.FamilyID, current.VariantID, ctx.Backend, catalogButtonLabel(def)))
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
	view := FeishuPageView{
		Title:          title,
		CatalogBackend: ctx.Backend,
		Interactive:    interactive,
		Sections:       sections,
	}
	if lines := splitFeishuCommandPageSummaryLines(summary); len(lines) != 0 {
		view.SummarySections = []FeishuCardTextSection{{Lines: lines}}
	}
	return NormalizeFeishuPageView(view)
}
