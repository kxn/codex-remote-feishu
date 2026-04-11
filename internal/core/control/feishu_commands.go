package control

import "strings"

const (
	FeishuCommandGroupCurrentWork  = "current_work"
	FeishuCommandGroupSendSettings = "send_settings"
	FeishuCommandGroupSwitchTarget = "switch_target"
	FeishuCommandGroupMaintenance  = "maintenance"
	FeishuCommandList              = "list"
	FeishuCommandStatus            = "status"
	FeishuCommandUse               = "use"
	FeishuCommandUseAll            = "useall"
	FeishuCommandNew               = "new"
	FeishuCommandFollow            = "follow"
	FeishuCommandDetach            = "detach"
	FeishuCommandStop              = "stop"
	FeishuCommandMode              = "mode"
	FeishuCommandAutoContinue      = "autocontinue"
	FeishuCommandModel             = "model"
	FeishuCommandReasoning         = "reasoning"
	FeishuCommandAccess            = "access"
	FeishuCommandHelp              = "help"
	FeishuCommandMenu              = "menu"
	FeishuCommandDebug             = "debug"
	FeishuCommandUpgrade           = "upgrade"
)

type FeishuCommandArgumentKind string

const (
	FeishuCommandArgumentNone   FeishuCommandArgumentKind = "none"
	FeishuCommandArgumentChoice FeishuCommandArgumentKind = "choice"
	FeishuCommandArgumentText   FeishuCommandArgumentKind = "text"
)

type FeishuCommandGroup struct {
	ID          string
	Title       string
	Description string
}

type FeishuCommandOption struct {
	Value       string
	Label       string
	Description string
	CommandText string
	MenuKey     string
}

type FeishuCommandDefinition struct {
	ID               string
	GroupID          string
	Title            string
	CanonicalSlash   string
	CanonicalMenuKey string
	ArgumentKind     FeishuCommandArgumentKind
	ArgumentFormHint string
	ArgumentFormNote string
	ArgumentSubmit   string
	Description      string
	Examples         []string
	Options          []FeishuCommandOption
	ShowInHelp       bool
	ShowInMenu       bool
	RecommendedMenu  *FeishuRecommendedMenu
}

type feishuCommandMatch struct {
	alias  string
	action Action
}

type feishuCommandPrefixMatch struct {
	alias string
	kind  ActionKind
}

type feishuCommandDynamicMenuMatch struct {
	prefix string
	kind   ActionKind
	build  func(string) (string, bool)
}

type feishuCommandSpec struct {
	definition   FeishuCommandDefinition
	textExact    []feishuCommandMatch
	textPrefixes []feishuCommandPrefixMatch
	menuExact    []feishuCommandMatch
	menuDynamic  []feishuCommandDynamicMenuMatch
}

type FeishuRecommendedMenu struct {
	Key         string
	Name        string
	Description string
}

var feishuCommandGroups = []FeishuCommandGroup{
	{
		ID:          FeishuCommandGroupCurrentWork,
		Title:       "当前工作",
		Description: "steady-state 主路径：停止当前执行，或在 normal 模式下准备新会话。",
	},
	{
		ID:          FeishuCommandGroupSendSettings,
		Title:       "发送设置",
		Description: "调整后续飞书消息的推理强度、模型和执行权限。",
	},
	{
		ID:          FeishuCommandGroupSwitchTarget,
		Title:       "切换目标",
		Description: "查看列表、切换会话、切换工作目标，或解除当前接管。",
	},
	{
		ID:          FeishuCommandGroupMaintenance,
		Title:       "低频与维护",
		Description: "查看状态、切换模式、auto-continue、帮助和调试入口。",
	},
}

var feishuCommandSpecs = []feishuCommandSpec{
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandStop,
			GroupID:          FeishuCommandGroupCurrentWork,
			Title:            "停止当前执行",
			CanonicalSlash:   "/stop",
			CanonicalMenuKey: "stop",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "中断当前执行，并丢弃飞书侧尚未发送的排队输入。",
			ShowInHelp:       true,
			ShowInMenu:       true,
			RecommendedMenu: &FeishuRecommendedMenu{
				Key:         "stop",
				Name:        "停止当前执行",
				Description: "中断当前执行，并丢弃飞书侧尚未发送的排队输入。",
			},
		},
		textExact: []feishuCommandMatch{
			{alias: "/stop", action: Action{Kind: ActionStop}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "stop", action: Action{Kind: ActionStop}},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandNew,
			GroupID:          FeishuCommandGroupCurrentWork,
			Title:            "新建会话",
			CanonicalSlash:   "/new",
			CanonicalMenuKey: "new",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "仅 normal 模式可用：准备一个新会话，下一条消息会作为首条输入。",
			ShowInHelp:       true,
			ShowInMenu:       true,
			RecommendedMenu: &FeishuRecommendedMenu{
				Key:         "new",
				Name:        "新建会话",
				Description: "仅 normal 模式可用：准备一个新会话，下一条消息会作为首条输入。",
			},
		},
		textExact: []feishuCommandMatch{
			{alias: "/new", action: Action{Kind: ActionNewThread}},
			{alias: "/newinstance", action: Action{Kind: ActionRemovedCommand, Text: "/newinstance"}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "new", action: Action{Kind: ActionNewThread}},
			{alias: "newthread", action: Action{Kind: ActionNewThread}},
			{alias: "newinstance", action: Action{Kind: ActionRemovedCommand, Text: "new_instance"}},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandReasoning,
			GroupID:          FeishuCommandGroupSendSettings,
			Title:            "推理强度",
			CanonicalSlash:   "/reasoning",
			CanonicalMenuKey: "reasoning",
			ArgumentKind:     FeishuCommandArgumentChoice,
			ArgumentFormHint: "high",
			ArgumentFormNote: "输入 low / medium / high / xhigh / clear。",
			ArgumentSubmit:   "应用",
			Description:      "查看当前推理强度；bare `/reasoning` 会返回可选参数卡片。",
			Examples:         []string{"/reasoning high", "/reasoning clear"},
			Options: []FeishuCommandOption{
				commandOption("/reasoning", "reasoning", "low", "low", "把后续飞书消息切到 low 推理，直到 clear 或接管清理。"),
				commandOption("/reasoning", "reasoning", "medium", "medium", "把后续飞书消息切到 medium 推理，直到 clear 或接管清理。"),
				commandOption("/reasoning", "reasoning", "high", "high", "把后续飞书消息切到 high 推理，直到 clear 或接管清理。"),
				commandOption("/reasoning", "reasoning", "xhigh", "xhigh", "把后续飞书消息切到 xhigh 推理，直到 clear 或接管清理。"),
				commandOption("/reasoning", "reasoning", "clear", "clear", "清除飞书临时推理强度覆盖。"),
			},
			ShowInHelp: true,
			ShowInMenu: true,
			RecommendedMenu: &FeishuRecommendedMenu{
				Key:         "reasoning",
				Name:        "推理强度",
				Description: "打开推理强度参数卡；如果知道完整 key，也可直接使用 `reasoning_high` 这类直达入口。",
			},
		},
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/reasoning", kind: ActionReasoningCommand},
			{alias: "/effort", kind: ActionReasoningCommand},
		},
		menuExact: []feishuCommandMatch{
			{alias: "reasoning", action: Action{Kind: ActionReasoningCommand, Text: "/reasoning"}},
			{alias: "reasonlow", action: Action{Kind: ActionReasoningCommand, Text: "/reasoning low"}},
			{alias: "reasonmedium", action: Action{Kind: ActionReasoningCommand, Text: "/reasoning medium"}},
			{alias: "reasonhigh", action: Action{Kind: ActionReasoningCommand, Text: "/reasoning high"}},
			{alias: "reasonxhigh", action: Action{Kind: ActionReasoningCommand, Text: "/reasoning xhigh"}},
		},
		menuDynamic: []feishuCommandDynamicMenuMatch{
			{prefix: "reasoning_", kind: ActionReasoningCommand, build: buildMenuReasoningText},
			{prefix: "reasoning-", kind: ActionReasoningCommand, build: buildMenuReasoningText},
			{prefix: "reason_", kind: ActionReasoningCommand, build: buildMenuReasoningText},
			{prefix: "reason-", kind: ActionReasoningCommand, build: buildMenuReasoningText},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandModel,
			GroupID:          FeishuCommandGroupSendSettings,
			Title:            "模型",
			CanonicalSlash:   "/model",
			CanonicalMenuKey: "model",
			ArgumentKind:     FeishuCommandArgumentText,
			ArgumentFormHint: "gpt-5.4 high",
			ArgumentFormNote: "输入模型名，或输入“模型名 推理强度”。",
			ArgumentSubmit:   "应用",
			Description:      "查看当前模型配置；bare `/model` 会给出常见模型与手动输入入口。",
			Examples:         []string{"/model gpt-5.4", "/model gpt-5.4 high", "/model clear"},
			ShowInHelp:       true,
			ShowInMenu:       true,
			RecommendedMenu: &FeishuRecommendedMenu{
				Key:         "model",
				Name:        "模型",
				Description: "打开模型卡片；如果知道完整 key，也可直接使用 `model_gpt-5.4` 这类直达入口。",
			},
		},
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/model", kind: ActionModelCommand},
		},
		menuExact: []feishuCommandMatch{
			{alias: "model", action: Action{Kind: ActionModelCommand, Text: "/model"}},
		},
		menuDynamic: []feishuCommandDynamicMenuMatch{
			{prefix: "model_", kind: ActionModelCommand, build: buildMenuModelText},
			{prefix: "model-", kind: ActionModelCommand, build: buildMenuModelText},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandAccess,
			GroupID:          FeishuCommandGroupSendSettings,
			Title:            "执行权限",
			CanonicalSlash:   "/access",
			CanonicalMenuKey: "access",
			ArgumentKind:     FeishuCommandArgumentChoice,
			ArgumentFormHint: "confirm",
			ArgumentFormNote: "输入 full / confirm / clear。",
			ArgumentSubmit:   "应用",
			Description:      "查看当前执行权限；bare `/access` 会返回可选参数卡片。",
			Examples:         []string{"/access confirm", "/access clear"},
			Options: []FeishuCommandOption{
				commandOption("/access", "access", "full", "full", "把后续飞书消息切到 full 权限，直到 clear 或接管清理。"),
				commandOption("/access", "access", "confirm", "confirm", "把后续飞书消息切到 confirm 权限，直到 clear 或接管清理。"),
				commandOption("/access", "access", "clear", "clear", "恢复飞书默认执行权限。"),
			},
			ShowInHelp: true,
			ShowInMenu: true,
			RecommendedMenu: &FeishuRecommendedMenu{
				Key:         "access",
				Name:        "执行权限",
				Description: "打开执行权限参数卡；如果知道完整 key，也可直接使用 `access_confirm` 这类直达入口。",
			},
		},
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/access", kind: ActionAccessCommand},
			{alias: "/approval", kind: ActionAccessCommand},
		},
		menuExact: []feishuCommandMatch{
			{alias: "access", action: Action{Kind: ActionAccessCommand, Text: "/access"}},
			{alias: "approval", action: Action{Kind: ActionAccessCommand, Text: "/access"}},
			{alias: "accessfull", action: Action{Kind: ActionAccessCommand, Text: "/access full"}},
			{alias: "approvalfull", action: Action{Kind: ActionAccessCommand, Text: "/access full"}},
			{alias: "accessconfirm", action: Action{Kind: ActionAccessCommand, Text: "/access confirm"}},
			{alias: "approvalconfirm", action: Action{Kind: ActionAccessCommand, Text: "/access confirm"}},
		},
		menuDynamic: []feishuCommandDynamicMenuMatch{
			{prefix: "access_", kind: ActionAccessCommand, build: buildMenuAccessText},
			{prefix: "access-", kind: ActionAccessCommand, build: buildMenuAccessText},
			{prefix: "approval_", kind: ActionAccessCommand, build: buildMenuAccessText},
			{prefix: "approval-", kind: ActionAccessCommand, build: buildMenuAccessText},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandList,
			GroupID:          FeishuCommandGroupSwitchTarget,
			Title:            "查看列表",
			CanonicalSlash:   "/list",
			CanonicalMenuKey: "list",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "默认列出最近 5 个可用工作区，并可卡片内展开全部；切到 `vscode` 模式后列出在线实例，并从 follow-first 路径接管。",
			ShowInHelp:       true,
			ShowInMenu:       true,
		},
		textExact: []feishuCommandMatch{
			{alias: "/list", action: Action{Kind: ActionListInstances}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "list", action: Action{Kind: ActionListInstances}},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandUse,
			GroupID:          FeishuCommandGroupSwitchTarget,
			Title:            "接管会话",
			CanonicalSlash:   "/use",
			CanonicalMenuKey: "use",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "展示最近会话；normal attached 时只看当前工作区并附带“显示全部”按钮，normal detached 时等同 `/useall` 的最近工作区总览，vscode detached 时需先 `/list`。",
			Examples:         []string{"/useall"},
			ShowInHelp:       true,
			ShowInMenu:       true,
		},
		textExact: []feishuCommandMatch{
			{alias: "/use", action: Action{Kind: ActionShowThreads}},
			{alias: "/threads", action: Action{Kind: ActionShowThreads}},
			{alias: "/sessions", action: Action{Kind: ActionShowThreads}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "use", action: Action{Kind: ActionShowThreads}},
			{alias: "threads", action: Action{Kind: ActionShowThreads}},
			{alias: "sessions", action: Action{Kind: ActionShowThreads}},
			{alias: "showthreads", action: Action{Kind: ActionShowThreads}},
			{alias: "showsessions", action: Action{Kind: ActionShowThreads}},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandUseAll,
			GroupID:          FeishuCommandGroupSwitchTarget,
			Title:            "全部会话",
			CanonicalSlash:   "/useall",
			CanonicalMenuKey: "useall",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "展示跨工作区会话总览；normal 模式下默认先显示最近 5 个工作区并可卡片内展开全部，vscode attached 时仍只看当前实例，detached 时需先 `/list`。",
			ShowInHelp:       true,
			ShowInMenu:       true,
		},
		textExact: []feishuCommandMatch{
			{alias: "/useall", action: Action{Kind: ActionShowAllThreads}},
			{alias: "/sessionsall", action: Action{Kind: ActionShowAllThreads}},
			{alias: "/sessions/all", action: Action{Kind: ActionShowAllThreads}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "useall", action: Action{Kind: ActionShowAllThreads}},
			{alias: "threadsall", action: Action{Kind: ActionShowAllThreads}},
			{alias: "sessionsall", action: Action{Kind: ActionShowAllThreads}},
			{alias: "showallthreads", action: Action{Kind: ActionShowAllThreads}},
			{alias: "showallsessions", action: Action{Kind: ActionShowAllThreads}},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandDetach,
			GroupID:          FeishuCommandGroupSwitchTarget,
			Title:            "解除接管",
			CanonicalSlash:   "/detach",
			CanonicalMenuKey: "detach",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "解除当前接管，停止把后续输入发送到当前实例。",
			ShowInHelp:       true,
			ShowInMenu:       true,
		},
		textExact: []feishuCommandMatch{
			{alias: "/detach", action: Action{Kind: ActionDetach}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "detach", action: Action{Kind: ActionDetach}},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandFollow,
			GroupID:          FeishuCommandGroupSwitchTarget,
			Title:            "跟随当前",
			CanonicalSlash:   "/follow",
			CanonicalMenuKey: "follow",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "仅 `vscode` 模式可用：跟随当前 VS Code 聚焦会话；normal 模式请改走 `/use`、`/new` 或 `/mode vscode`。",
			ShowInHelp:       true,
			ShowInMenu:       true,
		},
		textExact: []feishuCommandMatch{
			{alias: "/follow", action: Action{Kind: ActionFollowLocal}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "follow", action: Action{Kind: ActionFollowLocal}},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandStatus,
			GroupID:          FeishuCommandGroupMaintenance,
			Title:            "当前状态",
			CanonicalSlash:   "/status",
			CanonicalMenuKey: "status",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "查看当前模式、接管对象类型、输入目标和飞书侧临时覆盖。",
			ShowInHelp:       true,
			ShowInMenu:       true,
		},
		textExact: []feishuCommandMatch{
			{alias: "/status", action: Action{Kind: ActionStatus}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "status", action: Action{Kind: ActionStatus}},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandMode,
			GroupID:          FeishuCommandGroupMaintenance,
			Title:            "切换模式",
			CanonicalSlash:   "/mode",
			CanonicalMenuKey: "mode",
			ArgumentKind:     FeishuCommandArgumentChoice,
			ArgumentFormHint: "vscode",
			ArgumentFormNote: "输入 normal 或 vscode。",
			ArgumentSubmit:   "切换",
			Description:      "查看当前模式；bare `/mode` 会返回 normal / vscode 切换卡片。",
			Examples:         []string{"/mode normal", "/mode vscode"},
			Options: []FeishuCommandOption{
				commandOption("/mode", "mode", "normal", "normal", "切换到 normal 模式。"),
				commandOption("/mode", "mode", "vscode", "vscode", "切换到 vscode 模式。"),
			},
			ShowInHelp: true,
			ShowInMenu: true,
		},
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/mode", kind: ActionModeCommand},
		},
		menuExact: []feishuCommandMatch{
			{alias: "mode", action: Action{Kind: ActionModeCommand, Text: "/mode"}},
		},
		menuDynamic: []feishuCommandDynamicMenuMatch{
			{prefix: "mode_", kind: ActionModeCommand, build: buildMenuModeText},
			{prefix: "mode-", kind: ActionModeCommand, build: buildMenuModeText},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandAutoContinue,
			GroupID:          FeishuCommandGroupMaintenance,
			Title:            "自动续跑",
			CanonicalSlash:   "/autocontinue",
			CanonicalMenuKey: "autocontinue",
			ArgumentKind:     FeishuCommandArgumentChoice,
			ArgumentFormHint: "on",
			ArgumentFormNote: "输入 on 或 off。",
			ArgumentSubmit:   "应用",
			Description:      "查看当前 auto-continue 状态；bare `/autocontinue` 会返回 on / off 切换卡片。",
			Examples:         []string{"/autocontinue on", "/autocontinue off"},
			Options: []FeishuCommandOption{
				commandOption("/autocontinue", "autocontinue", "on", "on", "开启当前飞书会话的 auto-continue。"),
				commandOption("/autocontinue", "autocontinue", "off", "off", "关闭当前飞书会话的 auto-continue。"),
			},
			ShowInHelp: true,
			ShowInMenu: true,
		},
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/autocontinue", kind: ActionAutoContinueCommand},
		},
		menuExact: []feishuCommandMatch{
			{alias: "autocontinue", action: Action{Kind: ActionAutoContinueCommand, Text: "/autocontinue"}},
		},
		menuDynamic: []feishuCommandDynamicMenuMatch{
			{prefix: "autocontinue_", kind: ActionAutoContinueCommand, build: buildMenuAutoContinueText},
			{prefix: "autocontinue-", kind: ActionAutoContinueCommand, build: buildMenuAutoContinueText},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandHelp,
			GroupID:          FeishuCommandGroupMaintenance,
			Title:            "Slash 帮助",
			CanonicalSlash:   "/help",
			CanonicalMenuKey: "help",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "查看 canonical slash command 列表和示例。",
			ShowInHelp:       true,
			ShowInMenu:       true,
		},
		textExact: []feishuCommandMatch{
			{alias: "/help", action: Action{Kind: ActionShowCommandHelp}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "help", action: Action{Kind: ActionShowCommandHelp}},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandMenu,
			GroupID:          FeishuCommandGroupMaintenance,
			Title:            "命令菜单",
			CanonicalSlash:   "/menu",
			CanonicalMenuKey: "menu",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "打开阶段感知的命令菜单首页。",
			ShowInHelp:       true,
			ShowInMenu:       false,
			RecommendedMenu: &FeishuRecommendedMenu{
				Key:         "menu",
				Name:        "命令菜单",
				Description: "打开阶段感知的命令菜单首页。",
			},
		},
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/menu", kind: ActionShowCommandMenu},
		},
		menuExact: []feishuCommandMatch{
			{alias: "menu", action: Action{Kind: ActionShowCommandMenu}},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandUpgrade,
			GroupID:          FeishuCommandGroupMaintenance,
			Title:            "升级",
			CanonicalSlash:   "/upgrade",
			CanonicalMenuKey: "upgrade",
			ArgumentKind:     FeishuCommandArgumentChoice,
			ArgumentFormHint: "latest",
			ArgumentFormNote: "输入 latest 或 local。",
			ArgumentSubmit:   "执行",
			Description:      "查看升级状态；`/upgrade latest` 检查或继续 release 升级，`/upgrade local` 使用固定本地 artifact 发起升级。",
			Examples:         []string{"/upgrade latest", "/upgrade local"},
			Options: []FeishuCommandOption{
				commandOption("/upgrade", "upgrade", "latest", "latest", "检查或继续升级到当前 track 的最新 release。"),
				commandOption("/upgrade", "upgrade", "local", "local", "使用固定本地 artifact 发起升级。"),
			},
			ShowInHelp: true,
			ShowInMenu: true,
		},
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/upgrade", kind: ActionUpgradeCommand},
		},
		menuExact: []feishuCommandMatch{
			{alias: "upgrade", action: Action{Kind: ActionUpgradeCommand, Text: "/upgrade"}},
		},
		menuDynamic: []feishuCommandDynamicMenuMatch{
			{prefix: "upgrade_", kind: ActionUpgradeCommand, build: buildMenuUpgradeText},
			{prefix: "upgrade-", kind: ActionUpgradeCommand, build: buildMenuUpgradeText},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandDebug,
			GroupID:          FeishuCommandGroupMaintenance,
			Title:            "调试",
			CanonicalSlash:   "/debug",
			CanonicalMenuKey: "debug",
			ArgumentKind:     FeishuCommandArgumentText,
			ArgumentFormHint: "admin",
			ArgumentFormNote: "例如 admin、track、track alpha、track beta、track production。",
			ArgumentSubmit:   "执行",
			Description:      "查看调试状态，切换当前 release track，或生成临时管理页外链。",
			Examples:         []string{"/debug", "/debug admin", "/debug track beta"},
			ShowInHelp:       true,
			ShowInMenu:       true,
		},
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/debug", kind: ActionDebugCommand},
		},
		menuExact: []feishuCommandMatch{
			{alias: "debug", action: Action{Kind: ActionDebugCommand, Text: "/debug"}},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               "killinstance_legacy",
			GroupID:          FeishuCommandGroupMaintenance,
			Title:            "旧版 killinstance",
			CanonicalSlash:   "/killinstance",
			CanonicalMenuKey: "killinstance",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "历史兼容入口，提示改用 /detach。",
			ShowInHelp:       false,
			ShowInMenu:       false,
		},
		textExact: []feishuCommandMatch{
			{alias: "/killinstance", action: Action{Kind: ActionRemovedCommand, Text: "/killinstance"}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "killinstance", action: Action{Kind: ActionRemovedCommand, Text: "kill_instance"}},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               "vscode_migrate_hidden",
			GroupID:          FeishuCommandGroupMaintenance,
			Title:            "VS Code 迁移",
			CanonicalSlash:   "/vscode-migrate",
			CanonicalMenuKey: "vscode-migrate",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "内部迁移入口。",
			ShowInHelp:       false,
			ShowInMenu:       false,
		},
		textExact: []feishuCommandMatch{
			{alias: "/vscode-migrate", action: Action{Kind: ActionVSCodeMigrate}},
		},
	},
}

func ParseFeishuTextAction(text string) (Action, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return Action{}, false
	}
	fields := strings.Fields(trimmed)
	if len(fields) > 0 {
		first := strings.ToLower(fields[0])
		for _, spec := range feishuCommandSpecs {
			for _, prefix := range spec.textPrefixes {
				if first == prefix.alias {
					return Action{Kind: prefix.kind, Text: trimmed}, true
				}
			}
		}
	}
	for _, spec := range feishuCommandSpecs {
		for _, match := range spec.textExact {
			if trimmed == match.alias {
				return match.action, true
			}
		}
	}
	return Action{}, false
}

func ParseFeishuMenuAction(eventKey string) (Action, bool) {
	trimmed := strings.TrimSpace(eventKey)
	if trimmed == "" {
		return Action{}, false
	}
	lower := strings.ToLower(trimmed)
	for _, spec := range feishuCommandSpecs {
		for _, dynamic := range spec.menuDynamic {
			if strings.HasPrefix(lower, dynamic.prefix) {
				text, ok := dynamic.build(trimmed[len(dynamic.prefix):])
				if !ok {
					return Action{}, false
				}
				return Action{Kind: dynamic.kind, Text: text}, true
			}
		}
	}
	normalized := NormalizeFeishuMenuEventKey(trimmed)
	for _, spec := range feishuCommandSpecs {
		for _, match := range spec.menuExact {
			if normalized == NormalizeFeishuMenuEventKey(match.alias) {
				return match.action, true
			}
		}
	}
	return Action{}, false
}

func NormalizeFeishuMenuEventKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func FeishuCommandGroups() []FeishuCommandGroup {
	groups := make([]FeishuCommandGroup, 0, len(feishuCommandGroups))
	for _, group := range feishuCommandGroups {
		groups = append(groups, group)
	}
	return groups
}

func FeishuCommandGroupByID(groupID string) (FeishuCommandGroup, bool) {
	for _, group := range feishuCommandGroups {
		if group.ID == groupID {
			return group, true
		}
	}
	return FeishuCommandGroup{}, false
}

func FeishuCommandDefinitions() []FeishuCommandDefinition {
	defs := make([]FeishuCommandDefinition, 0, len(feishuCommandSpecs))
	for _, spec := range feishuCommandSpecs {
		defs = append(defs, cloneFeishuCommandDefinition(spec.definition))
	}
	return defs
}

func FeishuCommandDefinitionByID(commandID string) (FeishuCommandDefinition, bool) {
	for _, spec := range feishuCommandSpecs {
		if spec.definition.ID == commandID {
			return cloneFeishuCommandDefinition(spec.definition), true
		}
	}
	return FeishuCommandDefinition{}, false
}

func FeishuCommandDefinitionsForGroup(groupID string) []FeishuCommandDefinition {
	defs := make([]FeishuCommandDefinition, 0, len(feishuCommandSpecs))
	for _, spec := range feishuCommandSpecs {
		if spec.definition.GroupID != groupID {
			continue
		}
		defs = append(defs, cloneFeishuCommandDefinition(spec.definition))
	}
	return defs
}

func FeishuCommandHelpCatalog() CommandCatalog {
	return buildFeishuCommandCatalog(
		"Slash 命令帮助",
		"以下是当前主展示的 canonical slash command。历史 alias 仍可兼容，但不再作为新的主展示入口。",
		false,
	)
}

func FeishuCommandMenuCatalog() CommandCatalog {
	return buildFeishuCommandCatalog(
		"命令目录",
		"这是同源的静态命令目录。真正的 `/menu` 首页会在 service 层按当前阶段动态重排。",
		true,
	)
}

func FeishuRecommendedMenus() []FeishuRecommendedMenu {
	order := []string{
		FeishuCommandMenu,
		FeishuCommandStop,
		FeishuCommandNew,
		FeishuCommandReasoning,
		FeishuCommandModel,
		FeishuCommandAccess,
	}
	menus := make([]FeishuRecommendedMenu, 0, len(order))
	for _, commandID := range order {
		def, ok := FeishuCommandDefinitionByID(commandID)
		if !ok || def.RecommendedMenu == nil {
			continue
		}
		menu := *def.RecommendedMenu
		menus = append(menus, FeishuRecommendedMenu{
			Key:         strings.TrimSpace(menu.Key),
			Name:        strings.TrimSpace(menu.Name),
			Description: strings.TrimSpace(menu.Description),
		})
	}
	return menus
}

func buildFeishuCommandCatalog(title, summary string, interactive bool) CommandCatalog {
	sections := make([]CommandCatalogSection, 0, len(feishuCommandGroups))
	for _, group := range feishuCommandGroups {
		entries := make([]CommandCatalogEntry, 0, len(feishuCommandSpecs))
		for _, spec := range feishuCommandSpecs {
			if spec.definition.GroupID != group.ID {
				continue
			}
			if interactive && !spec.definition.ShowInMenu {
				continue
			}
			if !interactive && !spec.definition.ShowInHelp {
				continue
			}
			entry := CommandCatalogEntry{
				Title:       strings.TrimSpace(spec.definition.Title),
				Commands:    []string{spec.definition.CanonicalSlash},
				Description: spec.definition.Description,
				Examples:    append([]string(nil), spec.definition.Examples...),
			}
			if interactive {
				entry.Buttons = append(entry.Buttons, CommandCatalogButton{
					Label:       catalogButtonLabel(spec.definition),
					Kind:        CommandCatalogButtonRunCommand,
					CommandText: spec.definition.CanonicalSlash,
				})
			}
			entries = append(entries, entry)
		}
		if len(entries) == 0 {
			continue
		}
		sections = append(sections, CommandCatalogSection{
			Title:   group.Title,
			Entries: entries,
		})
	}
	return CommandCatalog{
		Title:       title,
		Summary:     summary,
		Interactive: interactive,
		Sections:    sections,
	}
}

func catalogButtonLabel(def FeishuCommandDefinition) string {
	switch def.ArgumentKind {
	case FeishuCommandArgumentChoice, FeishuCommandArgumentText:
		return "打开"
	default:
		return strings.TrimSpace(def.Title)
	}
}

func cloneFeishuCommandDefinition(def FeishuCommandDefinition) FeishuCommandDefinition {
	cloned := def
	cloned.Examples = append([]string(nil), def.Examples...)
	if len(def.Options) > 0 {
		cloned.Options = append([]FeishuCommandOption(nil), def.Options...)
	}
	if def.RecommendedMenu != nil {
		menu := *def.RecommendedMenu
		cloned.RecommendedMenu = &menu
	}
	return cloned
}

func FeishuCommandForm(commandID string) (*CommandCatalogForm, bool) {
	def, ok := FeishuCommandDefinitionByID(commandID)
	if !ok {
		return nil, false
	}
	switch def.ArgumentKind {
	case FeishuCommandArgumentChoice, FeishuCommandArgumentText:
	default:
		return nil, false
	}
	submit := strings.TrimSpace(def.ArgumentSubmit)
	if submit == "" {
		submit = "执行"
	}
	label := strings.TrimSpace(def.ArgumentFormNote)
	if label == "" {
		label = "输入这条命令后面的参数。"
	}
	return &CommandCatalogForm{
		CommandID:   def.ID,
		CommandText: def.CanonicalSlash,
		SubmitLabel: submit,
		Field: CommandCatalogFormField{
			Name:        "command_args",
			Kind:        CommandCatalogFormFieldText,
			Label:       label,
			Placeholder: strings.TrimSpace(def.ArgumentFormHint),
		},
	}, true
}

func commandOption(commandText, menuKey, value, label, description string) FeishuCommandOption {
	return FeishuCommandOption{
		Value:       value,
		Label:       label,
		Description: description,
		CommandText: commandText + " " + value,
		MenuKey:     menuKey + "_" + value,
	}
}

func buildMenuModelText(value string) (string, bool) {
	model := strings.TrimSpace(value)
	if model == "" {
		return "", false
	}
	return "/model " + model, true
}

func buildMenuReasoningText(value string) (string, bool) {
	effort := strings.ToLower(strings.TrimSpace(value))
	switch effort {
	case "low", "medium", "high", "xhigh", "clear":
		return "/reasoning " + effort, true
	default:
		return "", false
	}
}

func buildMenuAccessText(value string) (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "full", "confirm", "clear":
		return "/access " + mode, true
	default:
		return "", false
	}
}

func buildMenuModeText(value string) (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "normal", "vscode":
		return "/mode " + mode, true
	default:
		return "", false
	}
}

func buildMenuAutoContinueText(value string) (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "on", "off":
		return "/autocontinue " + mode, true
	default:
		return "", false
	}
}

func buildMenuUpgradeText(value string) (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "latest", "local":
		return "/upgrade " + mode, true
	default:
		return "", false
	}
}
