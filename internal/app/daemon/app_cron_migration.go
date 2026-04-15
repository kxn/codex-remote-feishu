package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	larkbitable "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func (a *App) loadCronJobsFromBinding(api feishu.BitableAPI, binding *cronBitableState) ([]cronJobState, string, error) {
	if binding == nil {
		return nil, "", fmt.Errorf("Cron 多维表格还没有初始化完成")
	}
	workspaceCtx, cancelWorkspace := context.WithTimeout(context.Background(), cronReloadWorkspaceTTL)
	defer cancelWorkspace()
	workspacesByRecord, err := a.loadCronWorkspaceIndex(workspaceCtx, api, binding)
	if err != nil {
		return nil, "", err
	}
	tasksCtx, cancelTasks := context.WithTimeout(context.Background(), cronReloadTasksTTL)
	defer cancelTasks()
	records, err := api.ListRecords(tasksCtx, binding.AppToken, binding.Tables.Tasks, nil)
	if err != nil {
		return nil, "", err
	}
	now := time.Now().UTC()
	var jobs []cronJobState
	var invalid []string
	disabled := 0
	for _, record := range records {
		job, skipDisabled, rowErr := cronJobFromRecord(record, workspacesByRecord, now)
		if skipDisabled {
			disabled++
			continue
		}
		if rowErr != nil {
			invalid = append(invalid, rowErr.Error())
			continue
		}
		jobs = append(jobs, job)
	}
	summary := fmt.Sprintf("已加载 %d 条任务，停用 %d 条。", len(jobs), disabled)
	if len(invalid) > 0 {
		preview := invalid
		if len(preview) > 5 {
			preview = preview[:5]
		}
		summary += fmt.Sprintf("\n发现 %d 条配置错误：\n- %s", len(invalid), strings.Join(preview, "\n- "))
	}
	return jobs, summary, nil
}

func (a *App) migrateCronOwnerNow(command control.DaemonCommand) (string, error) {
	resolution, err := a.resolveCronOwner(command, cronOwnerResolveOptions{CreateStateIfEmpty: true})
	if err != nil {
		return "", err
	}
	if err := cronOwnerActionError("迁移 Cron owner", resolution); err != nil {
		return "", err
	}
	if resolution.State == nil || resolution.State.Bitable == nil || strings.TrimSpace(resolution.State.Bitable.AppToken) == "" {
		return "", fmt.Errorf("尚未初始化 Cron 配置表，请先执行 `/cron repair`")
	}
	currentOwnerGateway := firstNonEmpty(strings.TrimSpace(resolution.Gateway.GatewayID), strings.TrimSpace(resolution.LegacyGateway))
	if resolution.CurrentOwner != nil {
		currentOwnerGateway = firstNonEmpty(currentOwnerGateway, strings.TrimSpace(resolution.CurrentOwner.GatewayID))
	}
	targetGatewayID := firstNonEmpty(strings.TrimSpace(command.GatewayID), a.service.SurfaceGatewayID(command.SurfaceSessionID))
	if strings.TrimSpace(targetGatewayID) == "" {
		return "", fmt.Errorf("当前无法确定要迁移到哪个 bot；请从目标 bot 对应的 surface 执行 `/cron migrate-owner`")
	}
	newIdentity, ok, err := a.cronGatewayIdentity(targetGatewayID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("找不到目标 owner bot `%s` 的运行时配置", targetGatewayID)
	}
	currentOwnerAppID := ""
	if resolution.CurrentOwner != nil {
		currentOwnerAppID = strings.TrimSpace(resolution.CurrentOwner.AppID)
	}
	if currentOwnerAppID == "" {
		currentOwnerAppID = strings.TrimSpace(resolution.Gateway.AppID)
	}
	if strings.TrimSpace(currentOwnerGateway) == strings.TrimSpace(newIdentity.GatewayID) && firstNonEmpty(currentOwnerAppID, strings.TrimSpace(newIdentity.AppID)) == strings.TrimSpace(newIdentity.AppID) {
		return fmt.Sprintf("当前 surface 对应的 bot `%s` 已经是 Cron owner，无需迁移。", newIdentity.GatewayID), nil
	}

	a.mu.Lock()
	activeRuns := len(a.cronRuns)
	a.mu.Unlock()
	if activeRuns > 0 {
		return "", fmt.Errorf("当前还有 %d 个运行中的 Cron 任务，暂时不能迁移 owner", activeRuns)
	}

	oldAPI, err := a.cronBitableAPI(currentOwnerGateway)
	if err != nil {
		return "", err
	}
	newAPI, err := a.cronBitableAPI(newIdentity.GatewayID)
	if err != nil {
		return "", err
	}
	binding := resolution.State.Bitable
	if binding == nil {
		return "", fmt.Errorf("Cron 绑定状态无效，无法迁移 owner")
	}
	copyCtx, cancelCopy := context.WithTimeout(context.Background(), cronBitableBootstrapTTL)
	defer cancelCopy()
	workspaceRecords, err := oldAPI.ListRecords(copyCtx, binding.AppToken, binding.Tables.Workspaces, nil)
	if err != nil {
		return "", err
	}
	taskRecords, err := oldAPI.ListRecords(copyCtx, binding.AppToken, binding.Tables.Tasks, nil)
	if err != nil {
		return "", err
	}
	runRecords, err := oldAPI.ListRecords(copyCtx, binding.AppToken, binding.Tables.Runs, nil)
	if err != nil {
		return "", err
	}
	newOwner := &cronOwnerBinding{GatewayID: newIdentity.GatewayID, AppID: newIdentity.AppID, BoundAt: time.Now().UTC()}
	newBinding, err := a.ensureCronBitableRemote(copyCtx, newAPI, cronBitableState{}, resolution.ScopeKey, resolution.Label, newOwner, nil)
	if err != nil {
		return "", err
	}
	workspaceIDMap, copiedWorkspaces, err := copyCronWorkspaceRecords(copyCtx, newAPI, newBinding, workspaceRecords)
	if err != nil {
		return "", err
	}
	copiedTasks, err := copyCronTaskRecords(copyCtx, newAPI, newBinding, taskRecords, workspaceIDMap)
	if err != nil {
		return "", err
	}
	copiedRuns, err := copyCronRunHistory(copyCtx, newAPI, newBinding, runRecords)
	if err != nil {
		return "", err
	}
	jobs, summary, err := a.loadCronJobsFromBinding(newAPI, &newBinding)
	if err != nil {
		return "", err
	}
	permissionWarning := ""
	permissionCtx, cancelPermission := context.WithTimeout(context.Background(), cronBitablePermissionTTL)
	defer cancelPermission()
	if err := a.ensureCronUserPermission(permissionCtx, newAPI, newBinding.AppToken, a.service.SurfaceActorUserID(command.SurfaceSessionID)); err != nil {
		permissionWarning = "\n已跳过当前 surface 用户的编辑权限补齐：" + err.Error()
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
	stateValue.Bitable = &newBinding
	applyCronOwnerBinding(stateValue, newOwner)
	stateValue.Jobs = jobs
	stateValue.LastWorkspaceSyncAt = now
	stateValue.LastReloadAt = now
	stateValue.LastReloadSummary = "owner migrated: " + summary
	a.cronNextScheduleScan = time.Time{}
	if err := a.writeCronStateLocked(); err != nil {
		return "", err
	}
	result := fmt.Sprintf("已将 Cron owner 从 `%s` 迁移到 `%s`，复制 %d 个工作区、%d 条任务、%d 条运行记录。\n%s", currentOwnerGateway, newIdentity.GatewayID, copiedWorkspaces, copiedTasks, copiedRuns, summary)
	if permissionWarning != "" {
		result += permissionWarning
	}
	return result, nil
}

func copyCronWorkspaceRecords(ctx context.Context, api feishu.BitableAPI, binding cronBitableState, records []*larkbitable.AppTableRecord) (map[string]string, int, error) {
	oldToNew := map[string]string{}
	if len(records) == 0 {
		return oldToNew, 0, nil
	}
	values := make([]map[string]any, 0, len(records))
	oldIDs := make([]string, 0, len(records))
	for _, record := range records {
		if record == nil || record.Fields == nil {
			continue
		}
		oldID := strings.TrimSpace(stringValue(record.RecordId))
		if oldID == "" {
			continue
		}
		oldIDs = append(oldIDs, oldID)
		values = append(values, cronCloneFields(record.Fields))
	}
	if len(values) == 0 {
		return oldToNew, 0, nil
	}
	created, err := api.BatchCreateRecords(ctx, binding.AppToken, binding.Tables.Workspaces, values)
	if err != nil {
		return nil, 0, err
	}
	if len(created) != len(oldIDs) {
		return nil, 0, fmt.Errorf("workspace migration returned %d records, want %d", len(created), len(oldIDs))
	}
	for index, oldID := range oldIDs {
		newID := ""
		if created[index] != nil {
			newID = strings.TrimSpace(stringValue(created[index].RecordId))
		}
		if newID == "" {
			return nil, 0, fmt.Errorf("workspace migration missing record id for %q", oldID)
		}
		oldToNew[oldID] = newID
	}
	return oldToNew, len(oldIDs), nil
}

func copyCronTaskRecords(ctx context.Context, api feishu.BitableAPI, binding cronBitableState, records []*larkbitable.AppTableRecord, workspaceIDMap map[string]string) (int, error) {
	if len(records) == 0 {
		return 0, nil
	}
	values := make([]map[string]any, 0, len(records))
	for _, record := range records {
		if record == nil || record.Fields == nil {
			continue
		}
		fields := cronCloneFields(record.Fields)
		fields["工作区"] = remapCronWorkspaceLinks(fields["工作区"], workspaceIDMap)
		values = append(values, fields)
	}
	if len(values) == 0 {
		return 0, nil
	}
	if _, err := api.BatchCreateRecords(ctx, binding.AppToken, binding.Tables.Tasks, values); err != nil {
		return 0, err
	}
	return len(values), nil
}

func copyCronRunHistory(ctx context.Context, api feishu.BitableAPI, binding cronBitableState, records []*larkbitable.AppTableRecord) (int, error) {
	if len(records) == 0 {
		return 0, nil
	}
	values := make([]map[string]any, 0, len(records))
	for _, record := range records {
		if record == nil || record.Fields == nil {
			continue
		}
		values = append(values, cronCloneFields(record.Fields))
	}
	if len(values) == 0 {
		return 0, nil
	}
	if _, err := api.BatchCreateRecords(ctx, binding.AppToken, binding.Tables.Runs, values); err != nil {
		return 0, err
	}
	return len(values), nil
}

func cronCloneFields(fields map[string]any) map[string]any {
	if fields == nil {
		return nil
	}
	cloned := make(map[string]any, len(fields))
	for key, value := range fields {
		cloned[key] = value
	}
	return cloned
}

func remapCronWorkspaceLinks(value any, workspaceIDMap map[string]string) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case []any:
		mapped := make([]any, 0, len(typed))
		for _, item := range typed {
			switch link := item.(type) {
			case string:
				if newID := strings.TrimSpace(workspaceIDMap[strings.TrimSpace(link)]); newID != "" {
					mapped = append(mapped, newID)
				}
			case map[string]any:
				recordIDs, ok := link["record_ids"].([]any)
				if !ok {
					continue
				}
				newIDs := make([]any, 0, len(recordIDs))
				for _, rawID := range recordIDs {
					oldID, _ := rawID.(string)
					if newID := strings.TrimSpace(workspaceIDMap[strings.TrimSpace(oldID)]); newID != "" {
						newIDs = append(newIDs, newID)
					}
				}
				if len(newIDs) > 0 {
					mapped = append(mapped, map[string]any{"record_ids": newIDs})
				}
			}
		}
		return mapped
	default:
		return value
	}
}
