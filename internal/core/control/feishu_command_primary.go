package control

var feishuPrimaryCommandSpec = feishuCommandSpec{
	definition: FeishuCommandDefinition{
		ID:               FeishuCommandPrimary,
		GroupID:          FeishuCommandGroupCommonTools,
		Title:            "群主机器人",
		CanonicalSlash:   "/primary",
		CanonicalMenuKey: "primary",
		ArgumentKind:     FeishuCommandArgumentChoice,
		ArgumentFormHint: "status",
		ArgumentFormNote: "输入 on / off / status / refresh。",
		ArgumentSubmit:   "执行",
		Description:      "设置或查看本群承接未 @ 普通消息的主机器人。",
		Examples:         []string{"/primary on", "/primary off", "/primary status", "/primary refresh"},
		Options: []FeishuCommandOption{
			commandOption("/primary", "primary", "on", "设为本群主机器人", "把当前机器人设为本群主机器人。"),
			commandOption("/primary", "primary", "off", "取消主机器人", "取消当前机器人作为本群主机器人。"),
			commandOption("/primary", "primary", "status", "查看状态", "查看本群主机器人和权限状态。"),
			commandOption("/primary", "primary", "refresh", "刷新权限", "刷新当前机器人的群普通消息权限状态。"),
		},
		ShowInHelp: true,
		ShowInMenu: true,
	},
	textPrefixes: []feishuCommandPrefixMatch{
		{alias: "/primary", kind: ActionPrimaryCommand},
	},
	menuExact: []feishuCommandMatch{
		{alias: "primary", action: Action{Kind: ActionPrimaryCommand, Text: "/primary status"}},
		{alias: "primaryon", action: Action{Kind: ActionPrimaryCommand, Text: "/primary on"}},
		{alias: "primaryoff", action: Action{Kind: ActionPrimaryCommand, Text: "/primary off"}},
		{alias: "primarystatus", action: Action{Kind: ActionPrimaryCommand, Text: "/primary status"}},
		{alias: "primaryrefresh", action: Action{Kind: ActionPrimaryCommand, Text: "/primary refresh"}},
	},
	menuDynamic: []feishuCommandDynamicMenuMatch{
		{prefix: "primary_", kind: ActionPrimaryCommand, parseArgument: normalizePrimaryMenuArgument},
		{prefix: "primary-", kind: ActionPrimaryCommand, parseArgument: normalizePrimaryMenuArgument},
	},
}

var feishuCoworkersCommandSpec = feishuCommandSpec{
	definition: FeishuCommandDefinition{
		ID:               FeishuCommandCoworkers,
		GroupID:          FeishuCommandGroupCommonTools,
		Title:            "群内并发上限",
		CanonicalSlash:   "/coworkers",
		CanonicalMenuKey: "coworkers",
		ArgumentKind:     FeishuCommandArgumentText,
		ArgumentFormHint: "2",
		ArgumentFormNote: "输入非负整数设置并发上限，或输入 status 查看；0 表示不限制。",
		ArgumentSubmit:   "设置",
		Description:      "设置或查看当前群同时运行的机器人数量上限；0 表示不限制。",
		Examples:         []string{"/coworkers 2", "/coworkers status"},
		ShowInHelp:       true,
		ShowInMenu:       true,
	},
	textPrefixes: []feishuCommandPrefixMatch{
		{alias: "/coworkers", kind: ActionCoworkersCommand},
	},
	menuExact: []feishuCommandMatch{
		{alias: "coworkers", action: Action{Kind: ActionCoworkersCommand, Text: "/coworkers status"}},
	},
	menuDynamic: []feishuCommandDynamicMenuMatch{
		{prefix: "coworkers_", kind: ActionCoworkersCommand, parseArgument: normalizeCoworkersMenuArgument},
		{prefix: "coworkers-", kind: ActionCoworkersCommand, parseArgument: normalizeCoworkersMenuArgument},
	},
}
