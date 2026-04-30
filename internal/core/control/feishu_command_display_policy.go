package control

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
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

func legacyCatalogContext(productMode, menuStage string) CatalogContext {
	ctx := CatalogContext{
		ProductMode: productMode,
		MenuStage:   menuStage,
	}
	if strings.EqualFold(strings.TrimSpace(productMode), "claude") {
		ctx.Backend = agentproto.BackendClaude
	}
	return ctx
}

// FeishuCommandDefinitionForDisplay projects a canonical command definition into
// the user-facing help/menu shape for the current surface mode.
func FeishuCommandDefinitionForDisplay(def FeishuCommandDefinition, productMode string, interactive bool, menuStage string) (FeishuCommandDefinition, bool) {
	return FeishuCommandDefinitionForDisplayContext(def, interactive, legacyCatalogContext(productMode, menuStage))
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
	strategy, ok := ResolveFeishuCommandStrategy(ctx, def.ID)
	if !ok || !strategy.Visible {
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

func BuildFeishuCommandDisplayPageView(title, summary string, interactive bool, productMode, menuStage string) FeishuPageView {
	return BuildFeishuCommandDisplayPageViewForContext(title, summary, interactive, legacyCatalogContext(productMode, menuStage))
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
