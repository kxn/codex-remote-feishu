package control

import "runtime"

func BuildFeishuAdminRootPageView(inMenu bool) FeishuPageView {
	return buildFeishuAdminRootPageViewForGOOS(inMenu, runtime.GOOS)
}

func buildFeishuAdminRootPageViewForGOOS(inMenu bool, goos string) FeishuPageView {
	sections := []CommandCatalogSection{{
		Title: "管理页",
		Entries: adminPageEntries(
			adminPageEntrySpec{
				Title:       "管理页外链",
				Description: "生成可从外部访问的临时管理页链接。",
				CommandText: "/admin web",
			},
			adminPageEntrySpec{
				Title:       "本地管理页",
				Description: "显示当前机器可直接打开的本地管理页地址。",
				CommandText: "/admin localweb",
			},
		),
	}}
	if feishuAdminAutostartSupportedPlatform(goos) {
		sections[0].Entries = append(sections[0].Entries, adminPageEntries(adminPageEntrySpec{
			Title:       "自动启动",
			Description: "查看并配置这台机器上的自动启动。",
			CommandText: "/admin autostart",
		})...)
	}
	sections = append(sections, CommandCatalogSection{
		Title: "维护命令",
		Entries: adminPageEntries(
			adminPageCommandSpec(FeishuCommandUpgrade),
			adminPageCommandSpec(FeishuCommandDebug),
			adminPageCommandSpec(FeishuCommandHelp),
		),
	})
	return NormalizeFeishuPageView(FeishuPageView{
		CommandID:    FeishuCommandAdmin,
		Title:        "系统管理",
		Interactive:  true,
		DisplayStyle: CommandCatalogDisplayCompactButtons,
		Breadcrumbs:  adminPageBreadcrumbs(inMenu, "系统管理"),
		Sections:     sections,
		RelatedButtons: func() []CommandCatalogButton {
			if !inMenu {
				return nil
			}
			return []CommandCatalogButton{{
				Label:       "返回上一层",
				Kind:        CommandCatalogButtonAction,
				CommandText: FeishuCommandMenuCommandText(""),
			}}
		}(),
	})
}

type adminPageEntrySpec struct {
	CommandID   string
	CommandText string
	Title       string
	Description string
}

func adminPageCommandSpec(commandID string) adminPageEntrySpec {
	def, ok := FeishuCommandDefinitionByID(commandID)
	if !ok {
		return adminPageEntrySpec{}
	}
	return adminPageEntrySpec{
		CommandID:   def.ID,
		CommandText: def.CanonicalSlash,
		Title:       def.Title,
		Description: def.Description,
	}
}

func adminPageEntries(specs ...adminPageEntrySpec) []CommandCatalogEntry {
	entries := make([]CommandCatalogEntry, 0, len(specs))
	for _, spec := range specs {
		if entry, ok := adminPageEntry(spec); ok {
			entries = append(entries, entry)
		}
	}
	return entries
}

func adminPageEntry(spec adminPageEntrySpec) (CommandCatalogEntry, bool) {
	if spec.Title == "" || spec.CommandText == "" {
		return CommandCatalogEntry{}, false
	}
	return CommandCatalogEntry{
		Title:       spec.Title,
		Description: spec.Description,
		Buttons: []CommandCatalogButton{
			FeishuLocalPageCommandButton(spec.Title, spec.CommandText, "", false),
		},
	}, true
}

func adminPageBreadcrumbs(inMenu bool, labels ...string) []CommandCatalogBreadcrumb {
	breadcrumbs := make([]CommandCatalogBreadcrumb, 0, len(labels)+1)
	if inMenu {
		breadcrumbs = append(breadcrumbs, CommandCatalogBreadcrumb{Label: "菜单首页"})
	}
	for _, label := range labels {
		if label == "" {
			continue
		}
		breadcrumbs = append(breadcrumbs, CommandCatalogBreadcrumb{Label: label})
	}
	return breadcrumbs
}
