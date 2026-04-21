package control

func sendFileCommandSpec() feishuCommandSpec {
	return feishuCommandSpec{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandSendFile,
			GroupID:          FeishuCommandGroupCommonTools,
			Title:            "发送文件",
			CanonicalSlash:   "/sendfile",
			CanonicalMenuKey: "sendfile",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "打开文件选择卡，从当前工作区挑一个文件发送到当前聊天。",
			ShowInHelp:       true,
			ShowInMenu:       true,
		},
		textExact: []feishuCommandMatch{
			{alias: "/sendfile", action: Action{Kind: ActionSendFile}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "sendfile", action: Action{Kind: ActionSendFile}},
		},
	}
}
