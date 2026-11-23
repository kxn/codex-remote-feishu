package control

func privateProjectCommandSpec() feishuCommandSpec {
	return feishuCommandSpec{definition: FeishuCommandDefinition{ID: FeishuCommandProject, GroupID: FeishuCommandGroupCommonTools, Title: "项目资料与分享", CanonicalSlash: "/project", CanonicalMenuKey: "project", ArgumentKind: FeishuCommandArgumentText, Description: "查看本人有权访问的项目与资料，或确认只分享指定内容。", Examples: []string{"/project"}, ShowInHelp: true, ShowInMenu: true}, textPrefixes: []feishuCommandPrefixMatch{{alias: "/project", kind: ActionPrivateProject}}, menuExact: []feishuCommandMatch{{alias: "project", action: Action{Kind: ActionPrivateProject, Text: "/project"}}}}
}
