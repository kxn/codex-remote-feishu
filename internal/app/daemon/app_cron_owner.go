package daemon

import (
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

type cronGatewayIdentity struct {
	GatewayID string
	AppID     string
}

type cronOwnerStatus string

const (
	cronOwnerStatusNone        cronOwnerStatus = "none"
	cronOwnerStatusHealthy     cronOwnerStatus = "healthy"
	cronOwnerStatusBootstrap   cronOwnerStatus = "bootstrap"
	cronOwnerStatusLegacy      cronOwnerStatus = "legacy"
	cronOwnerStatusUnavailable cronOwnerStatus = "unavailable"
	cronOwnerStatusMismatch    cronOwnerStatus = "mismatch"
	cronOwnerStatusUnresolved  cronOwnerStatus = "unresolved"
)

type cronOwnerBinding struct {
	GatewayID string
	AppID     string
	BoundAt   time.Time
}

type cronOwnerResolution struct {
	Status        cronOwnerStatus
	State         *cronStateFile
	ScopeKey      string
	Label         string
	Binding       cronBitableState
	Gateway       cronGatewayIdentity
	PersistOwner  *cronOwnerBinding
	CurrentOwner  *cronOwnerBinding
	LegacyGateway string
	Message       string
}

type cronOwnerView struct {
	Status      cronOwnerStatus
	StatusLabel string
	Detail      string
	NextAction  string
	NeedsRepair bool
}

type cronOwnerResolveOptions struct {
	AllowCreate        bool
	CreateStateIfEmpty bool
}

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
	lookup := a.cronGatewayIdentityLookup
	if lookup == nil {
		lookup = a.defaultCronGatewayIdentityLookup
	}
	return lookup(strings.TrimSpace(gatewayID))
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
		if currentOwner.AppID == "" && identity.AppID != "" {
			result.PersistOwner = &cronOwnerBinding{GatewayID: identity.GatewayID, AppID: identity.AppID, BoundAt: currentOwner.BoundAt}
		}
		result.Status = cronOwnerStatusHealthy
		result.Message = fmt.Sprintf("Cron owner 为 `%s`。", identity.GatewayID)
		return result, nil
	}
	legacyGateway := strings.TrimSpace(stateValue.GatewayID)
	result.LegacyGateway = legacyGateway
	if legacyGateway != "" {
		identity, ok, err := a.cronGatewayIdentity(legacyGateway)
		if err != nil {
			return cronOwnerResolution{}, err
		}
		if ok {
			result.Status = cronOwnerStatusLegacy
			result.Gateway = identity
			result.PersistOwner = &cronOwnerBinding{GatewayID: identity.GatewayID, AppID: identity.AppID, BoundAt: time.Now().UTC()}
			result.Message = fmt.Sprintf("当前仍使用 legacy gateway `%s`，下次成功修复或 reload 后会回填正式 owner。", identity.GatewayID)
			return result, nil
		}
		result.Status = cronOwnerStatusUnresolved
		result.Message = fmt.Sprintf("存在 legacy gateway `%s`，但当前无法安全确认它仍是 Cron owner。", legacyGateway)
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

func cronOwnerBindingFromState(stateValue *cronStateFile) *cronOwnerBinding {
	if stateValue == nil {
		return nil
	}
	gatewayID := strings.TrimSpace(stateValue.OwnerGatewayID)
	appID := strings.TrimSpace(stateValue.OwnerAppID)
	if gatewayID == "" && appID == "" {
		return nil
	}
	return &cronOwnerBinding{
		GatewayID: gatewayID,
		AppID:     appID,
		BoundAt:   stateValue.OwnerBoundAt.UTC(),
	}
}

func applyCronOwnerBinding(stateValue *cronStateFile, owner *cronOwnerBinding) {
	if stateValue == nil || owner == nil {
		return
	}
	stateValue.OwnerGatewayID = strings.TrimSpace(owner.GatewayID)
	stateValue.OwnerAppID = strings.TrimSpace(owner.AppID)
	stateValue.OwnerBoundAt = owner.BoundAt.UTC()
	if strings.TrimSpace(owner.GatewayID) != "" {
		stateValue.GatewayID = strings.TrimSpace(owner.GatewayID)
	}
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
	case cronOwnerStatusLegacy:
		view.StatusLabel = "待修复"
		view.Detail = "当前 Cron 配置仍使用历史兼容绑定。"
		view.NextAction = "执行 `/cron repair` 完成修复并同步工作区；或执行 `/cron reload` 回填并重新加载任务。"
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

func cronOwnerActionError(action string, resolution cronOwnerResolution) error {
	action = strings.TrimSpace(action)
	if action == "" {
		action = "执行 Cron 操作"
	}
	switch resolution.Status {
	case cronOwnerStatusHealthy, cronOwnerStatusBootstrap, cronOwnerStatusLegacy:
		return nil
	case cronOwnerStatusNone:
		return fmt.Errorf("尚未初始化 Cron 配置表，请先执行 `/cron repair`")
	case cronOwnerStatusUnavailable:
		return fmt.Errorf("%s失败：当前 Cron 绑定已失效，请先执行 `/cron repair`", action)
	case cronOwnerStatusMismatch:
		return fmt.Errorf("%s失败：当前 Cron 绑定与运行时配置不一致，请先执行 `/cron repair`", action)
	case cronOwnerStatusUnresolved:
		return fmt.Errorf("%s失败：当前 Cron 绑定待修复，请先执行 `/cron repair`", action)
	default:
		return fmt.Errorf("%s失败：Cron owner 状态无效", action)
	}
}

func (r cronOwnerResolution) writebackTarget() cronWritebackTarget {
	if strings.TrimSpace(r.Binding.AppToken) == "" {
		return cronWritebackTarget{}
	}
	gatewayID := strings.TrimSpace(r.Gateway.GatewayID)
	if gatewayID == "" && r.CurrentOwner != nil {
		gatewayID = strings.TrimSpace(r.CurrentOwner.GatewayID)
	}
	if gatewayID == "" {
		gatewayID = strings.TrimSpace(r.LegacyGateway)
	}
	return cronWritebackTarget{
		GatewayID: gatewayID,
		Bitable:   r.Binding,
	}
}
