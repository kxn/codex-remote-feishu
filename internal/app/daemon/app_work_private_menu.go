package daemon

import (
	"fmt"
	"runtime"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func teamPrivateMenuPanel(tx *workstore.Tx, source teamHumanSource, nav work.PrivateNavigation, backend agentproto.Backend, kind, note string) (workui.Panel, error) {
	panel := workui.Panel{}
	add := func(label, action string) {
		panel.Buttons = append(panel.Buttons, workui.Button{Label: label, Action: action})
	}
	input := func(name, placeholder, label, action string) {
		panel.Forms = append(panel.Forms, workui.InputForm{Name: name, Placeholder: placeholder, Submit: workui.Button{Label: label, Action: action}})
	}
	if note != "" {
		panel.Paragraphs = append(panel.Paragraphs, note)
	}
	switch kind {
	case "menu":
		panel.Title = "工作菜单"
		if nav.Mode == work.PrivateProject && tx.RequireProjectPrincipalAccess(nav.ProjectScopeID, source.PrincipalID, time.Now().UTC()) == nil {
			scope, err := tx.GetScope(nav.ProjectScopeID)
			if err != nil {
				return panel, err
			}
			project, err := tx.GetProject(scope.ProjectID)
			if err != nil {
				return panel, err
			}
			panel.Paragraphs = append(panel.Paragraphs, "当前工作：与「"+project.Name+"」关联的私人讨论")
		} else {
			panel.Paragraphs = append(panel.Paragraphs, "当前工作：私人工作")
		}
		add("基本命令", "private.basic")
		add("参数设置", "private.settings")
		add("工作区与会话", "private.workspace")
		add("常用工具", "private.tools")
		add("系统管理", "private.admin")
	case "basic":
		panel.Title = "基本命令"
		add("当前状态", "private.status")
		add("新建会话", "private.new")
		inputs, err := tx.ListInbox(source.ScopeID)
		if err != nil {
			return panel, err
		}
		pending, running := false, false
		for _, input := range inputs {
			if input.RecipientContextID != nav.FrontContextID {
				continue
			}
			pending = pending || input.State == work.InboxPending
			running = running || input.State == work.InboxClaimed
		}
		workers, err := tx.ListWorkers(source.ScopeID)
		if err != nil {
			return panel, err
		}
		readyCodex, runningCodex := false, false
		for _, worker := range workers {
			if worker.ContextID != nav.FrontContextID {
				continue
			}
			running = running || worker.State == work.WorkerBusy
			if worker.Backend != work.BackendCodex || worker.NativeSessionID == "" {
				continue
			}
			readyCodex = readyCodex || worker.State == work.WorkerReady
			runningCodex = runningCodex || worker.State == work.WorkerBusy
		}
		if pending || running {
			add("停止当前私人执行", "private.stop")
		}
		legacyStop, err := tx.HasLegacyPrivateStop(source.ScopeID, nav.FrontContextID)
		if err != nil {
			return panel, err
		}
		if legacyStop {
			add("解除此会话的旧暂停", "private.resume")
		}
		if runningCodex && pending {
			add("将待办追加到当前执行", "private.steer")
		}
		if readyCodex {
			add("压缩当前会话上下文", "private.compact")
		}
	case "workspace":
		panel.Title = "工作区与会话"
		add("工作目录", "private.resources")
		add("切换已有会话", "private.contexts")
		add("进入项目相关讨论", "private.projects")
		add("新建会话", "private.new")
		if nav.Mode == work.PrivateProject {
			add("返回私人工作", "private.return_personal")
		}
		add("新建工作区", "private.workspace-tools")
	case "workspace-tools":
		panel.Title = "新建工作区"
		panel.Paragraphs = append(panel.Paragraphs, "先选择允许操作的工作目录。提交后，助手会在该范围内准备新目录或检出 Git 仓库。")
		input("workspace_dir", "目录名称或现成目录路径", "从目录新建", "private.workspace-dir")
		input("workspace_git", "Git 仓库 URL", "从 Git URL 新建", "private.workspace-git")
		add("选择工作目录", "private.resources")
		add("返回工作区与会话", "private.workspace")
	case "tools":
		panel.Title = "常用工具"
		add("查看会话历史", "private.contexts")
		add("发送工作目录里的文件", "private.file-help")
		if backend == "" || backend == agentproto.BackendCodex {
			add("审阅代码变更", "private.review")
			add("MCP 服务与登录", "private.mcp")
			add("会话目标", "private.goal-help")
		}
		add("自动检查未完成工作", "automation.autowhip.status")
	case "admin":
		panel.Title = "系统管理"
		add("打开管理页", "private.admin-web")
		add("查看网络模式", "private.admin-network")
		if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
			add("查看自动启动状态", "private.admin-autostart")
		}
		add("查看系统升级", "private.upgrade")
		add("命令帮助", "private.help")
	case "review":
		panel.Title = "审阅代码变更"
		panel.Paragraphs = append(panel.Paragraphs, "审查在所选目录的独立只读会话中运行。")
		input("review_commit", "提交 SHA", "审查指定提交", "private.review-commit")
		add("审查尚未提交的变更", "private.review-uncommitted")
		add("选择默认审查目录", "private.resources")
		add("返回常用工具", "private.tools")
	case "mcp":
		panel.Title = "MCP 服务与登录"
		panel.Paragraphs = append(panel.Paragraphs, "会话启动后可以查看加载的服务，并为需要授权的服务发起登录。")
		input("mcp_server", "服务名", "发起登录", "private.mcp-login")
		add("查看已加载的服务", "private.mcp-status")
		add("查看登录进度", "private.mcp-auth-status")
		add("返回常用工具", "private.tools")
	case "file-help":
		panel.Title = "发送工作目录里的文件"
		panel.Paragraphs = append(panel.Paragraphs, "相对路径从当前默认目录查找，也可以填写已选目录内的绝对路径。")
		input("file_path", "文件路径", "发送到这个私聊", "private.file-send")
		add("返回常用工具", "private.tools")
	case "goal-help":
		panel.Title = "会话目标"
		panel.Paragraphs = append(panel.Paragraphs, "发送 /goal 查看当前 Codex 会话的目标。使用 /goal set 和 /goal clear 调整目标。")
		add("查看当前会话目标", "private.goal")
		add("返回常用工具", "private.tools")
	case "help", "command-error":
		panel.Title = "命令帮助"
		panel.Paragraphs = append(panel.Paragraphs,
			"/menu 打开工作菜单 · /status 查看当前执行",
			"/new 新建私人会话 · /history 回看或切换既有会话 · /workspace 选择目录",
			"/stop 停止当前私人执行并取消已排队的私人输入，随后可直接发送新消息",
			"/mode 切换执行后端 · /model 查看模型 · /reasoning 调整推理强度 · /access 设置权限 · /plan 设置计划模式 · /verbose 设置详细程度",
			"/sendfile <文件路径> 从已选目录发送文件 · /admin 查看管理入口 · /upgrade 查看升级")
		if backend == "" || backend == agentproto.BackendCodex {
			panel.Paragraphs = append(panel.Paragraphs, "/compact 压缩空闲会话 · /steerall 将待办追加到运行中的执行 · /review 审查代码 · /mcp status 查看服务 · /goal 查看会话目标")
		}
	case string(work.AutomationWhip), string(work.AutomationContinue):
		automation := work.AutomationKind(kind)
		panel.Title = "自动检查未完成工作"
		backLabel, back := "返回常用工具", "private.tools"
		if automation == work.AutomationContinue {
			panel.Title = "断线后自动继续"
			backLabel, back = "返回参数设置", "private.settings"
		}
		policy, err := tx.GetAutomationPolicy(nav.FrontContextID, automation)
		if err != nil && err != workstore.ErrNotFound {
			return panel, err
		}
		if policy.Enabled {
			panel.Paragraphs = append(panel.Paragraphs, "当前设置：开启")
			add("关闭"+panel.Title, "automation."+kind+".off")
		} else {
			panel.Paragraphs = append(panel.Paragraphs, "当前设置：关闭")
			add("开启"+panel.Title, "automation."+kind+".on")
		}
		if automation == work.AutomationContinue {
			panel.Paragraphs = append(panel.Paragraphs, "已核实为可恢复的连接或服务错误时，自动重试原有工作。")
		} else {
			panel.Paragraphs = append(panel.Paragraphs, "助手报告本轮未完成时，安排一次独立的检查，限于原先已授权的工作。")
		}
		add(backLabel, back)
	case "status", "workers":
		add("返回基本命令", "private.basic")
	default:
		return panel, fmt.Errorf("未知的私聊菜单页面")
	}
	if kind != "menu" {
		add("返回菜单首页", "private.menu")
	}
	return panel, nil
}
