package control

import "strings"

func BuildFeishuCommandMenuHomePageView() FeishuCommandPageView {
	return FeishuCommandPageView{
		CommandID:    FeishuCommandMenu,
		Title:        "命令菜单",
		Interactive:  true,
		DisplayStyle: CommandCatalogDisplayCompactButtons,
		Sections: []CommandCatalogSection{{
			Title:   "全部分组",
			Entries: buildFeishuCommandMenuGroupEntries(),
		}},
	}
}

func BuildFeishuCommandMenuPageView(view FeishuCommandMenuView, productMode, menuStage string) FeishuCommandPageView {
	groupID := strings.TrimSpace(view.GroupID)
	if groupID == "" {
		return BuildFeishuCommandMenuHomePageView()
	}
	stage := strings.TrimSpace(view.Stage)
	if stage == "" {
		stage = strings.TrimSpace(menuStage)
	}
	return BuildFeishuCommandMenuGroupPageView(groupID, productMode, stage)
}

func BuildFeishuCommandMenuGroupPageView(groupID, productMode, menuStage string) FeishuCommandPageView {
	group, ok := FeishuCommandGroupByID(groupID)
	if !ok {
		return BuildFeishuCommandMenuHomePageView()
	}
	entries := make([]CommandCatalogEntry, 0, 6)
	for _, def := range FeishuCommandDefinitionsForGroup(groupID) {
		def, ok := FeishuCommandDefinitionForDisplay(def, productMode, true, menuStage)
		if !ok {
			continue
		}
		entries = append(entries, buildFeishuCommandMenuEntry(def))
	}
	return FeishuCommandPageView{
		CommandID:    FeishuCommandMenu,
		Title:        "命令菜单",
		Interactive:  true,
		DisplayStyle: CommandCatalogDisplayCompactButtons,
		Breadcrumbs:  FeishuCommandBreadcrumbs(groupID, ""),
		Sections: []CommandCatalogSection{{
			Title:   group.Title,
			Entries: entries,
		}},
		RelatedButtons: []CommandCatalogButton{{
			Label:       "返回上一层",
			Kind:        CommandCatalogButtonRunCommand,
			CommandText: FeishuCommandMenuCommandText(""),
		}},
	}
}

func BuildFeishuAttachmentRequiredPageView(def FeishuCommandDefinition, view FeishuCommandConfigView) FeishuCommandPageView {
	bodySections := BuildFeishuCommandConfigBodySections(def, view)
	noticeSections := BuildFeishuCommandConfigNoticeSections(def, view)
	return NormalizeFeishuCommandPageView(FeishuCommandPageView{
		CommandID:       strings.TrimSpace(def.ID),
		Title:           strings.TrimSpace(def.Title),
		SummarySections: append([]FeishuCardTextSection(nil), bodySections...),
		BodySections:    append([]FeishuCardTextSection(nil), bodySections...),
		NoticeSections:  append([]FeishuCardTextSection(nil), noticeSections...),
		Interactive:     true,
		DisplayStyle:    CommandCatalogDisplayCompactButtons,
		Breadcrumbs:     FeishuCommandBreadcrumbs(def.GroupID, def.Title),
		Sections: []CommandCatalogSection{{
			Title:   "开始 / 继续工作",
			Entries: buildFeishuRecoveryEntries(),
		}},
		RelatedButtons: FeishuCommandBackButtons(def.GroupID),
	})
}

func FeishuCommandBreadcrumbs(groupID, title string) []CommandCatalogBreadcrumb {
	breadcrumbs := []CommandCatalogBreadcrumb{{Label: "菜单首页"}}
	if group, ok := FeishuCommandGroupByID(groupID); ok {
		breadcrumbs = append(breadcrumbs, CommandCatalogBreadcrumb{Label: group.Title})
	}
	if title = strings.TrimSpace(title); title != "" {
		breadcrumbs = append(breadcrumbs, CommandCatalogBreadcrumb{Label: title})
	}
	return breadcrumbs
}

func FeishuCommandBackButtons(groupID string) []CommandCatalogButton {
	if group, ok := FeishuCommandGroupByID(groupID); ok {
		return []CommandCatalogButton{{
			Label:       "返回" + group.Title,
			Kind:        CommandCatalogButtonRunCommand,
			CommandText: FeishuCommandMenuCommandText(groupID),
		}}
	}
	return nil
}

func FeishuCommandMenuCommandText(view string) string {
	if strings.TrimSpace(view) == "" {
		return "/menu"
	}
	return "/menu " + strings.TrimSpace(view)
}

func buildFeishuCommandMenuGroupEntries() []CommandCatalogEntry {
	entries := make([]CommandCatalogEntry, 0, len(FeishuCommandGroups()))
	for _, group := range FeishuCommandGroups() {
		entries = append(entries, CommandCatalogEntry{
			Title:       strings.TrimSpace(group.Title),
			Description: strings.TrimSpace(group.Description),
			Buttons: []CommandCatalogButton{{
				Label:       feishuSubmenuButtonLabel(group.Title),
				Kind:        CommandCatalogButtonRunCommand,
				CommandText: FeishuCommandMenuCommandText(group.ID),
			}},
		})
	}
	return entries
}

func buildFeishuRecoveryEntries() []CommandCatalogEntry {
	return []CommandCatalogEntry{
		buildFeishuRecoveryEntry(FeishuCommandList),
		buildFeishuRecoveryEntry(FeishuCommandUse),
		buildFeishuRecoveryEntry(FeishuCommandStatus),
	}
}

func buildFeishuRecoveryEntry(commandID string) CommandCatalogEntry {
	def, ok := FeishuCommandDefinitionByID(commandID)
	if !ok {
		return CommandCatalogEntry{}
	}
	return buildFeishuCommandMenuEntry(def)
}

func buildFeishuCommandMenuEntry(def FeishuCommandDefinition) CommandCatalogEntry {
	return buildFeishuCommandCatalogEntry(def, feishuCommandMenuButtonLabel(def))
}

func buildFeishuCommandCatalogEntry(def FeishuCommandDefinition, buttonLabel string) CommandCatalogEntry {
	command := strings.TrimSpace(def.CanonicalSlash)
	entry := CommandCatalogEntry{
		Title:       strings.TrimSpace(def.Title),
		Description: strings.TrimSpace(def.Description),
		Examples:    append([]string(nil), def.Examples...),
	}
	if command != "" {
		entry.Commands = []string{command}
	}
	if buttonLabel = strings.TrimSpace(buttonLabel); buttonLabel != "" && command != "" {
		entry.Buttons = append(entry.Buttons, CommandCatalogButton{
			Label:       buttonLabel,
			Kind:        CommandCatalogButtonRunCommand,
			CommandText: command,
		})
	}
	return entry
}

func feishuCommandMenuButtonLabel(def FeishuCommandDefinition) string {
	title := strings.TrimSpace(def.Title)
	command := strings.TrimSpace(def.CanonicalSlash)
	switch {
	case title == "":
		return command
	case command == "":
		return title
	default:
		return title + " " + command
	}
}

func feishuSubmenuButtonLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "打开子菜单"
	}
	return label + " ›"
}
