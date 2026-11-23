package feishuapp

import "strings"

type TeamMenuAction string

const (
	TeamMenuHome         TeamMenuAction = "menu"
	TeamMenuStatus       TeamMenuAction = "status"
	TeamMenuStop         TeamMenuAction = "stop"
	TeamMenuNew          TeamMenuAction = "new"
	TeamMenuSteer        TeamMenuAction = "steerall"
	TeamMenuProjects     TeamMenuAction = "projects"
	TeamMenuResources    TeamMenuAction = "resources"
	TeamMenuHistory      TeamMenuAction = "history"
	TeamMenuContexts     TeamMenuAction = "contexts"
	TeamMenuSettings     TeamMenuAction = "settings"
	TeamMenuReview       TeamMenuAction = "review"
	TeamMenuAutoWhip     TeamMenuAction = "autowhip"
	TeamMenuAutoContinue TeamMenuAction = "autocontinue"
)

var teamMenus = [...]MenuRequirement{
	{Key: string(TeamMenuHome), Name: "命令菜单", Description: "打开私人工作菜单。"},
	{Key: string(TeamMenuStop), Name: "停止推理", Description: "中断当前执行，并取消此前排队的输入。"},
	{Key: string(TeamMenuSteer), Name: "全部加速", Description: "当前 Codex 执行支持时，将可追加的待办并入本轮。"},
	{Key: string(TeamMenuNew), Name: "新建会话", Description: "准备一个新会话。"},
	{Key: "reasoning", Name: "推理强度", Description: "打开推理强度参数卡。"},
	{Key: "model", Name: "使用模型", Description: "打开模型参数卡。"},
	{Key: "access", Name: "执行权限", Description: "打开执行权限参数卡。"},
}

type TeamMenuRoute struct {
	Action   TeamMenuAction
	Argument string
	Note     string
}

func ResolveTeamMenu(key string) TeamMenuRoute {
	key = strings.ToLower(strings.TrimSpace(key))
	// Already published app menus can still send their previous event keys.
	switch key {
	case "menu", "status", "settings", "projects", "resources", "history":
		return TeamMenuRoute{Action: TeamMenuAction(key)}
	case "steerall", "steer_all":
		return TeamMenuRoute{Action: TeamMenuSteer}
	case "project":
		return TeamMenuRoute{Action: TeamMenuProjects}
	case "workspace":
		return TeamMenuRoute{Action: TeamMenuResources}
	case "list", "use", "useall":
		return TeamMenuRoute{Action: TeamMenuContexts}
	case "model", "reasoning", "access", "plan", "verbose":
		return TeamMenuRoute{Action: TeamMenuSettings, Argument: key}
	case "mode":
		return TeamMenuRoute{Action: TeamMenuSettings, Argument: "backend"}
	case "codexprofile", "claudeprofile", "opencodeprofile":
		return TeamMenuRoute{Action: TeamMenuSettings, Argument: strings.TrimSuffix(key, "profile") + "-profile"}
	case string(TeamMenuReview):
		return TeamMenuRoute{Action: TeamMenuReview}
	case string(TeamMenuAutoWhip), "autowhip_on", "autowhip_off", "autowhip_status":
		_, mode, _ := strings.Cut(key, "_")
		return TeamMenuRoute{Action: TeamMenuAutoWhip, Argument: mode}
	case string(TeamMenuAutoContinue), "autocontinue_on", "autocontinue_off", "autocontinue_status":
		_, mode, _ := strings.Cut(key, "_")
		return TeamMenuRoute{Action: TeamMenuAutoContinue, Argument: mode}
	case string(TeamMenuStop):
		return TeamMenuRoute{Action: TeamMenuStop}
	case string(TeamMenuNew), "newthread":
		return TeamMenuRoute{Action: TeamMenuNew}
	default:
		return TeamMenuRoute{Action: TeamMenuHome, Note: "这个菜单入口已不可用。请从工作菜单选择当前可用的操作。"}
	}
}
