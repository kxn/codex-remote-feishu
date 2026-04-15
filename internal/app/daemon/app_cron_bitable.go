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

func (a *App) ensureCronBitable(command control.DaemonCommand) (*cronStateFile, string, error) {
	a.mu.Lock()
	stateValue, err := a.loadCronStateLocked(true)
	if err != nil {
		a.mu.Unlock()
		return nil, "", err
	}
	gatewayID := firstNonEmpty(strings.TrimSpace(command.GatewayID), strings.TrimSpace(stateValue.GatewayID), a.service.SurfaceGatewayID(command.SurfaceSessionID))
	stateValue.GatewayID = gatewayID
	scopeKey := strings.TrimSpace(stateValue.InstanceScopeKey)
	label := strings.TrimSpace(stateValue.InstanceLabel)
	binding := cronBitableState{}
	if stateValue.Bitable != nil {
		binding = *stateValue.Bitable
	}
	workspaces := a.cronWorkspaceRowsLocked()
	a.mu.Unlock()

	api, err := a.cronBitableAPI(gatewayID)
	if err != nil {
		return nil, "", err
	}
	persistProgress := func(next cronBitableState) error {
		return a.persistCronBitableBindingProgress(gatewayID, scopeKey, label, next)
	}
	bootstrapCtx, cancelBootstrap := context.WithTimeout(context.Background(), cronBitableBootstrapTTL)
	defer cancelBootstrap()

	updatedBinding, err := a.ensureCronBitableRemote(bootstrapCtx, api, binding, scopeKey, label, persistProgress)
	if err != nil {
		return nil, "", err
	}
	workspaceCtx, cancelWorkspace := context.WithTimeout(context.Background(), cronBitableWorkspaceTTL)
	defer cancelWorkspace()
	if _, err := a.syncCronWorkspaceTable(workspaceCtx, api, updatedBinding, workspaces); err != nil {
		return nil, "", err
	}
	permissionCtx, cancelPermission := context.WithTimeout(context.Background(), cronBitablePermissionTTL)
	defer cancelPermission()
	if err := a.ensureCronUserPermission(permissionCtx, api, updatedBinding.AppToken, a.service.SurfaceActorUserID(command.SurfaceSessionID)); err != nil {
		return nil, "", err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	stateValue, err = a.loadCronStateLocked(true)
	if err != nil {
		return nil, "", err
	}
	stateValue.GatewayID = gatewayID
	stateValue.InstanceScopeKey = scopeKey
	stateValue.InstanceLabel = label
	stateValue.Bitable = &updatedBinding
	stateValue.LastWorkspaceSyncAt = time.Now().UTC()
	if err := a.writeCronStateLocked(); err != nil {
		return nil, "", err
	}
	return cloneCronState(stateValue), fmt.Sprintf("已同步 %d 个工作区。编辑表格后发送 `/cron reload` 生效。", len(workspaces)), nil
}

func (a *App) persistCronBitableBindingProgress(gatewayID, scopeKey, label string, binding cronBitableState) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	stateValue, err := a.loadCronStateLocked(true)
	if err != nil {
		return err
	}
	stateValue.GatewayID = gatewayID
	if strings.TrimSpace(scopeKey) != "" {
		stateValue.InstanceScopeKey = scopeKey
	}
	if strings.TrimSpace(label) != "" {
		stateValue.InstanceLabel = label
	}
	stateValue.Bitable = &binding
	return a.writeCronStateLocked()
}

func (a *App) reloadCronJobsNow(command control.DaemonCommand) (string, error) {
	stateValue, _, err := a.ensureCronBitable(command)
	if err != nil {
		return "", err
	}
	if stateValue == nil || stateValue.Bitable == nil {
		return "", fmt.Errorf("Cron 多维表格还没有初始化完成")
	}
	api, err := a.cronBitableAPI(stateValue.GatewayID)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cronReloadReadTTL)
	defer cancel()

	workspacesByRecord, err := a.loadCronWorkspaceIndex(ctx, api, stateValue.Bitable)
	if err != nil {
		return "", err
	}
	records, err := api.ListRecords(ctx, stateValue.Bitable.AppToken, stateValue.Bitable.Tables.Tasks, []string{
		"任务名", "启用", "调度类型", "每天-时", "每天-分", "间隔", "工作区", "提示词", "超时（分钟）",
	})
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
	stateValue, err = a.loadCronStateLocked(true)
	if err != nil {
		return "", err
	}
	stateValue.Jobs = jobs
	stateValue.LastReloadAt = now
	stateValue.LastReloadSummary = summary
	a.cronNextScheduleScan = time.Time{}
	if err := a.writeCronStateLocked(); err != nil {
		return "", err
	}
	return summary, nil
}

func (a *App) ensureCronBitableRemote(ctx context.Context, api feishu.BitableAPI, previous cronBitableState, scopeKey, label string, persist func(cronBitableState) error) (cronBitableState, error) {
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
	defaultTableID := firstNonEmpty(strings.TrimSpace(binding.DefaultTable), strings.TrimSpace(stringValue(app.DefaultTableId)))
	if defaultTableID == "" &&
		strings.TrimSpace(binding.Tables.Tasks) == "" &&
		strings.TrimSpace(binding.Tables.Workspaces) == "" &&
		strings.TrimSpace(binding.Tables.Runs) == "" &&
		strings.TrimSpace(binding.Tables.Meta) == "" &&
		len(byID) == 1 {
		for tableID := range byID {
			defaultTableID = tableID
		}
	}
	binding.Tables.Tasks, err = a.ensureCronNamedTable(ctx, api, binding.AppToken, byID, byName, binding.Tables.Tasks, cronTasksTableName, "任务名", defaultTableID)
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
	if err := a.ensureCronTableSchemas(ctx, api, binding, scopeKey, label); err != nil {
		return cronBitableState{}, err
	}
	metaRecordID, err := a.ensureCronMetaRecord(ctx, api, binding, scopeKey, label)
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

func (a *App) ensureCronTableSchemas(ctx context.Context, api feishu.BitableAPI, binding cronBitableState, scopeKey, label string) error {
	if err := a.ensureCronFields(ctx, api, binding.AppToken, binding.Tables.Workspaces, []cronFieldSpec{
		{Name: "工作区键", Type: 1},
		{Name: "当前状态", Type: 1},
	}); err != nil {
		return err
	}
	if err := a.ensureCronFields(ctx, api, binding.AppToken, binding.Tables.Tasks, []cronFieldSpec{
		{Name: "启用", Type: 3, Property: cronSelectFieldProperty([]string{"启用", "停用"})},
		{Name: "调度类型", Type: 3, Property: cronSelectFieldProperty([]string{cronScheduleTypeDaily, cronScheduleTypeInterval})},
		{Name: "每天-时", Type: 2},
		{Name: "每天-分", Type: 2},
		{Name: "间隔", Type: 3, Property: cronSelectFieldProperty(cronIntervalLabels())},
		{Name: "工作区", Type: 18, Property: larkbitable.NewAppTableFieldPropertyBuilder().TableId(binding.Tables.Workspaces).Multiple(false).Build()},
		{Name: "提示词", Type: 1},
		{Name: "超时（分钟）", Type: 2},
		{Name: "最近运行时间", Type: 5},
		{Name: "最近状态", Type: 1},
		{Name: "最近结果摘要", Type: 1},
		{Name: "最近错误", Type: 1},
	}); err != nil {
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
	}); err != nil {
		return err
	}
	_ = scopeKey
	_ = label
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
			if field.Type != nil && *field.Type != spec.Type {
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

func (a *App) ensureCronMetaRecord(ctx context.Context, api feishu.BitableAPI, binding cronBitableState, scopeKey, label string) (string, error) {
	records, err := api.ListRecords(ctx, binding.AppToken, binding.Tables.Meta, []string{"名称", "schema_version", "instance_scope_key", "instance_label", "created_at"})
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
	if existing[key] {
		return nil
	}
	return api.GrantPermission(ctx, appToken, cronBitablePermissionDocType, memberType, strings.TrimSpace(actorUserID), principalType)
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
				if _, err := api.UpdateRecord(ctx, binding.AppToken, binding.Tables.Workspaces, recordID, fields); err != nil {
					return nil, err
				}
				continue
			}
		}
		created, err := api.CreateRecord(ctx, binding.AppToken, binding.Tables.Workspaces, fields)
		if err != nil {
			return nil, err
		}
		result[row.Key] = strings.TrimSpace(stringValue(created.RecordId))
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
		if _, err := api.UpdateRecord(ctx, binding.AppToken, binding.Tables.Workspaces, recordID, map[string]any{"当前状态": "已失效"}); err != nil {
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

func cronSelectFieldProperty(labels []string) *larkbitable.AppTableFieldProperty {
	options := make([]*larkbitable.AppTableFieldPropertyOption, 0, len(labels))
	for index, label := range labels {
		options = append(options, larkbitable.NewAppTableFieldPropertyOptionBuilder().
			Name(label).
			Color(index%54).
			Build())
	}
	return larkbitable.NewAppTableFieldPropertyBuilder().Options(options).Build()
}

func cronIntervalLabels() []string {
	values := make([]string, 0, len(cronIntervalChoices))
	for _, item := range cronIntervalChoices {
		values = append(values, item.Label)
	}
	return values
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
	enabled := strings.TrimSpace(cronValueString(record.Fields["启用"]))
	if enabled == "" || enabled == "停用" {
		return cronJobState{}, true, nil
	}
	if enabled != "启用" {
		return cronJobState{}, false, fmt.Errorf("任务 `%s` 的启用值无效：%s", name, enabled)
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
		job.DailyHour = cronValueInt(record.Fields["每天-时"])
		job.DailyMinute = cronValueInt(record.Fields["每天-分"])
		if job.DailyHour < 0 || job.DailyHour > 23 || job.DailyMinute < 0 || job.DailyMinute > 59 {
			return cronJobState{}, false, fmt.Errorf("任务 `%s` 的每天定时时间无效", name)
		}
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
