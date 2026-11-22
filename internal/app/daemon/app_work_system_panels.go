package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func systemCommandButton(label, command string) workui.Button {
	return workui.Button{Label: label, Action: "system.command", Values: map[string]any{"command": command}}
}

func systemNetworkPanel(mode string) workui.Panel {
	panel := workui.Panel{Title: "网络设置", Paragraphs: []string{"当前网络模式：" + mode}}
	for _, option := range []struct{ label, mode string }{{"WAN 外网访问", "wan"}, {"Local 仅本机访问", "local"}, {"LAN 局域网访问", "lan"}} {
		panel.Buttons = append(panel.Buttons, systemCommandButton(option.label, "/admin network "+option.mode))
	}
	panel.Forms = []workui.InputForm{{Name: "network_ip", Placeholder: "局域网 IP，例如 192.168.1.20", Submit: workui.Button{Label: "使用指定局域网 IP", Action: "system.network-ip"}}}
	panel.Buttons = append(panel.Buttons, systemCommandButton("刷新网络设置", "/admin network"))
	return panel
}

func systemAutostartPanel(status install.AutostartStatus) workui.Panel {
	panel := workui.Panel{Title: "自动启动", Paragraphs: adminAutostartSummaryLines(status)}
	for _, note := range []string{status.Warning, status.LingerHint} {
		if strings.TrimSpace(note) != "" {
			panel.Paragraphs = append(panel.Paragraphs, note)
		}
	}
	panel.Buttons = append(panel.Buttons, systemCommandButton("刷新自动启动状态", "/admin autostart"))
	if status.Supported && status.CanApply && !status.Enabled {
		panel.Buttons = append(panel.Buttons, systemCommandButton("启用自动启动", "/admin autostart on"))
	}
	if status.Supported && status.Enabled {
		panel.Buttons = append(panel.Buttons, systemCommandButton("关闭自动启动", "/admin autostart off"))
	}
	return panel
}

func systemFailurePanel(title, refresh string, problem error, uncertain bool) workui.Panel {
	panel := workui.Panel{Title: title, Paragraphs: []string{title + "未完成：" + problem.Error()}}
	if uncertain {
		panel.Paragraphs = append(panel.Paragraphs, "这次操作的结果尚未确认，不会自动重复执行。请先刷新状态核对。")
	}
	if refresh != "" {
		panel.Buttons = append(panel.Buttons, systemCommandButton("刷新状态", refresh))
	}
	return panel
}

func (a *App) queueTeamSystemPanel(ctx context.Context, source teamHumanSource, panel workui.Panel) (string, error) {
	return a.queueTeamSystemPanelKind(ctx, source, panel, "private.navigation", source.ScopeID)
}

func (a *App) queueTeamSystemPanelKind(ctx context.Context, source teamHumanSource, panel workui.Panel, kind, target string) (string, error) {
	id := workui.ID(source.Key, "system", panel.Title)
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error { return a.queueTeamSystemPanelTx(tx, source, panel, kind, target, id) })
	return id, err
}

func (a *App) queueTeamSystemPanelTx(tx *workstore.Tx, source teamHumanSource, panel workui.Panel, kind, target, id string) error {
	if _, err := tx.GetUIView(id); err == nil {
		return nil
	} else if err != workstore.ErrNotFound {
		return err
	}
	front, revision, err := worksettings.FrozenContext(tx, source.Authority)
	if err != nil {
		return err
	}
	binding, err := tx.GetRoleBotBinding(source.RoleID)
	if err != nil {
		return err
	}
	command := map[string]string{"网络设置": "/admin network", "自动启动": "/admin autostart", "管理页": "/admin web", "管理页生成状态": "/admin web", "系统升级": "/upgrade", "升级通道": "/upgrade track", "确认主机升级": "/upgrade", "已退出升级确认": "/upgrade"}[panel.Title]
	if command != "" {
		panel.Title += " " + command
	}
	panel.PrivateMenu = true
	panel.Breadcrumb = "菜单首页 › 系统管理 › " + panel.Title
	panel.Navigation = []workui.Button{{Label: "返回系统管理", Action: "private.admin"}, {Label: "返回菜单首页", Action: "private.menu"}}
	panel.Footer = []workui.Button{{Label: "当前状态", Action: "private.status"}}
	content := panel.Buttons[:0]
	for _, button := range panel.Buttons {
		if strings.HasPrefix(button.Label, "刷新") {
			panel.Footer = append(panel.Footer, button)
		} else {
			content = append(content, button)
		}
	}
	panel.Buttons = content
	view := workstore.UIView{ID: id, ScopeID: source.ScopeID, ContextID: front.ID, Kind: kind, TargetID: target, TargetRevision: revision, Binding: binding, Tenant: source.Tenant, ChatID: source.ChatID, PrincipalID: source.PrincipalID, ReplyToMessageID: source.MessageID}
	if err = workui.PrivateNavigationFromInteraction(tx, &view, source.Authority.InteractionID); err != nil {
		return err
	}
	card, err := workui.Card(id, panel)
	if err != nil {
		return err
	}
	_, err = workui.Queue(tx, view, card)
	return err
}

func (a *App) handleTeamSystemViewOperation(ctx context.Context, source teamHumanSource, action string, form map[string]any) (string, error) {
	var frozen map[string]any
	if err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		interaction, err := tx.GetHumanInteraction(source.Authority.InteractionID)
		if err != nil {
			return err
		}
		id, _ := interaction.Envelope.Values["team_view"].(string)
		view, err := tx.GetUIView(id)
		if err != nil {
			return err
		}
		elements, err := teamFrozenCardElements(tx, view, interaction)
		if err != nil {
			return err
		}
		frozen = teamFrozenCallback(elements, interaction.Envelope.Values, action)
		if frozen == nil {
			return workstore.ErrScopeDenied
		}
		return nil
	}); err != nil {
		return "", err
	}
	text := ""
	switch action {
	case "system.network-ip":
		value, _ := form["network_ip"].(string)
		value = strings.TrimSpace(value)
		if net.ParseIP(value) == nil {
			return a.queueTeamSystemPanel(ctx, source, systemFailurePanel("网络设置", "/admin network", fmt.Errorf("请填写有效的局域网 IP 地址"), false))
		}
		text = "/admin network " + value
	case "system.command":
		text, _ = frozen["command"].(string)
	case "system.web-status":
		id, _ := frozen["job_id"].(string)
		return a.systemWebStatus(ctx, source, id)
	default:
		return "", workstore.ErrScopeDenied
	}
	command, known := control.ParseFeishuTextActionWithoutCatalog(text)
	if !known {
		return "", workstore.ErrScopeDenied
	}
	switch command.Kind {
	case control.ActionAdminCommand, control.ActionAdminRoot, control.ActionUpgradeCommand:
		return a.handleTeamPrivateOperation(ctx, source, command)
	default:
		return "", workstore.ErrScopeDenied
	}
}

func decodeSystemPanel(raw string, title ...string) workui.Panel {
	var panel workui.Panel
	if json.Unmarshal([]byte(raw), &panel) != nil || panel.Title == "" {
		name := "主机管理"
		if len(title) > 0 {
			name = title[0]
		}
		return workui.Panel{Title: name, Paragraphs: []string{raw}}
	}
	return panel
}
