package control

import (
	"sort"
	"strings"
)

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
	sort.SliceStable(defs, func(i, j int) bool {
		return feishuCommandDisplayRank(groupID, defs[i].ID) < feishuCommandDisplayRank(groupID, defs[j].ID)
	})
	return defs
}

func BuildFeishuCommandStaticPageView(title, summary string, interactive bool) FeishuPageView {
	return BuildFeishuCommandStaticPageViewForContext(title, summary, interactive, CatalogContext{})
}

func BuildFeishuCommandStaticPageViewForContext(title, summary string, interactive bool, ctx CatalogContext) FeishuPageView {
	ctx = NormalizeCatalogContext(ctx)
	sections := make([]CommandCatalogSection, 0, len(feishuCommandGroups))
	for _, group := range feishuCommandGroups {
		resolved := ResolveFeishuCommandDisplayGroup(group.ID, interactive, ctx)
		entries := make([]CommandCatalogEntry, 0, len(resolved))
		for _, current := range resolved {
			def := current.Definition
			if interactive {
				entries = append(entries, buildFeishuCommandMenuEntryFromResolution(current, ctx.Backend))
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

func FeishuCommandHelpPageView() FeishuPageView {
	return BuildFeishuCommandStaticPageViewForContext(
		"命令帮助",
		"以下是当前主展示的 canonical slash command。",
		false,
		CatalogContext{},
	)
}

func FeishuCommandMenuPageView() FeishuPageView {
	return BuildFeishuCommandStaticPageViewForContext(
		"命令目录",
		"这是同源的静态命令目录。真正的 `/menu` 首页会在 service 层按当前阶段动态重排。",
		true,
		CatalogContext{},
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

func feishuCommandDisplayRank(groupID, commandID string) int {
	switch strings.TrimSpace(groupID) {
	case FeishuCommandGroupCurrentWork:
		return commandRank(commandID, FeishuCommandStop, FeishuCommandCompact, FeishuCommandSteerAll, FeishuCommandNew, FeishuCommandStatus)
	case FeishuCommandGroupSendSettings:
		return commandRank(commandID, FeishuCommandMode, FeishuCommandReasoning, FeishuCommandModel, FeishuCommandAccess, FeishuCommandPlan, FeishuCommandVerbose, FeishuCommandAutoContinue)
	case FeishuCommandGroupSwitchTarget:
		return commandRank(
			commandID,
			FeishuCommandWorkspace,
			FeishuCommandWorkspaceList,
			FeishuCommandWorkspaceNew,
			FeishuCommandWorkspaceNewDir,
			FeishuCommandWorkspaceNewGit,
			FeishuCommandWorkspaceNewWorktree,
			FeishuCommandWorkspaceDetach,
			FeishuCommandList,
			FeishuCommandUse,
			FeishuCommandUseAll,
			FeishuCommandDetach,
			FeishuCommandFollow,
		)
	case FeishuCommandGroupCommonTools:
		return commandRank(commandID, FeishuCommandReview, FeishuCommandPatch, FeishuCommandAutoWhip, FeishuCommandHistory, FeishuCommandCron, FeishuCommandSendFile)
	case FeishuCommandGroupMaintenance:
		return commandRank(commandID, FeishuCommandAdmin, FeishuCommandUpgrade, FeishuCommandDebug, FeishuCommandHelp, FeishuCommandVSCodeMigrate)
	default:
		return 1_000_000
	}
}

func commandRank(commandID string, ordered ...string) int {
	for index, id := range ordered {
		if strings.TrimSpace(commandID) == strings.TrimSpace(id) {
			return index
		}
	}
	return len(ordered) + 1_000
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
