package control

import "strings"

func FeishuCommandGroups() []FeishuCommandGroup {
	groups := make([]FeishuCommandGroup, 0, len(feishuCommandGroups))
	for _, group := range feishuCommandGroups {
		groups = append(groups, group)
	}
	return groups
}

func FeishuCommandGroupByID(groupID string) (FeishuCommandGroup, bool) {
	for _, group := range feishuCommandGroups {
		if group.ID == groupID {
			return group, true
		}
	}
	return FeishuCommandGroup{}, false
}

func FeishuCommandDefinitions() []FeishuCommandDefinition {
	defs := make([]FeishuCommandDefinition, 0, len(feishuCommandSpecs))
	for _, spec := range feishuCommandSpecs {
		defs = append(defs, runtimeFeishuCommandDefinition(spec))
	}
	return defs
}

func FeishuCommandDefinitionByID(commandID string) (FeishuCommandDefinition, bool) {
	for _, spec := range feishuCommandSpecs {
		if spec.definition.ID == commandID {
			return runtimeFeishuCommandDefinition(spec), true
		}
	}
	return FeishuCommandDefinition{}, false
}

func FeishuCommandDefinitionsForGroup(groupID string) []FeishuCommandDefinition {
	defs := make([]FeishuCommandDefinition, 0, len(feishuCommandSpecs))
	for _, spec := range feishuCommandSpecs {
		if spec.definition.GroupID != groupID {
			continue
		}
		defs = append(defs, runtimeFeishuCommandDefinition(spec))
	}
	return defs
}

func BuildFeishuCommandStaticPageView(title, summary string, interactive bool) FeishuCommandPageView {
	sections := make([]CommandCatalogSection, 0, len(feishuCommandGroups))
	for _, group := range feishuCommandGroups {
		entries := make([]CommandCatalogEntry, 0, len(feishuCommandSpecs))
		for _, spec := range feishuCommandSpecs {
			def := runtimeFeishuCommandDefinition(spec)
			if interactive && !def.ShowInMenu {
				continue
			}
			if !interactive && !def.ShowInHelp {
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

func FeishuCommandHelpPageView() FeishuCommandPageView {
	return BuildFeishuCommandStaticPageView(
		"Slash 命令帮助",
		"以下是当前主展示的 canonical slash command。历史 alias 仍可兼容，但不再作为新的主展示入口。",
		false,
	)
}

func FeishuCommandMenuPageView() FeishuCommandPageView {
	return BuildFeishuCommandStaticPageView(
		"命令目录",
		"这是同源的静态命令目录。真正的 `/menu` 首页会在 service 层按当前阶段动态重排。",
		true,
	)
}

func FeishuRecommendedMenus() []FeishuRecommendedMenu {
	order := []string{
		FeishuCommandMenu,
		FeishuCommandStop,
		FeishuCommandSteerAll,
		FeishuCommandNew,
		FeishuCommandReasoning,
		FeishuCommandModel,
		FeishuCommandAccess,
	}
	menus := make([]FeishuRecommendedMenu, 0, len(order))
	for _, commandID := range order {
		def, ok := FeishuCommandDefinitionByID(commandID)
		if !ok || def.RecommendedMenu == nil {
			continue
		}
		menu := *def.RecommendedMenu
		menus = append(menus, FeishuRecommendedMenu{
			Key:         strings.TrimSpace(menu.Key),
			Name:        strings.TrimSpace(menu.Name),
			Description: strings.TrimSpace(menu.Description),
		})
	}
	return menus
}

func catalogButtonLabel(def FeishuCommandDefinition) string {
	switch def.ArgumentKind {
	case FeishuCommandArgumentChoice, FeishuCommandArgumentText:
		return "打开"
	default:
		return strings.TrimSpace(def.Title)
	}
}

func cloneFeishuCommandDefinition(def FeishuCommandDefinition) FeishuCommandDefinition {
	cloned := def
	cloned.Examples = append([]string(nil), def.Examples...)
	if len(def.Options) > 0 {
		cloned.Options = append([]FeishuCommandOption(nil), def.Options...)
	}
	if def.RecommendedMenu != nil {
		menu := *def.RecommendedMenu
		cloned.RecommendedMenu = &menu
	}
	return cloned
}
