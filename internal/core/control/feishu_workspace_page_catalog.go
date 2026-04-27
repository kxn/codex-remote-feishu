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
			Entries: []CommandCatalogEntry{
				workspacePageButtonEntry("切换", "/workspace list"),
				workspacePageButtonEntry("从目录新建", "/workspace new dir"),
				workspacePageButtonEntry("从 GIT URL 新建", "/workspace new git"),
				workspacePageButtonEntry("从 Worktree 新建", "/workspace new worktree"),
				workspacePageButtonEntry("解除接管", "/workspace detach"),
			},
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
			Entries: []CommandCatalogEntry{
				workspacePageButtonEntry("从目录新建", "/workspace new dir"),
				workspacePageButtonEntry("从 GIT URL 新建", "/workspace new git"),
				workspacePageButtonEntry("从 Worktree 新建", "/workspace new worktree"),
			},
		}},
		RelatedButtons: []CommandCatalogButton{{
			Label:       "返回上一层",
			Kind:        CommandCatalogButtonAction,
			CommandText: "/workspace",
		}},
	})
}

func workspacePageButtonEntry(label, commandText string) CommandCatalogEntry {
	label = strings.TrimSpace(label)
	commandText = strings.TrimSpace(commandText)
	if label == "" || commandText == "" {
		return CommandCatalogEntry{}
	}
	return CommandCatalogEntry{
		Title: label,
		Buttons: []CommandCatalogButton{{
			Label:       label,
			Kind:        CommandCatalogButtonAction,
			CommandText: commandText,
		}},
	}
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
		CommandText: "/menu",
	}}
}
