package feishuapp

import "strings"

type TeamMenuAction string

const (
	TeamMenuHome         TeamMenuAction = "menu"
	TeamMenuStatus       TeamMenuAction = "status"
	TeamMenuSteer        TeamMenuAction = "steerall"
	TeamMenuProjects     TeamMenuAction = "projects"
	TeamMenuResources    TeamMenuAction = "resources"
	TeamMenuHistory      TeamMenuAction = "history"
	TeamMenuSettings     TeamMenuAction = "settings"
	TeamMenuReview       TeamMenuAction = "review"
	TeamMenuAutoWhip     TeamMenuAction = "autowhip"
	TeamMenuAutoContinue TeamMenuAction = "autocontinue"
)

var teamMenus = [...]MenuRequirement{
	{Key: string(TeamMenuHome), Name: "工作菜单", Description: "打开私人工作入口。"},
	{Key: string(TeamMenuStatus), Name: "工作状态", Description: "查看当前工作及执行状态。"},
	{Key: string(TeamMenuSteer), Name: "追加待办", Description: "当前 Codex 执行支持时，将可追加的待办并入本轮。"},
	{Key: string(TeamMenuProjects), Name: "项目", Description: "打开有权访问的项目列表。"},
	{Key: string(TeamMenuResources), Name: "工作资料", Description: "查看当前工作的资料和目录。"},
	{Key: string(TeamMenuHistory), Name: "历史工作", Description: "回看已有的私人工作。"},
	{Key: string(TeamMenuSettings), Name: "执行设置", Description: "查看和调整当前工作的执行设置。"},
}

type TeamMenuRoute struct {
	Action   TeamMenuAction
	Argument string
	Note     string
}

func ResolveTeamMenu(key string) TeamMenuRoute {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, menu := range teamMenus {
		if key == menu.Key {
			return TeamMenuRoute{Action: TeamMenuAction(menu.Key)}
		}
	}

	// Already published app menus can still send their previous event keys.
	switch key {
	case "steer_all":
		return TeamMenuRoute{Action: TeamMenuSteer}
	case "project":
		return TeamMenuRoute{Action: TeamMenuProjects}
	case "workspace":
		return TeamMenuRoute{Action: TeamMenuResources}
	case "model", "reasoning", "access":
		return TeamMenuRoute{Action: TeamMenuSettings, Argument: key}
	case string(TeamMenuReview):
		return TeamMenuRoute{Action: TeamMenuReview}
	case string(TeamMenuAutoWhip), "autowhip_on", "autowhip_off", "autowhip_status":
		_, mode, _ := strings.Cut(key, "_")
		return TeamMenuRoute{Action: TeamMenuAutoWhip, Argument: mode}
	case string(TeamMenuAutoContinue), "autocontinue_on", "autocontinue_off", "autocontinue_status":
		_, mode, _ := strings.Cut(key, "_")
		return TeamMenuRoute{Action: TeamMenuAutoContinue, Argument: mode}
	case "stop":
		return TeamMenuRoute{Action: TeamMenuHome, Note: "这个旧菜单按钮已停用。请在私聊发送 /stop 停止当前工作。"}
	case "new":
		return TeamMenuRoute{Action: TeamMenuHome, Note: "这个旧菜单按钮已停用。请在私聊发送 /new 开始新的工作。"}
	default:
		return TeamMenuRoute{Action: TeamMenuHome, Note: "这个菜单入口已不可用。请从工作菜单选择当前可用的操作。"}
	}
}
