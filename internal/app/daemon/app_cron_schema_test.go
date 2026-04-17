package daemon

import (
	"fmt"
	"testing"
	"time"

	larkbitable "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestEnsureCronBitableTaskSchemaMatchesProductOrder(t *testing.T) {
	api := newFlakyCronBootstrapBitableAPI()
	api.failCreateField = false

	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	setCronGatewayLookup(app, "gateway-1", "app-1")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronLoaded = true
	app.cronState = &cronStateFile{
		SchemaVersion:    cronStateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		GatewayID:        "gateway-1",
		Bitable:          &cronBitableState{},
		Jobs:             []cronJobState{},
	}
	app.cronBitableFactory = func(string) (feishu.BitableAPI, error) { return api, nil }

	if _, _, err := app.ensureCronBitable(control.DaemonCommand{GatewayID: "gateway-1"}); err != nil {
		t.Fatalf("ensureCronBitable: %v", err)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	fields := api.fieldsByTable[app.cronState.Bitable.Tables.Tasks]
	gotNames := make([]string, 0, len(fields))
	enableType := 0
	recentRunFormatter := ""
	for _, field := range fields {
		if field == nil {
			continue
		}
		name := stringValue(field.FieldName)
		gotNames = append(gotNames, name)
		if name == "启用" && field.Type != nil {
			enableType = *field.Type
		}
		if name == "最近运行时间" && field.Property != nil {
			recentRunFormatter = stringValue(field.Property.DateFormatter)
		}
	}
	wantNames := []string{
		"任务名",
		"启用",
		"来源类型",
		"工作区",
		"Git 仓库引用",
		"提示词",
		"调度类型",
		"调度时间",
		"间隔",
		"并发度",
		"超时（分钟）",
		"最近运行时间",
		"最近状态",
		"最近结果摘要",
		"最近错误",
	}
	if fmt.Sprintf("%q", gotNames) != fmt.Sprintf("%q", wantNames) {
		t.Fatalf("task field order = %v, want %v", gotNames, wantNames)
	}
	if enableType != 7 {
		t.Fatalf("enable field type = %d, want 7 (checkbox)", enableType)
	}
	if recentRunFormatter != "yyyy/MM/dd HH:mm" {
		t.Fatalf("recent run formatter = %q, want yyyy/MM/dd HH:mm", recentRunFormatter)
	}
}

func TestEnsureCronBitableRepairsExistingDateFieldFormatter(t *testing.T) {
	api := newFlakyCronBootstrapBitableAPI()
	api.failCreateField = false
	api.tables["tbl-workspaces"] = &larkbitable.AppTable{TableId: stringPtr("tbl-workspaces"), Name: stringPtr(cronWorkspacesTableName)}
	api.tables["tbl-tasks"] = &larkbitable.AppTable{TableId: stringPtr("tbl-tasks"), Name: stringPtr(cronTasksTableName)}
	api.tables["tbl-runs"] = &larkbitable.AppTable{TableId: stringPtr("tbl-runs"), Name: stringPtr(cronRunsTableName)}
	api.tables["tbl-meta"] = &larkbitable.AppTable{TableId: stringPtr("tbl-meta"), Name: stringPtr(cronMetaTableName)}
	api.fieldsByTable["tbl-workspaces"] = []*larkbitable.AppTableField{{
		FieldId:   stringPtr("fld-workspaces-primary"),
		FieldName: stringPtr("工作区名称"),
		Type:      intPtr(1),
		IsPrimary: boolPtr(true),
	}}
	api.fieldsByTable["tbl-tasks"] = []*larkbitable.AppTableField{
		{FieldId: stringPtr("fld-tasks-primary"), FieldName: stringPtr("任务名"), Type: intPtr(1), IsPrimary: boolPtr(true)},
		{FieldId: stringPtr("fld-tasks-recent"), FieldName: stringPtr("最近运行时间"), Type: intPtr(5), IsPrimary: boolPtr(false)},
	}
	api.fieldsByTable["tbl-runs"] = []*larkbitable.AppTableField{
		{FieldId: stringPtr("fld-runs-primary"), FieldName: stringPtr("任务名"), Type: intPtr(1), IsPrimary: boolPtr(true)},
		{FieldId: stringPtr("fld-runs-triggered"), FieldName: stringPtr("触发时间"), Type: intPtr(5), IsPrimary: boolPtr(false)},
		{FieldId: stringPtr("fld-runs-started"), FieldName: stringPtr("开始时间"), Type: intPtr(5), IsPrimary: boolPtr(false)},
		{FieldId: stringPtr("fld-runs-finished"), FieldName: stringPtr("结束时间"), Type: intPtr(5), IsPrimary: boolPtr(false)},
	}
	api.fieldsByTable["tbl-meta"] = []*larkbitable.AppTableField{
		{FieldId: stringPtr("fld-meta-primary"), FieldName: stringPtr("名称"), Type: intPtr(1), IsPrimary: boolPtr(true)},
		{FieldId: stringPtr("fld-meta-created"), FieldName: stringPtr("created_at"), Type: intPtr(5), IsPrimary: boolPtr(false)},
		{FieldId: stringPtr("fld-meta-owner-bound"), FieldName: stringPtr("owner_bound_at"), Type: intPtr(5), IsPrimary: boolPtr(false)},
	}

	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	setCronGatewayLookup(app, "gateway-1", "app-1")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronLoaded = true
	app.cronState = &cronStateFile{
		SchemaVersion:    cronStateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		GatewayID:        "gateway-1",
		Bitable: &cronBitableState{
			AppToken: "app-cron",
			Tables: cronTableIDs{
				Workspaces: "tbl-workspaces",
				Tasks:      "tbl-tasks",
				Runs:       "tbl-runs",
				Meta:       "tbl-meta",
			},
		},
		Jobs: []cronJobState{},
	}
	app.cronBitableFactory = func(string) (feishu.BitableAPI, error) { return api, nil }

	if _, _, err := app.ensureCronBitable(control.DaemonCommand{GatewayID: "gateway-1"}); err != nil {
		t.Fatalf("ensureCronBitable: %v", err)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	for _, check := range []struct {
		tableID string
		name    string
	}{
		{tableID: "tbl-tasks", name: "最近运行时间"},
		{tableID: "tbl-runs", name: "触发时间"},
		{tableID: "tbl-runs", name: "开始时间"},
		{tableID: "tbl-runs", name: "结束时间"},
		{tableID: "tbl-meta", name: "created_at"},
		{tableID: "tbl-meta", name: "owner_bound_at"},
	} {
		found := false
		for _, field := range api.fieldsByTable[check.tableID] {
			if field == nil || stringValue(field.FieldName) != check.name {
				continue
			}
			found = true
			if field.Property == nil || stringValue(field.Property.DateFormatter) != "yyyy/MM/dd HH:mm" {
				t.Fatalf("%s/%s formatter = %q, want yyyy/MM/dd HH:mm", check.tableID, check.name, stringValue(field.Property.DateFormatter))
			}
		}
		if !found {
			t.Fatalf("missing field %s in table %s", check.name, check.tableID)
		}
	}
}

func TestEnsureCronBitableRepairsExistingFieldTypeMismatch(t *testing.T) {
	api := newFlakyCronBootstrapBitableAPI()
	api.failCreateField = false
	api.tables["tbl-workspaces"] = &larkbitable.AppTable{TableId: stringPtr("tbl-workspaces"), Name: stringPtr(cronWorkspacesTableName)}
	api.tables["tbl-tasks"] = &larkbitable.AppTable{TableId: stringPtr("tbl-tasks"), Name: stringPtr(cronTasksTableName)}
	api.tables["tbl-runs"] = &larkbitable.AppTable{TableId: stringPtr("tbl-runs"), Name: stringPtr(cronRunsTableName)}
	api.tables["tbl-meta"] = &larkbitable.AppTable{TableId: stringPtr("tbl-meta"), Name: stringPtr(cronMetaTableName)}
	api.fieldsByTable["tbl-workspaces"] = []*larkbitable.AppTableField{{
		FieldId:   stringPtr("fld-workspaces-primary"),
		FieldName: stringPtr("工作区名称"),
		Type:      intPtr(1),
		IsPrimary: boolPtr(true),
	}}
	api.fieldsByTable["tbl-tasks"] = []*larkbitable.AppTableField{
		{FieldId: stringPtr("fld-tasks-primary"), FieldName: stringPtr("任务名"), Type: intPtr(1), IsPrimary: boolPtr(true)},
		{FieldId: stringPtr("fld-tasks-git"), FieldName: stringPtr(cronTaskGitRepoInputField), Type: intPtr(15), IsPrimary: boolPtr(false)},
	}
	api.fieldsByTable["tbl-runs"] = []*larkbitable.AppTableField{
		{FieldId: stringPtr("fld-runs-primary"), FieldName: stringPtr("任务名"), Type: intPtr(1), IsPrimary: boolPtr(true)},
	}
	api.fieldsByTable["tbl-meta"] = []*larkbitable.AppTableField{
		{FieldId: stringPtr("fld-meta-primary"), FieldName: stringPtr("名称"), Type: intPtr(1), IsPrimary: boolPtr(true)},
	}

	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	setCronGatewayLookup(app, "gateway-1", "app-1")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronLoaded = true
	app.cronState = &cronStateFile{
		SchemaVersion:    cronStateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		GatewayID:        "gateway-1",
		Bitable: &cronBitableState{
			AppToken: "app-cron",
			Tables: cronTableIDs{
				Workspaces: "tbl-workspaces",
				Tasks:      "tbl-tasks",
				Runs:       "tbl-runs",
				Meta:       "tbl-meta",
			},
		},
		Jobs: []cronJobState{},
	}
	app.cronBitableFactory = func(string) (feishu.BitableAPI, error) { return api, nil }

	if _, _, err := app.ensureCronBitable(control.DaemonCommand{GatewayID: "gateway-1"}); err != nil {
		t.Fatalf("ensureCronBitable: %v", err)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	found := false
	for _, field := range api.fieldsByTable["tbl-tasks"] {
		if field == nil || stringValue(field.FieldName) != cronTaskGitRepoInputField {
			continue
		}
		found = true
		if field.Type == nil || *field.Type != 1 {
			t.Fatalf("Git 仓库引用 type = %#v, want 1 (text)", field.Type)
		}
	}
	if !found {
		t.Fatalf("missing field %s in task table", cronTaskGitRepoInputField)
	}
}
