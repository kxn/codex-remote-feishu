package daemon

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func (a *App) handleAdminDaemonCommand(command control.DaemonCommand) []eventcontract.Event {
	parsed, err := parseAdminCommandText(command.Text)
	if err != nil {
		return adminUsageEvents(command.SurfaceSessionID, err.Error())
	}
	switch parsed.Mode {
	case adminCommandWeb:
		return a.handleAdminWebCommand(command)
	case adminCommandLocalWeb:
		return commandPageEvents(command.SurfaceSessionID, buildAdminLocalWebPageView(a.localAdminURLLocked()))
	case adminCommandAutostart:
		status, err := detectAutostart(a.installStatePath())
		if err != nil {
			return adminNoticePageEvents(command.SurfaceSessionID, "自动启动", fmt.Sprintf("读取自动启动状态失败：%v", err))
		}
		return commandPageEvents(command.SurfaceSessionID, buildAdminAutostartPageView(status, "", ""))
	case adminCommandAutostartOn:
		return a.handleAdminAutostartApplyCommand(command)
	case adminCommandAutostartOff:
		return a.handleAdminAutostartDisableCommand(command)
	default:
		return adminUsageEvents(command.SurfaceSessionID, "不支持的 /admin 子命令。")
	}
}

func (a *App) handleAdminWebCommand(command control.DaemonCommand) []eventcontract.Event {
	service, localURL, err := a.ensureExternalAccessIssueTargetLocked()
	if err != nil {
		return []eventcontract.Event{adminNoticeEvent(command.SurfaceSessionID, "admin_web_issue_failed", fmt.Sprintf("生成管理页外链失败：%v\n如只需要本机地址，可改用 `/admin localweb`。", err))}
	}
	req := debugAdminIssueRequest(a.admin.adminURL)
	surfaceID := command.SurfaceSessionID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		issued, err := service.IssueURL(ctx, req, localURL)

		a.mu.Lock()
		defer a.mu.Unlock()
		if a.shuttingDown {
			return
		}
		if err != nil {
			a.handleUIEventsLocked(context.Background(), []eventcontract.Event{
				adminNoticeEvent(surfaceID, "admin_web_issue_failed", fmt.Sprintf("生成管理页外链失败：%v\n如只需要本机地址，可改用 `/admin localweb`。", err)),
			})
			return
		}
		text := fmt.Sprintf(
			"临时管理页外链已生成：\n[打开管理页](%s)\n\n链接：`%s`\n有效期到：`%s`",
			issued.ExternalURL,
			issued.ExternalURL,
			issued.ExpiresAt.UTC().Format(time.RFC3339),
		)
		a.handleUIEventsLocked(context.Background(), []eventcontract.Event{
			adminNoticeEvent(surfaceID, "admin_web_link_ready", text),
		})
	}()
	return []eventcontract.Event{
		adminNoticeEvent(command.SurfaceSessionID, "admin_web_prepare_started", "正在准备临时管理页外链。首次启动 tunnel 或重新拉起 external access 时，可能需要几十秒，请稍候。"),
	}
}

func (a *App) handleAdminAutostartApplyCommand(command control.DaemonCommand) []eventcontract.Event {
	currentBinary, err := a.currentBinaryPath()
	if err != nil {
		return adminNoticePageEvents(command.SurfaceSessionID, "自动启动", fmt.Sprintf("解析当前 binary 路径失败：%v", err))
	}
	status, err := applyAutostart(install.AutostartApplyOptions{
		StatePath:       a.installStatePath(),
		InstalledBinary: currentBinary,
		CurrentVersion:  a.currentBinaryVersion(),
	})
	if err != nil {
		return a.adminAutostartErrorPage(command.SurfaceSessionID, fmt.Sprintf("启用自动启动失败：%v", err))
	}
	return commandPageEvents(command.SurfaceSessionID, buildAdminAutostartPageView(status, "success", "已启用自动启动。"))
}

func (a *App) handleAdminAutostartDisableCommand(command control.DaemonCommand) []eventcontract.Event {
	status, err := disableAutostart(a.installStatePath())
	if err != nil {
		return a.adminAutostartErrorPage(command.SurfaceSessionID, fmt.Sprintf("关闭自动启动失败：%v", err))
	}
	return commandPageEvents(command.SurfaceSessionID, buildAdminAutostartPageView(status, "success", "已关闭自动启动。"))
}

func (a *App) adminAutostartErrorPage(surfaceID, message string) []eventcontract.Event {
	status, detectErr := detectAutostart(a.installStatePath())
	if detectErr != nil {
		return adminNoticePageEvents(surfaceID, "自动启动", message)
	}
	return commandPageEvents(surfaceID, buildAdminAutostartPageView(status, "error", message))
}

func (a *App) localAdminURLLocked() string {
	port := strings.TrimSpace(a.admin.adminListenPort)
	if port == "" {
		if parsed, err := url.Parse(strings.TrimSpace(a.admin.adminURL)); err == nil {
			port = parsed.Port()
		}
	}
	return httpURL("localhost", port, "/admin/")
}

func buildAdminLocalWebPageView(localURL string) control.FeishuPageView {
	return control.NormalizeFeishuPageView(control.FeishuPageView{
		CommandID:       control.FeishuCommandAdmin,
		Title:           "本地管理页",
		SummarySections: commandCatalogSummarySections("请在当前 daemon 所在机器的浏览器里打开本地管理页。", fmt.Sprintf("地址：%s", firstNonEmpty(strings.TrimSpace(localURL), "未解析"))),
		Interactive:     true,
		DisplayStyle:    control.CommandCatalogDisplayCompactButtons,
		Sections: []control.CommandCatalogSection{{
			Title: "打开页面",
			Entries: []control.CommandCatalogEntry{{
				Title:       "本地管理页",
				Description: "仅适用于当前 daemon 所在机器。",
				Buttons: []control.CommandCatalogButton{
					openURLButton("打开本地管理页", localURL, "primary", strings.TrimSpace(localURL) == ""),
				},
			}},
		}},
		RelatedButtons: control.FeishuCommandBackToRootButtons(control.FeishuCommandAdmin),
	})
}

func buildAdminAutostartPageView(status install.AutostartStatus, statusKind, statusText string) control.FeishuPageView {
	buttons := []control.CommandCatalogButton{
		runCommandButton("刷新状态", "/admin autostart", "", false),
		runCommandButton("启用自动启动", "/admin autostart on", "primary", !status.Supported || status.Enabled || !status.CanApply),
		runCommandButton("关闭自动启动", "/admin autostart off", "", !status.Supported || !status.Enabled),
	}
	noticeSections := []control.FeishuCardTextSection{}
	if warning := strings.TrimSpace(status.Warning); warning != "" {
		noticeSections = append(noticeSections, commandCatalogTextSection("提示", warning))
	}
	if lingerHint := strings.TrimSpace(status.LingerHint); lingerHint != "" {
		noticeSections = append(noticeSections, commandCatalogTextSection("额外说明", lingerHint))
	}
	return control.NormalizeFeishuPageView(control.FeishuPageView{
		CommandID:       control.FeishuCommandAdmin,
		Title:           "自动启动",
		StatusKind:      strings.TrimSpace(statusKind),
		StatusText:      strings.TrimSpace(statusText),
		SummarySections: commandCatalogSummarySections(adminAutostartSummaryLines(status)...),
		NoticeSections:  noticeSections,
		Interactive:     true,
		DisplayStyle:    control.CommandCatalogDisplayCompactButtons,
		Sections: []control.CommandCatalogSection{{
			Title: "操作",
			Entries: []control.CommandCatalogEntry{{
				Buttons: buttons,
			}},
		}},
		RelatedButtons: control.FeishuCommandBackToRootButtons(control.FeishuCommandAdmin),
	})
}

func adminAutostartSummaryLines(status install.AutostartStatus) []string {
	lines := []string{
		fmt.Sprintf("平台：%s", firstNonEmpty(strings.TrimSpace(status.Platform), "unknown")),
		fmt.Sprintf("状态：%s", adminAutostartStatusLabel(status)),
	}
	if status.Supported {
		lines = append(lines, fmt.Sprintf("管理方式：%s", firstNonEmpty(string(status.Manager), string(status.CurrentManager), "unknown")))
	}
	if path := strings.TrimSpace(status.ServiceUnitPath); path != "" {
		lines = append(lines, fmt.Sprintf("服务文件：%s", path))
	}
	return lines
}

func adminAutostartStatusLabel(status install.AutostartStatus) string {
	switch {
	case !status.Supported:
		return "当前平台不支持"
	case status.Enabled:
		return "已启用"
	case status.Configured:
		return "已配置但未启用"
	default:
		return "未启用"
	}
}

func adminNoticePageEvents(surfaceID, title, message string) []eventcontract.Event {
	return commandPageEvents(surfaceID, control.NormalizeFeishuPageView(control.FeishuPageView{
		CommandID:       control.FeishuCommandAdmin,
		Title:           title,
		StatusKind:      "error",
		StatusText:      strings.TrimSpace(message),
		SummarySections: commandCatalogSummarySections(strings.TrimSpace(message)),
		RelatedButtons:  control.FeishuCommandBackToRootButtons(control.FeishuCommandAdmin),
	}))
}
