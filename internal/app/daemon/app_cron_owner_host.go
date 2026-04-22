package daemon

import (
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func (a *App) defaultCronGatewayIdentityLookup(gatewayID string) (cronGatewayIdentity, bool, error) {
	gatewayID = strings.TrimSpace(gatewayID)
	if gatewayID == "" {
		return cronGatewayIdentity{}, false, nil
	}
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return cronGatewayIdentity{}, false, err
	}
	runtimeCfg, ok := a.runtimeGatewayConfigFor(loaded.Config, gatewayID)
	if !ok {
		return cronGatewayIdentity{}, false, nil
	}
	return cronGatewayIdentity{
		GatewayID: strings.TrimSpace(runtimeCfg.GatewayID),
		AppID:     strings.TrimSpace(runtimeCfg.AppID),
	}, true, nil
}

func (a *App) cronGatewayIdentity(gatewayID string) (cronGatewayIdentity, bool, error) {
	lookup := a.cronRuntime.gatewayIdentityLookup
	if lookup == nil {
		lookup = a.defaultCronGatewayIdentityLookup
	}
	return lookup(strings.TrimSpace(gatewayID))
}

func (a *App) migrateCronLegacyOwnerStateLocked(stateValue *cronStateFile) (bool, error) {
	if stateValue == nil {
		return false, nil
	}
	if currentOwner := cronOwnerBindingFromState(stateValue); currentOwner != nil {
		identity, ok, err := a.cronGatewayIdentity(currentOwner.GatewayID)
		if err != nil || !ok {
			return false, err
		}
		if nextOwner, changed := cronOwnerBindingBackfill(currentOwner, identity); changed {
			applyCronOwnerBinding(stateValue, nextOwner)
			return true, nil
		}
		return false, nil
	}
	legacyGateway := strings.TrimSpace(stateValue.GatewayID)
	if legacyGateway == "" {
		return false, nil
	}
	identity, ok, err := a.cronGatewayIdentity(legacyGateway)
	if err != nil || !ok {
		return false, err
	}
	applyCronOwnerBinding(stateValue, &cronOwnerBinding{
		GatewayID: identity.GatewayID,
		AppID:     identity.AppID,
		BoundAt:   time.Now().UTC(),
	})
	return true, nil
}

func (a *App) resolveCronOwner(command control.DaemonCommand, opts cronOwnerResolveOptions) (cronOwnerResolution, error) {
	a.mu.Lock()
	stateValue, err := a.loadCronStateLocked(opts.CreateStateIfEmpty)
	if err != nil {
		a.mu.Unlock()
		return cronOwnerResolution{}, err
	}
	var snapshot *cronStateFile
	if stateValue != nil {
		snapshot = cloneCronState(stateValue)
	}
	a.mu.Unlock()
	return a.resolveCronOwnerFromState(snapshot, command, opts)
}

func (a *App) resolveCronOwnerFromState(stateValue *cronStateFile, command control.DaemonCommand, opts cronOwnerResolveOptions) (cronOwnerResolution, error) {
	result := cronOwnerResolution{State: cloneCronState(stateValue)}
	if stateValue == nil {
		result.Status = cronOwnerStatusNone
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
	currentOwner := cronOwnerBindingFromState(stateValue)
	if currentOwner != nil {
		result.CurrentOwner = currentOwner
		identity, ok, err := a.cronGatewayIdentity(currentOwner.GatewayID)
		if err != nil {
			return cronOwnerResolution{}, err
		}
		if !ok {
			result.Status = cronOwnerStatusUnavailable
			result.Message = fmt.Sprintf("Cron owner `%s` 当前不在运行时配置中。", currentOwner.GatewayID)
			return result, nil
		}
		result.Gateway = identity
		if currentOwner.AppID != "" && identity.AppID != "" && identity.AppID != currentOwner.AppID {
			result.Status = cronOwnerStatusMismatch
			result.Message = fmt.Sprintf("Cron owner `%s` 的当前 AppID 与已绑定 ownerAppID 不一致。", currentOwner.GatewayID)
			return result, nil
		}
		if nextOwner, changed := cronOwnerBindingBackfill(currentOwner, identity); changed {
			result.PersistOwner = nextOwner
		}
		result.Status = cronOwnerStatusHealthy
		result.Message = fmt.Sprintf("Cron owner 为 `%s`。", identity.GatewayID)
		return result, nil
	}
	legacyGateway := strings.TrimSpace(stateValue.GatewayID)
	if legacyGateway != "" {
		identity, ok, err := a.cronGatewayIdentity(legacyGateway)
		if err != nil {
			return cronOwnerResolution{}, err
		}
		if ok {
			result.Status = cronOwnerStatusHealthy
			result.Gateway = identity
			result.PersistOwner = &cronOwnerBinding{GatewayID: identity.GatewayID, AppID: identity.AppID, BoundAt: time.Now().UTC()}
			result.Message = fmt.Sprintf("Cron owner 为 `%s`。", identity.GatewayID)
			return result, nil
		}
		result.Status = cronOwnerStatusUnresolved
		result.Message = fmt.Sprintf("历史 gateway `%s` 当前无法安全迁到正式 Cron owner。", legacyGateway)
		return result, nil
	}
	if opts.AllowCreate {
		return a.resolveCronBootstrapOwner(result, command)
	}
	result.Status = cronOwnerStatusNone
	result.Message = "当前实例还没有初始化 Cron 配置表。"
	return result, nil
}

func (a *App) resolveCronBootstrapOwner(result cronOwnerResolution, command control.DaemonCommand) (cronOwnerResolution, error) {
	candidateGatewayID := firstNonEmpty(strings.TrimSpace(command.GatewayID), a.service.SurfaceGatewayID(command.SurfaceSessionID))
	if strings.TrimSpace(candidateGatewayID) == "" {
		result.Status = cronOwnerStatusUnresolved
		result.Message = "当前无法确定用于创建 Cron 配置表的 bot。"
		return result, nil
	}
	identity, ok, err := a.cronGatewayIdentity(candidateGatewayID)
	if err != nil {
		return cronOwnerResolution{}, err
	}
	if !ok {
		result.Status = cronOwnerStatusUnavailable
		result.Message = fmt.Sprintf("找不到用于创建 Cron 配置表的 gateway `%s`。", candidateGatewayID)
		return result, nil
	}
	result.Status = cronOwnerStatusBootstrap
	result.Gateway = identity
	result.PersistOwner = &cronOwnerBinding{GatewayID: identity.GatewayID, AppID: identity.AppID, BoundAt: time.Now().UTC()}
	result.Message = fmt.Sprintf("将使用当前 surface 对应的 bot `%s` 初始化 Cron 配置表。", identity.GatewayID)
	return result, nil
}

func (a *App) inspectCronOwnerView(stateValue *cronStateFile) cronOwnerView {
	resolution, err := a.resolveCronOwnerFromState(stateValue, control.DaemonCommand{}, cronOwnerResolveOptions{})
	if err != nil {
		return cronOwnerView{
			Status:      cronOwnerStatusUnresolved,
			StatusLabel: "无法检查",
			Detail:      err.Error(),
			NextAction:  "稍后重试 `/cron`，或先检查本地飞书运行时配置。",
		}
	}
	view := cronOwnerView{Status: resolution.Status}
	switch resolution.Status {
	case cronOwnerStatusHealthy:
		view.StatusLabel = "正常"
		view.Detail = "当前 Cron 配置可正常读写。"
		view.NextAction = "编辑表格后执行 `/cron reload` 生效；如需同步 schema 或工作区清单，执行 `/cron repair`。"
	case cronOwnerStatusBootstrap:
		view.StatusLabel = "待初始化"
		view.Detail = "当前实例还没有初始化 Cron 配置表。"
		view.NextAction = "执行 `/cron repair` 初始化 Cron 配置。"
		view.NeedsRepair = true
	case cronOwnerStatusUnavailable:
		view.StatusLabel = "需要修复"
		view.Detail = "当前 Cron 绑定已失效。"
		view.NextAction = "执行 `/cron repair` 后，将由当前 bot 接管 Cron 配置。"
		view.NeedsRepair = true
	case cronOwnerStatusMismatch:
		view.StatusLabel = "需要修复"
		view.Detail = "当前 Cron 绑定与运行时配置不一致。"
		view.NextAction = "执行 `/cron repair` 后，将由当前 bot 接管 Cron 配置。"
		view.NeedsRepair = true
	case cronOwnerStatusUnresolved:
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
