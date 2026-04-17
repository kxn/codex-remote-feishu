package control

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
	FeishuCommandHistory           = "history"
	FeishuCommandSendFile          = "sendfile"
	FeishuCommandFollow            = "follow"
	FeishuCommandDetach            = "detach"
	FeishuCommandStop              = "stop"
	FeishuCommandCompact           = "compact"
	FeishuCommandSteerAll          = "steerall"
	FeishuCommandMode              = "mode"
	FeishuCommandAutoContinue      = "autowhip"
	FeishuCommandModel             = "model"
	FeishuCommandReasoning         = "reasoning"
	FeishuCommandAccess            = "access"
	FeishuCommandVerbose           = "verbose"
	FeishuCommandHelp              = "help"
	FeishuCommandMenu              = "menu"
	FeishuCommandDebug             = "debug"
	FeishuCommandCron              = "cron"
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
		Description: "查看状态、切换模式、Cron、升级、autowhip、帮助和调试入口。",
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
			ID:               FeishuCommandCompact,
			GroupID:          FeishuCommandGroupCurrentWork,
			Title:            "整理上下文",
			CanonicalSlash:   "/compact",
			CanonicalMenuKey: "compact",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "对当前已绑定 thread 发起一次手动上下文整理；当前有其他任务时会直接拒绝。",
			ShowInHelp:       true,
			ShowInMenu:       true,
		},
		textExact: []feishuCommandMatch{
			{alias: "/compact", action: Action{Kind: ActionCompact}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "compact", action: Action{Kind: ActionCompact}},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandSteerAll,
			GroupID:          FeishuCommandGroupCurrentWork,
			Title:            "Steer All",
			CanonicalSlash:   "/steerall",
			CanonicalMenuKey: "steerall",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "把当前队列里可并入本轮执行的输入一次性并入当前 running turn。",
			ShowInHelp:       true,
			ShowInMenu:       true,
			RecommendedMenu: &FeishuRecommendedMenu{
				Key:         "steerall",
				Name:        "Steer All",
				Description: "把当前队列里可并入本轮执行的输入一次性并入当前 running turn。",
			},
		},
		textExact: []feishuCommandMatch{
			{alias: "/steerall", action: Action{Kind: ActionSteerAll}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "steerall", action: Action{Kind: ActionSteerAll}},
			{alias: "steer_all", action: Action{Kind: ActionSteerAll}},
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
			ID:               FeishuCommandHistory,
			GroupID:          FeishuCommandGroupCurrentWork,
			Title:            "查看会话历史",
			CanonicalSlash:   "/history",
			CanonicalMenuKey: "history",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "查看当前输入目标 thread 的历史 turn 列表，并在卡片里继续查看某一轮的详情。",
			ShowInHelp:       true,
			ShowInMenu:       true,
			RecommendedMenu: &FeishuRecommendedMenu{
				Key:         "history",
				Name:        "查看会话历史",
				Description: "查看当前输入目标 thread 的历史 turn 列表，并可进入某一轮的详情。",
			},
		},
		textExact: []feishuCommandMatch{
			{alias: "/history", action: Action{Kind: ActionShowHistory, Text: "/history"}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "history", action: Action{Kind: ActionShowHistory, Text: "/history"}},
		},
	},
	sendFileCommandSpec(),
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
			ID:               FeishuCommandVerbose,
			GroupID:          FeishuCommandGroupSendSettings,
			Title:            "前端详细程度",
			CanonicalSlash:   "/verbose",
			CanonicalMenuKey: "verbose",
			ArgumentKind:     FeishuCommandArgumentChoice,
			ArgumentFormHint: "normal",
			ArgumentFormNote: "输入 quiet / normal / verbose。",
			ArgumentSubmit:   "应用",
			Description:      "控制飞书前端显示过程消息的详细程度；bare `/verbose` 会返回可选档位卡片。",
			Examples:         []string{"/verbose quiet", "/verbose normal", "/verbose verbose"},
			Options: []FeishuCommandOption{
				commandOption("/verbose", "verbose", "quiet", "quiet", "只显示最终答复和必须可见的交互提示。"),
				commandOption("/verbose", "verbose", "normal", "normal", "显示普通过程文本、plan 和最终答复；不显示共享进行中卡。"),
				commandOption("/verbose", "verbose", "verbose", "verbose", "显示共享进行中卡、普通过程文本、plan 和最终答复，并为未来更细的过程事件预留。"),
			},
			ShowInHelp: true,
			ShowInMenu: true,
		},
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/verbose", kind: ActionVerboseCommand},
		},
		menuExact: []feishuCommandMatch{
			{alias: "verbose", action: Action{Kind: ActionVerboseCommand, Text: "/verbose"}},
		},
		menuDynamic: []feishuCommandDynamicMenuMatch{
			{prefix: "verbose_", kind: ActionVerboseCommand, build: buildMenuVerboseText},
			{prefix: "verbose-", kind: ActionVerboseCommand, build: buildMenuVerboseText},
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
			{alias: "/killinstance", action: Action{Kind: ActionDetach}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "detach", action: Action{Kind: ActionDetach}},
			{alias: "killinstance", action: Action{Kind: ActionDetach}},
			{alias: "kill_instance", action: Action{Kind: ActionDetach}},
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
			Title:            "autowhip",
			CanonicalSlash:   "/autowhip",
			CanonicalMenuKey: "autowhip",
			ArgumentKind:     FeishuCommandArgumentChoice,
			ArgumentFormHint: "on",
			ArgumentFormNote: "输入 on 或 off。",
			ArgumentSubmit:   "应用",
			Description:      "查看当前 autowhip 状态；bare `/autowhip` 会返回 on / off 切换卡片。",
			Examples:         []string{"/autowhip on", "/autowhip off"},
			Options: []FeishuCommandOption{
				commandOption("/autowhip", "autowhip", "on", "on", "开启当前飞书会话的 autowhip。"),
				commandOption("/autowhip", "autowhip", "off", "off", "关闭当前飞书会话的 autowhip。"),
			},
			ShowInHelp: true,
			ShowInMenu: true,
		},
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/autowhip", kind: ActionAutoContinueCommand},
			{alias: "/autocontinue", kind: ActionAutoContinueCommand},
		},
		menuExact: []feishuCommandMatch{
			{alias: "autowhip", action: Action{Kind: ActionAutoContinueCommand, Text: "/autowhip"}},
			{alias: "autocontinue", action: Action{Kind: ActionAutoContinueCommand, Text: "/autowhip"}},
		},
		menuDynamic: []feishuCommandDynamicMenuMatch{
			{prefix: "autowhip_", kind: ActionAutoContinueCommand, build: buildMenuAutoContinueText},
			{prefix: "autowhip-", kind: ActionAutoContinueCommand, build: buildMenuAutoContinueText},
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
			ID:               FeishuCommandCron,
			GroupID:          FeishuCommandGroupMaintenance,
			Title:            "定时任务",
			CanonicalSlash:   "/cron",
			CanonicalMenuKey: "cron",
			ArgumentKind:     FeishuCommandArgumentText,
			ArgumentFormHint: "reload",
			ArgumentFormNote: "例如 reload。",
			ArgumentSubmit:   "执行",
			Description:      "打开当前服务实例专属的定时任务多维表格，或用 `/cron reload` 重新加载任务配置。",
			Examples:         []string{"/cron", "/cron reload"},
			ShowInHelp:       true,
			ShowInMenu:       true,
		},
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/cron", kind: ActionCronCommand},
		},
		menuExact: []feishuCommandMatch{
			{alias: "cron", action: Action{Kind: ActionCronCommand, Text: "/cron"}},
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
			ArgumentFormNote: "例如 latest、local、track、track beta。",
			ArgumentSubmit:   "执行",
			Description:      "查看升级状态、查看或切换当前 release track；`/upgrade latest` 检查或继续 release 升级，`/upgrade local` 使用固定本地 artifact 发起升级。",
			Examples:         []string{"/upgrade latest", "/upgrade track beta", "/upgrade local"},
			Options: []FeishuCommandOption{
				commandOption("/upgrade", "upgrade", "latest", "latest", "检查或继续升级到当前 track 的最新 release。"),
				commandOption("/upgrade", "upgrade", "track", "track", "查看当前 track。"),
				commandOption("/upgrade track", "upgrade_track", "alpha", "track alpha", "切换到 alpha track。"),
				commandOption("/upgrade track", "upgrade_track", "beta", "track beta", "切换到 beta track。"),
				commandOption("/upgrade track", "upgrade_track", "production", "track production", "切换到 production track。"),
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
			ArgumentFormNote: "例如 admin。",
			ArgumentSubmit:   "执行",
			Description:      "查看调试状态，或生成临时管理页外链。历史兼容的 `/debug track` 请改用 `/upgrade track`。",
			Examples:         []string{"/debug", "/debug admin"},
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
