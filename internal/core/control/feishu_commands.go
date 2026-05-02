package control

const (
	FeishuCommandWorkspace            = "workspace"
	FeishuCommandWorkspaceList        = "workspace_list"
	FeishuCommandWorkspaceNew         = "workspace_new"
	FeishuCommandWorkspaceNewDir      = "workspace_new_dir"
	FeishuCommandWorkspaceNewGit      = "workspace_new_git"
	FeishuCommandWorkspaceNewWorktree = "workspace_new_worktree"
	FeishuCommandWorkspaceDetach      = "workspace_detach"
	FeishuCommandList                 = "list"
	FeishuCommandStatus               = "status"
	FeishuCommandUse                  = "use"
	FeishuCommandUseAll               = "useall"
	FeishuCommandNew                  = "new"
	FeishuCommandHistory              = "history"
	FeishuCommandReview               = "review"
	FeishuCommandSendFile             = "sendfile"
	FeishuCommandFollow               = "follow"
	FeishuCommandDetach               = "detach"
	FeishuCommandStop                 = "stop"
	FeishuCommandCompact              = "compact"
	FeishuCommandSteerAll             = "steerall"
	FeishuCommandMode                 = "mode"
	FeishuCommandAutoWhip             = "autowhip"
	FeishuCommandAutoContinue         = "autocontinue"
	FeishuCommandModel                = "model"
	FeishuCommandReasoning            = "reasoning"
	FeishuCommandAccess               = "access"
	FeishuCommandPlan                 = "plan"
	FeishuCommandVerbose              = "verbose"
	FeishuCommandCodexProvider        = "codex_provider"
	FeishuCommandClaudeProfile        = "claude_profile"
	FeishuCommandHelp                 = "help"
	FeishuCommandMenu                 = "menu"
	FeishuCommandDebug                = "debug"
	FeishuCommandCron                 = "cron"
	FeishuCommandUpgrade              = "upgrade"
	FeishuCommandPatch                = "patch"
	FeishuCommandVSCodeMigrate        = "vscode_migrate"
)

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
	prefix        string
	kind          ActionKind
	parseArgument func(string) (string, bool)
}

type feishuCommandSpec struct {
	definition        FeishuCommandDefinition
	textExact         []feishuCommandMatch
	textPrefixes      []feishuCommandPrefixMatch
	menuExact         []feishuCommandMatch
	menuDynamic       []feishuCommandDynamicMenuMatch
	extraActionRoutes []feishuCommandActionRoute
}

type FeishuRecommendedMenu struct {
	Key         string
	Name        string
	Description string
}

var feishuCommandSpecs = []feishuCommandSpec{
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandStop,
			GroupID:          FeishuCommandGroupCurrentWork,
			Title:            "停止推理",
			CanonicalSlash:   "/stop",
			CanonicalMenuKey: "stop",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "中断当前执行，并丢弃飞书侧尚未发送的排队输入。",
			ShowInHelp:       true,
			ShowInMenu:       true,
			RecommendedMenu: &FeishuRecommendedMenu{
				Key:         "stop",
				Name:        "停止推理",
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
			Title:            "压缩上下文",
			CanonicalSlash:   "/compact",
			CanonicalMenuKey: "compact",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "对当前已选择的会话手动触发一次上下文压缩；当前有其他任务时会直接拒绝。",
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
			Title:            "全部加速",
			CanonicalSlash:   "/steerall",
			CanonicalMenuKey: "steerall",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "把当前队列里可并入本轮执行的输入一次性并入当前 running turn。",
			ShowInHelp:       true,
			ShowInMenu:       true,
			RecommendedMenu: &FeishuRecommendedMenu{
				Key:         "steerall",
				Name:        "全部加速",
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
			Description:      "仅 headless 模式可用：准备一个新会话，下一条消息会作为首条输入。",
			ShowInHelp:       true,
			ShowInMenu:       true,
			RecommendedMenu: &FeishuRecommendedMenu{
				Key:         "new",
				Name:        "新建会话",
				Description: "仅 headless 模式可用：准备一个新会话，下一条消息会作为首条输入。",
			},
		},
		textExact: []feishuCommandMatch{
			{alias: "/new", action: Action{Kind: ActionNewThread}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "new", action: Action{Kind: ActionNewThread}},
			{alias: "newthread", action: Action{Kind: ActionNewThread}},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandHistory,
			GroupID:          FeishuCommandGroupCommonTools,
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
	reviewCommandSpec(),
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
			{alias: "reasonmax", action: Action{Kind: ActionReasoningCommand, Text: "/reasoning max"}},
		},
		menuDynamic: []feishuCommandDynamicMenuMatch{
			{prefix: "reasoning_", kind: ActionReasoningCommand, parseArgument: normalizeReasoningMenuArgument},
			{prefix: "reasoning-", kind: ActionReasoningCommand, parseArgument: normalizeReasoningMenuArgument},
			{prefix: "reason_", kind: ActionReasoningCommand, parseArgument: normalizeReasoningMenuArgument},
			{prefix: "reason-", kind: ActionReasoningCommand, parseArgument: normalizeReasoningMenuArgument},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandModel,
			GroupID:          FeishuCommandGroupSendSettings,
			Title:            "使用模型",
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
				Name:        "使用模型",
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
			{prefix: "model_", kind: ActionModelCommand, parseArgument: normalizeModelMenuArgument},
			{prefix: "model-", kind: ActionModelCommand, parseArgument: normalizeModelMenuArgument},
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
			{prefix: "access_", kind: ActionAccessCommand, parseArgument: normalizeAccessMenuArgument},
			{prefix: "access-", kind: ActionAccessCommand, parseArgument: normalizeAccessMenuArgument},
			{prefix: "approval_", kind: ActionAccessCommand, parseArgument: normalizeAccessMenuArgument},
			{prefix: "approval-", kind: ActionAccessCommand, parseArgument: normalizeAccessMenuArgument},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandPlan,
			GroupID:          FeishuCommandGroupSendSettings,
			Title:            "Plan 模式",
			CanonicalSlash:   "/plan",
			CanonicalMenuKey: "plan",
			ArgumentKind:     FeishuCommandArgumentChoice,
			ArgumentFormHint: "on",
			ArgumentFormNote: "输入 on / off。",
			ArgumentSubmit:   "应用",
			Description:      "控制后续新 turn 是否按 upstream Plan mode 启动；bare `/plan` 会返回可选参数卡片。",
			Examples:         []string{"/plan on", "/plan off"},
			Options: []FeishuCommandOption{
				commandOption("/plan", "plan", "on", "开启", "后续新 turn 按 Plan mode 启动。"),
				commandOption("/plan", "plan", "off", "关闭", "后续新 turn 按默认执行模式启动。"),
			},
			ShowInHelp: true,
			ShowInMenu: true,
			RecommendedMenu: &FeishuRecommendedMenu{
				Key:         "plan",
				Name:        "Plan 模式",
				Description: "打开 Plan 模式参数卡，切换后续新 turn 是否启用 Plan 模式。",
			},
		},
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/plan", kind: ActionPlanCommand},
		},
		menuExact: []feishuCommandMatch{
			{alias: "plan", action: Action{Kind: ActionPlanCommand, Text: "/plan"}},
		},
		menuDynamic: []feishuCommandDynamicMenuMatch{
			{prefix: "plan_", kind: ActionPlanCommand, parseArgument: normalizePlanMenuArgument},
			{prefix: "plan-", kind: ActionPlanCommand, parseArgument: normalizePlanMenuArgument},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandVerbose,
			GroupID:          FeishuCommandGroupSendSettings,
			Title:            "提示详细程度",
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
				commandOption("/verbose", "verbose", "normal", "normal", "显示 plan、最终答复，以及会影响当前状态的共享过程项，例如文件修改、上下文压缩、MCP 调用。"),
				commandOption("/verbose", "verbose", "verbose", "verbose", "显示全部共享过程卡，包括执行、探索、Web 搜索、工具调用和 reasoning 状态。"),
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
			{prefix: "verbose_", kind: ActionVerboseCommand, parseArgument: normalizeVerboseMenuArgument},
			{prefix: "verbose-", kind: ActionVerboseCommand, parseArgument: normalizeVerboseMenuArgument},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandCodexProvider,
			GroupID:          FeishuCommandGroupSendSettings,
			Title:            "切换 Codex Provider",
			CanonicalSlash:   "/codexprovider",
			CanonicalMenuKey: "codex_provider",
			ArgumentKind:     FeishuCommandArgumentText,
			ArgumentFormHint: "default",
			ArgumentFormNote: "输入已存在的 Codex Provider ID。",
			ArgumentSubmit:   "切换",
			Description:      "查看当前 Codex Provider；bare `/codexprovider` 会返回可切换的配置下拉卡片。",
			Examples:         []string{"/codexprovider default"},
			ShowInHelp:       true,
			ShowInMenu:       true,
		},
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/codexprovider", kind: ActionCodexProviderCommand},
		},
		menuExact: []feishuCommandMatch{
			{alias: "codex_provider", action: Action{Kind: ActionCodexProviderCommand, Text: "/codexprovider"}},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandClaudeProfile,
			GroupID:          FeishuCommandGroupSendSettings,
			Title:            "切换 Claude 配置",
			CanonicalSlash:   "/claudeprofile",
			CanonicalMenuKey: "claude_profile",
			ArgumentKind:     FeishuCommandArgumentText,
			ArgumentFormHint: "default",
			ArgumentFormNote: "输入已存在的 Claude 配置 ID。",
			ArgumentSubmit:   "切换",
			Description:      "查看当前 Claude 配置；bare `/claudeprofile` 会返回可切换的配置下拉卡片。",
			Examples:         []string{"/claudeprofile default"},
			ShowInHelp:       true,
			ShowInMenu:       true,
		},
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/claudeprofile", kind: ActionClaudeProfileCommand},
		},
		menuExact: []feishuCommandMatch{
			{alias: "claude_profile", action: Action{Kind: ActionClaudeProfileCommand, Text: "/claudeprofile"}},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandWorkspace,
			GroupID:          FeishuCommandGroupSwitchTarget,
			Title:            "工作区与会话",
			CanonicalSlash:   "/workspace",
			CanonicalMenuKey: "workspace",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "打开 headless 模式的工作区与会话主页，里面提供切换、从目录新建、从 GIT URL 新建、从 Worktree 新建、解除接管。",
			ShowInHelp:       true,
			ShowInMenu:       true,
		},
		textExact: []feishuCommandMatch{
			{alias: "/workspace", action: Action{Kind: ActionWorkspaceRoot}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "workspace", action: Action{Kind: ActionWorkspaceRoot}},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandWorkspaceList,
			GroupID:          FeishuCommandGroupSwitchTarget,
			Title:            "切换工作区与会话",
			CanonicalSlash:   "/workspace list",
			CanonicalMenuKey: "workspace_list",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "打开工作区与会话切换卡；headless 模式下 `/list`、`/use`、`/useall` 都会汇合到这里。",
			ShowInHelp:       true,
			ShowInMenu:       true,
		},
		textExact: []feishuCommandMatch{
			{alias: "/workspace list", action: Action{Kind: ActionWorkspaceList}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "workspace_list", action: Action{Kind: ActionWorkspaceList}},
			{alias: "workspacelist", action: Action{Kind: ActionWorkspaceList}},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandWorkspaceNew,
			GroupID:          FeishuCommandGroupSwitchTarget,
			Title:            "新建工作区",
			CanonicalSlash:   "/workspace new",
			CanonicalMenuKey: "workspace_new",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "打开新建工作区入口页，可继续选择从目录新建、从 GIT URL 新建或从 Worktree 新建。",
			ShowInHelp:       true,
			ShowInMenu:       true,
		},
		textExact: []feishuCommandMatch{
			{alias: "/workspace new", action: Action{Kind: ActionWorkspaceNew}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "workspace_new", action: Action{Kind: ActionWorkspaceNew}},
			{alias: "workspacenew", action: Action{Kind: ActionWorkspaceNew}},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandWorkspaceNewDir,
			GroupID:          FeishuCommandGroupSwitchTarget,
			Title:            "从目录新建",
			CanonicalSlash:   "/workspace new dir",
			CanonicalMenuKey: "workspace_new_dir",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "直接打开从本地目录新建工作区卡片。",
			ShowInHelp:       true,
			ShowInMenu:       true,
		},
		textExact: []feishuCommandMatch{
			{alias: "/workspace new dir", action: Action{Kind: ActionWorkspaceNewDir}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "workspace_new_dir", action: Action{Kind: ActionWorkspaceNewDir}},
			{alias: "workspacenewdir", action: Action{Kind: ActionWorkspaceNewDir}},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandWorkspaceNewGit,
			GroupID:          FeishuCommandGroupSwitchTarget,
			Title:            "从 GIT URL 新建",
			CanonicalSlash:   "/workspace new git",
			CanonicalMenuKey: "workspace_new_git",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "直接打开从 GIT URL 新建工作区卡片。",
			ShowInHelp:       true,
			ShowInMenu:       true,
		},
		textExact: []feishuCommandMatch{
			{alias: "/workspace new git", action: Action{Kind: ActionWorkspaceNewGit}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "workspace_new_git", action: Action{Kind: ActionWorkspaceNewGit}},
			{alias: "workspacenewgit", action: Action{Kind: ActionWorkspaceNewGit}},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandWorkspaceNewWorktree,
			GroupID:          FeishuCommandGroupSwitchTarget,
			Title:            "从 Worktree 新建",
			CanonicalSlash:   "/workspace new worktree",
			CanonicalMenuKey: "workspace_new_worktree",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "基于一个已接入的 Git 工作区创建新的 worktree，并自动进入新会话。",
			ShowInHelp:       true,
			ShowInMenu:       true,
		},
		textExact: []feishuCommandMatch{
			{alias: "/workspace new worktree", action: Action{Kind: ActionWorkspaceNewWorktree}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "workspace_new_worktree", action: Action{Kind: ActionWorkspaceNewWorktree}},
			{alias: "workspacenewworktree", action: Action{Kind: ActionWorkspaceNewWorktree}},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandWorkspaceDetach,
			GroupID:          FeishuCommandGroupSwitchTarget,
			Title:            "解除接管",
			CanonicalSlash:   "/workspace detach",
			CanonicalMenuKey: "workspace_detach",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "解除当前接管；headless 模式下 `/detach` 会汇合到这里。",
			ShowInHelp:       true,
			ShowInMenu:       true,
		},
		textExact: []feishuCommandMatch{
			{alias: "/workspace detach", action: Action{Kind: ActionWorkspaceDetach}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "workspace_detach", action: Action{Kind: ActionWorkspaceDetach}},
			{alias: "workspacedetach", action: Action{Kind: ActionWorkspaceDetach}},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandList,
			GroupID:          FeishuCommandGroupSwitchTarget,
			Title:            "工作区与会话",
			CanonicalSlash:   "/list",
			CanonicalMenuKey: "list",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "打开工作区/会话目录；headless 模式走统一工作区/会话选择，vscode 模式列出可接管实例。",
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
			Description:      "展示最近会话；headless attached 时只看当前工作区并附带“显示全部”按钮，headless detached 时等同 `/useall` 的最近工作区总览，vscode detached 时需先 `/list`。",
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
			Description:      "展示跨工作区会话总览；headless 模式下默认先显示最近 5 个工作区并可卡片内展开全部，vscode attached 时仍只看当前实例，detached 时需先 `/list`。",
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
			Description:      "仅 `vscode` 模式可用：跟随当前 VS Code 聚焦会话；headless 模式请改走 `/use`、`/new` 或 `/mode vscode`。",
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
			GroupID:          FeishuCommandGroupCurrentWork,
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
			ArgumentFormHint: "codex",
			ArgumentFormNote: "输入 codex / claude / vscode；`normal` 仍兼容为 `codex`。",
			ArgumentSubmit:   "切换",
			Description:      "查看当前模式；bare `/mode` 会返回 codex / claude / vscode 切换卡片。",
			Examples:         []string{"/mode codex", "/mode claude", "/mode vscode", "/mode normal"},
			Options: []FeishuCommandOption{
				commandOption("/mode", "mode", "codex", "codex", "切换到 headless 的 Codex 模式。"),
				commandOption("/mode", "mode", "claude", "claude", "切换到 headless 的 Claude 模式。"),
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
			{prefix: "mode_", kind: ActionModeCommand, parseArgument: normalizeModeMenuArgument},
			{prefix: "mode-", kind: ActionModeCommand, parseArgument: normalizeModeMenuArgument},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandAutoWhip,
			GroupID:          FeishuCommandGroupCommonTools,
			Title:            "AutoWhip",
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
			{alias: "/autowhip", kind: ActionAutoWhipCommand},
		},
		menuExact: []feishuCommandMatch{
			{alias: "autowhip", action: Action{Kind: ActionAutoWhipCommand, Text: "/autowhip"}},
		},
		menuDynamic: []feishuCommandDynamicMenuMatch{
			{prefix: "autowhip_", kind: ActionAutoWhipCommand, parseArgument: normalizeAutoWhipMenuArgument},
			{prefix: "autowhip-", kind: ActionAutoWhipCommand, parseArgument: normalizeAutoWhipMenuArgument},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandAutoContinue,
			GroupID:          FeishuCommandGroupSendSettings,
			Title:            "自动继续",
			CanonicalSlash:   "/autocontinue",
			CanonicalMenuKey: "autocontinue",
			ArgumentKind:     FeishuCommandArgumentChoice,
			ArgumentFormHint: "on",
			ArgumentFormNote: "输入 on 或 off。",
			ArgumentSubmit:   "应用",
			Description:      "查看当前自动继续状态；只处理上游可重试失败，不影响 AutoWhip。",
			Examples:         []string{"/autocontinue on", "/autocontinue off"},
			Options: []FeishuCommandOption{
				commandOption("/autocontinue", "autocontinue", "on", "on", "开启当前飞书会话的自动继续。"),
				commandOption("/autocontinue", "autocontinue", "off", "off", "关闭当前飞书会话的自动继续。"),
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
			{prefix: "autocontinue_", kind: ActionAutoContinueCommand, parseArgument: normalizeAutoContinueMenuArgument},
			{prefix: "autocontinue-", kind: ActionAutoContinueCommand, parseArgument: normalizeAutoContinueMenuArgument},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandHelp,
			GroupID:          FeishuCommandGroupMaintenance,
			Title:            "命令帮助",
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
			GroupID:          FeishuCommandGroupCommonTools,
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
			Title:            "升级系统",
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
			{prefix: "upgrade_", kind: ActionUpgradeCommand, parseArgument: normalizeUpgradeMenuArgument},
			{prefix: "upgrade-", kind: ActionUpgradeCommand, parseArgument: normalizeUpgradeMenuArgument},
		},
	},
	{
		definition: FeishuCommandDefinition{
			ID:               FeishuCommandPatch,
			GroupID:          FeishuCommandGroupCommonTools,
			Title:            "修补当前会话",
			CanonicalSlash:   "/bendtomywill",
			CanonicalMenuKey: "patch",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "对当前会话最新一轮助手回复打开受控修补卡；只支持 headless 模式，且当前实例必须空闲。",
			ShowInHelp:       true,
			ShowInMenu:       true,
		},
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/bendtomywill", kind: ActionTurnPatchCommand},
		},
		menuExact: []feishuCommandMatch{
			{alias: "patch", action: Action{Kind: ActionTurnPatchCommand, Text: "/bendtomywill"}},
		},
		extraActionRoutes: []feishuCommandActionRoute{
			{kind: ActionTurnPatchRollback, canonicalSlash: "/bendtomywill rollback"},
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
			Description:      "查看调试状态，或生成临时管理页外链。",
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
			ID:               FeishuCommandVSCodeMigrate,
			GroupID:          FeishuCommandGroupMaintenance,
			Title:            "VS Code 迁移",
			CanonicalSlash:   "/vscode-migrate",
			CanonicalMenuKey: "vscode-migrate",
			ArgumentKind:     FeishuCommandArgumentNone,
			Description:      "打开 VS Code 迁移页，检查是否需要迁移到当前统一的 managed shim 接入方式。",
			ShowInHelp:       true,
			ShowInMenu:       false,
		},
		textExact: []feishuCommandMatch{
			{alias: "/vscode-migrate", action: Action{Kind: ActionVSCodeMigrateCommand, Text: "/vscode-migrate"}},
		},
	},
}
