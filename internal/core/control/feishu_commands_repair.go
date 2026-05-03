package control

func repairCommandSpec() feishuCommandSpec {
	return feishuCommandSpec{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandRepair,
			GroupID:          FeishuCommandGroupMaintenance,
			Title:            "修复连接",
			CanonicalSlash:   "/repair",
			CanonicalMenuKey: "repair",
			ArgumentKind:     FeishuCommandArgumentChoice,
			ArgumentFormHint: "daemon",
			ArgumentFormNote: "留空执行低扰动修复；输入 daemon 则重启托管 daemon。",
			ArgumentSubmit:   "执行",
			Description:      "一键修复当前飞书会话的断联状态：默认重连当前 bot runtime，并在当前实例空闲时重启 provider child；`/repair daemon` 会在需要时重启托管 daemon。",
			Examples:         []string{"/repair", "/repair daemon"},
			Options: []FeishuCommandOption{
				commandOption("/repair", "repair", "daemon", "重启 daemon", "仅当当前 daemon 已由安装 lifecycle manager 托管时重启 daemon。"),
			},
			ShowInHelp: true,
			ShowInMenu: true,
		},
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/repair", kind: ActionRepairCommand},
		},
		menuExact: []feishuCommandMatch{
			{alias: "repair", action: Action{Kind: ActionRepairCommand, Text: "/repair"}},
		},
		menuDynamic: []feishuCommandDynamicMenuMatch{
			{prefix: "repair_", kind: ActionRepairCommand, parseArgument: normalizeRepairMenuArgument},
			{prefix: "repair-", kind: ActionRepairCommand, parseArgument: normalizeRepairMenuArgument},
		},
	}
}
