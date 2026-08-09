package control

func openCodeProfileCommandSpec() feishuCommandSpec {
	return feishuCommandSpec{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandOpenCodeProfile,
			GroupID:          FeishuCommandGroupSendSettings,
			Title:            "切换 OpenCode Profile",
			CanonicalSlash:   "/opencodeprofile",
			CanonicalMenuKey: "opencode_profile",
			ArgumentKind:     FeishuCommandArgumentText,
			ArgumentFormHint: "op_default",
			ArgumentFormNote: "输入已存在的 OpenCode Profile ID。",
			ArgumentSubmit:   "切换",
			Description:      "查看当前 OpenCode Profile；bare `/opencodeprofile` 会返回可切换的 Profile 下拉卡片。",
			Examples:         []string{"/opencodeprofile op_default"},
			ShowInHelp:       true,
			ShowInMenu:       true,
		},
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/opencodeprofile", kind: ActionOpenCodeProfileCommand},
		},
		menuExact: []feishuCommandMatch{
			{alias: "opencode_profile", action: Action{Kind: ActionOpenCodeProfileCommand, Text: "/opencodeprofile"}},
		},
	}
}
