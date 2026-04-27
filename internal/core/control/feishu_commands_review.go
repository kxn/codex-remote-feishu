package control

func reviewCommandSpec() feishuCommandSpec {
	return feishuCommandSpec{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandReview,
			GroupID:          FeishuCommandGroupCommonTools,
			Title:            "Review 待提交内容",
			CanonicalSlash:   "/review uncommitted",
			CanonicalMenuKey: "review_uncommitted",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "对当前会话所在 Git 工作区的待提交内容发起 detached 审阅；当前仅支持 `/review uncommitted`。",
			Examples:         []string{"/review uncommitted"},
			ShowInHelp:       true,
			ShowInMenu:       true,
			RecommendedMenu: &FeishuRecommendedMenu{
				Key:         "review_uncommitted",
				Name:        "Review 待提交内容",
				Description: "对当前会话所在 Git 工作区的待提交内容发起 detached 审阅。",
			},
		},
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/review", kind: ActionReviewCommand},
		},
		menuExact: []feishuCommandMatch{
			{alias: "review_uncommitted", action: Action{Kind: ActionReviewCommand, Text: "/review uncommitted"}},
			{alias: "reviewuncommitted", action: Action{Kind: ActionReviewCommand, Text: "/review uncommitted"}},
		},
	}
}
