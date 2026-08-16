package control

func gpuStatusCommandSpec() feishuCommandSpec {
	return feishuCommandSpec{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandGPUStatus,
			GroupID:          FeishuCommandGroupCommonTools,
			Title:            "GPU 状态",
			CanonicalSlash:   "/gpu",
			CanonicalMenuKey: "gpu",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "直接读取本机 nvidia-smi 并显示 GPU 负载、温度、显存和功耗；不经过 AI。",
			ShowInHelp:       true,
			ShowInMenu:       true,
			RecommendedMenu: &FeishuRecommendedMenu{
				Key:         "gpu",
				Name:        "GPU 状态",
				Description: "直接读取本机 GPU 状态，不经过 AI。",
			},
		},
		textExact: []feishuCommandMatch{
			{alias: "/gpu", action: Action{Kind: ActionGPUStatus, Text: "/gpu"}},
			{alias: "/nvidia-smi", action: Action{Kind: ActionGPUStatus, Text: "/gpu"}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "gpu", action: Action{Kind: ActionGPUStatus, Text: "/gpu"}},
			{alias: "nvidiasmi", action: Action{Kind: ActionGPUStatus, Text: "/gpu"}},
		},
	}
}
