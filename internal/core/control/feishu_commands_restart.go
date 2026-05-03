package control

func restartCommandSpec() feishuCommandSpec {
	return feishuCommandSpec{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandRestart,
			GroupID:          FeishuCommandGroupMaintenance,
			Title:            "重启运行时",
			CanonicalSlash:   "/restart",
			CanonicalMenuKey: "restart",
			ArgumentKind:     FeishuCommandArgumentChoice,
			ArgumentFormHint: "child",
			ArgumentFormNote: "例如 child。",
			ArgumentSubmit:   "执行",
			Description:      "查看可用的运行时重启操作；`/restart child` 重启当前 attached instance 的 provider child，不重启 daemon。",
			Examples:         []string{"/restart", "/restart child"},
			Options: []FeishuCommandOption{
				commandOption("/restart", "restart", "child", "child", "重启当前 attached instance 的 provider child。"),
			},
			ShowInHelp: true,
			ShowInMenu: true,
		},
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/restart", kind: ActionRestartCommand},
		},
		menuExact: []feishuCommandMatch{
			{alias: "restart", action: Action{Kind: ActionRestartCommand, Text: "/restart"}},
		},
	}
}
