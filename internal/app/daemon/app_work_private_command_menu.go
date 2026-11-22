package daemon

import (
	"context"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func (a *App) queueTeamCurrentHistory(ctx context.Context, source teamHumanSource) (string, error) {
	contextID := ""
	if err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		front, _, err := worksettings.FrozenContext(tx, source.Authority)
		contextID = front.ID
		return err
	}); err != nil {
		return "", err
	}
	return a.queueTeamContextHistory(ctx, source, contextID, 0)
}

// Keep the existing slash page names routed to the same sections as menu buttons.
func teamPrivateCommandMenuPage(text string) (string, error) {
	fields := strings.Fields(text)
	if len(fields) == 1 {
		return "menu", nil
	}
	if len(fields) == 2 {
		switch strings.ToLower(fields[1]) {
		case control.FeishuCommandGroupCurrentWork, "basic":
			return "basic", nil
		case control.FeishuCommandGroupSendSettings, "settings":
			return "settings", nil
		case control.FeishuCommandGroupSwitchTarget, "workspace":
			return "workspace", nil
		case control.FeishuCommandGroupCommonTools, "tools":
			return "tools", nil
		case control.FeishuCommandGroupMaintenance, "admin":
			return "admin", nil
		}
	}
	return "", fmt.Errorf("无法识别这个菜单页面。请发送 /menu，或使用 current_work、send_settings、switch_target、common_tools、maintenance")
}

func (a *App) queueTeamPrivateCommandMenu(ctx context.Context, source teamHumanSource, text string) (string, error) {
	page, err := teamPrivateCommandMenuPage(text)
	if err != nil {
		return "", err
	}
	if page == "settings" {
		return a.queueTeamSettings(ctx, source, "overview", "")
	}
	return a.queuePrivatePanel(ctx, source, page, "")
}
