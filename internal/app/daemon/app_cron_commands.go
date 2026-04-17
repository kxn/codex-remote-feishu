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
	parsed, err := parseCronCommandText(command.Text)
	if err != nil {
		return cronUsageEvents(command.SurfaceSessionID, err.Error())
	}
	switch parsed.Mode {
	case cronCommandMenu, cronCommandStatus, cronCommandList, cronCommandEdit:
		a.mu.Lock()
		shuttingDown := a.shuttingDown
		a.mu.Unlock()
		if shuttingDown {
			return nil
		}
		catalog, err := a.prepareCronCatalog(command, parsed.Mode)
		if err != nil {
			return append([]control.UIEvent{
				cronNoticeEvent(command.SurfaceSessionID, "cron_catalog_failed", fmt.Sprintf("Cron 信息读取失败：%v", err)),
			}, cronUsageEvents(command.SurfaceSessionID, "")...)
		}
		if catalog == nil {
			return cronUsageEvents(command.SurfaceSessionID, "")
		}
		return []control.UIEvent{*catalog}
	default:
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.shuttingDown {
			return nil
		}
		return a.handleCronMutatingDaemonCommandLocked(command, parsed)
	}
}

func (a *App) handleCronDaemonCommandLocked(command control.DaemonCommand) []control.UIEvent {
	parsed, err := parseCronCommandText(command.Text)
	if err != nil {
		return cronUsageEvents(command.SurfaceSessionID, err.Error())
	}
	switch parsed.Mode {
	case cronCommandMenu, cronCommandStatus, cronCommandList, cronCommandEdit:
		a.mu.Unlock()
		defer a.mu.Lock()
		catalog, err := a.prepareCronCatalog(command, parsed.Mode)
		if err != nil {
			return append([]control.UIEvent{
				cronNoticeEvent(command.SurfaceSessionID, "cron_catalog_failed", fmt.Sprintf("Cron 信息读取失败：%v", err)),
			}, cronUsageEvents(command.SurfaceSessionID, "")...)
		}
		if catalog == nil {
			return cronUsageEvents(command.SurfaceSessionID, "")
		}
		return []control.UIEvent{*catalog}
	default:
		return a.handleCronMutatingDaemonCommandLocked(command, parsed)
	}
}

func (a *App) handleCronMutatingDaemonCommandLocked(command control.DaemonCommand, parsed parsedCronCommand) []control.UIEvent {
	switch parsed.Mode {
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

func (a *App) prepareCronCatalog(command control.DaemonCommand, mode cronCommandMode) (*control.UIEvent, error) {
	stateValue, ownerView, extraSummary, configReady, err := a.prepareCronCatalogState(command, mode)
	if err != nil {
		return nil, err
	}
	var catalog *control.FeishuDirectCommandCatalog
	switch mode {
	case cronCommandMenu:
		catalog = buildCronMenuCatalog(stateValue, ownerView, extraSummary, configReady)
	case cronCommandStatus:
		catalog = buildCronStatusCatalog(stateValue, ownerView, extraSummary, configReady)
	case cronCommandList:
		catalog = buildCronListCatalog(stateValue, ownerView, extraSummary)
	case cronCommandEdit:
		catalog = buildCronEditCatalog(stateValue, ownerView, extraSummary, configReady)
	default:
		return nil, fmt.Errorf("unsupported cron catalog mode: %s", mode)
	}
	return &control.UIEvent{
		Kind:                       control.UIEventFeishuDirectCommandCatalog,
		SurfaceSessionID:           command.SurfaceSessionID,
		FeishuDirectCommandCatalog: catalog,
	}, nil
}

func (a *App) prepareCronCatalogState(command control.DaemonCommand, mode cronCommandMode) (*cronStateFile, cronOwnerView, string, bool, error) {
	a.mu.Lock()
	stateValue, err := a.loadCronStateLocked(false)
	if err != nil {
		a.mu.Unlock()
		return nil, cronOwnerView{}, "", false, err
	}
	snapshot := cloneCronState(stateValue)
	ownerView := a.inspectCronOwnerView(snapshot)
	a.mu.Unlock()

	configReady := true
	extraSummary := ""
	if !cronCatalogNeedsWorkspaceSync(mode, snapshot) {
		return snapshot, ownerView, extraSummary, configReady, nil
	}

	snapshot, ownerView, extraSummary, configReady = a.syncCronWorkspacesBeforeCatalog(command, snapshot, ownerView)
	return snapshot, ownerView, extraSummary, configReady, nil
}

func cronCatalogNeedsWorkspaceSync(mode cronCommandMode, stateValue *cronStateFile) bool {
	if !cronStateHasBinding(stateValue) {
		return false
	}
	switch mode {
	case cronCommandMenu, cronCommandStatus, cronCommandEdit:
		return true
	default:
		return false
	}
}

func (a *App) syncCronWorkspacesBeforeCatalog(command control.DaemonCommand, snapshot *cronStateFile, ownerView cronOwnerView) (*cronStateFile, cronOwnerView, string, bool) {
	resolution, err := a.resolveCronOwnerFromState(snapshot, command, cronOwnerResolveOptions{})
	if err != nil {
		return snapshot, ownerView, "工作区清单同步失败，已暂时隐藏配置入口：" + err.Error(), false
	}
	if err := cronOwnerActionError("同步工作区清单", resolution); err != nil {
		return snapshot, ownerView, "工作区清单尚未同步完成，已暂时隐藏配置入口：" + err.Error(), false
	}

	a.mu.Lock()
	if a.cronSyncInFlight {
		a.mu.Unlock()
		return snapshot, ownerView, "当前已有 Cron 配置同步在进行中，已暂时隐藏配置入口，请稍后重试。", false
	}
	a.cronSyncInFlight = true
	workspaces := a.cronWorkspaceRowsLocked()
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.cronSyncInFlight = false
		a.mu.Unlock()
	}()

	api, err := a.cronBitableAPI(resolution.Gateway.GatewayID)
	if err != nil {
		return snapshot, ownerView, "工作区清单同步失败，已暂时隐藏配置入口：" + err.Error(), false
	}
	workspaceCtx, cancelWorkspace := context.WithTimeout(context.Background(), cronReloadWorkspaceTTL)
	defer cancelWorkspace()
	if _, err := a.syncCronWorkspaceTable(workspaceCtx, api, resolution.Binding, workspaces); err != nil {
		return snapshot, ownerView, "工作区清单同步失败，已暂时隐藏配置入口：" + err.Error(), false
	}

	now := time.Now().UTC()
	if snapshot != nil {
		snapshot.LastWorkspaceSyncAt = now
	}
	a.mu.Lock()
	stateValue, err := a.loadCronStateLocked(false)
	if err == nil && stateValue != nil {
		stateValue.LastWorkspaceSyncAt = now
		if writeErr := a.writeCronStateLocked(); writeErr != nil {
			err = writeErr
		}
	}
	a.mu.Unlock()
	if err != nil {
		return snapshot, ownerView, "工作区清单已同步，但最近同步时间写回失败：" + err.Error(), true
	}
	return snapshot, ownerView, "", true
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
