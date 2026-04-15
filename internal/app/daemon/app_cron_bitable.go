package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	larkbitable "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const cronBitablePermissionDocType = "bitable"
const cronBitablePermissionPermEdit = "edit"

type cronFieldSpec struct {
	Name     string
	Type     int
	Property *larkbitable.AppTableFieldProperty
}

type cronWorkspaceRow struct {
	Name   string
	Key    string
	Status string
}

// ensureCronBitable is kept as a narrow compatibility wrapper for tests while
// the command path moves to explicit `/cron repair`.
func (a *App) ensureCronBitable(command control.DaemonCommand) (*cronStateFile, string, error) {
	summary, err := a.repairCronBitableNow(command)
	if err != nil {
		return nil, "", err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	stateValue, err := a.loadCronStateLocked(true)
	if err != nil {
		return nil, "", err
	}
	return cloneCronState(stateValue), summary, nil
}

func (a *App) repairCronBitableNow(command control.DaemonCommand) (string, error) {
	resolution, err := a.resolveCronOwner(command, cronOwnerResolveOptions{AllowCreate: true, CreateStateIfEmpty: true})
	if err != nil {
		return "", err
	}
	if err := cronOwnerActionError("修复 Cron 配置表", resolution); err != nil {
		return "", err
	}
	api, err := a.cronBitableAPI(resolution.Gateway.GatewayID)
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	workspaces := a.cronWorkspaceRowsLocked()
	a.mu.Unlock()
	persistProgress := func(next cronBitableState) error {
		progressOwner := resolution.PersistOwner
		if resolution.Status != cronOwnerStatusBootstrap {
			progressOwner = nil
		}
		return a.persistCronBitableBindingProgress(resolution.ScopeKey, resolution.Label, next, progressOwner)
	}
	bootstrapCtx, cancelBootstrap := context.WithTimeout(context.Background(), cronBitableBootstrapTTL)
	defer cancelBootstrap()
	updatedBinding, err := a.ensureCronBitableRemote(bootstrapCtx, api, resolution.Binding, resolution.ScopeKey, resolution.Label, resolution.PersistOwner, persistProgress)
	if err != nil {
		return "", err
	}
	workspaceCtx, cancelWorkspace := context.WithTimeout(context.Background(), cronBitableWorkspaceTTL)
	defer cancelWorkspace()
	if _, err := a.syncCronWorkspaceTable(workspaceCtx, api, updatedBinding, workspaces); err != nil {
		return "", err
	}
	var permissionWarning string
	permissionCtx, cancelPermission := context.WithTimeout(context.Background(), cronBitablePermissionTTL)
	defer cancelPermission()
	if err := a.ensureCronUserPermission(permissionCtx, api, updatedBinding.AppToken, a.service.SurfaceActorUserID(command.SurfaceSessionID)); err != nil {
		permissionWarning = "已跳过当前 surface 用户的编辑权限补齐：" + err.Error()
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
	if resolution.PersistOwner != nil {
		applyCronOwnerBinding(stateValue, resolution.PersistOwner)
	}
	if err := a.writeCronStateLocked(); err != nil {
		return "", err
	}
	summary := fmt.Sprintf("已通过 owner bot `%s` 同步 %d 个工作区。编辑表格后发送 `/cron reload` 生效。", firstNonEmpty(resolution.Gateway.GatewayID, stateValue.OwnerGatewayID), len(workspaces))
	if permissionWarning != "" {
		summary += "\n" + permissionWarning
	}
	return summary, nil
}

func (a *App) persistCronBitableBindingProgress(scopeKey, label string, binding cronBitableState, owner *cronOwnerBinding) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	stateValue, err := a.loadCronStateLocked(true)
	if err != nil {
		return err
	}
	if strings.TrimSpace(scopeKey) != "" {
		stateValue.InstanceScopeKey = scopeKey
	}
	if strings.TrimSpace(label) != "" {
		stateValue.InstanceLabel = label
	}
	if owner != nil {
		applyCronOwnerBinding(stateValue, owner)
	}
	stateValue.Bitable = &binding
	return a.writeCronStateLocked()
}

func (a *App) reloadCronJobsNow(command control.DaemonCommand) (string, error) {
	resolution, err := a.resolveCronOwner(command, cronOwnerResolveOptions{CreateStateIfEmpty: true})
	if err != nil {
		return "", err
	}
	if err := cronOwnerActionError("重新加载 Cron 配置", resolution); err != nil {
		return "", err
	}
	if resolution.State == nil || resolution.State.Bitable == nil {
		return "", fmt.Errorf("Cron 多维表格还没有初始化完成")
	}
	api, err := a.cronBitableAPI(resolution.Gateway.GatewayID)
	if err != nil {
		return "", err
	}
	workspaceCtx, cancelWorkspace := context.WithTimeout(context.Background(), cronReloadWorkspaceTTL)
	defer cancelWorkspace()
	workspacesByRecord, err := a.loadCronWorkspaceIndex(workspaceCtx, api, resolution.State.Bitable)
	if err != nil {
		return "", err
	}
	tasksCtx, cancelTasks := context.WithTimeout(context.Background(), cronReloadTasksTTL)
	defer cancelTasks()
	// Fetch all fields so reload stays compatible while task-table schema evolves.
	records, err := api.ListRecords(tasksCtx, resolution.State.Bitable.AppToken, resolution.State.Bitable.Tables.Tasks, nil)
	if err != nil {
		return "", err
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

	a.mu.Lock()
	defer a.mu.Unlock()
	stateValue, err := a.loadCronStateLocked(true)
	if err != nil {
		return "", err
	}
	stateValue.Jobs = jobs
	if resolution.PersistOwner != nil {
		applyCronOwnerBinding(stateValue, resolution.PersistOwner)
	}
	stateValue.LastReloadAt = now
	stateValue.LastReloadSummary = summary
	a.cronNextScheduleScan = time.Time{}
	if err := a.writeCronStateLocked(); err != nil {
		return "", err
	}
	if resolution.PersistOwner != nil {
		summary += "\n已回填正式 owner 绑定。"
	}
	return summary, nil
}

func (a *App) ensureCronBitableRemote(ctx context.Context, api feishu.BitableAPI, previous cronBitableState, scopeKey, label string, owner *cronOwnerBinding, persist func(cronBitableState) error) (cronBitableState, error) {
	binding := previous
	var app *larkbitable.App
	var err error
	if strings.TrimSpace(binding.AppToken) != "" {
		app, err = api.GetApp(ctx, binding.AppToken)
		if err != nil {
			return cronBitableState{}, err
		}
	} else {
		app, err = api.CreateApp(ctx, cronAppTitle(label), cronTimeZone())
		if err != nil {
			return cronBitableState{}, err
		}
	}
	if app == nil || strings.TrimSpace(stringValue(app.AppToken)) == "" {
		return cronBitableState{}, fmt.Errorf("缺少 Cron 多维表格 app token")
	}
	binding.AppToken = stringValue(app.AppToken)
	binding.AppURL = firstNonEmpty(strings.TrimSpace(binding.AppURL), strings.TrimSpace(stringValue(app.Url)))
	binding.DefaultTable = firstNonEmpty(strings.TrimSpace(binding.DefaultTable), strings.TrimSpace(stringValue(app.DefaultTableId)))
	if binding.CreatedAt.IsZero() {
		binding.CreatedAt = time.Now().UTC()
	}
	if persist != nil {
		if err := persist(binding); err != nil {
			return cronBitableState{}, err
		}
	}

	tables, err := api.ListTables(ctx, binding.AppToken)
	if err != nil {
		return cronBitableState{}, err
	}
	byID, byName := cronIndexTables(tables)
	binding.Tables.Tasks, err = a.ensureCronNamedTable(ctx, api, binding.AppToken, byID, byName, binding.Tables.Tasks, cronTasksTableName, "任务名", "")
	if err != nil {
		return cronBitableState{}, err
	}
	if persist != nil {
		if err := persist(binding); err != nil {
			return cronBitableState{}, err
		}
	}
	delete(byName, cronTasksTableName)
	binding.Tables.Workspaces, err = a.ensureCronNamedTable(ctx, api, binding.AppToken, byID, byName, binding.Tables.Workspaces, cronWorkspacesTableName, "工作区名称", "")
	if err != nil {
		return cronBitableState{}, err
	}
	if persist != nil {
		if err := persist(binding); err != nil {
			return cronBitableState{}, err
		}
	}
	binding.Tables.Runs, err = a.ensureCronNamedTable(ctx, api, binding.AppToken, byID, byName, binding.Tables.Runs, cronRunsTableName, "任务名", "")
	if err != nil {
		return cronBitableState{}, err
	}
	if persist != nil {
		if err := persist(binding); err != nil {
			return cronBitableState{}, err
		}
	}
	binding.Tables.Meta, err = a.ensureCronNamedTable(ctx, api, binding.AppToken, byID, byName, binding.Tables.Meta, cronMetaTableName, "名称", "")
	if err != nil {
		return cronBitableState{}, err
	}
	if persist != nil {
		if err := persist(binding); err != nil {
			return cronBitableState{}, err
		}
	}
	if err := a.ensureCronTableSchemas(ctx, api, binding, scopeKey, label, owner); err != nil {
		return cronBitableState{}, err
	}
	metaRecordID, err := a.ensureCronMetaRecord(ctx, api, binding, scopeKey, label, owner)
	if err != nil {
		return cronBitableState{}, err
	}
	binding.MetaRecordID = metaRecordID
	binding.LastVerified = time.Now().UTC()
	if persist != nil {
		if err := persist(binding); err != nil {
			return cronBitableState{}, err
		}
	}
	return binding, nil
}

func (a *App) ensureCronNamedTable(ctx context.Context, api feishu.BitableAPI, appToken string, byID map[string]*larkbitable.AppTable, byName map[string]*larkbitable.AppTable, currentID, desiredName, primaryFieldName, reusableTableID string) (string, error) {
	if table := byID[strings.TrimSpace(currentID)]; table != nil {
		if strings.TrimSpace(stringValue(table.Name)) != desiredName {
			if err := api.RenameTable(ctx, appToken, strings.TrimSpace(stringValue(table.TableId)), desiredName); err != nil {
				return "", err
			}
		}
		if err := a.ensureCronPrimaryFieldName(ctx, api, appToken, strings.TrimSpace(stringValue(table.TableId)), primaryFieldName); err != nil {
			return "", err
		}
		return strings.TrimSpace(stringValue(table.TableId)), nil
	}
	if table := byName[desiredName]; table != nil {
		tableID := strings.TrimSpace(stringValue(table.TableId))
		if err := a.ensureCronPrimaryFieldName(ctx, api, appToken, tableID, primaryFieldName); err != nil {
			return "", err
		}
		return tableID, nil
	}
	if table := byID[strings.TrimSpace(reusableTableID)]; table != nil {
		tableID := strings.TrimSpace(stringValue(table.TableId))
		if err := api.RenameTable(ctx, appToken, tableID, desiredName); err != nil {
			return "", err
		}
		if err := a.ensureCronPrimaryFieldName(ctx, api, appToken, tableID, primaryFieldName); err != nil {
			return "", err
		}
		return tableID, nil
	}
	created, err := api.CreateTable(ctx, appToken, larkbitable.NewReqTableBuilder().
		Name(desiredName).
		Fields([]*larkbitable.AppTableCreateHeader{
			larkbitable.NewAppTableCreateHeaderBuilder().
				FieldName(primaryFieldName).
				Type(1).
				Build(),
		}).
		Build())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(stringValue(created.TableId)), nil
}

func (a *App) ensureCronPrimaryFieldName(ctx context.Context, api feishu.BitableAPI, appToken, tableID, desiredName string) error {
	fields, err := api.ListFields(ctx, appToken, tableID)
	if err != nil {
		return err
	}
	for _, field := range fields {
		if field == nil {
			continue
		}
		if strings.TrimSpace(stringValue(field.FieldName)) == desiredName {
			return nil
		}
	}
	for _, field := range fields {
		if field == nil || field.IsPrimary == nil || !*field.IsPrimary {
			continue
		}
		_, err := api.UpdateField(ctx, appToken, tableID, strings.TrimSpace(stringValue(field.FieldId)), larkbitable.NewAppTableFieldBuilder().
			FieldName(desiredName).
			Type(1).
			Build())
		return err
	}
	return nil
}

func (a *App) ensureCronTableSchemas(ctx context.Context, api feishu.BitableAPI, binding cronBitableState, scopeKey, label string, owner *cronOwnerBinding) error {
	if err := a.ensureCronFields(ctx, api, binding.AppToken, binding.Tables.Workspaces, []cronFieldSpec{
		{Name: "工作区键", Type: 1},
		{Name: "当前状态", Type: 1},
	}); err != nil {
		return err
	}
	if err := a.ensureCronFields(ctx, api, binding.AppToken, binding.Tables.Tasks, cronTaskFieldSpecs(binding.Tables.Workspaces)); err != nil {
		return err
	}
	if err := a.ensureCronFields(ctx, api, binding.AppToken, binding.Tables.Runs, []cronFieldSpec{
		{Name: "触发时间", Type: 5},
		{Name: "开始时间", Type: 5},
		{Name: "结束时间", Type: 5},
		{Name: "状态", Type: 1},
		{Name: "耗时（秒）", Type: 2},
		{Name: "工作区", Type: 1},
		{Name: "结果摘要", Type: 1},
		{Name: "最终回复", Type: 1},
		{Name: "错误信息", Type: 1},
	}); err != nil {
		return err
	}
	if err := a.ensureCronFields(ctx, api, binding.AppToken, binding.Tables.Meta, []cronFieldSpec{
		{Name: "schema_version", Type: 2},
		{Name: "instance_scope_key", Type: 1},
		{Name: "instance_label", Type: 1},
		{Name: "created_at", Type: 5},
		{Name: "owner_gateway_id", Type: 1},
		{Name: "owner_app_id", Type: 1},
		{Name: "owner_bound_at", Type: 5},
	}); err != nil {
		return err
	}
	_ = scopeKey
	_ = label
	_ = owner
	return nil
}

func (a *App) ensureCronFields(ctx context.Context, api feishu.BitableAPI, appToken, tableID string, specs []cronFieldSpec) error {
	fields, err := api.ListFields(ctx, appToken, tableID)
	if err != nil {
		return err
	}
	existing := map[string]*larkbitable.AppTableField{}
	for _, field := range fields {
		if field == nil {
			continue
		}
		name := strings.TrimSpace(stringValue(field.FieldName))
		if name == "" {
			continue
		}
		existing[name] = field
	}
	for _, spec := range specs {
		if field := existing[spec.Name]; field != nil {
			if field.Type != nil && !cronFieldTypeMatches(spec, *field.Type) {
				return fmt.Errorf("Cron 表 `%s` 字段 `%s` 类型不匹配：当前=%d 期望=%d", tableID, spec.Name, *field.Type, spec.Type)
			}
			if spec.Type == 18 && spec.Property != nil && spec.Property.TableId != nil && field.Property != nil && field.Property.TableId != nil && strings.TrimSpace(stringValue(field.Property.TableId)) != strings.TrimSpace(stringValue(spec.Property.TableId)) {
				return fmt.Errorf("Cron 表 `%s` 字段 `%s` 关联表不匹配，请修复后重试", tableID, spec.Name)
			}
			continue
		}
		if _, err := api.CreateField(ctx, appToken, tableID, larkbitable.NewAppTableFieldBuilder().
			FieldName(spec.Name).
			Type(spec.Type).
			Property(spec.Property).
			Build()); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) ensureCronMetaRecord(ctx context.Context, api feishu.BitableAPI, binding cronBitableState, scopeKey, label string, owner *cronOwnerBinding) (string, error) {
	records, err := api.ListRecords(ctx, binding.AppToken, binding.Tables.Meta, []string{"名称", "schema_version", "instance_scope_key", "instance_label", "created_at", "owner_gateway_id", "owner_app_id", "owner_bound_at"})
	if err != nil {
		return "", err
	}
	fields := map[string]any{
		"名称":                 "当前实例",
		"schema_version":     cronStateSchemaVersion,
		"instance_scope_key": scopeKey,
		"instance_label":     label,
		"created_at":         cronMilliseconds(binding.CreatedAt),
	}
	if owner != nil {
		fields["owner_gateway_id"] = strings.TrimSpace(owner.GatewayID)
		fields["owner_app_id"] = strings.TrimSpace(owner.AppID)
		fields["owner_bound_at"] = cronMilliseconds(owner.BoundAt)
	}
	for _, record := range records {
		if record == nil {
			continue
		}
		currentKey := cronValueString(record.Fields["instance_scope_key"])
		if currentKey != "" && currentKey != scopeKey {
			return "", fmt.Errorf("当前 Cron 多维表格已绑定到其他实例：%s", currentKey)
		}
		recordID := strings.TrimSpace(stringValue(record.RecordId))
		if recordID == "" {
			continue
		}
		if _, err := api.UpdateRecord(ctx, binding.AppToken, binding.Tables.Meta, recordID, fields); err != nil {
			return "", err
		}
		return recordID, nil
	}
	created, err := api.CreateRecord(ctx, binding.AppToken, binding.Tables.Meta, fields)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(stringValue(created.RecordId)), nil
}

func (a *App) ensureCronUserPermission(ctx context.Context, api feishu.BitableAPI, appToken, actorUserID string) error {
	memberType, principalType, ok := cronUserPermissionPrincipal(actorUserID)
	if !ok {
		return nil
	}
	key := memberType + ":" + strings.TrimSpace(actorUserID)
	existing, err := api.ListPermissionMembers(ctx, appToken, cronBitablePermissionDocType)
	if err != nil {
		return err
	}
	member, exists := existing[key]
	if exists && cronPermissionSatisfies(member.Perm, cronBitablePermissionPermEdit) {
		return nil
	}
	if exists {
		return api.UpdatePermission(ctx, appToken, cronBitablePermissionDocType, memberType, strings.TrimSpace(actorUserID), principalType, cronBitablePermissionPermEdit, member.PermType)
	}
	return api.GrantPermission(ctx, appToken, cronBitablePermissionDocType, memberType, strings.TrimSpace(actorUserID), principalType, cronBitablePermissionPermEdit)
}

func (a *App) syncCronWorkspaceTable(ctx context.Context, api feishu.BitableAPI, binding cronBitableState, rows []cronWorkspaceRow) (map[string]string, error) {
	records, err := api.ListRecords(ctx, binding.AppToken, binding.Tables.Workspaces, []string{"工作区名称", "工作区键", "当前状态"})
	if err != nil {
		return nil, err
	}
	existing := map[string]*larkbitable.AppTableRecord{}
	for _, record := range records {
		if record == nil {
			continue
		}
		key := cronValueString(record.Fields["工作区键"])
		if strings.TrimSpace(key) == "" {
			key = cronValueString(record.Fields["工作区名称"])
		}
		if strings.TrimSpace(key) == "" {
			continue
		}
		existing[key] = record
	}
	result := map[string]string{}
	desired := map[string]cronWorkspaceRow{}
	pendingCreateKeys := make([]string, 0, len(rows))
	pendingCreates := make([]map[string]any, 0, len(rows))
	pendingUpdates := make([]feishu.BitableRecordUpdate, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Key) == "" {
			continue
		}
		desired[row.Key] = row
		fields := map[string]any{
			"工作区名称": row.Name,
			"工作区键":  row.Key,
			"当前状态":  row.Status,
		}
		if record := existing[row.Key]; record != nil {
			recordID := strings.TrimSpace(stringValue(record.RecordId))
			if recordID != "" {
				result[row.Key] = recordID
				if cronValueString(record.Fields["工作区名称"]) == row.Name && cronValueString(record.Fields["当前状态"]) == row.Status {
					continue
				}
				pendingUpdates = append(pendingUpdates, feishu.BitableRecordUpdate{RecordID: recordID, Fields: fields})
				continue
			}
		}
		pendingCreateKeys = append(pendingCreateKeys, row.Key)
		pendingCreates = append(pendingCreates, fields)
	}
	for key, record := range existing {
		if _, ok := desired[key]; ok {
			continue
		}
		recordID := strings.TrimSpace(stringValue(record.RecordId))
		if recordID == "" {
			continue
		}
		if cronValueString(record.Fields["当前状态"]) == "已失效" {
			continue
		}
		pendingUpdates = append(pendingUpdates, feishu.BitableRecordUpdate{
			RecordID: recordID,
			Fields:   map[string]any{"当前状态": "已失效"},
		})
	}
	if len(pendingCreates) > 0 {
		created, err := api.BatchCreateRecords(ctx, binding.AppToken, binding.Tables.Workspaces, pendingCreates)
		if err != nil {
			return nil, err
		}
		if len(created) != len(pendingCreateKeys) {
			return nil, fmt.Errorf("cron workspace batch create returned %d records, want %d", len(created), len(pendingCreateKeys))
		}
		for i, key := range pendingCreateKeys {
			recordID := ""
			if created[i] != nil {
				recordID = strings.TrimSpace(stringValue(created[i].RecordId))
			}
			if recordID == "" {
				return nil, fmt.Errorf("cron workspace batch create missing record id for key %q", key)
			}
			result[key] = recordID
		}
	}
	if len(pendingUpdates) > 0 {
		if _, err := api.BatchUpdateRecords(ctx, binding.AppToken, binding.Tables.Workspaces, pendingUpdates); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (a *App) loadCronWorkspaceIndex(ctx context.Context, api feishu.BitableAPI, binding *cronBitableState) (map[string]cronWorkspaceRow, error) {
	records, err := api.ListRecords(ctx, binding.AppToken, binding.Tables.Workspaces, []string{"工作区名称", "工作区键", "当前状态"})
	if err != nil {
		return nil, err
	}
	values := map[string]cronWorkspaceRow{}
	for _, record := range records {
		if record == nil {
			continue
		}
		recordID := strings.TrimSpace(stringValue(record.RecordId))
		if recordID == "" {
			continue
		}
		values[recordID] = cronWorkspaceRow{
			Name:   cronValueString(record.Fields["工作区名称"]),
			Key:    cronValueString(record.Fields["工作区键"]),
			Status: cronValueString(record.Fields["当前状态"]),
		}
	}
	return values, nil
}

func (a *App) cronWorkspaceRowsLocked() []cronWorkspaceRow {
	recency := a.service.RecentPersistedWorkspaces(500)
	liveNames := map[string]string{}
	for _, inst := range a.service.Instances() {
		if inst == nil {
			continue
		}
		workspaceKey := state.ResolveWorkspaceKey(inst.WorkspaceKey, inst.WorkspaceRoot)
		if workspaceKey == "" {
			continue
		}
		recency[workspaceKey] = time.Now().UTC()
		liveNames[workspaceKey] = firstNonEmpty(strings.TrimSpace(inst.ShortName), cronWorkspaceDisplayName(workspaceKey))
	}
	keys := make([]string, 0, len(recency))
	for key := range recency {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left := recency[keys[i]]
		right := recency[keys[j]]
		if !left.Equal(right) {
			return left.After(right)
		}
		return keys[i] < keys[j]
	})
	values := make([]cronWorkspaceRow, 0, len(keys))
	for _, key := range keys {
		key = state.ResolveWorkspaceKey(key)
		if key == "" {
			continue
		}
		values = append(values, cronWorkspaceRow{
			Name:   firstNonEmpty(liveNames[key], cronWorkspaceDisplayName(key)),
			Key:    key,
			Status: "可用",
		})
	}
	return values
}

func cronWorkspaceDisplayName(workspaceKey string) string {
	base := strings.TrimSpace(filepath.Base(strings.TrimSpace(workspaceKey)))
	switch base {
	case "", ".", string(filepath.Separator):
		return strings.TrimSpace(workspaceKey)
	default:
		return base
	}
}

func cronIndexTables(tables []*larkbitable.AppTable) (map[string]*larkbitable.AppTable, map[string]*larkbitable.AppTable) {
	byID := map[string]*larkbitable.AppTable{}
	byName := map[string]*larkbitable.AppTable{}
	for _, table := range tables {
		if table == nil {
			continue
		}
		tableID := strings.TrimSpace(stringValue(table.TableId))
		name := strings.TrimSpace(stringValue(table.Name))
		if tableID != "" {
			byID[tableID] = table
		}
		if name != "" {
			byName[name] = table
		}
	}
	return byID, byName
}

func cronUserPermissionPrincipal(actorUserID string) (string, string, bool) {
	actorUserID = strings.TrimSpace(actorUserID)
	if actorUserID == "" {
		return "", "", false
	}
	switch {
	case strings.HasPrefix(actorUserID, "ou_"):
		return "openid", "user", true
	case strings.HasPrefix(actorUserID, "on_"):
		return "unionid", "user", true
	default:
		return "userid", "user", true
	}
}

func cloneCronState(stateValue *cronStateFile) *cronStateFile {
	if stateValue == nil {
		return nil
	}
	cloned := *stateValue
	if stateValue.Bitable != nil {
		copyBinding := *stateValue.Bitable
		cloned.Bitable = &copyBinding
	}
	if stateValue.Jobs != nil {
		cloned.Jobs = append([]cronJobState(nil), stateValue.Jobs...)
	}
	return &cloned
}

func cronJobFromRecord(record *larkbitable.AppTableRecord, workspacesByRecord map[string]cronWorkspaceRow, now time.Time) (cronJobState, bool, error) {
	if record == nil {
		return cronJobState{}, false, fmt.Errorf("empty task record")
	}
	name := strings.TrimSpace(cronValueString(record.Fields["任务名"]))
	if name == "" {
		name = strings.TrimSpace(stringValue(record.RecordId))
	}
	enabled, valid := cronValueBool(record.Fields["启用"])
	if !enabled && valid {
		return cronJobState{}, true, nil
	}
	if !valid {
		return cronJobState{}, false, fmt.Errorf("任务 `%s` 的启用值无效：%s", name, strings.TrimSpace(cronValueString(record.Fields["启用"])))
	}
	scheduleType := strings.TrimSpace(cronValueString(record.Fields["调度类型"]))
	prompt := strings.TrimSpace(cronValueString(record.Fields["提示词"]))
	if prompt == "" {
		return cronJobState{}, false, fmt.Errorf("任务 `%s` 缺少提示词", name)
	}
	workspaceLinks := cronValueStringSlice(record.Fields["工作区"])
	if len(workspaceLinks) != 1 {
		return cronJobState{}, false, fmt.Errorf("任务 `%s` 需要且只能选择一个工作区", name)
	}
	workspaceRow, ok := workspacesByRecord[workspaceLinks[0]]
	if !ok || strings.TrimSpace(workspaceRow.Key) == "" {
		return cronJobState{}, false, fmt.Errorf("任务 `%s` 选择的工作区已不存在", name)
	}
	if strings.TrimSpace(workspaceRow.Status) == "已失效" {
		return cronJobState{}, false, fmt.Errorf("任务 `%s` 选择的工作区已失效", name)
	}
	timeoutMinutes := cronDefaultTimeoutMinutes(cronValueInt(record.Fields["超时（分钟）"]))
	job := cronJobState{
		RecordID:          strings.TrimSpace(stringValue(record.RecordId)),
		Name:              name,
		ScheduleType:      scheduleType,
		WorkspaceKey:      workspaceRow.Key,
		WorkspaceRecordID: workspaceLinks[0],
		Prompt:            prompt,
		TimeoutMinutes:    timeoutMinutes,
	}
	switch scheduleType {
	case cronScheduleTypeDaily:
		hour, minute, ok := cronDailyTimeFromFields(record.Fields)
		if !ok {
			return cronJobState{}, false, fmt.Errorf("任务 `%s` 的每天定时时间无效，应填写为 HH:mm", name)
		}
		job.DailyHour = hour
		job.DailyMinute = minute
	case cronScheduleTypeInterval:
		intervalLabel := strings.TrimSpace(cronValueString(record.Fields["间隔"]))
		minutes, ok := intervalMinutesForLabel(intervalLabel)
		if !ok {
			return cronJobState{}, false, fmt.Errorf("任务 `%s` 的间隔值无效：%s", name, intervalLabel)
		}
		job.IntervalMinutes = minutes
	default:
		return cronJobState{}, false, fmt.Errorf("任务 `%s` 的调度类型无效：%s", name, scheduleType)
	}
	job.NextRunAt = cronNextRunAt(job, now)
	return job, false, nil
}

func cronValueString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case map[string]any:
		for _, key := range []string{"text", "name", "label", "title", "value", "id", "record_id", "recordId"} {
			if text := strings.TrimSpace(cronValueString(typed[key])); text != "" {
				return text
			}
		}
		if values := cronValueStringSlice(typed); len(values) > 0 {
			return strings.Join(values, "\n")
		}
		return ""
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(cronValueString(item)); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case []string:
		return strings.Join(typed, "\n")
	default:
		return fmt.Sprint(value)
	}
}

func cronValueBool(value any) (bool, bool) {
	switch typed := value.(type) {
	case nil:
		return false, true
	case bool:
		return typed, true
	case int:
		return typed != 0, true
	case int32:
		return typed != 0, true
	case int64:
		return typed != 0, true
	case float32:
		return typed != 0, true
	case float64:
		return typed != 0, true
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return parsed != 0, true
		}
		return false, false
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "", "0", "false", "off", "no", "unchecked", "停用":
			return false, true
		case "1", "true", "on", "yes", "checked", "启用":
			return true, true
		default:
			return false, false
		}
	case map[string]any:
		for _, key := range []string{"checked", "value", "text", "name", "label"} {
			if nested, ok := typed[key]; ok {
				if enabled, valid := cronValueBool(nested); valid {
					return enabled, true
				}
			}
		}
		return false, false
	case []any:
		if len(typed) == 0 {
			return false, true
		}
		if len(typed) == 1 {
			return cronValueBool(typed[0])
		}
		return false, false
	case []string:
		if len(typed) == 0 {
			return false, true
		}
		if len(typed) == 1 {
			return cronValueBool(typed[0])
		}
		return false, false
	default:
		return cronValueBool(fmt.Sprint(value))
	}
}

func cronValueStringSlice(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case []string:
		return append([]string(nil), typed...)
	case map[string]any:
		for _, key := range []string{"record_ids", "recordIds", "ids", "values"} {
			if values := cronValueStringSlice(typed[key]); len(values) > 0 {
				return values
			}
		}
		for _, key := range []string{"record_id", "recordId", "id", "value", "text", "name", "label"} {
			if text := strings.TrimSpace(cronValueString(typed[key])); text != "" {
				return []string{text}
			}
		}
		return nil
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if nested := cronValueStringSlice(item); len(nested) > 0 {
				values = append(values, nested...)
				continue
			}
			if text := strings.TrimSpace(cronValueString(item)); text != "" {
				values = append(values, text)
			}
		}
		return values
	default:
		text := strings.TrimSpace(cronValueString(value))
		if text == "" {
			return nil
		}
		return []string{text}
	}
}

func cronValueInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	case map[string]any:
		for _, key := range []string{"value", "number", "text"} {
			if keyValue, ok := typed[key]; ok {
				return cronValueInt(keyValue)
			}
		}
		return 0
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	default:
		parsed, _ := strconv.Atoi(strings.TrimSpace(fmt.Sprint(value)))
		return parsed
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
