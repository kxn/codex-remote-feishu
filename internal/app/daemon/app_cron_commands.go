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
	case cronCommandMenu, cronCommandStatus, cronCommandList, cronCommandEdit:
		catalog, err := a.prepareCronCatalogLocked(command, parsed.Mode)
		if err != nil {
			return append([]control.UIEvent{
				cronNoticeEvent(command.SurfaceSessionID, "cron_catalog_failed", fmt.Sprintf("Cron 信息读取失败：%v", err)),
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
		return []control.UIEvent{cronNoticeEvent(command.SurfaceSessionID, "cron_repair_started", "正在修复 Cron 配置表；如果检测到绑定失效，将自动由当前 bot 接管 Cron 配置，请稍候。")}
	case cronCommandReload:
		if a.cronSyncInFlight {
			return []control.UIEvent{cronNoticeEvent(command.SurfaceSessionID, "cron_busy", "当前已有一个 Cron 配置同步在进行中，请稍后再试。")}
		}
		a.cronSyncInFlight = true
		go a.runCronReloadCommand(command)
		return []control.UIEvent{cronNoticeEvent(command.SurfaceSessionID, "cron_reload_started", "正在重新加载 Cron 任务配置，并校验表格内容。")}
	case cronCommandRun:
		if a.cronSyncInFlight {
			return []control.UIEvent{cronNoticeEvent(command.SurfaceSessionID, "cron_busy", "当前已有一个 Cron 配置同步在进行中，请稍后再试。")}
		}
		go a.runCronTriggerCommand(command, parsed.JobRecordID)
		return []control.UIEvent{cronNoticeEvent(command.SurfaceSessionID, "cron_run_started", "正在立即触发所选 Cron 任务。")}
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

func (a *App) runCronTriggerCommand(command control.DaemonCommand, jobRecordID string) {
	notice, err := a.triggerCronJob(command, jobRecordID)
	a.finishCronAsyncCommand(command.SurfaceSessionID, notice, err)
}

func (a *App) finishCronBackgroundCommand(surfaceID string, event *control.UIEvent, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cronSyncInFlight = false
	a.finishCronAsyncCommandLocked(surfaceID, event, err)
}

func (a *App) finishCronAsyncCommand(surfaceID string, event *control.UIEvent, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.finishCronAsyncCommandLocked(surfaceID, event, err)
}

func (a *App) finishCronAsyncCommandLocked(surfaceID string, event *control.UIEvent, err error) {
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

func (a *App) triggerCronJob(command control.DaemonCommand, jobRecordID string) (*control.UIEvent, error) {
	jobRecordID = strings.TrimSpace(jobRecordID)
	if jobRecordID == "" {
		return nil, fmt.Errorf("缺少要立即触发的 Cron 任务记录 ID")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	stateValue, err := a.loadCronStateLocked(false)
	if err != nil {
		return nil, err
	}
	if stateValue == nil || !cronStateHasBinding(stateValue) {
		return nil, fmt.Errorf("当前实例还没有可用的 Cron 配置表，请先执行 `/cron repair`")
	}
	snapshot := cloneCronState(stateValue)
	ownerView := a.inspectCronOwnerView(snapshot)
	if !cronOwnerAllowsLoadedJobs(ownerView.Status) {
		return nil, fmt.Errorf("当前 Cron 绑定需要先修复后才能手动触发任务，请先执行 `/cron repair`")
	}
	var job *cronJobState
	for index := range snapshot.Jobs {
		if strings.TrimSpace(snapshot.Jobs[index].RecordID) == jobRecordID {
			job = &snapshot.Jobs[index]
			break
		}
	}
	if job == nil {
		return nil, fmt.Errorf("找不到 Cron 任务 `%s`，请先执行 `/cron reload` 更新任务列表", jobRecordID)
	}
	activeCount := a.cronActiveRunCountLocked(job.RecordID, job.Name)
	maxConcurrency := cronDefaultMaxConcurrency(job.MaxConcurrency)
	if activeCount >= maxConcurrency {
		return nil, fmt.Errorf("任务 `%s` 当前运行中实例数已达到并发上限（%d），请稍后再试", firstNonEmpty(strings.TrimSpace(job.Name), job.RecordID), maxConcurrency)
	}
	triggeredAt := time.Now().UTC()
	request := a.newCronLaunchRequestLocked(*job, triggeredAt)
	if err := a.launchCronRequestLocked(request); err != nil {
		a.recordCronImmediateResultWithTargetLocked(request.WritebackTarget, request.Job, request.TriggeredAt, "failed", err.Error())
		return nil, err
	}
	return &control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: command.SurfaceSessionID,
		Notice: &control.Notice{
			Code:  "cron_run_ready",
			Title: "Cron",
			Text:  fmt.Sprintf("已立即触发 `%s`；本次不会改动原有下次调度时间。", firstNonEmpty(strings.TrimSpace(job.Name), job.RecordID)),
		},
	}, nil
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

func (a *App) prepareCronCatalogLocked(command control.DaemonCommand, mode cronCommandMode) (*control.UIEvent, error) {
	stateValue, err := a.loadCronStateLocked(false)
	if err != nil {
		return nil, err
	}
	snapshot := cloneCronState(stateValue)
	ownerView := a.inspectCronOwnerView(snapshot)
	var catalog *control.FeishuDirectCommandCatalog
	switch mode {
	case cronCommandMenu:
		catalog = buildCronMenuCatalog(snapshot, ownerView, "")
	case cronCommandStatus:
		catalog = buildCronStatusCatalog(snapshot, ownerView, "")
	case cronCommandList:
		catalog = buildCronListCatalog(snapshot, ownerView, "")
	case cronCommandEdit:
		catalog = buildCronEditCatalog(snapshot, ownerView, "")
	default:
		return nil, fmt.Errorf("unsupported cron catalog mode: %s", mode)
	}
	return &control.UIEvent{
		Kind:                       control.UIEventFeishuDirectCommandCatalog,
		SurfaceSessionID:           command.SurfaceSessionID,
		FeishuDirectCommandCatalog: catalog,
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
