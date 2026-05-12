package daemon

import (
	"fmt"
	"strings"
	"time"

	cronrt "github.com/kxn/codex-remote-feishu/internal/app/cronruntime"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func (a *App) defaultCronGatewayIdentityLookup(gatewayID string) (cronrt.GatewayIdentity, bool, error) {
	gatewayID = strings.TrimSpace(gatewayID)
	if gatewayID == "" {
		return cronrt.GatewayIdentity{}, false, nil
	}
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return cronrt.GatewayIdentity{}, false, err
	}
	runtimeCfg, ok := a.runtimeGatewayConfigFor(loaded.Config, gatewayID)
	if !ok {
		return cronrt.GatewayIdentity{}, false, nil
	}
	return cronrt.GatewayIdentity{
		GatewayID: strings.TrimSpace(runtimeCfg.GatewayID),
		AppID:     strings.TrimSpace(runtimeCfg.AppID),
	}, true, nil
}

func (a *App) cronGatewayIdentity(gatewayID string) (cronrt.GatewayIdentity, bool, error) {
	lookup := a.cronRuntime.gatewayIdentityLookup
	if lookup == nil {
		lookup = a.defaultCronGatewayIdentityLookup
	}
	return lookup(strings.TrimSpace(gatewayID))
}

func (a *App) resolveCronOwner(command control.DaemonCommand, opts cronrt.OwnerResolveOptions) (cronrt.OwnerResolution, error) {
	a.mu.Lock()
	stateValue, err := a.loadCronStateLocked(opts.CreateStateIfEmpty)
	if err != nil {
		a.mu.Unlock()
		return cronrt.OwnerResolution{}, err
	}
	var snapshot *cronrt.StateFile
	if stateValue != nil {
		snapshot = cronrt.CloneState(stateValue)
	}
	a.mu.Unlock()
	return a.resolveCronOwnerFromState(snapshot, command, opts)
}

func (a *App) resolveCronOwnerFromState(stateValue *cronrt.StateFile, command control.DaemonCommand, opts cronrt.OwnerResolveOptions) (cronrt.OwnerResolution, error) {
	result := cronrt.OwnerResolution{State: cronrt.CloneState(stateValue)}
	if stateValue == nil {
		result.Status = cronrt.OwnerStatusNone
		result.Message = "当前实例还没有初始化 Cron 配置表。"
		if opts.AllowCreate {
			return a.resolveCronBootstrapOwner(result, command)
		}
		return result, nil
	}
	result.ScopeKey = strings.TrimSpace(stateValue.InstanceScopeKey)
	result.Label = strings.TrimSpace(stateValue.InstanceLabel)
	if stateValue.Bitable != nil {
		result.Binding = *stateValue.Bitable
	}
	currentOwner := cronrt.OwnerBindingFromState(stateValue)
	if currentOwner != nil {
		result.CurrentOwner = currentOwner
		identity, ok, err := a.cronGatewayIdentity(currentOwner.GatewayID)
		if err != nil {
			return cronrt.OwnerResolution{}, err
		}
		if !ok {
			result.Status = cronrt.OwnerStatusUnavailable
			result.Message = fmt.Sprintf("Cron owner `%s` 当前不在运行时配置中。", currentOwner.GatewayID)
			return result, nil
		}
		result.Gateway = identity
		if currentOwner.AppID != "" && identity.AppID != "" && identity.AppID != currentOwner.AppID {
			result.Status = cronrt.OwnerStatusMismatch
			result.Message = fmt.Sprintf("Cron owner `%s` 的当前 AppID 与已绑定 ownerAppID 不一致。", currentOwner.GatewayID)
			return result, nil
		}
		if nextOwner, changed := cronrt.OwnerBindingBackfill(currentOwner, identity); changed {
			result.PersistOwner = nextOwner
		}
		result.Status = cronrt.OwnerStatusHealthy
		result.Message = fmt.Sprintf("Cron owner 为 `%s`。", identity.GatewayID)
		return result, nil
	}
	if opts.AllowCreate {
		return a.resolveCronBootstrapOwner(result, command)
	}
	result.Status = cronrt.OwnerStatusNone
	result.Message = "当前实例还没有初始化 Cron 配置表。"
	return result, nil
}

func (a *App) resolveCronBootstrapOwner(result cronrt.OwnerResolution, command control.DaemonCommand) (cronrt.OwnerResolution, error) {
	candidateGatewayID := firstNonEmpty(strings.TrimSpace(command.GatewayID), a.service.SurfaceGatewayID(command.SurfaceSessionID))
	if strings.TrimSpace(candidateGatewayID) == "" {
		result.Status = cronrt.OwnerStatusUnresolved
		result.Message = "当前无法确定用于创建 Cron 配置表的 bot。"
		return result, nil
	}
	identity, ok, err := a.cronGatewayIdentity(candidateGatewayID)
	if err != nil {
		return cronrt.OwnerResolution{}, err
	}
	if !ok {
		result.Status = cronrt.OwnerStatusUnavailable
		result.Message = fmt.Sprintf("找不到用于创建 Cron 配置表的 gateway `%s`。", candidateGatewayID)
		return result, nil
	}
	result.Status = cronrt.OwnerStatusBootstrap
	result.Gateway = identity
	result.PersistOwner = &cronrt.OwnerBinding{GatewayID: identity.GatewayID, AppID: identity.AppID, BoundAt: time.Now().UTC()}
	result.Message = fmt.Sprintf("将使用当前飞书会话对应的 bot `%s` 初始化 Cron 配置表。", identity.GatewayID)
	return result, nil
}

func (a *App) inspectCronOwnerView(stateValue *cronrt.StateFile) cronrt.OwnerView {
	resolution, err := a.resolveCronOwnerFromState(stateValue, control.DaemonCommand{}, cronrt.OwnerResolveOptions{})
	if err != nil {
		return cronrt.OwnerView{
			Status:      cronrt.OwnerStatusUnresolved,
			StatusLabel: "无法检查",
			Detail:      err.Error(),
			NextAction:  "稍后重试 `/cron`，或先检查本地飞书运行时配置。",
		}
	}
	view := cronrt.OwnerView{Status: resolution.Status}
	switch resolution.Status {
	case cronrt.OwnerStatusHealthy:
		view.StatusLabel = "正常"
		view.Detail = "当前 Cron 配置可正常读写。"
		view.NextAction = "编辑表格后执行 `/cron reload` 生效；如需同步 schema 或工作区清单，执行 `/cron repair`。"
	case cronrt.OwnerStatusBootstrap:
		view.StatusLabel = "待初始化"
		view.Detail = "当前实例还没有初始化 Cron 配置表。"
		view.NextAction = "执行 `/cron repair` 初始化 Cron 配置。"
		view.NeedsRepair = true
	case cronrt.OwnerStatusUnavailable:
		view.StatusLabel = "需要修复"
		view.Detail = "当前 Cron 绑定已失效。"
		view.NextAction = "执行 `/cron repair` 后，将由当前 bot 接管 Cron 配置。"
		view.NeedsRepair = true
	case cronrt.OwnerStatusMismatch:
		view.StatusLabel = "需要修复"
		view.Detail = "当前 Cron 绑定与运行时配置不一致。"
		view.NextAction = "执行 `/cron repair` 后，将由当前 bot 接管 Cron 配置。"
		view.NeedsRepair = true
	case cronrt.OwnerStatusUnresolved:
		view.StatusLabel = "需要修复"
		view.Detail = "当前 Cron 绑定待修复。"
		view.NextAction = "执行 `/cron repair` 后，将由当前 bot 接管 Cron 配置。"
		view.NeedsRepair = true
	default:
		view.StatusLabel = "未初始化"
		view.Detail = "当前实例还没有初始化 Cron 配置表。"
		view.NextAction = "执行 `/cron repair` 创建配置表。"
		view.NeedsRepair = true
	}
	return view
}
