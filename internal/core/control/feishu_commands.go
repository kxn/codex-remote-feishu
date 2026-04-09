package control

import "strings"

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
	section          string
	helpCommands     []string
	description      string
	examples         []string
	buttons          []CommandCatalogButton
	recommendedMenus []FeishuRecommendedMenu
	showInHelp       bool
	showInMenu       bool
	textExact        []feishuCommandMatch
	textPrefixes     []feishuCommandPrefixMatch
	menuExact        []feishuCommandMatch
	menuDynamic      []feishuCommandDynamicMenuMatch
}

type FeishuRecommendedMenu struct {
	Key         string
	Name        string
	Description string
}

var feishuCommandSections = []string{
	"实例与会话",
	"发送前覆盖",
	"帮助",
}

var feishuCommandSpecs = []feishuCommandSpec{
	{
		section:      "实例与会话",
		helpCommands: []string{"/list"},
		description:  "Normal 模式列出可用工作区；VS Code 模式列出在线实例，并提供接管入口。",
		buttons: []CommandCatalogButton{
			{Label: "查看列表", CommandText: "/list"},
		},
		recommendedMenus: []FeishuRecommendedMenu{
			{Key: "list", Name: "查看列表", Description: "Normal 模式列出可用工作区；VS Code 模式列出在线实例，并提供接管入口。"},
		},
		showInHelp: true,
		showInMenu: true,
		textExact: []feishuCommandMatch{
			{alias: "/list", action: Action{Kind: ActionListInstances}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "list", action: Action{Kind: ActionListInstances}},
		},
	},
	{
		section:      "实例与会话",
		helpCommands: []string{"/status"},
		description:  "查看当前接管状态、输入目标和飞书侧临时覆盖。",
		buttons: []CommandCatalogButton{
			{Label: "当前状态", CommandText: "/status"},
		},
		recommendedMenus: []FeishuRecommendedMenu{
			{Key: "status", Name: "当前状态", Description: "查看当前接管状态、输入目标和飞书侧临时覆盖。"},
		},
		showInHelp: true,
		showInMenu: true,
		textExact: []feishuCommandMatch{
			{alias: "/status", action: Action{Kind: ActionStatus}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "status", action: Action{Kind: ActionStatus}},
		},
	},
	{
		section:      "实例与会话",
		helpCommands: []string{"/mode", "/mode normal", "/mode vscode"},
		description:  "查看或切换当前飞书会话的产品模式。",
		examples:     []string{"/mode normal", "/mode vscode"},
		buttons: []CommandCatalogButton{
			{Label: "Normal 模式", CommandText: "/mode normal"},
			{Label: "VS Code 模式", CommandText: "/mode vscode"},
		},
		showInHelp: true,
		showInMenu: true,
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/mode", kind: ActionModeCommand},
		},
	},
	{
		section:      "实例与会话",
		helpCommands: []string{"/autocontinue", "/autocontinue on", "/autocontinue off"},
		description:  "查看或切换当前飞书会话的 auto-continue 状态。",
		examples:     []string{"/autocontinue on", "/autocontinue off"},
		showInHelp:   true,
		showInMenu:   true,
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/autocontinue", kind: ActionAutoContinueCommand},
		},
	},
	{
		section:      "实例与会话",
		helpCommands: []string{"/new"},
		description:  "准备一个新的远端会话，下一条消息会作为新会话首条输入。",
		buttons: []CommandCatalogButton{
			{Label: "新建会话", CommandText: "/new"},
		},
		showInHelp: true,
		showInMenu: true,
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
		section:      "实例与会话",
		helpCommands: []string{"/threads", "/use", "/sessions"},
		description:  "展示最近可见会话，并切换后续输入目标。",
		buttons: []CommandCatalogButton{
			{Label: "最近会话", CommandText: "/use"},
		},
		recommendedMenus: []FeishuRecommendedMenu{
			{Key: "threads", Name: "切换会话", Description: "展示最近可见会话，并切换后续输入目标。"},
		},
		showInHelp: true,
		showInMenu: true,
		textExact: []feishuCommandMatch{
			{alias: "/threads", action: Action{Kind: ActionShowThreads}},
			{alias: "/use", action: Action{Kind: ActionShowThreads}},
			{alias: "/sessions", action: Action{Kind: ActionShowThreads}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "threads", action: Action{Kind: ActionShowThreads}},
			{alias: "use", action: Action{Kind: ActionShowThreads}},
			{alias: "sessions", action: Action{Kind: ActionShowThreads}},
			{alias: "showthreads", action: Action{Kind: ActionShowThreads}},
			{alias: "showsessions", action: Action{Kind: ActionShowThreads}},
		},
	},
	{
		section:      "实例与会话",
		helpCommands: []string{"/useall", "/sessionsall", "/sessions/all"},
		description:  "展示全部可见会话，用于切回旧会话或恢复之前的会话。",
		buttons: []CommandCatalogButton{
			{Label: "全部会话", CommandText: "/useall"},
		},
		showInHelp: true,
		showInMenu: true,
		textExact: []feishuCommandMatch{
			{alias: "/useall", action: Action{Kind: ActionShowAllThreads}},
			{alias: "/sessionsall", action: Action{Kind: ActionShowAllThreads}},
			{alias: "/sessions/all", action: Action{Kind: ActionShowAllThreads}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "threadsall", action: Action{Kind: ActionShowAllThreads}},
			{alias: "useall", action: Action{Kind: ActionShowAllThreads}},
			{alias: "sessionsall", action: Action{Kind: ActionShowAllThreads}},
			{alias: "showallthreads", action: Action{Kind: ActionShowAllThreads}},
			{alias: "showallsessions", action: Action{Kind: ActionShowAllThreads}},
		},
	},
	{
		section:      "实例与会话",
		helpCommands: []string{"/follow"},
		description:  "清空显式会话绑定，后续输入跟随当前 VS Code 聚焦会话。",
		buttons: []CommandCatalogButton{
			{Label: "跟随当前", CommandText: "/follow"},
		},
		showInHelp: true,
		showInMenu: true,
		textExact: []feishuCommandMatch{
			{alias: "/follow", action: Action{Kind: ActionFollowLocal}},
		},
	},
	{
		section:      "实例与会话",
		helpCommands: []string{"/detach"},
		description:  "解除当前接管，停止把后续输入发送到当前实例。",
		buttons: []CommandCatalogButton{
			{Label: "解除接管", CommandText: "/detach"},
		},
		showInHelp: true,
		showInMenu: true,
		textExact: []feishuCommandMatch{
			{alias: "/detach", action: Action{Kind: ActionDetach}},
		},
	},
	{
		section:      "实例与会话",
		helpCommands: []string{"/stop"},
		description:  "中断当前执行，并丢弃飞书侧尚未发送的排队输入。",
		buttons: []CommandCatalogButton{
			{Label: "停止", CommandText: "/stop"},
		},
		recommendedMenus: []FeishuRecommendedMenu{
			{Key: "stop", Name: "停止当前执行", Description: "中断当前执行，并丢弃飞书侧尚未发送的排队输入。"},
		},
		showInHelp: true,
		showInMenu: true,
		textExact: []feishuCommandMatch{
			{alias: "/stop", action: Action{Kind: ActionStop}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "stop", action: Action{Kind: ActionStop}},
		},
	},
	{
		section:      "实例与会话",
		helpCommands: []string{"/killinstance"},
		description:  "历史兼容入口，提示改用 /detach。",
		showInHelp:   false,
		showInMenu:   false,
		textExact: []feishuCommandMatch{
			{alias: "/killinstance", action: Action{Kind: ActionRemovedCommand, Text: "/killinstance"}},
		},
		menuExact: []feishuCommandMatch{
			{alias: "killinstance", action: Action{Kind: ActionRemovedCommand, Text: "kill_instance"}},
		},
	},
	{
		section:      "发送前覆盖",
		helpCommands: []string{"/model <模型名>"},
		description:  "只覆盖下一条消息使用的模型。",
		examples:     []string{"/model gpt-5.4"},
		showInHelp:   true,
		showInMenu:   true,
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/model", kind: ActionModelCommand},
		},
		menuDynamic: []feishuCommandDynamicMenuMatch{
			{prefix: "model_", kind: ActionModelCommand, build: buildMenuModelText},
			{prefix: "model-", kind: ActionModelCommand, build: buildMenuModelText},
		},
	},
	{
		section:      "发送前覆盖",
		helpCommands: []string{"/reasoning <low|medium|high|xhigh>", "/effort <low|medium|high|xhigh>"},
		description:  "只覆盖下一条消息的推理强度。",
		examples:     []string{"/reasoning high"},
		buttons: []CommandCatalogButton{
			{Label: "low", CommandText: "/reasoning low"},
			{Label: "medium", CommandText: "/reasoning medium"},
			{Label: "high", CommandText: "/reasoning high"},
			{Label: "xhigh", CommandText: "/reasoning xhigh"},
		},
		recommendedMenus: []FeishuRecommendedMenu{
			{Key: "reason_low", Name: "推理 Low", Description: "只覆盖下一条消息的推理强度为 low。"},
			{Key: "reason_medium", Name: "推理 Medium", Description: "只覆盖下一条消息的推理强度为 medium。"},
			{Key: "reason_high", Name: "推理 High", Description: "只覆盖下一条消息的推理强度为 high。"},
			{Key: "reason_xhigh", Name: "推理 XHigh", Description: "只覆盖下一条消息的推理强度为 xhigh。"},
		},
		showInHelp: true,
		showInMenu: true,
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/reasoning", kind: ActionReasoningCommand},
			{alias: "/effort", kind: ActionReasoningCommand},
		},
		menuExact: []feishuCommandMatch{
			{alias: "reasonlow", action: Action{Kind: ActionReasoningCommand, Text: "/reasoning low"}},
			{alias: "reasonmedium", action: Action{Kind: ActionReasoningCommand, Text: "/reasoning medium"}},
			{alias: "reasonhigh", action: Action{Kind: ActionReasoningCommand, Text: "/reasoning high"}},
			{alias: "reasonxhigh", action: Action{Kind: ActionReasoningCommand, Text: "/reasoning xhigh"}},
		},
		menuDynamic: []feishuCommandDynamicMenuMatch{
			{prefix: "reason_", kind: ActionReasoningCommand, build: buildMenuReasoningText},
			{prefix: "reason-", kind: ActionReasoningCommand, build: buildMenuReasoningText},
		},
	},
	{
		section:      "发送前覆盖",
		helpCommands: []string{"/access <full|confirm>", "/approval <full|confirm>"},
		description:  "只覆盖下一条消息的执行权限。",
		examples:     []string{"/access confirm"},
		buttons: []CommandCatalogButton{
			{Label: "全部允许", CommandText: "/access full"},
			{Label: "逐次确认", CommandText: "/access confirm"},
		},
		recommendedMenus: []FeishuRecommendedMenu{
			{Key: "access_full", Name: "执行权限 Full", Description: "只覆盖下一条消息的执行权限为 full。"},
			{Key: "access_confirm", Name: "执行权限 Confirm", Description: "只覆盖下一条消息的执行权限为 confirm。"},
		},
		showInHelp: true,
		showInMenu: true,
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/access", kind: ActionAccessCommand},
			{alias: "/approval", kind: ActionAccessCommand},
		},
		menuExact: []feishuCommandMatch{
			{alias: "accessfull", action: Action{Kind: ActionAccessCommand, Text: "/access full"}},
			{alias: "approvalfull", action: Action{Kind: ActionAccessCommand, Text: "/access full"}},
			{alias: "accessconfirm", action: Action{Kind: ActionAccessCommand, Text: "/access confirm"}},
			{alias: "approvalconfirm", action: Action{Kind: ActionAccessCommand, Text: "/access confirm"}},
		},
	},
	{
		section:      "帮助",
		helpCommands: []string{"/debug", "/debug upgrade", "/debug track", "/debug track alpha|beta|production"},
		description:  "查看升级状态、手动检查更新，或切换当前 release track。",
		examples:     []string{"/debug", "/debug upgrade", "/debug track beta"},
		showInHelp:   true,
		textPrefixes: []feishuCommandPrefixMatch{
			{alias: "/debug", kind: ActionDebugCommand},
		},
	},
	{
		section:      "帮助",
		helpCommands: []string{"/help"},
		description:  "查看当前支持的 slash command 和说明。",
		buttons: []CommandCatalogButton{
			{Label: "Slash 帮助", CommandText: "/help"},
		},
		showInHelp: true,
		showInMenu: true,
		textExact: []feishuCommandMatch{
			{alias: "/help", action: Action{Kind: ActionShowCommandHelp}},
		},
	},
	{
		section:      "帮助",
		helpCommands: []string{"menu", "/menu"},
		description:  "发送一张命令汇总卡片，固定动作可直接点击。",
		showInHelp:   true,
		textExact: []feishuCommandMatch{
			{alias: "menu", action: Action{Kind: ActionShowCommandMenu}},
			{alias: "/menu", action: Action{Kind: ActionShowCommandMenu}},
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

func FeishuCommandHelpCatalog() CommandCatalog {
	return buildFeishuCommandCatalog(
		"Slash 命令帮助",
		"当前支持的 slash command 如下。带参数的命令请直接发送文字，例如 /model gpt-5.4。",
		false,
	)
}

func FeishuCommandMenuCatalog() CommandCatalog {
	return buildFeishuCommandCatalog(
		"命令菜单",
		"固定动作可直接点击。需要自定义参数的命令请参考说明中的示例后，直接给机器人发送文字。",
		true,
	)
}

func FeishuRecommendedMenus() []FeishuRecommendedMenu {
	menus := make([]FeishuRecommendedMenu, 0, len(feishuCommandSpecs))
	seen := make(map[string]struct{}, len(feishuCommandSpecs))
	for _, spec := range feishuCommandSpecs {
		for _, menu := range spec.recommendedMenus {
			key := strings.TrimSpace(menu.Key)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			menus = append(menus, FeishuRecommendedMenu{
				Key:         key,
				Name:        strings.TrimSpace(menu.Name),
				Description: strings.TrimSpace(menu.Description),
			})
		}
	}
	return menus
}

func buildFeishuCommandCatalog(title, summary string, interactive bool) CommandCatalog {
	sections := make([]CommandCatalogSection, 0, len(feishuCommandSections))
	for _, sectionTitle := range feishuCommandSections {
		entries := make([]CommandCatalogEntry, 0, len(feishuCommandSpecs))
		for _, spec := range feishuCommandSpecs {
			if spec.section != sectionTitle {
				continue
			}
			if interactive && !spec.showInMenu {
				continue
			}
			if !interactive && !spec.showInHelp {
				continue
			}
			entry := CommandCatalogEntry{
				Commands:    append([]string(nil), spec.helpCommands...),
				Description: spec.description,
				Examples:    append([]string(nil), spec.examples...),
			}
			if interactive {
				entry.Buttons = append([]CommandCatalogButton(nil), spec.buttons...)
			}
			entries = append(entries, entry)
		}
		if len(entries) == 0 {
			continue
		}
		sections = append(sections, CommandCatalogSection{
			Title:   sectionTitle,
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
	case "low", "medium", "high", "xhigh":
		return "/reasoning " + effort, true
	default:
		return "", false
	}
}
