package daemon

import (
	"context"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func (a *App) handleTeamPrivateOperation(ctx context.Context, source teamHumanSource, action control.Action) (string, error) {
	switch action.Kind {
	case control.ActionStop:
		return a.applyTeamPrivateStop(ctx, source, 0, false)
	case control.ActionModelCommand, control.ActionReasoningCommand, control.ActionModeCommand, control.ActionCodexProfileCommand, control.ActionClaudeProfileCommand, control.ActionOpenCodeProfileCommand, control.ActionAccessCommand, control.ActionPlanCommand, control.ActionVerboseCommand:
		return a.handleTeamSettingsCommand(ctx, source, action)
	case control.ActionAdminRoot:
		return a.queuePrivatePanel(ctx, source, "admin", "")
	case control.ActionWorkspaceNew:
		return a.queuePrivatePanel(ctx, source, "workspace-tools", "")
	case control.ActionWorkspaceNewDir, control.ActionWorkspaceNewGit:
		if strings.TrimSpace(action.Text) == "/workspace new dir" || strings.TrimSpace(action.Text) == "/workspace new git" {
			return a.queuePrivatePanel(ctx, source, "workspace-tools", "")
		}
		return a.handleTeamNativeCommand(ctx, source, action)
	case control.ActionWorkspaceRoot, control.ActionWorkspaceList:
		return a.queueTeamResourcePicker(ctx, source)
	default:
		return a.handleTeamNativeCommand(ctx, source, action)
	}
}

func (a *App) handleTeamPrivateViewOperation(ctx context.Context, source teamHumanSource, view workstore.UIView, action string, form map[string]any) (string, error) {
	switch action {
	case "private.stop":
		return a.applyTeamPrivateStop(ctx, source, view.TargetRevision, false)
	case "private.resume":
		return a.applyTeamPrivateStop(ctx, source, view.TargetRevision, true)
	case "private.new", "private.return_personal":
		verb, note := "new", "已新建私人会话。下一条消息会进入新会话。"
		if action == "private.return_personal" {
			verb, note = "return_personal", "已返回私人工作。"
		}
		if err := a.navigateFromSource(ctx, source, verb, "", "", view.TargetRevision); err != nil {
			return "", err
		}
		return a.queuePrivatePanel(ctx, source, "workspace", note)
	case "private.contexts":
		return a.queueTeamContexts(ctx, source, 0)
	case "private.projects":
		return a.queueTeamProjectPicker(ctx, source)
	case "private.resources":
		return a.queueTeamResourcePicker(ctx, source)
	case "private.settings":
		return a.queueTeamSettings(ctx, source, "overview", "")
	case "private.status", "private.workspace-tools", "private.review", "private.file-help", "private.mcp", "private.goal-help", "private.help":
		return a.queuePrivatePanel(ctx, source, strings.TrimPrefix(action, "private."), "")
	}
	if _, automation, ok := strings.Cut(action, "automation."); ok {
		kind, verb, found := strings.Cut(automation, ".")
		if found && (kind == "autowhip" || kind == "autocontinue") {
			if command, known := control.ParseFeishuTextActionWithoutCatalog("/" + kind + " " + verb); known {
				return a.handleTeamAutomation(ctx, source, command)
			}
		}
	}
	if command, found, err := teamMenuToolCommand(action, form); found {
		if err != nil {
			return "", err
		}
		return a.handleTeamNativeCommand(ctx, source, command)
	}
	commands := map[string]string{
		"private.compact": "/compact", "private.steer": "/steerall",
		"private.review-uncommitted": "/review uncommitted", "private.goal": "/goal",
		"private.mcp-status": "/mcp status", "private.mcp-auth-status": "/mcp oauth-status",
		"private.admin-web": "/admin web", "private.admin-network": "/admin network",
		"private.admin-autostart": "/admin autostart status", "private.upgrade": "/upgrade",
	}
	if command, ok := control.ParseFeishuTextActionWithoutCatalog(commands[action]); ok {
		return a.handleTeamNativeCommand(ctx, source, command)
	}
	return "", fmt.Errorf("这个操作已不可用，请重新打开当前菜单。")
}

func (a *App) applyTeamPrivateStop(ctx context.Context, source teamHumanSource, revision int64, resume bool) (string, error) {
	if revision == 0 {
		revision = source.NavigationRevision
	}
	if revision < 1 {
		return "", fmt.Errorf("这条操作缺少原来的工作位置，请重新打开菜单。")
	}

	req := workservice.TrustedPrivateStopRequest{Key: source.Key, Source: source.Authority, ExpectedNavRevision: revision}
	var err error
	if resume {
		_, err = a.teamRuntime.service.TrustedPrivateResume(ctx, req)
	} else {
		_, err = a.teamRuntime.service.TrustedPrivateStop(ctx, req)
	}
	if err != nil {
		return "", err
	}
	note := "已提交停止请求，并取消这项私人工作中此前排队的输入。可以发送新消息，待旧执行停稳后处理。"
	if resume {
		note = "已解除这项私人的旧暂停设置。"
	}
	return a.queuePrivatePanel(ctx, source, "status", note)
}

func teamMenuToolCommand(action string, form map[string]any) (control.Action, bool, error) {
	tools := map[string]struct{ field, prefix, label string }{
		"private.workspace-dir": {"workspace_dir", "/workspace new dir ", "目录"},
		"private.workspace-git": {"workspace_git", "/workspace new git ", "Git 仓库 URL"},
		"private.review-commit": {"review_commit", "/review commit ", "提交 SHA"},
		"private.file-send":     {"file_path", "/sendfile ", "文件路径"},
		"private.mcp-login":     {"mcp_server", "/mcp login ", "MCP 服务名"},
	}
	tool, ok := tools[action]
	if !ok {
		return control.Action{}, false, nil
	}
	value, _ := form[tool.field].(string)
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 1024 || strings.ContainsAny(value, "\x00\r\n") {
		return control.Action{}, true, fmt.Errorf("请填写有效的%s", tool.label)
	}
	command, known := parseTeamPrivateCommand(tool.prefix + value)
	if !known {
		return control.Action{}, true, fmt.Errorf("无法识别%s，请核对输入", tool.label)
	}
	return command, true, nil
}

func parseTeamPrivateCommand(text string) (control.Action, bool) {
	if command, known := control.ParseFeishuTextActionWithoutCatalog(text); known {
		return command, true
	}
	for _, command := range []struct {
		prefix string
		kind   control.ActionKind
	}{
		{"/workspace new dir ", control.ActionWorkspaceNewDir},
		{"/workspace new git ", control.ActionWorkspaceNewGit},
		{"/workspace new worktree ", control.ActionWorkspaceNewWorktree},
	} {
		if argument, ok := strings.CutPrefix(text, command.prefix); ok && strings.TrimSpace(argument) != "" {
			return control.Action{Kind: command.kind, Text: text}, true
		}
	}
	return control.Action{}, false
}
