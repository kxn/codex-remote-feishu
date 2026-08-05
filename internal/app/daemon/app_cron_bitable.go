package daemon

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	larkbitable "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/bitablevalue"
	cronrt "github.com/kxn/codex-remote-feishu/internal/app/cronruntime"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

const cronBitablePermissionDocType = "bitable"
const cronBitablePermissionPermEdit = "edit"

func (a *App) repairCronBitableNow(command control.DaemonCommand) (string, error) {
	resolution, err := a.resolveCronOwner(command, cronrt.OwnerResolveOptions{AllowCreate: true, CreateStateIfEmpty: true})
	if err != nil {
		return "", err
	}
	return a.repairCronBitableForResolution(command, resolution)
}

func (a *App) persistCronBitableBindingProgress(scopeKey, label string, binding cronrt.BitableState, owner *cronrt.OwnerBinding) error {
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
		cronrt.ApplyOwnerBinding(stateValue, owner)
	}
	stateValue.Bitable = &binding
	return a.writeCronStateLocked()
}

func (a *App) reloadCronJobsResultNow(command control.DaemonCommand) (cronrt.ReloadResult, error) {
	resolution, err := a.resolveCronOwner(command, cronrt.OwnerResolveOptions{CreateStateIfEmpty: true})
	if err != nil {
		return cronrt.ReloadResult{}, err
	}
	if err := cronrt.OwnerActionError("重新加载 Cron 配置", resolution); err != nil {
		return cronrt.ReloadResult{}, err
	}
	if resolution.State == nil || resolution.State.Bitable == nil {
		return cronrt.ReloadResult{}, fmt.Errorf("Cron 多维表格还没有初始化完成")
	}
	api, err := a.cronBitableAPI(resolution.Gateway.GatewayID)
	if err != nil {
		return cronrt.ReloadResult{}, err
	}
	workspaceCtx, cancelWorkspace := context.WithTimeout(context.Background(), cronrt.ReloadWorkspaceTTL)
	defer cancelWorkspace()
	workspacesByRecord, err := a.loadCronWorkspaceIndex(workspaceCtx, api, resolution.State.Bitable)
	if err != nil {
		return cronrt.ReloadResult{}, err
	}
	tasksCtx, cancelTasks := context.WithTimeout(context.Background(), cronrt.ReloadTasksTTL)
	defer cancelTasks()
	// Fetch all fields so reload stays compatible while task-table schema evolves.
	records, err := api.ListRecords(tasksCtx, resolution.State.Bitable.AppToken, resolution.State.Bitable.Tables.Tasks, nil)
	if err != nil {
		return cronrt.ReloadResult{}, err
	}

	cronZone := cronrt.ConfiguredTimeZone(resolution.State)
	now := cronrt.SchedulerTimeIn(time.Now().UTC(), cronZone)
	a.mu.Lock()
	stateValue, err := a.loadCronStateLocked(true)
	if err != nil {
		a.mu.Unlock()
		return cronrt.ReloadResult{}, err
	}
	previousJobs := append([]cronrt.JobState(nil), stateValue.Jobs...)
	a.mu.Unlock()

	result := cronrt.BuildReloadResult(records, workspacesByRecord, now, previousJobs, cronZone)
	a.mu.Lock()
	defer a.mu.Unlock()
	stateValue, err = a.loadCronStateLocked(true)
	if err != nil {
		return cronrt.ReloadResult{}, err
	}
	stateValue.Jobs = result.Jobs
	if resolution.PersistOwner != nil {
		cronrt.ApplyOwnerBinding(stateValue, resolution.PersistOwner)
	}
	stateValue.LastReloadAt = now
	stateValue.LastReloadSummary = result.CompactSummary()
	a.cronRuntime.nextScheduleScan = time.Time{}
	if err := a.writeCronStateLocked(); err != nil {
		return cronrt.ReloadResult{}, err
	}
	return result, nil
}

func (a *App) reloadCronJobsNow(command control.DaemonCommand) (string, error) {
	result, err := a.reloadCronJobsResultNow(command)
	if err != nil {
		return "", err
	}
	return result.CompactSummary(), nil
}

func (a *App) ensureCronBitableRemote(ctx context.Context, api feishu.BitableAPI, previous cronrt.BitableState, scopeKey, label string, owner *cronrt.OwnerBinding, persist func(cronrt.BitableState) error) (cronrt.BitableState, error) {
	binding := previous
	desiredTimeZone := xutil.FirstNonEmpty(cronrt.NormalizeTimeZone(binding.TimeZone), cronrt.SystemTimeZone())
	var app *larkbitable.App
	var err error
	if strings.TrimSpace(binding.AppToken) != "" {
		app, err = api.GetApp(ctx, binding.AppToken)
		if err != nil {
			return cronrt.BitableState{}, err
		}
	} else {
		app, err = api.CreateApp(ctx, cronrt.AppTitle(label), desiredTimeZone)
		if err != nil {
			return cronrt.BitableState{}, err
		}
	}
	if app == nil || strings.TrimSpace(stringValue(app.AppToken)) == "" {
		return cronrt.BitableState{}, fmt.Errorf("缺少 Cron 多维表格 app token")
	}
	binding.AppToken = stringValue(app.AppToken)
	binding.AppURL = xutil.FirstNonEmpty(strings.TrimSpace(binding.AppURL), strings.TrimSpace(stringValue(app.Url)))
	binding.TimeZone = xutil.FirstNonEmpty(
		cronrt.NormalizeTimeZone(stringValue(app.TimeZone)),
		desiredTimeZone,
	)
	binding.DefaultTable = xutil.FirstNonEmpty(strings.TrimSpace(binding.DefaultTable), strings.TrimSpace(stringValue(app.DefaultTableId)))
	if binding.CreatedAt.IsZero() {
		binding.CreatedAt = time.Now().UTC()
	}
	if persist != nil {
		if err := persist(binding); err != nil {
			return cronrt.BitableState{}, err
		}
	}

	tables, err := api.ListTables(ctx, binding.AppToken)
	if err != nil {
		return cronrt.BitableState{}, err
	}
	byID, byName := cronIndexTables(tables)
	binding.Tables.Tasks, err = a.ensureCronNamedTable(ctx, api, binding.AppToken, byID, byName, binding.Tables.Tasks, cronrt.TasksTableName, "任务名", "")
	if err != nil {
		return cronrt.BitableState{}, err
	}
	if persist != nil {
		if err := persist(binding); err != nil {
			return cronrt.BitableState{}, err
		}
	}
	delete(byName, cronrt.TasksTableName)
	binding.Tables.Workspaces, err = a.ensureCronNamedTable(ctx, api, binding.AppToken, byID, byName, binding.Tables.Workspaces, cronrt.WorkspacesTableName, "工作区名称", "")
	if err != nil {
		return cronrt.BitableState{}, err
	}
	if persist != nil {
		if err := persist(binding); err != nil {
			return cronrt.BitableState{}, err
		}
	}
	binding.Tables.Runs, err = a.ensureCronNamedTable(ctx, api, binding.AppToken, byID, byName, binding.Tables.Runs, cronrt.RunsTableName, "任务名", "")
	if err != nil {
		return cronrt.BitableState{}, err
	}
	if persist != nil {
		if err := persist(binding); err != nil {
			return cronrt.BitableState{}, err
		}
	}
	binding.Tables.Meta, err = a.ensureCronNamedTable(ctx, api, binding.AppToken, byID, byName, binding.Tables.Meta, cronrt.MetaTableName, "名称", "")
	if err != nil {
		return cronrt.BitableState{}, err
	}
	if persist != nil {
		if err := persist(binding); err != nil {
			return cronrt.BitableState{}, err
		}
	}
	if err := a.ensureCronTableSchemas(ctx, api, binding, scopeKey, label, owner); err != nil {
		return cronrt.BitableState{}, err
	}
	metaRecordID, err := a.ensureCronMetaRecord(ctx, api, binding, scopeKey, label, owner)
	if err != nil {
		return cronrt.BitableState{}, err
	}
	binding.MetaRecordID = metaRecordID
	binding.LastVerified = time.Now().UTC()
	if persist != nil {
		if err := persist(binding); err != nil {
			return cronrt.BitableState{}, err
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

func (a *App) ensureCronTableSchemas(ctx context.Context, api feishu.BitableAPI, binding cronrt.BitableState, scopeKey, label string, owner *cronrt.OwnerBinding) error {
	if err := a.ensureCronFields(ctx, api, binding.AppToken, binding.Tables.Workspaces, []cronrt.FieldSpec{
		{Name: "工作区键", Type: 1},
		{Name: "当前状态", Type: 1},
	}); err != nil {
		return err
	}
	if err := a.ensureCronFields(ctx, api, binding.AppToken, binding.Tables.Tasks, cronrt.TaskFieldSpecs(binding.Tables.Workspaces)); err != nil {
		return err
	}
	if err := a.ensureCronFields(ctx, api, binding.AppToken, binding.Tables.Runs, []cronrt.FieldSpec{
		{Name: "触发时间", Type: 5, Property: cronrt.DateTimeFieldProperty()},
		{Name: "开始时间", Type: 5, Property: cronrt.DateTimeFieldProperty()},
		{Name: "结束时间", Type: 5, Property: cronrt.DateTimeFieldProperty()},
		{Name: "状态", Type: 1},
		{Name: "耗时（秒）", Type: 2},
		{Name: "工作区", Type: 1},
		{Name: "结果摘要", Type: 1},
		{Name: "最终回复", Type: 1},
		{Name: "错误信息", Type: 1},
	}); err != nil {
		return err
	}
	if err := a.ensureCronFields(ctx, api, binding.AppToken, binding.Tables.Meta, []cronrt.FieldSpec{
		{Name: "schema_version", Type: 2},
		{Name: "instance_scope_key", Type: 1},
		{Name: "instance_label", Type: 1},
		{Name: "created_at", Type: 5, Property: cronrt.DateTimeFieldProperty()},
		{Name: "owner_gateway_id", Type: 1},
		{Name: "owner_app_id", Type: 1},
		{Name: "owner_bound_at", Type: 5, Property: cronrt.DateTimeFieldProperty()},
	}); err != nil {
		return err
	}
	_ = scopeKey
	_ = label
	_ = owner
	return nil
}

func (a *App) ensureCronFields(ctx context.Context, api feishu.BitableAPI, appToken, tableID string, specs []cronrt.FieldSpec) error {
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
			if cronFieldNeedsSchemaUpdate(spec, field) {
				fieldID := strings.TrimSpace(stringValue(field.FieldId))
				if fieldID == "" {
					return fmt.Errorf("Cron 表 `%s` 字段 `%s` 缺少 field id，无法修正 schema", tableID, spec.Name)
				}
				if _, err := api.UpdateField(ctx, appToken, tableID, fieldID, larkbitable.NewAppTableFieldBuilder().
					FieldName(spec.Name).
					Type(spec.Type).
					Property(spec.Property).
					Build()); err != nil {
					return fmt.Errorf("Cron 表 `%s` 字段 `%s` schema 修复失败：%w", tableID, spec.Name, err)
				}
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

func cronFieldNeedsSchemaUpdate(spec cronrt.FieldSpec, field *larkbitable.AppTableField) bool {
	if field == nil {
		return false
	}
	if field.Type == nil {
		return true
	}
	if !cronrt.FieldTypeMatches(spec, *field.Type) {
		return true
	}
	if spec.Type == 18 && !cronFieldLinkTableMatches(spec, field) {
		return true
	}
	return cronFieldNeedsPropertyUpdate(spec, field)
}

func cronFieldLinkTableMatches(spec cronrt.FieldSpec, field *larkbitable.AppTableField) bool {
	if spec.Type != 18 {
		return true
	}
	if spec.Property == nil || spec.Property.TableId == nil {
		return true
	}
	if field == nil || field.Property == nil || field.Property.TableId == nil {
		return false
	}
	return strings.TrimSpace(stringValue(field.Property.TableId)) == strings.TrimSpace(stringValue(spec.Property.TableId))
}

func cronFieldNeedsPropertyUpdate(spec cronrt.FieldSpec, field *larkbitable.AppTableField) bool {
	if field == nil {
		return false
	}
	switch spec.Type {
	case 5:
		return strings.TrimSpace(stringValue(cronFieldDateFormatter(field.Property))) != strings.TrimSpace(stringValue(cronFieldDateFormatter(spec.Property)))
	default:
		return false
	}
}

func cronFieldDateFormatter(property *larkbitable.AppTableFieldProperty) *string {
	if property == nil {
		return nil
	}
	return property.DateFormatter
}

func (a *App) ensureCronMetaRecord(ctx context.Context, api feishu.BitableAPI, binding cronrt.BitableState, scopeKey, label string, owner *cronrt.OwnerBinding) (string, error) {
	records, err := api.ListRecords(ctx, binding.AppToken, binding.Tables.Meta, []string{"名称", "schema_version", "instance_scope_key", "instance_label", "created_at", "owner_gateway_id", "owner_app_id", "owner_bound_at"})
	if err != nil {
		return "", err
	}
	fields := map[string]any{
		"名称":                 "当前实例",
		"schema_version":     cronrt.StateSchemaVersion,
		"instance_scope_key": scopeKey,
		"instance_label":     label,
		"created_at":         cronrt.Milliseconds(binding.CreatedAt),
	}
	if owner != nil {
		fields["owner_gateway_id"] = strings.TrimSpace(owner.GatewayID)
		fields["owner_app_id"] = strings.TrimSpace(owner.AppID)
		fields["owner_bound_at"] = cronrt.Milliseconds(owner.BoundAt)
	}
	for _, record := range records {
		if record == nil {
			continue
		}
		currentKey := bitablevalue.String(record.Fields["instance_scope_key"])
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
	if exists && cronrt.PermissionSatisfies(member.Perm, cronBitablePermissionPermEdit) {
		return nil
	}
	if exists {
		return api.UpdatePermission(ctx, appToken, cronBitablePermissionDocType, memberType, strings.TrimSpace(actorUserID), principalType, cronBitablePermissionPermEdit, member.PermType)
	}
	return api.GrantPermission(ctx, appToken, cronBitablePermissionDocType, memberType, strings.TrimSpace(actorUserID), principalType, cronBitablePermissionPermEdit)
}

func (a *App) syncCronWorkspaceTable(ctx context.Context, api feishu.BitableAPI, binding cronrt.BitableState, rows []cronrt.WorkspaceRow) (map[string]string, error) {
	records, err := api.ListRecords(ctx, binding.AppToken, binding.Tables.Workspaces, []string{"工作区名称", "工作区键", "当前状态"})
	if err != nil {
		return nil, err
	}
	existing := map[string]*larkbitable.AppTableRecord{}
	for _, record := range records {
		if record == nil {
			continue
		}
		key := bitablevalue.String(record.Fields["工作区键"])
		if strings.TrimSpace(key) == "" {
			key = bitablevalue.String(record.Fields["工作区名称"])
		}
		if strings.TrimSpace(key) == "" {
			continue
		}
		existing[key] = record
	}
	result := map[string]string{}
	desired := map[string]cronrt.WorkspaceRow{}
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
				if bitablevalue.String(record.Fields["工作区名称"]) == row.Name && bitablevalue.String(record.Fields["当前状态"]) == row.Status {
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
		if bitablevalue.String(record.Fields["当前状态"]) == "已失效" {
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

func (a *App) loadCronWorkspaceIndex(ctx context.Context, api feishu.BitableAPI, binding *cronrt.BitableState) (map[string]cronrt.WorkspaceRow, error) {
	records, err := api.ListRecords(ctx, binding.AppToken, binding.Tables.Workspaces, []string{"工作区名称", "工作区键", "当前状态"})
	if err != nil {
		return nil, err
	}
	values := map[string]cronrt.WorkspaceRow{}
	for _, record := range records {
		if record == nil {
			continue
		}
		recordID := strings.TrimSpace(stringValue(record.RecordId))
		if recordID == "" {
			continue
		}
		values[recordID] = cronrt.WorkspaceRow{
			Name:   bitablevalue.String(record.Fields["工作区名称"]),
			Key:    bitablevalue.String(record.Fields["工作区键"]),
			Status: bitablevalue.String(record.Fields["当前状态"]),
		}
	}
	return values, nil
}

func (a *App) cronWorkspaceRowsLocked() []cronrt.WorkspaceRow {
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
		liveNames[workspaceKey] = xutil.FirstNonEmpty(strings.TrimSpace(inst.ShortName), cronWorkspaceDisplayName(workspaceKey))
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
	values := make([]cronrt.WorkspaceRow, 0, len(keys))
	for _, key := range keys {
		key = state.ResolveWorkspaceKey(key)
		if key == "" {
			continue
		}
		values = append(values, cronrt.WorkspaceRow{
			Name:   xutil.FirstNonEmpty(liveNames[key], cronWorkspaceDisplayName(key)),
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

func cronJobFromRecord(record *larkbitable.AppTableRecord, workspacesByRecord map[string]cronrt.WorkspaceRow, now time.Time) (cronrt.JobState, bool, error) {
	job, disabled, reloadErr := cronrt.JobFromReloadRecord(record, workspacesByRecord, now, cronrt.SystemTimeZone(), "", 0)
	if reloadErr != nil {
		return cronrt.JobState{}, false, reloadErr
	}
	return job, disabled, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
