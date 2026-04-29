package daemon

import (
	"context"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type restartCommandMode = control.RestartCommandMode
type parsedRestartCommand = control.ParsedRestartCommand

const (
	restartCommandShowStatus = control.RestartCommandShowStatus
	restartCommandChild      = control.RestartCommandChild
)

type restartChildAvailability struct {
	Available   bool
	Code        string
	StatusKind  string
	StatusText  string
	InstanceID  string
	DisplayName string
}

func (a *App) handleRestartDaemonCommand(command control.DaemonCommand) []eventcontract.Event {
	availability := a.restartChildAvailabilityLocked(command.SurfaceSessionID)
	parsed, err := parseRestartCommandText(command.Text)
	if err != nil {
		return restartUsageEvents(command.SurfaceSessionID, availability, err.Error())
	}

	switch parsed.Mode {
	case restartCommandShowStatus:
		return commandPageEvents(command.SurfaceSessionID, buildRestartRootPageView(availability, "", ""))
	case restartCommandChild:
		if !availability.Available {
			return []eventcontract.Event{restartNoticeEvent(command.SurfaceSessionID, availability.Code, availability.StatusText)}
		}
		surfaceID := strings.TrimSpace(command.SurfaceSessionID)
		instanceID := strings.TrimSpace(availability.InstanceID)
		displayName := firstNonEmpty(strings.TrimSpace(availability.DisplayName), instanceID)
		go a.runRestartChildCommand(surfaceID, instanceID, displayName)
		return []eventcontract.Event{restartNoticeEvent(
			surfaceID,
			"restart_child_prepare_started",
			fmt.Sprintf("正在重启 %s 的 provider child。当前实例会短暂断开并恢复，请稍候。", displayName),
		)}
	default:
		return restartUsageEvents(command.SurfaceSessionID, availability, "不支持的 /restart 子命令。")
	}
}

func parseRestartCommandText(text string) (parsedRestartCommand, error) {
	parsed, err := control.ParseFeishuRestartCommandText(text)
	if err == nil {
		return parsed, nil
	}
	if argument := strings.TrimSpace(control.FeishuActionArgumentText(text)); argument != "" {
		return parsedRestartCommand{}, fmt.Errorf("%s", restartCommandUsageSyntax())
	}
	return parsedRestartCommand{}, fmt.Errorf("%s", restartSubcommandUsageSummary())
}

func (a *App) restartChildAvailabilityLocked(surfaceID string) restartChildAvailability {
	instanceID := strings.TrimSpace(a.service.AttachedInstanceID(surfaceID))
	if instanceID == "" {
		return restartChildAvailability{
			Code:       "restart_child_requires_attached",
			StatusKind: "error",
			StatusText: "当前没有已接管实例。请先 /list 或 /workspace 重新接入，再发送 /restart child。",
		}
	}
	inst := a.service.Instance(instanceID)
	if inst == nil {
		return restartChildAvailability{
			Code:       "restart_child_instance_missing",
			StatusKind: "error",
			StatusText: "当前接管实例记录已失效。请重新接管后再试。",
		}
	}
	displayName := firstNonEmpty(strings.TrimSpace(inst.DisplayName), strings.TrimSpace(inst.ShortName), instanceID)
	availability := restartChildAvailability{
		InstanceID:  instanceID,
		DisplayName: displayName,
	}
	if !inst.Online {
		availability.Code = "restart_child_instance_offline"
		availability.StatusKind = "error"
		availability.StatusText = fmt.Sprintf("%s 当前不在线，请等待实例恢复或重新接管。", displayName)
		return availability
	}
	if a.childRestartWaitInFlightLocked(instanceID) {
		availability.Code = "restart_child_already_running"
		availability.StatusKind = "error"
		availability.StatusText = fmt.Sprintf("%s 已经在等待上一次 child restart 结果，请稍后再试。", displayName)
		return availability
	}
	if reason := restartChildBusyMessage(a.service.Surface(surfaceID), inst, a.service.PendingRemoteTurns(), a.service.ActiveRemoteTurns(), instanceID, displayName); reason != "" {
		availability.Code = "restart_child_busy"
		availability.StatusKind = "error"
		availability.StatusText = reason
		return availability
	}
	availability.Available = true
	return availability
}

func restartChildBusyMessage(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, pending, active []orchestrator.RemoteTurnStatus, instanceID, displayName string) string {
	if inst != nil && strings.TrimSpace(inst.ActiveTurnID) != "" {
		return fmt.Sprintf("%s 当前仍有正在执行的任务，请等待完成或先 /stop。", displayName)
	}
	for _, current := range active {
		if strings.TrimSpace(current.InstanceID) == strings.TrimSpace(instanceID) {
			return fmt.Sprintf("%s 当前仍有正在执行的任务，请等待完成或先 /stop。", displayName)
		}
	}
	for _, current := range pending {
		if strings.TrimSpace(current.InstanceID) == strings.TrimSpace(instanceID) {
			return fmt.Sprintf("%s 当前仍有待发送或待恢复的任务，请等待完成或先 /stop。", displayName)
		}
	}
	if surface == nil {
		return ""
	}
	switch {
	case surface.ActiveQueueItemID != "":
		return "当前请求正在派发或执行，暂时不能重启 child。请等待完成或先 /stop。"
	case len(surface.QueuedQueueItemIDs) != 0:
		return "当前还有排队输入，暂时不能重启 child。请等待队列清空或先 /stop。"
	case surface.Abandoning:
		return "当前接管正在结束，请稍后再试。"
	default:
		return ""
	}
}

func restartSubcommandUsageSummary() string {
	return "`/restart` 只支持 `child`。"
}

func restartCommandUsageSyntax() string {
	return "`/restart` 只支持 `/restart child`。"
}

func buildRestartRootPageView(availability restartChildAvailability, statusKind, statusText string) control.FeishuPageView {
	if strings.TrimSpace(statusKind) == "" && strings.TrimSpace(statusText) == "" {
		statusKind = availability.StatusKind
		statusText = availability.StatusText
	}
	return control.FeishuPageView{
		CommandID:    control.FeishuCommandRestart,
		StatusKind:   strings.TrimSpace(statusKind),
		StatusText:   strings.TrimSpace(statusText),
		Interactive:  true,
		DisplayStyle: control.CommandCatalogDisplayCompactButtons,
		Sections: []control.CommandCatalogSection{
			{
				Title: "重启运行时",
				Entries: []control.CommandCatalogEntry{{
					Buttons: []control.CommandCatalogButton{
						runCommandButton("重启 child", "/restart child", "primary", !availability.Available),
					},
					Description: "重启当前 attached instance 的 provider child；不会重启 daemon。",
				}},
			},
		},
	}
}

func restartUsageEvents(surfaceID string, availability restartChildAvailability, message string) []eventcontract.Event {
	return commandPageEvents(surfaceID, buildRestartRootPageView(availability, "error", message))
}

func restartNoticeEvent(surfaceID, code, text string) eventcontract.Event {
	return eventcontract.Event{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: surfaceID,
		Notice: &control.Notice{
			Code:  code,
			Title: "Restart",
			Text:  text,
		},
	}
}

func (a *App) runRestartChildCommand(surfaceID, instanceID, displayName string) {
	ctx, cancel := context.WithTimeout(context.Background(), childRestartOutcomeTimeout)
	defer cancel()

	err := a.restartRelayChildCodexAndWait(ctx, instanceID)

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.shuttingDown {
		return
	}
	if err != nil {
		a.handleUIEventsLocked(context.Background(), []eventcontract.Event{restartNoticeEvent(
			surfaceID,
			"restart_child_failed",
			fmt.Sprintf("重启 %s 的 provider child 失败：%s", displayName, err.Error()),
		)})
		return
	}
	a.handleUIEventsLocked(context.Background(), []eventcontract.Event{restartNoticeEvent(
		surfaceID,
		"restart_child_succeeded",
		fmt.Sprintf("已重启 %s 的 provider child。现在可以继续发送消息。", displayName),
	)})
}
