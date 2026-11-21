package daemon

import (
	"context"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func (a *App) handleTeamPrivateOperation(ctx context.Context, source teamHumanSource, action control.Action) (string, error) {
	switch action.Kind {
	case control.ActionStop:
		return a.changePrivatePause(ctx, source, 0, false)
	case control.ActionModelCommand, control.ActionReasoningCommand, control.ActionModeCommand, control.ActionCodexProfileCommand, control.ActionClaudeProfileCommand, control.ActionOpenCodeProfileCommand, control.ActionAccessCommand, control.ActionPlanCommand, control.ActionVerboseCommand:
		return a.handleTeamSettingsCommand(ctx, source, action)
	case control.ActionWorkspaceRoot, control.ActionWorkspaceList:
		return a.queueTeamResourcePicker(ctx, source)
	default:
		return a.handleTeamNativeCommand(ctx, source, action)
	}
}

func (a *App) handleTeamPrivateViewOperation(ctx context.Context, source teamHumanSource, view workstore.UIView, action string) (string, error) {
	switch action {
	case "private.stop":
		return a.changePrivatePause(ctx, source, view.TargetRevision, false)
	case "private.resume":
		return a.changePrivatePause(ctx, source, view.TargetRevision, true)
	case "private.projects":
		return a.queueTeamProjectPicker(ctx, source)
	case "private.resources":
		return a.queueTeamResourcePicker(ctx, source)
	case "private.settings":
		return a.handleTeamSettingsView(ctx, source, view, action)
	default:
		return "", fmt.Errorf("这个操作已不可用，请重新打开当前菜单。")
	}
}

func (a *App) changePrivatePause(ctx context.Context, source teamHumanSource, revision int64, resume bool) (string, error) {
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
	note := "已暂停接收新的执行，正在等待当前执行实际停止。尚未完成的项目工作会保留。"
	if resume {
		note = "已打开这项工作的继续入口。若还有未解决的阻塞，会保留阻塞并说明原因。"
	}
	return a.queuePrivatePanel(ctx, source, "status", note)
}
