package control

func goalCommandSpec() feishuCommandSpec {
	return feishuCommandSpec{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandGoal,
			GroupID:          FeishuCommandGroupSendSettings,
			Title:            "Goal 目标",
			CanonicalSlash:   "/goal",
			CanonicalMenuKey: "goal",
			ArgumentKind:     FeishuCommandArgumentText,
			ArgumentFormHint: "new|edit|pause|resume|clear",
			ArgumentFormNote: "查看/创建/编辑/暂停/恢复/清除当前会话 Goal。",
			ArgumentSubmit:   "打开",
			Description:      "查看当前会话的 Goal 状态；bare `/goal` 返回状态卡，`/goal new|edit <目标>` 创建/编辑，`/goal pause|resume|clear` 控制。",
			Examples:         []string{"/goal", "/goal new 完成登录流程重构", "/goal pause"},
			ShowInHelp:       true,
			ShowInMenu:       true,
		},
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/goal", kind: ActionGoalCommand},
		},
		menuExact: []feishuCommandMatch{
			{alias: "goal", action: Action{Kind: ActionGoalCommand, Text: "/goal"}},
		},
	}
}
