package feishuapp

type Manifest struct {
	Scopes    ScopesImport       `json:"scopesImport"`
	Events    []EventRequirement `json:"events"`
	Menus     []MenuRequirement  `json:"menus"`
	Checklist []ChecklistSection `json:"checklist"`
}

type ScopesImport struct {
	Scopes PermissionScopes `json:"scopes"`
}

type PermissionScopes struct {
	Tenant []string `json:"tenant"`
	User   []string `json:"user"`
}

type EventRequirement struct {
	Event   string `json:"event"`
	Purpose string `json:"purpose,omitempty"`
}

type MenuRequirement struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type ChecklistSection struct {
	Area  string   `json:"area"`
	Items []string `json:"items"`
}

func DefaultManifest() Manifest {
	return Manifest{
		Scopes: ScopesImport{
			Scopes: PermissionScopes{
				Tenant: []string{
					"drive:drive",
					"im:message",
					"im:message.group_at_msg:readonly",
					"im:message.group_msg",
					"im:message.p2p_msg:readonly",
					"im:message.reactions:read",
					"im:message.reactions:write_only",
					"im:message:send_as_bot",
					"im:resource",
				},
				User: []string{},
			},
		},
		Events: []EventRequirement{
			{Event: "im.message.receive_v1", Purpose: "接收飞书中的文本和图片消息。"},
			{Event: "im.message.recalled_v1", Purpose: "用户撤回尚未执行的输入时，取消排队消息或 staged image。"},
			{Event: "im.message.reaction.created_v1", Purpose: "用户对待发送图片或排队消息加 reaction 时，表示取消。"},
			{Event: "application.bot.menu_v6", Purpose: "处理机器人菜单里的实例、状态、推理强度和执行权限快捷键。"},
			{Event: "card.action.trigger", Purpose: "处理选择卡片、approval request 卡片和其他交互回调。"},
		},
		Menus: []MenuRequirement{
			{Key: "list", Name: "列出实例", Description: "列出当前在线的 Codex 实例，并等待回复序号进行 attach。"},
			{Key: "status", Name: "当前状态", Description: "查看当前接管实例、会话、模型和排队状态。"},
			{Key: "threads", Name: "切换会话", Description: "列出当前实例的已知会话，并等待回复序号切换。"},
			{Key: "stop", Name: "停止当前执行", Description: "向当前 turn 发送 stop，并丢弃尚未发出的飞书队列。"},
			{Key: "reasonlow", Name: "推理 Low", Description: "把之后飞书发出的消息推理强度切到 low。"},
			{Key: "reasonmedium", Name: "推理 Medium", Description: "把之后飞书发出的消息推理强度切到 medium。"},
			{Key: "reasonhigh", Name: "推理 High", Description: "把之后飞书发出的消息推理强度切到 high。"},
			{Key: "reasonxhigh", Name: "推理 XHigh", Description: "把之后飞书发出的消息推理强度切到 xhigh。"},
			{Key: "access_full", Name: "执行权限 Full", Description: "把之后飞书发出的消息执行权限切到 full access。"},
			{Key: "access_confirm", Name: "执行权限 Confirm", Description: "把之后飞书发出的消息执行权限切到 confirm。"},
		},
		Checklist: []ChecklistSection{
			{
				Area: "凭证与基础信息",
				Items: []string{
					"在飞书开放平台记录 App ID 和 App Secret。",
					"在当前页面保存凭证后再做长连接验证。",
				},
			},
			{
				Area: "权限导入",
				Items: []string{
					"将 scopes import JSON 导入权限管理。",
					"如果需要 Markdown 预览，保持 drive:drive 权限启用。",
				},
			},
			{
				Area: "事件与回调",
				Items: []string{
					"手工订阅 manifest 里的全部事件。",
					"手工配置卡片回调与菜单回调，当前控制台不支持用 JSON 导入。",
				},
			},
			{
				Area: "机器人菜单与发布",
				Items: []string{
					"创建 manifest 里的全部机器人菜单 key。",
					"完成配置后发布机器人版本，再回到管理页观察连接状态。",
				},
			},
		},
	}
}
