package control

const (
	FeishuCommandGroupCurrentWork  = "current_work"
	FeishuCommandGroupSendSettings = "send_settings"
	FeishuCommandGroupSwitchTarget = "switch_target"
	FeishuCommandGroupCommonTools  = "common_tools"
	FeishuCommandGroupMaintenance  = "maintenance"
)

type FeishuCommandArgumentKind string

const (
	FeishuCommandArgumentNone   FeishuCommandArgumentKind = "none"
	FeishuCommandArgumentChoice FeishuCommandArgumentKind = "choice"
	FeishuCommandArgumentText   FeishuCommandArgumentKind = "text"
)

type FeishuCommandGroup struct {
	ID            string
	Title         string
	Description   string
	RootCommandID string
}

var feishuCommandGroups = []FeishuCommandGroup{
	{
		ID:          FeishuCommandGroupCurrentWork,
		Title:       "基本命令",
		Description: "",
	},
	{
		ID:          FeishuCommandGroupSendSettings,
		Title:       "参数设置",
		Description: "",
	},
	{
		ID:            FeishuCommandGroupSwitchTarget,
		Title:         "工作区与会话",
		Description:   "",
		RootCommandID: FeishuCommandWorkspace,
	},
	{
		ID:          FeishuCommandGroupCommonTools,
		Title:       "常用工具",
		Description: "",
	},
	{
		ID:            FeishuCommandGroupMaintenance,
		Title:         "系统管理",
		Description:   "",
		RootCommandID: FeishuCommandAdmin,
	},
}
