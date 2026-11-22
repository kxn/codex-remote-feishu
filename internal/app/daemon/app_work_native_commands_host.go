package daemon

import (
	"context"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func (a *App) teamUpgradeStatus(ctx context.Context, source teamHumanSource, action control.Action) (string, error) {
	parsed, err := parseUpgradeCommandText(action.Text)
	if err != nil {
		return a.queueTeamSystemPanel(ctx, source, systemFailurePanel("系统升级", "/upgrade", err, false))
	}
	a.mu.Lock()
	state, _, err := a.loadUpgradeStateLocked(parsed.Mode != upgradeCommandShowStatus && parsed.Mode != upgradeCommandShowTrack)
	a.mu.Unlock()
	if err != nil {
		return a.queueTeamSystemPanel(ctx, source, systemFailurePanel("系统升级", "/upgrade", fmt.Errorf("读取安装状态失败：%w", err), false))
	}
	if parsed.Mode != upgradeCommandShowStatus && parsed.Mode != upgradeCommandShowTrack {
		id, err := a.teamUpgradeAction(ctx, source, parsed, state)
		if err != nil {
			return a.queueTeamSystemPanel(ctx, source, systemFailurePanel("系统升级", "/upgrade", err, false))
		}
		return id, nil
	}
	note := fmt.Sprintf("当前版本：%s\n发布轨道：%s\n已知最新版本：%s", a.currentBinaryVersion(), state.CurrentTrack, state.LastKnownLatestVersion)
	if state.PendingUpgrade != nil {
		note += fmt.Sprintf("\n持久升级事务：%s，目标 %s", state.PendingUpgrade.Phase, state.PendingUpgrade.TargetVersion)
	}
	panel := workui.Panel{Title: "系统升级", Paragraphs: []string{note}}
	buttons := buildUpgradeRootButtons(a.teamRuntime.codexUpgrader != nil)
	if parsed.Mode == upgradeCommandShowTrack {
		panel.Title = "升级通道"
		buttons = buildUpgradeTrackButtons(string(state.CurrentTrack))
	}
	for _, button := range buttons {
		if button.Disabled {
			continue
		}
		if button.CommandText == "/upgrade local" && !install.CurrentBuildAllowsLocalUpgrade() {
			continue
		}
		if button.CommandText == "/upgrade dev" && !install.CurrentBuildAllowsDevUpgrade() {
			continue
		}
		panel.Buttons = append(panel.Buttons, systemCommandButton(button.Label, button.CommandText))
	}
	panel.Buttons = append(panel.Buttons, systemCommandButton("刷新升级状态", "/upgrade"))
	return a.queueTeamSystemPanel(ctx, source, panel)
}
