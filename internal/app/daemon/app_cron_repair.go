package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	cronrt "github.com/kxn/codex-remote-feishu/internal/app/cronruntime"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func (a *App) repairCronBitableForResolution(command control.DaemonCommand, resolution cronrt.OwnerResolution) (string, error) {
	if cronOwnerNeedsRepairTakeover(resolution.Status) {
		return a.repairCronBitableTakeoverNow(command, resolution)
	}
	if err := cronrt.OwnerActionError("修复 Cron 配置表", resolution); err != nil {
		return "", err
	}
	return a.repairCronBitableWithBinding(command, resolution, resolution.Binding, resolution.PersistOwner, false)
}

func cronOwnerNeedsRepairTakeover(status cronrt.OwnerStatus) bool {
	switch status {
	case cronrt.OwnerStatusUnavailable, cronrt.OwnerStatusMismatch, cronrt.OwnerStatusUnresolved:
		return true
	default:
		return false
	}
}

func (a *App) repairCronBitableTakeoverNow(command control.DaemonCommand, current cronrt.OwnerResolution) (string, error) {
	a.mu.Lock()
	activeRuns := len(a.cronRuntime.runs)
	a.mu.Unlock()
	if activeRuns > 0 {
		return "", fmt.Errorf("当前还有 %d 个运行中的 Cron 任务，暂时不能接管 Cron 配置", activeRuns)
	}
	target, err := a.resolveCronBootstrapOwner(cronrt.OwnerResolution{
		State:    current.State,
		ScopeKey: current.ScopeKey,
		Label:    current.Label,
	}, command)
	if err != nil {
		return "", err
	}
	if target.Status != cronrt.OwnerStatusBootstrap {
		return "", cronrt.OwnerActionError("接管 Cron 配置", target)
	}
	summary, err := a.repairCronBitableWithBinding(command, target, cronrt.BitableState{}, target.PersistOwner, true)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(current.Message) != "" {
		return summary + "\n原绑定状态：" + strings.TrimSpace(current.Message), nil
	}
	return summary, nil
}

func (a *App) repairCronBitableWithBinding(command control.DaemonCommand, resolution cronrt.OwnerResolution, previous cronrt.BitableState, persistOwner *cronrt.OwnerBinding, takeover bool) (string, error) {
	api, err := a.cronBitableAPI(resolution.Gateway.GatewayID)
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	workspaces := a.cronWorkspaceRowsLocked()
	a.mu.Unlock()
	persistProgress := func(next cronrt.BitableState) error {
		progressOwner := persistOwner
		if !takeover && resolution.Status != cronrt.OwnerStatusBootstrap {
			progressOwner = nil
		}
		return a.persistCronBitableBindingProgress(resolution.ScopeKey, resolution.Label, next, progressOwner)
	}
	bootstrapCtx, cancelBootstrap := context.WithTimeout(context.Background(), cronrt.BitableBootstrapTTL)
	defer cancelBootstrap()
	updatedBinding, err := a.ensureCronBitableRemote(bootstrapCtx, api, previous, resolution.ScopeKey, resolution.Label, persistOwner, persistProgress)
	if err != nil {
		return "", err
	}
	workspaceCtx, cancelWorkspace := context.WithTimeout(context.Background(), cronrt.BitableWorkspaceTTL)
	defer cancelWorkspace()
	if _, err := a.syncCronWorkspaceTable(workspaceCtx, api, updatedBinding, workspaces); err != nil {
		return "", err
	}
	var permissionWarning string
	permissionCtx, cancelPermission := context.WithTimeout(context.Background(), cronrt.BitablePermissionTTL)
	defer cancelPermission()
	if err := a.ensureCronUserPermission(permissionCtx, api, updatedBinding.AppToken, a.service.SurfaceActorUserID(command.SurfaceSessionID)); err != nil {
		permissionWarning = "已跳过当前飞书会话用户的编辑权限补齐：" + err.Error()
	}

	now := time.Now().UTC()
	a.mu.Lock()
	defer a.mu.Unlock()
	stateValue, err := a.loadCronStateLocked(true)
	if err != nil {
		return "", err
	}
	stateValue.InstanceScopeKey = resolution.ScopeKey
	stateValue.InstanceLabel = resolution.Label
	stateValue.Bitable = &updatedBinding
	stateValue.LastWorkspaceSyncAt = now
	if persistOwner != nil {
		cronrt.ApplyOwnerBinding(stateValue, persistOwner)
	}
	if takeover {
		stateValue.Jobs = []cronrt.JobState{}
		stateValue.LastReloadAt = time.Time{}
		stateValue.LastReloadSummary = ""
		a.cronRuntime.nextScheduleScan = time.Time{}
	}
	if err := a.writeCronStateLocked(); err != nil {
		return "", err
	}
	var summary string
	if takeover {
		summary = fmt.Sprintf("已由当前 bot `%s` 接管 Cron 配置，并同步 %d 个工作区。旧表数据未自动迁移；如需保留旧配置或历史，请先恢复原 owner 环境。编辑表格后发送 `/cron reload` 生效。", firstNonEmpty(resolution.Gateway.GatewayID, stateValue.OwnerGatewayID), len(workspaces))
	} else {
		summary = fmt.Sprintf("已同步 %d 个工作区。编辑表格后发送 `/cron reload` 生效。", len(workspaces))
	}
	if permissionWarning != "" {
		summary += "\n" + permissionWarning
	}
	return summary, nil
}
