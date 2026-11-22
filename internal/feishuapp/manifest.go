package feishuapp

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
	manifest := Manifest{
		ScopeRequirements: []ScopeRequirement{
			{
				Scope:     "application:application:self_manage",
				ScopeType: "tenant",
				Feature:   "setup_app_management",
				Required:  true,
			},
			{
				Scope:     "drive:drive",
				ScopeType: "tenant",
				Feature:   "markdown_preview",
				Required:  true,
			},
			{
				Scope:          "docx:document",
				ScopeType:      "tenant",
				Feature:        "team_docx_resources",
				DegradeMessage: "机器人无法创建和维护飞书项目首页及 Docx 成果，可以在群里继续协作。",
			},
			{
				Scope:          "wiki:wiki",
				ScopeType:      "tenant",
				Feature:        "team_wiki_resources",
				DegradeMessage: "机器人无法使用知识库节点，可以选择云空间作为项目资料位置。",
			},
			{
				Scope:          "bitable:app",
				ScopeType:      "tenant",
				Feature:        "team_bitable_resources",
				DegradeMessage: "机器人无法读写多维表格，可以继续使用其他项目资料。",
			},
			{
				Scope:     "im:message:readonly",
				ScopeType: "tenant",
				Feature:   "core_message_flow",
				Required:  true,
			},
			{
				Scope:     "im:message.group_at_msg:readonly",
				ScopeType: "tenant",
				Feature:   "group_mentions",
				Required:  true,
			},
			{
				Scope:     "im:message.group_at_msg.include_bot:readonly",
				ScopeType: "tenant",
				Feature:   "group_mentions",
				Required:  true,
			},
			{
				Scope:     "im:message.group_msg",
				ScopeType: "tenant",
				Feature:   "project_group_messages",
				Required:  true,
			},
			{
				Scope:     "im:chat:readonly",
				ScopeType: "tenant",
				Feature:   "project_group_membership",
				Required:  true,
			},
			{
				Scope:     "im:chat:read",
				ScopeType: "tenant",
				Feature:   "project_group_membership",
				Required:  true,
			},
			{
				Scope:     "im:chat.members:bot_access",
				ScopeType: "tenant",
				Feature:   "project_group_membership",
				Required:  true,
			},
			{
				Scope:          "im:chat.members:write_only",
				ScopeType:      "tenant",
				Feature:        "project_member_invites",
				DegradeMessage: "负责人无法自动邀请队员，可以在飞书群里手动添加机器人。",
			},
			{
				Scope:          "im:chat.tabs:read",
				ScopeType:      "tenant",
				Feature:        "project_navigation",
				DegradeMessage: "机器人无法核对群标签页，可以从群里的项目导航卡打开项目。",
			},
			{
				Scope:          "im:chat.tabs:write_only",
				ScopeType:      "tenant",
				Feature:        "project_navigation",
				DegradeMessage: "机器人无法维护群标签页，可以从群里的项目导航卡打开项目。",
			},
			{
				Scope:     "im:message.p2p_msg:readonly",
				ScopeType: "tenant",
				Feature:   "p2p_chat",
				Required:  true,
			},
			{
				Scope:     "im:message.reactions:read",
				ScopeType: "tenant",
				Feature:   "reaction_feedback",
				Required:  true,
			},
			{
				Scope:     "im:message.reactions:write_only",
				ScopeType: "tenant",
				Feature:   "reaction_feedback",
				Required:  true,
			},
			{
				Scope:     "im:message:send_as_bot",
				ScopeType: "tenant",
				Feature:   "core_message_flow",
				Required:  true,
			},
			{
				Scope:     "im:resource:upload",
				ScopeType: "tenant",
				Feature:   "core_message_flow",
				Required:  true,
			},
		},
		Events: []EventRequirement{
			{
				Event:    "im.message.receive_v1",
				Purpose:  "接收私聊和项目群的消息，包括回复与附件",
				Feature:  "core_message_flow",
				Required: true,
			},
			{
				Event:    "im.message.recalled_v1",
				Purpose:  "同步撤回的项目输入及其处理状态",
				Feature:  "message_recall_sync",
				Required: true,
			},
			{
				Event:    "im.message.reaction.created_v1",
				Purpose:  "处理用户对消息的反馈动作",
				Feature:  "reaction_feedback",
				Required: true,
			},
			{
				Event:    "im.chat.member.bot.added_v1",
				Purpose:  "记录机器人加入项目群",
				Feature:  "project_group_membership",
				Required: true,
			},
			{
				Event:    "im.chat.member.bot.deleted_v1",
				Purpose:  "处理机器人从项目群移除",
				Feature:  "project_group_membership",
				Required: true,
			},
			{
				Event:    "im.chat.disbanded_v1",
				Purpose:  "处理项目群解散",
				Feature:  "project_group_membership",
				Required: true,
			},
			{
				Event:    "application.bot.menu_v6",
				Purpose:  "处理机器人菜单点击",
				Feature:  "bot_menu",
				Required: true,
			},
		},
		Callbacks: []CallbackRequirement{
			{
				Callback: "card.action.trigger",
				Purpose:  "处理负责人选择、项目导航、资料选择和待处理事项等卡片操作",
				Feature:  "interactive_cards",
				Required: true,
			},
		},
		Menus: append([]MenuRequirement(nil), teamMenus[:]...),
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
					"先补齐必需权限，再按使用的项目能力补齐其余权限。",
				},
			},
			{
				Area: "事件订阅",
				Items: []string{
					"打开“事件与回调”页，在“订阅方式”里确认长连接并保存。",
					"添加清单中的消息、机器人进退群、群解散和菜单事件。",
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

	permissions := PermissionScopes{Tenant: []string{}, User: []string{}}
	for _, requirement := range manifest.ScopeRequirements {
		switch requirement.ScopeType {
		case "tenant":
			permissions.Tenant = append(permissions.Tenant, requirement.Scope)
		case "user":
			permissions.User = append(permissions.User, requirement.Scope)
		}
	}
	manifest.Scopes = ScopesImport{Scopes: permissions}
	return manifest
}
