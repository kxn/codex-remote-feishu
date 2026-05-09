package feishuapp

import "github.com/kxn/codex-remote-feishu/internal/core/control"

type Manifest struct {
	Scopes            ScopesImport          `json:"scopesImport"`
	ScopeRequirements []ScopeRequirement    `json:"scopeRequirements,omitempty"`
	Events            []EventRequirement    `json:"events"`
	Callbacks         []CallbackRequirement `json:"callbacks"`
	Menus             []MenuRequirement     `json:"menus"`
	Checklist         []ChecklistSection    `json:"checklist"`
}

type ScopesImport struct {
	Scopes PermissionScopes `json:"scopes"`
}

type PermissionScopes struct {
	Tenant []string `json:"tenant"`
	User   []string `json:"user"`
}

type ScopeRequirement struct {
	Scope          string `json:"scope"`
	ScopeType      string `json:"scopeType,omitempty"`
	Feature        string `json:"feature,omitempty"`
	Required       bool   `json:"required"`
	DegradeMessage string `json:"degradeMessage,omitempty"`
}

type EventRequirement struct {
	Event          string `json:"event"`
	Purpose        string `json:"purpose,omitempty"`
	Feature        string `json:"feature,omitempty"`
	Required       bool   `json:"required"`
	DegradeMessage string `json:"degradeMessage,omitempty"`
}

type CallbackRequirement struct {
	Callback       string `json:"callback"`
	Purpose        string `json:"purpose,omitempty"`
	Feature        string `json:"feature,omitempty"`
	Required       bool   `json:"required"`
	DegradeMessage string `json:"degradeMessage,omitempty"`
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
	menus := control.FeishuRecommendedMenus()
	manifestMenus := make([]MenuRequirement, 0, len(menus))
	for _, menu := range menus {
		manifestMenus = append(manifestMenus, MenuRequirement{
			Key:         menu.Key,
			Name:        menu.Name,
			Description: menu.Description,
		})
	}
	return Manifest{
		Scopes: ScopesImport{
			Scopes: PermissionScopes{
				Tenant: []string{
					"drive:drive",
					"base:app:create",
					"bitable:app",
					"im:datasync.feed_card.time_sensitive:write",
					"im:message",
					"im:message.group_at_msg:readonly",
					"im:message.p2p_msg:readonly",
					"im:message.reactions:read",
					"im:message.reactions:write_only",
					"im:message:send_as_bot",
					"im:resource",
				},
				User: []string{},
			},
		},
		ScopeRequirements: []ScopeRequirement{
			{
				Scope:          "drive:drive",
				ScopeType:      "tenant",
				Feature:        "markdown_preview",
				Required:       false,
				DegradeMessage: "缺少该权限时，本地 Markdown 文件不会自动转成飞书预览链接。",
			},
			{
				Scope:          "base:app:create",
				ScopeType:      "tenant",
				Feature:        "cron_bitable",
				Required:       false,
				DegradeMessage: "缺少该权限时，无法自动创建 /cron 所需的多维表格应用。",
			},
			{
				Scope:          "bitable:app",
				ScopeType:      "tenant",
				Feature:        "cron_bitable",
				Required:       false,
				DegradeMessage: "缺少该权限时，无法在飞书里直接管理 /cron 对应的多维表格。",
			},
			{
				Scope:          "im:datasync.feed_card.time_sensitive:write",
				ScopeType:      "tenant",
				Feature:        "time_sensitive_indicator",
				Required:       false,
				DegradeMessage: "缺少该权限时，飞书客户端里的“等待你输入”提醒不会显示。",
			},
			{
				Scope:     "im:message",
				ScopeType: "tenant",
				Feature:   "core_message_flow",
				Required:  true,
			},
			{
				Scope:          "im:message.group_at_msg:readonly",
				ScopeType:      "tenant",
				Feature:        "group_mentions",
				Required:       false,
				DegradeMessage: "缺少该权限时，机器人无法稳定处理群聊 @ 消息。",
			},
			{
				Scope:          "im:message.p2p_msg:readonly",
				ScopeType:      "tenant",
				Feature:        "p2p_chat",
				Required:       false,
				DegradeMessage: "缺少该权限时，机器人无法在单聊中接收消息。",
			},
			{
				Scope:          "im:message.reactions:read",
				ScopeType:      "tenant",
				Feature:        "reaction_feedback",
				Required:       false,
				DegradeMessage: "缺少该权限时，机器人无法读取用户的消息 reaction 反馈。",
			},
			{
				Scope:          "im:message.reactions:write_only",
				ScopeType:      "tenant",
				Feature:        "reaction_feedback",
				Required:       false,
				DegradeMessage: "缺少该权限时，机器人无法写入消息 reaction 反馈。",
			},
			{
				Scope:     "im:message:send_as_bot",
				ScopeType: "tenant",
				Feature:   "core_message_flow",
				Required:  true,
			},
			{
				Scope:     "im:resource",
				ScopeType: "tenant",
				Feature:   "core_message_flow",
				Required:  true,
			},
		},
		Events: []EventRequirement{
			{
				Event:    "im.message.receive_v1",
				Purpose:  "接收用户发给机器人的文本和图片消息",
				Feature:  "core_message_flow",
				Required: true,
			},
			{
				Event:          "im.message.recalled_v1",
				Purpose:        "处理用户撤回消息",
				Feature:        "message_recall_sync",
				Required:       false,
				DegradeMessage: "缺少该事件时，用户撤回消息不会同步取消已排队输入。",
			},
			{
				Event:          "im.message.reaction.created_v1",
				Purpose:        "处理用户对消息的反馈动作",
				Feature:        "reaction_feedback",
				Required:       false,
				DegradeMessage: "缺少该事件时，机器人无法响应用户新增的消息 reaction 反馈。",
			},
			{
				Event:          "im.message.reaction.deleted_v1",
				Purpose:        "处理用户对消息的反馈动作",
				Feature:        "reaction_feedback",
				Required:       false,
				DegradeMessage: "缺少该事件时，机器人无法响应用户撤销的消息 reaction 反馈。",
			},
			{
				Event:          "application.bot.menu_v6",
				Purpose:        "处理机器人菜单点击",
				Feature:        "bot_menu",
				Required:       false,
				DegradeMessage: "缺少该事件时，飞书里的机器人菜单快捷入口不可用。",
			},
		},
		Callbacks: []CallbackRequirement{
			{
				Callback: "card.action.trigger",
				Purpose:  "处理卡片按钮和卡片交互回调",
				Feature:  "interactive_cards",
				Required: true,
			},
		},
		Menus: manifestMenus,
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
					"打开“权限管理”里的“批量导入/导出权限”，粘贴 scopes import JSON。",
					"点击“保存并申请开通”，再回到当前页面继续。",
					"如果需要在单聊列表里标记“等待你输入”的机器人，保持 im:datasync.feed_card.time_sensitive:write 启用。",
					"如果需要 Markdown 预览，保持 drive:drive 权限启用。",
				},
			},
			{
				Area: "事件订阅",
				Items: []string{
					"打开“事件与回调”页，在“订阅方式”里确认长连接并保存。",
					"手工订阅 manifest 里的消息事件和菜单事件。",
				},
			},
			{
				Area: "回调配置",
				Items: []string{
					"在“事件与回调”页的“回调配置”里，将“回调订阅方式”设为长连接。",
					"确认 manifest 里的卡片回调项已经配置完成。",
					"当前版本不需要填写 HTTP 回调 URL。",
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
