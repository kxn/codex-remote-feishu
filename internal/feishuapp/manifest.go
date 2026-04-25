package feishuapp

import "github.com/kxn/codex-remote-feishu/internal/core/control"

type Manifest struct {
	Scopes    ScopesImport          `json:"scopesImport"`
	Events    []EventRequirement    `json:"events"`
	Callbacks []CallbackRequirement `json:"callbacks"`
	Menus     []MenuRequirement     `json:"menus"`
	Checklist []ChecklistSection    `json:"checklist"`
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

type CallbackRequirement struct {
	Callback string `json:"callback"`
	Purpose  string `json:"purpose,omitempty"`
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
		Events: []EventRequirement{
			{Event: "im.message.receive_v1", Purpose: "接收用户发给机器人的文本和图片消息"},
			{Event: "im.message.recalled_v1", Purpose: "处理用户撤回消息"},
			{Event: "im.message.reaction.created_v1", Purpose: "处理用户对消息的反馈动作"},
			{Event: "im.message.reaction.deleted_v1", Purpose: "处理用户对消息的反馈动作"},
			{Event: "application.bot.menu_v6", Purpose: "处理机器人菜单点击"},
		},
		Callbacks: []CallbackRequirement{
			{Callback: "card.action.trigger", Purpose: "处理卡片按钮和卡片交互回调"},
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
