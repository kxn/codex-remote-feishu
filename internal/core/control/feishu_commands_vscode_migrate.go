package control

func vscodeMigrateCommandSpec() feishuCommandSpec {
	return feishuCommandSpec{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandVSCodeMigrate,
			GroupID:          FeishuCommandGroupMaintenance,
			Title:            "VS Code 迁移",
			CanonicalSlash:   "/vscode-migrate",
			CanonicalMenuKey: "vscode-migrate",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "打开 VS Code 迁移页，检查是否需要迁移到当前统一的 managed shim 接入方式。",
			ShowInHelp:       true,
			ShowInMenu:       false,
		},
		textExact: []feishuCommandMatch{
			{alias: "/vscode-migrate", action: Action{Kind: ActionVSCodeMigrateCommand, Text: "/vscode-migrate"}},
		},
	}
}
