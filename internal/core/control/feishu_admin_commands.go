package control

func adminCommandSpec() feishuCommandSpec {
	return feishuCommandSpec{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandAdmin,
			GroupID:          FeishuCommandGroupMaintenance,
			Title:            "系统管理",
			CanonicalSlash:   "/admin",
			CanonicalMenuKey: "admin",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "打开系统管理入口，并查看管理页、自动启动与维护命令。",
			Examples:         []string{"/admin web", "/admin localweb", "/admin autostart on"},
			ShowInHelp:       true,
			ShowInMenu:       true,
		},
		textExact: []feishuCommandMatch{
			{alias: "/admin", action: Action{Kind: ActionAdminRoot}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "admin", action: Action{Kind: ActionAdminRoot, Text: "/admin"}},
		},
	}
}

func adminSubcommandSpec() feishuCommandSpec {
	return feishuCommandSpec{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandAdminSubcommand,
			GroupID:          FeishuCommandGroupMaintenance,
			Title:            "系统管理子命令",
			CanonicalSlash:   "/admin",
			CanonicalMenuKey: "admin-subcommand",
			ArgumentKind:     FeishuCommandArgumentText,
			ArgumentFormHint: "web",
			ArgumentFormNote: "例如 web、localweb、autostart on。",
			ArgumentSubmit:   "执行",
			Description:      "内部命令入口：承接 `/admin` 的具体子命令。",
			ShowInHelp:       false,
			ShowInMenu:       false,
		},
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/admin", kind: ActionAdminCommand},
		},
	}
}
