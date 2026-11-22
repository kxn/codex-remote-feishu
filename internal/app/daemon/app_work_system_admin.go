package daemon

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func (a *App) handleTeamAdminCommand(ctx context.Context, source teamHumanSource, action control.Action) (string, error) {
	text := strings.TrimSpace(action.Text)
	if text == "/admin" {
		return a.queuePrivatePanel(ctx, source, "admin", "")
	}
	parsed, err := parseAdminCommandText(text)
	if err != nil {
		return a.queueTeamSystemPanel(ctx, source, systemFailurePanel("系统管理", "/admin", err, false))
	}
	localLink := ""
	if parsed.Mode == adminCommandWeb {
		if panel, found, err := a.legacySystemWeb(ctx, source, text); err != nil {
			return "", err
		} else if found {
			return a.queueTeamSystemPanel(ctx, source, panel)
		}
		a.mu.Lock()
		mode := a.externalAccessNetworkModeLocked()
		localLink = a.localAdminURLLocked()
		a.mu.Unlock()
		if mode != "local" {
			return a.queueTeamSystemWeb(ctx, source)
		}
	}
	title, refresh, readOnly := "管理页", "/admin web", true
	switch parsed.Mode {
	case adminCommandNetwork, adminCommandNetworkSet:
		title, refresh, readOnly = "网络设置", "/admin network", parsed.Mode == adminCommandNetwork
	case adminCommandAutostart, adminCommandAutostartOn, adminCommandAutostartOff:
		title, refresh, readOnly = "自动启动", "/admin autostart", parsed.Mode == adminCommandAutostart
	}
	return a.teamHostPanelAction(ctx, source, "host.admin."+string(parsed.Mode), struct {
		Text   string
		Source workstore.PrivateActionSource
	}{text, source.Authority}, title, refresh, readOnly, func() (workui.Panel, error) {
		switch parsed.Mode {
		case adminCommandWeb:
			return systemAdminLinkPanel(localLink, ""), nil
		case adminCommandNetwork, adminCommandNetworkSet:
			if parsed.Mode == adminCommandNetworkSet {
				if err := a.applyExternalAccessNetworkMode(parsed.NetworkMode); err != nil {
					return workui.Panel{}, fmt.Errorf("保存网络模式失败：%w", err)
				}
			}
			a.mu.Lock()
			mode := a.externalAccessNetworkModeLocked()
			a.mu.Unlock()
			panel := systemNetworkPanel(mode)
			if parsed.Mode == adminCommandNetworkSet {
				panel.Paragraphs = append([]string{"网络模式已保存。"}, panel.Paragraphs...)
			}
			return panel, nil
		case adminCommandAutostart, adminCommandAutostartOn, adminCommandAutostartOff:
			var status install.AutostartStatus
			var err error
			switch parsed.Mode {
			case adminCommandAutostart:
				status, err = detectAutostart(a.installStatePath())
			case adminCommandAutostartOff:
				status, err = disableAutostart(a.installStatePath())
			case adminCommandAutostartOn:
				binary, resolveErr := a.currentBinaryPath()
				if resolveErr != nil {
					return workui.Panel{}, fmt.Errorf("无法定位当前安装程序：%w", resolveErr)
				}
				status, err = applyAutostart(install.AutostartApplyOptions{StatePath: a.installStatePath(), InstalledBinary: binary, CurrentVersion: a.currentBinaryVersion()})
			}
			if err != nil {
				return workui.Panel{}, fmt.Errorf("%s自动启动失败：%w", map[adminCommandMode]string{adminCommandAutostart: "读取", adminCommandAutostartOn: "启用", adminCommandAutostartOff: "关闭"}[parsed.Mode], err)
			}
			return systemAutostartPanel(status), nil
		}
		return workui.Panel{}, fmt.Errorf("无法识别系统管理操作")
	})
}

func systemAdminLinkPanel(link, note string) workui.Panel {
	panel := workui.Panel{Title: "管理页", Paragraphs: []string{link}}
	if note != "" {
		panel.Paragraphs = append(panel.Paragraphs, note)
	}
	if parsed, err := url.Parse(link); err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		panel.Buttons = append(panel.Buttons, workui.Button{Label: "打开管理页", URL: link})
	}
	panel.Buttons = append(panel.Buttons, systemCommandButton("重新生成管理页链接", "/admin web"))
	return panel
}

func (a *App) issueSystemAdminLink(ctx context.Context) (workui.Panel, error) {
	a.mu.Lock()
	mode := a.externalAccessNetworkModeLocked()
	local := a.localAdminURLLocked()
	if mode == "local" {
		a.mu.Unlock()
		return systemAdminLinkPanel(local, ""), nil
	}
	service, localURL, err := a.ensureExternalAccessIssueTargetLocked()
	request := debugAdminIssueRequest(a.admin.adminURL)
	a.mu.Unlock()
	if err != nil {
		return workui.Panel{}, fmt.Errorf("准备管理页访问通道失败：%w", err)
	}
	issued, err := service.IssueURL(ctx, request, localURL)
	if err != nil {
		return workui.Panel{}, fmt.Errorf("生成临时管理页链接失败：%w", err)
	}
	return systemAdminLinkPanel(issued.ExternalURL, "有效期至 "+issued.ExpiresAt.UTC().Format(time.RFC3339)), nil
}
