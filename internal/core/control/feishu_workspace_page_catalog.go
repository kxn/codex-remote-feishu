package control

import "strings"

func BuildFeishuWorkspaceRootPageView(inMenu bool) FeishuPageView {
	return NormalizeFeishuPageView(FeishuPageView{
		CommandID:    FeishuCommandWorkspace,
		Title:        "工作会话",
		Interactive:  true,
		DisplayStyle: CommandCatalogDisplayCompactButtons,
		Breadcrumbs:  workspacePageBreadcrumbs(inMenu, "工作会话"),
		Sections: []CommandCatalogSection{{
			Entries: workspacePageEntries(
				workspacePageEntrySpec{CommandID: FeishuCommandWorkspaceList, Label: "切换"},
				workspacePageEntrySpec{CommandID: FeishuCommandWorkspaceNewDir},
				workspacePageEntrySpec{CommandID: FeishuCommandWorkspaceNewGit},
				workspacePageEntrySpec{CommandID: FeishuCommandWorkspaceNewWorktree},
				workspacePageEntrySpec{CommandID: FeishuCommandWorkspaceDetach},
			),
		}},
		RelatedButtons: workspaceRootRelatedButtons(inMenu),
	})
}

func BuildFeishuWorkspaceNewPageView(inMenu bool) FeishuPageView {
	return NormalizeFeishuPageView(FeishuPageView{
		CommandID:    FeishuCommandWorkspaceNew,
		Title:        "新建工作区",
		Interactive:  true,
		DisplayStyle: CommandCatalogDisplayCompactButtons,
		Breadcrumbs:  workspacePageBreadcrumbs(inMenu, "工作会话", "新建工作区"),
		Sections: []CommandCatalogSection{{
			Entries: workspacePageEntries(
				workspacePageEntrySpec{CommandID: FeishuCommandWorkspaceNewDir},
				workspacePageEntrySpec{CommandID: FeishuCommandWorkspaceNewGit},
				workspacePageEntrySpec{CommandID: FeishuCommandWorkspaceNewWorktree},
			),
		}},
		RelatedButtons: []CommandCatalogButton{{
			Label:       "返回上一层",
			Kind:        CommandCatalogButtonAction,
			CommandText: workspacePageCommandText(FeishuCommandWorkspace),
		}},
	})
}

type workspacePageEntrySpec struct {
	CommandID string
	Label     string
}

func workspacePageEntries(specs ...workspacePageEntrySpec) []CommandCatalogEntry {
	entries := make([]CommandCatalogEntry, 0, len(specs))
	for _, spec := range specs {
		if entry, ok := workspacePageButtonEntry(spec.CommandID, spec.Label); ok {
			entries = append(entries, entry)
		}
	}
	return entries
}

func workspacePageButtonEntry(commandID, label string) (CommandCatalogEntry, bool) {
	def, ok := FeishuCommandDefinitionByID(commandID)
	if !ok {
		return CommandCatalogEntry{}, false
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = strings.TrimSpace(def.Title)
	}
	commandText := strings.TrimSpace(def.CanonicalSlash)
	if label == "" || commandText == "" {
		return CommandCatalogEntry{}, false
	}
	return CommandCatalogEntry{
		Title: label,
		Buttons: []CommandCatalogButton{{
			Label:       label,
			Kind:        CommandCatalogButtonAction,
			CommandText: commandText,
			CommandID:   strings.TrimSpace(def.ID),
		}},
	}, true
}

func workspacePageCommandText(commandID string) string {
	def, ok := FeishuCommandDefinitionByID(commandID)
	if !ok {
		return ""
	}
	return strings.TrimSpace(def.CanonicalSlash)
}

func workspacePageBreadcrumbs(inMenu bool, labels ...string) []CommandCatalogBreadcrumb {
	breadcrumbs := make([]CommandCatalogBreadcrumb, 0, len(labels)+1)
	if inMenu {
		breadcrumbs = append(breadcrumbs, CommandCatalogBreadcrumb{Label: "菜单首页"})
	}
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		breadcrumbs = append(breadcrumbs, CommandCatalogBreadcrumb{Label: label})
	}
	return breadcrumbs
}

func workspaceRootRelatedButtons(inMenu bool) []CommandCatalogButton {
	if !inMenu {
		return nil
	}
	return []CommandCatalogButton{{
		Label:       "返回上一层",
		Kind:        CommandCatalogButtonAction,
		CommandText: FeishuCommandMenuCommandText(""),
	}}
}
