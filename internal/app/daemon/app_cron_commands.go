package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func (a *App) handleCronDaemonCommand(command control.DaemonCommand) []control.UIEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.shuttingDown {
		return nil
	}
	return a.handleCronDaemonCommandLocked(command)
}

func (a *App) handleCronDaemonCommandLocked(command control.DaemonCommand) []control.UIEvent {
	parsed, err := parseCronCommandText(command.Text)
	if err != nil {
		return cronUsageEvents(command.SurfaceSessionID, err.Error())
	}
	switch parsed.Mode {
	case cronCommandShow:
		catalog, err := a.prepareCronCatalogLocked(command)
		if err != nil {
			return append([]control.UIEvent{
				cronNoticeEvent(command.SurfaceSessionID, "cron_status_failed", fmt.Sprintf("Cron 状态读取失败：%v", err)),
			}, cronUsageEvents(command.SurfaceSessionID, "")...)
		}
		if catalog == nil {
			return cronUsageEvents(command.SurfaceSessionID, "")
		}
		return []control.UIEvent{*catalog}
	case cronCommandRepair:
		if a.cronSyncInFlight {
			return []control.UIEvent{cronNoticeEvent(command.SurfaceSessionID, "cron_busy", "当前已有一个 Cron 配置同步在进行中，请稍后再试。")}
		}
		a.cronSyncInFlight = true
		go a.runCronRepairCommand(command)
		return []control.UIEvent{cronNoticeEvent(command.SurfaceSessionID, "cron_repair_started", "正在修复 Cron 配置表并同步工作区清单，请稍候。")}
	case cronCommandReload:
		if a.cronSyncInFlight {
			return []control.UIEvent{cronNoticeEvent(command.SurfaceSessionID, "cron_busy", "当前已有一个 Cron 配置同步在进行中，请稍后再试。")}
		}
		a.cronSyncInFlight = true
		go a.runCronReloadCommand(command)
		return []control.UIEvent{cronNoticeEvent(command.SurfaceSessionID, "cron_reload_started", "正在重新加载 Cron 任务配置，并校验表格内容。")}
	case cronCommandMigrateOwner:
		if a.cronSyncInFlight {
			return []control.UIEvent{cronNoticeEvent(command.SurfaceSessionID, "cron_busy", "当前已有一个 Cron 配置同步在进行中，请稍后再试。")}
		}
		a.cronSyncInFlight = true
		go a.runCronMigrateOwnerCommand(command)
		return []control.UIEvent{cronNoticeEvent(command.SurfaceSessionID, "cron_migrate_owner_started", "正在迁移 Cron owner 到当前 surface 对应 bot，请稍候。")}
	default:
		return cronUsageEvents(command.SurfaceSessionID, "不支持的 /cron 子命令。")
	}
}

func (a *App) runCronReloadCommand(command control.DaemonCommand) {
	notice, err := a.reloadCronJobs(command)
	a.finishCronBackgroundCommand(command.SurfaceSessionID, notice, err)
}

func (a *App) runCronRepairCommand(command control.DaemonCommand) {
	notice, err := a.repairCronBitable(command)
	a.finishCronBackgroundCommand(command.SurfaceSessionID, notice, err)
}

func (a *App) runCronMigrateOwnerCommand(command control.DaemonCommand) {
	notice, err := a.migrateCronOwner(command)
	a.finishCronBackgroundCommand(command.SurfaceSessionID, notice, err)
}

func (a *App) finishCronBackgroundCommand(surfaceID string, event *control.UIEvent, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cronSyncInFlight = false
	if a.shuttingDown {
		return
	}
	if err != nil {
		a.handleUIEventsLocked(context.Background(), []control.UIEvent{
			cronNoticeEvent(surfaceID, "cron_command_failed", fmt.Sprintf("Cron 操作失败：%v", err)),
		})
		return
	}
	if event != nil {
		a.handleUIEventsLocked(context.Background(), []control.UIEvent{*event})
	}
}

func (a *App) defaultCronBitableFactory(gatewayID string) (feishu.BitableAPI, error) {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return nil, err
	}
	runtimeCfg, ok := a.runtimeGatewayConfigFor(loaded.Config, gatewayID)
	if !ok {
		return nil, fmt.Errorf("找不到 gateway %q 对应的飞书运行时配置", gatewayID)
	}
	api := feishu.NewLiveBitableAPI(runtimeCfg.AppID, runtimeCfg.AppSecret)
	if api == nil {
		return nil, fmt.Errorf("gateway %q 缺少可用的 App ID / App Secret", gatewayID)
	}
	return api, nil
}

func (a *App) cronBitableAPI(gatewayID string) (feishu.BitableAPI, error) {
	factory := a.cronBitableFactory
	if factory == nil {
		factory = a.defaultCronBitableFactory
	}
	return factory(strings.TrimSpace(gatewayID))
}

func (a *App) prepareCronCatalogLocked(command control.DaemonCommand) (*control.UIEvent, error) {
	stateValue, err := a.loadCronStateLocked(false)
	if err != nil {
		return nil, err
	}
	snapshot := cloneCronState(stateValue)
	ownerView := a.inspectCronOwnerView(snapshot)
	return &control.UIEvent{
		Kind:             control.UIEventFeishuDirectCommandCatalog,
		SurfaceSessionID: command.SurfaceSessionID,
		FeishuDirectCommandCatalog: buildCronStatusCatalog(
			snapshot,
			ownerView,
			"",
		),
	}, nil
}

func (a *App) repairCronBitable(command control.DaemonCommand) (*control.UIEvent, error) {
	summary, err := a.repairCronBitableNow(command)
	if err != nil {
		return nil, err
	}
	return &control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: command.SurfaceSessionID,
		Notice: &control.Notice{
			Code:  "cron_repair_ready",
			Title: "Cron",
			Text:  summary,
		},
	}, nil
}

func (a *App) migrateCronOwner(command control.DaemonCommand) (*control.UIEvent, error) {
	summary, err := a.migrateCronOwnerNow(command)
	if err != nil {
		return nil, err
	}
	return &control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: command.SurfaceSessionID,
		Notice: &control.Notice{
			Code:  "cron_owner_migrated",
			Title: "Cron",
			Text:  summary,
		},
	}, nil
}

func (a *App) reloadCronJobs(command control.DaemonCommand) (*control.UIEvent, error) {
	result, err := a.reloadCronJobsResultNow(command)
	if err != nil {
		return nil, err
	}
	return &control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: command.SurfaceSessionID,
		Notice: &control.Notice{
			Code:  "cron_reload_ready",
			Title: "Cron",
			Text:  result.DetailedText(),
		},
	}, nil
}

func intervalMinutesForLabel(label string) (int, bool) {
	label = strings.TrimSpace(label)
	for _, item := range cronIntervalChoices {
		if item.Label == label {
			return item.Minutes, true
		}
	}
	return 0, false
}

func nextCronScheduleScan(now time.Time) time.Time {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return now.Add(cronScheduleScanEvery)
}
