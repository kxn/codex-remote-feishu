package feishuapp

var teamMenus = [...]MenuRequirement{
	{Key: "menu", Name: "命令菜单", Description: "打开私人工作菜单。"},
	{Key: "stop", Name: "停止推理", Description: "中断当前执行，并取消此前排队的输入。"},
	{Key: "steerall", Name: "全部加速", Description: "当前 Codex 执行支持时，将可追加的待办并入本轮。"},
	{Key: "new", Name: "新建会话", Description: "准备一个新会话。"},
	{Key: "reasoning", Name: "推理强度", Description: "打开推理强度参数卡。"},
	{Key: "model", Name: "使用模型", Description: "打开模型参数卡。"},
	{Key: "access", Name: "执行权限", Description: "打开执行权限参数卡。"},
}
