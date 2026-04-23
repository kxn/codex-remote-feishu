package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	cronrt "github.com/kxn/codex-remote-feishu/internal/app/cronruntime"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func (a *App) handleCronDaemonCommand(command control.DaemonCommand) []eventcontract.Event {
	parsed, err := cronrt.ParseCommandText(command.Text)
	if err != nil {
		return cronrt.UsageEvents(command.SurfaceSessionID, commandArgumentText(command.Text), err.Error())
	}
	switch parsed.Mode {
	case cronrt.CommandModeMenu, cronrt.CommandModeStatus, cronrt.CommandModeList, cronrt.CommandModeEdit:
		a.mu.Lock()
		shuttingDown := a.shuttingDown
		a.mu.Unlock()
		if shuttingDown {
			return nil
		}
		catalog, err := a.prepareCronCatalog(command, parsed.Mode)
		if err != nil {
			return append([]eventcontract.Event{
				cronrt.NoticeEvent(command.SurfaceSessionID, "cron_catalog_failed", fmt.Sprintf("Cron 信息读取失败：%v", err)),
			}, cronrt.UsageEvents(command.SurfaceSessionID, "", "")...)
		}
		if catalog == nil {
			return cronrt.UsageEvents(command.SurfaceSessionID, "", "")
		}
		return []eventcontract.Event{*catalog}
	default:
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.shuttingDown {
			return nil
		}
		return a.handleCronMutatingDaemonCommandLocked(command, parsed)
	}
}

func (a *App) handleCronDaemonCommandLocked(command control.DaemonCommand) []eventcontract.Event {
	parsed, err := cronrt.ParseCommandText(command.Text)
	if err != nil {
		return cronrt.UsageEvents(command.SurfaceSessionID, commandArgumentText(command.Text), err.Error())
	}
	switch parsed.Mode {
	case cronrt.CommandModeMenu, cronrt.CommandModeStatus, cronrt.CommandModeList, cronrt.CommandModeEdit:
		a.mu.Unlock()
		defer a.mu.Lock()
		catalog, err := a.prepareCronCatalog(command, parsed.Mode)
		if err != nil {
			return append([]eventcontract.Event{
				cronrt.NoticeEvent(command.SurfaceSessionID, "cron_catalog_failed", fmt.Sprintf("Cron 信息读取失败：%v", err)),
			}, cronrt.UsageEvents(command.SurfaceSessionID, "", "")...)
		}
		if catalog == nil {
			return cronrt.UsageEvents(command.SurfaceSessionID, "", "")
		}
		return []eventcontract.Event{*catalog}
	default:
		return a.handleCronMutatingDaemonCommandLocked(command, parsed)
	}
}

func (a *App) handleCronMutatingDaemonCommandLocked(command control.DaemonCommand, parsed cronrt.ParsedCommand) []eventcontract.Event {
	switch parsed.Mode {
	case cronrt.CommandModeRepair:
		if a.cronRuntime.syncInFlight {
			return []eventcontract.Event{cronrt.NoticeEvent(command.SurfaceSessionID, "cron_busy", "当前已有一个 Cron 配置同步在进行中，请稍后再试。")}
		}
		a.cronRuntime.syncInFlight = true
		go a.runCronRepairCommand(command)
		return []eventcontract.Event{cronrt.NoticeEvent(command.SurfaceSessionID, "cron_repair_started", "正在修复 Cron 配置表；如果检测到绑定失效，将自动由当前 bot 接管 Cron 配置，请稍候。")}
	case cronrt.CommandModeReload:
		if a.cronRuntime.syncInFlight {
			return []eventcontract.Event{cronrt.NoticeEvent(command.SurfaceSessionID, "cron_busy", "当前已有一个 Cron 配置同步在进行中，请稍后再试。")}
		}
		a.cronRuntime.syncInFlight = true
		go a.runCronReloadCommand(command)
		return []eventcontract.Event{cronrt.NoticeEvent(command.SurfaceSessionID, "cron_reload_started", "正在重新加载 Cron 任务配置，并校验表格内容。")}
	case cronrt.CommandModeRun:
		if a.cronRuntime.syncInFlight {
			return []eventcontract.Event{cronrt.NoticeEvent(command.SurfaceSessionID, "cron_busy", "当前已有一个 Cron 配置同步在进行中，请稍后再试。")}
		}
		go a.runCronTriggerCommand(command, parsed.JobRecordID)
		return []eventcontract.Event{cronrt.NoticeEvent(command.SurfaceSessionID, "cron_run_started", "正在立即触发所选 Cron 任务。")}
	default:
		return cronrt.UsageEvents(command.SurfaceSessionID, commandArgumentText(command.Text), "不支持的 /cron 子命令。")
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

func (a *App) finishCronBackgroundCommand(surfaceID string, event *eventcontract.Event, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cronRuntime.syncInFlight = false
	a.finishCronAsyncCommandLocked(surfaceID, event, err)
}

func (a *App) finishCronAsyncCommand(surfaceID string, event *eventcontract.Event, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.finishCronAsyncCommandLocked(surfaceID, event, err)
}

func (a *App) finishCronAsyncCommandLocked(surfaceID string, event *eventcontract.Event, err error) {
	if a.shuttingDown {
		return
	}
	if err != nil {
		a.handleUIEventsLocked(context.Background(), []eventcontract.Event{
			cronrt.NoticeEvent(surfaceID, "cron_command_failed", fmt.Sprintf("Cron 操作失败：%v", err)),
		})
		return
	}
	if event != nil {
		a.handleUIEventsLocked(context.Background(), []eventcontract.Event{*event})
	}
}

func (a *App) triggerCronJob(command control.DaemonCommand, jobRecordID string) (*eventcontract.Event, error) {
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
	if stateValue == nil || !cronrt.StateHasBinding(stateValue) {
		return nil, fmt.Errorf("当前实例还没有可用的 Cron 配置表，请先执行 `/cron repair`")
	}
	snapshot := cronrt.CloneState(stateValue)
	ownerView := a.inspectCronOwnerView(snapshot)
	if !cronrt.OwnerAllowsLoadedJobs(ownerView.Status) {
		return nil, fmt.Errorf("当前 Cron 绑定需要先修复后才能手动触发任务，请先执行 `/cron repair`")
	}
	var job *cronrt.JobState
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
	maxConcurrency := cronrt.DefaultMaxConcurrency(job.MaxConcurrency)
	if activeCount >= maxConcurrency {
		return nil, fmt.Errorf("任务 `%s` 当前运行中实例数已达到并发上限（%d），请稍后再试", firstNonEmpty(strings.TrimSpace(job.Name), job.RecordID), maxConcurrency)
	}
	triggeredAt := time.Now().UTC()
	request := a.newCronLaunchRequestLocked(*job, triggeredAt)
	if err := a.launchCronRequestLocked(request); err != nil {
		a.recordCronImmediateResultWithTargetLocked(request.WritebackTarget, request.Job, request.TriggeredAt, "failed", err.Error())
		return nil, err
	}
	return &eventcontract.Event{
		Kind:             eventcontract.KindNotice,
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
	api := feishu.NewLiveBitableAPI(runtimeCfg.GatewayID, runtimeCfg.AppID, runtimeCfg.AppSecret)
	if api == nil {
		return nil, fmt.Errorf("gateway %q 缺少可用的 App ID / App Secret", gatewayID)
	}
	return api, nil
}

func (a *App) cronBitableAPI(gatewayID string) (feishu.BitableAPI, error) {
	factory := a.cronRuntime.bitableFactory
	if factory == nil {
		factory = a.defaultCronBitableFactory
	}
	return factory(strings.TrimSpace(gatewayID))
}

func (a *App) prepareCronCatalog(command control.DaemonCommand, mode cronrt.CommandMode) (*eventcontract.Event, error) {
	stateValue, ownerView, extraSummary, configReady, err := a.prepareCronCatalogState(command, mode)
	if err != nil {
		return nil, err
	}
	var view control.FeishuPageView
	switch mode {
	case cronrt.CommandModeMenu:
		view = cronrt.BuildRootPageView(stateValue, ownerView, extraSummary, configReady, "", "", "")
	case cronrt.CommandModeStatus:
		view = cronrt.BuildStatusPageView(stateValue, ownerView, extraSummary, configReady)
	case cronrt.CommandModeList:
		view = cronrt.BuildListPageView(stateValue, ownerView, extraSummary)
	case cronrt.CommandModeEdit:
		view = cronrt.BuildEditPageView(stateValue, ownerView, extraSummary, configReady)
	default:
		return nil, fmt.Errorf("unsupported cron catalog mode: %s", mode)
	}
	event := commandPageEvent(command.SurfaceSessionID, view)
	return &event, nil
}

func (a *App) prepareCronCatalogState(command control.DaemonCommand, mode cronrt.CommandMode) (*cronrt.StateFile, cronrt.OwnerView, string, bool, error) {
	a.mu.Lock()
	stateValue, err := a.loadCronStateLocked(false)
	if err != nil {
		a.mu.Unlock()
		return nil, cronrt.OwnerView{}, "", false, err
	}
	snapshot := cronrt.CloneState(stateValue)
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

func cronCatalogNeedsWorkspaceSync(mode cronrt.CommandMode, stateValue *cronrt.StateFile) bool {
	if !cronrt.StateHasBinding(stateValue) {
		return false
	}
	switch mode {
	case cronrt.CommandModeMenu, cronrt.CommandModeStatus, cronrt.CommandModeEdit:
		return true
	default:
		return false
	}
}

func (a *App) syncCronWorkspacesBeforeCatalog(command control.DaemonCommand, snapshot *cronrt.StateFile, ownerView cronrt.OwnerView) (*cronrt.StateFile, cronrt.OwnerView, string, bool) {
	resolution, err := a.resolveCronOwnerFromState(snapshot, command, cronrt.OwnerResolveOptions{})
	if err != nil {
		return snapshot, ownerView, "工作区清单同步失败，已暂时隐藏配置入口：" + err.Error(), false
	}
	if err := cronrt.OwnerActionError("同步工作区清单", resolution); err != nil {
		return snapshot, ownerView, "工作区清单尚未同步完成，已暂时隐藏配置入口：" + err.Error(), false
	}

	a.mu.Lock()
	if a.cronRuntime.syncInFlight {
		a.mu.Unlock()
		return snapshot, ownerView, "当前已有 Cron 配置同步在进行中，已暂时隐藏配置入口，请稍后重试。", false
	}
	a.cronRuntime.syncInFlight = true
	workspaces := a.cronWorkspaceRowsLocked()
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.cronRuntime.syncInFlight = false
		a.mu.Unlock()
	}()

	api, err := a.cronBitableAPI(resolution.Gateway.GatewayID)
	if err != nil {
		return snapshot, ownerView, "工作区清单同步失败，已暂时隐藏配置入口：" + err.Error(), false
	}
	workspaceCtx, cancelWorkspace := context.WithTimeout(context.Background(), cronrt.ReloadWorkspaceTTL)
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

func (a *App) repairCronBitable(command control.DaemonCommand) (*eventcontract.Event, error) {
	summary, err := a.repairCronBitableNow(command)
	if err != nil {
		return nil, err
	}
	return &eventcontract.Event{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: command.SurfaceSessionID,
		Notice: &control.Notice{
			Code:  "cron_repair_ready",
			Title: "Cron",
			Text:  summary,
		},
	}, nil
}

func (a *App) reloadCronJobs(command control.DaemonCommand) (*eventcontract.Event, error) {
	result, err := a.reloadCronJobsResultNow(command)
	if err != nil {
		return nil, err
	}
	return &eventcontract.Event{
		Kind:             eventcontract.KindNotice,
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
	for _, item := range cronrt.IntervalChoices {
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
	return now.Add(cronrt.ScheduleScanEvery)
}
