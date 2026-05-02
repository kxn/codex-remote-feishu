package control

func reviewCommandSpec() feishuCommandSpec {
	return feishuCommandSpec{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandReview,
			GroupID:          FeishuCommandGroupCommonTools,
			Title:            "审阅代码变更",
			CanonicalSlash:   "/review",
			CanonicalMenuKey: "review",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "对当前会话所在 Git 工作区发起独立审阅；支持待提交内容和指定提交记录。",
			Examples:         []string{"/review", "/review uncommitted", "/review commit <sha>"},
			ShowInHelp:       true,
			ShowInMenu:       true,
			RecommendedMenu: &FeishuRecommendedMenu{
				Key:         "review",
				Name:        "审阅代码变更",
				Description: "对当前会话所在 Git 工作区发起独立审阅。",
			},
		},
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/review", kind: ActionReviewCommand},
		},
		menuExact: []feishuCommandMatch{
			{alias: "review", action: Action{Kind: ActionReviewCommand, Text: "/review"}},
			{alias: "reviewcommit", action: Action{Kind: ActionReviewCommand, Text: "/review commit"}},
			{alias: "review_uncommitted", action: Action{Kind: ActionReviewCommand, Text: "/review uncommitted"}},
			{alias: "reviewuncommitted", action: Action{Kind: ActionReviewCommand, Text: "/review uncommitted"}},
		},
		extraActionRoutes: []feishuCommandActionRoute{
			{kind: ActionReviewStartUncommitted, canonicalSlash: "/review uncommitted"},
			{kind: ActionReviewOpenCommitPicker, canonicalSlash: "/review commit"},
		},
	}
}
