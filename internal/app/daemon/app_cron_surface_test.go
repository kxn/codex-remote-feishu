package daemon

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	cronrt "github.com/kxn/codex-remote-feishu/internal/app/cronruntime"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestCronMenuCatalogUsesSteadyStateActions(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	api := &fakeCronBitableAPI{}
	setCronGatewayLookup(app, "gateway-1", "app-1")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronRuntime.loaded = true
	app.cronRuntime.bitableFactory = func(string) (feishu.BitableAPI, error) { return api, nil }
	app.cronRuntime.state = &cronrt.StateFile{
		SchemaVersion:    cronrt.StateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		GatewayID:        "gateway-1",
		OwnerGatewayID:   "gateway-1",
		OwnerAppID:       "app-1",
		OwnerBoundAt:     time.Now().UTC().Add(-time.Hour),
		Bitable: &cronrt.BitableState{
			AppToken: "app-cron",
			AppURL:   "https://example.feishu.cn/base/app-cron",
			Tables: cronrt.TableIDs{
				Tasks:      "tbl-tasks",
				Runs:       "tbl-runs",
				Workspaces: "tbl-workspaces",
			},
		},
		Jobs: []cronrt.JobState{{
			RecordID:        "rec-1",
			Name:            "Nightly",
			ScheduleType:    cronrt.ScheduleTypeInterval,
			IntervalMinutes: 15,
			WorkspaceKey:    "/tmp/project",
			NextRunAt:       time.Now().Add(15 * time.Minute),
		}},
	}

	events := app.handleCronDaemonCommand(control.DaemonCommand{
		Text:             "/cron",
		GatewayID:        "gateway-1",
		SurfaceSessionID: "surface-1",
	})
	if len(events) != 1 {
		t.Fatalf("events = %#v, want one command catalog event", events)
	}
	catalog := catalogFromUIEvent(t, events[0])
	assertCatalogUsesPlainTextContracts(t, catalog)
	summary := catalogSummaryText(catalog)
	if strings.Contains(summary, "当前状态：正常") || strings.Contains(summary, "当前已加载任务：1 条") {
		t.Fatalf("summary = %q, want root page without default status dump", summary)
	}
	buttons := collectCronCatalogButtons(catalog)
	if _, ok := buttons["/cron migrate-owner"]; ok {
		t.Fatalf("menu must not expose migrate-owner: %#v", buttons)
	}
	if buttons["/cron edit"].Style != "primary" {
		t.Fatalf("expected /cron edit to be primary, got %#v", buttons["/cron edit"])
	}
	if buttons["/cron repair"].Style == "primary" {
		t.Fatalf("repair must not be primary in steady state: %#v", buttons["/cron repair"])
	}
	if containsString(catalogCommands(catalog), "/cron migrate-owner") {
		t.Fatalf("manual commands must not expose migrate-owner: %#v", catalogCommands(catalog))
	}
}

func TestCronStatusListAndEditCommandsReturnSpecificCatalogs(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	api := &fakeCronBitableAPI{}
	setCronGatewayLookup(app, "gateway-1", "app-1")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronRuntime.loaded = true
	app.cronRuntime.bitableFactory = func(string) (feishu.BitableAPI, error) { return api, nil }
	app.cronRuntime.state = &cronrt.StateFile{
		SchemaVersion:    cronrt.StateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		GatewayID:        "gateway-1",
		OwnerGatewayID:   "gateway-1",
		OwnerAppID:       "app-1",
		OwnerBoundAt:     time.Now().UTC().Add(-time.Hour),
		Bitable: &cronrt.BitableState{
			AppToken: "app-cron",
			AppURL:   "https://example.feishu.cn/base/app-cron",
			TimeZone: "Asia/Shanghai",
			Tables:   cronrt.TableIDs{Tasks: "tbl-tasks", Runs: "tbl-runs", Workspaces: "tbl-workspaces"},
		},
		LastWorkspaceSyncAt: time.Date(2026, 4, 17, 1, 2, 3, 0, time.UTC),
		LastReloadAt:        time.Date(2026, 4, 17, 2, 3, 4, 0, time.UTC),
		LastReloadSummary:   "已加载 2 条任务",
		Jobs: []cronrt.JobState{
			{
				RecordID:        "rec-1",
				Name:            "Nightly",
				ScheduleType:    cronrt.ScheduleTypeInterval,
				IntervalMinutes: 15,
				WorkspaceKey:    "/tmp/project",
				NextRunAt:       time.Date(2026, 4, 18, 3, 0, 0, 0, time.UTC),
			},
			{
				RecordID:           "rec-2",
				Name:               "Git Sync",
				ScheduleType:       cronrt.ScheduleTypeDaily,
				DailyHour:          9,
				DailyMinute:        30,
				SourceType:         cronrt.JobSourceGitRepo,
				GitRepoSourceInput: "https://github.com/kxn/codex-remote-feishu#ref=master",
				GitRepoURL:         "https://github.com/kxn/codex-remote-feishu",
				GitRef:             "master",
				NextRunAt:          time.Date(2026, 4, 17, 9, 30, 0, 0, time.UTC),
			},
		},
	}

	assertCatalog := func(commandText, wantTitle string, wantFragments ...string) {
		t.Helper()
		events := app.handleCronDaemonCommand(control.DaemonCommand{
			Text:             commandText,
			GatewayID:        "gateway-1",
			SurfaceSessionID: "surface-1",
		})
		if len(events) != 1 {
			t.Fatalf("%s events = %#v, want one command catalog", commandText, events)
		}
		catalog := catalogFromUIEvent(t, events[0])
		if catalog.Title != wantTitle {
			t.Fatalf("%s title = %q, want %q", commandText, catalog.Title, wantTitle)
		}
		assertCatalogUsesPlainTextContracts(t, catalog)
		summary := catalogSummaryText(catalog)
		for _, fragment := range wantFragments {
			if !strings.Contains(summary, fragment) {
				t.Fatalf("%s summary missing %q: %q", commandText, fragment, summary)
			}
		}
		if len(catalog.RelatedButtons) != 1 || catalog.RelatedButtons[0].CommandText != "/cron" {
			t.Fatalf("%s related buttons = %#v, want back-to-root", commandText, catalog.RelatedButtons)
		}
	}

	assertCatalog("/cron status", "Cron 状态",
		"当前状态：正常",
		"当前已加载任务：2 条",
		"最近工作区同步：",
		"最近 reload：2026-04-17 10:03",
		"最近 reload 摘要：已加载 2 条任务",
		"配置表：可从下方外部入口打开",
		"运行状态：可从下方外部入口打开",
	)
	assertCatalog("/cron edit", "Cron 配置", "配置表：可从下方外部入口打开", "执行 /cron reload 生效")

	statusCatalog := catalogFromUIEvent(t, app.handleCronDaemonCommand(control.DaemonCommand{
		Text:             "/cron status",
		GatewayID:        "gateway-1",
		SurfaceSessionID: "surface-1",
	})[0])
	if !catalogHasOpenURLButton(statusCatalog, "打开 Cron 配置表", "https://example.feishu.cn/base/app-cron?table=tbl-tasks") {
		t.Fatalf("expected status catalog to expose config url button, got %#v", statusCatalog.Sections)
	}
	if !catalogHasOpenURLButton(statusCatalog, "打开运行记录", "https://example.feishu.cn/base/app-cron?table=tbl-runs") {
		t.Fatalf("expected status catalog to expose runs url button, got %#v", statusCatalog.Sections)
	}
	editCatalog := catalogFromUIEvent(t, app.handleCronDaemonCommand(control.DaemonCommand{
		Text:             "/cron edit",
		GatewayID:        "gateway-1",
		SurfaceSessionID: "surface-1",
	})[0])
	if !catalogHasOpenURLButton(editCatalog, "打开 Cron 配置表", "https://example.feishu.cn/base/app-cron?table=tbl-tasks") {
		t.Fatalf("expected edit catalog to expose config url button, got %#v", editCatalog.Sections)
	}
	if catalogHasOpenURLButton(editCatalog, "打开运行记录", "https://example.feishu.cn/base/app-cron?table=tbl-runs") {
		t.Fatalf("did not expect edit catalog to expose runs url button, got %#v", editCatalog.Sections)
	}

	listEvents := app.handleCronDaemonCommand(control.DaemonCommand{
		Text:             "/cron list",
		GatewayID:        "gateway-1",
		SurfaceSessionID: "surface-1",
	})
	if len(listEvents) != 1 {
		t.Fatalf("/cron list events = %#v, want one command catalog", listEvents)
	}
	listCatalog := catalogFromUIEvent(t, listEvents[0])
	assertCatalogUsesPlainTextContracts(t, listCatalog)
	if listCatalog.Title != "Cron 任务" {
		t.Fatalf("/cron list title = %q, want %q", listCatalog.Title, "Cron 任务")
	}
	if !listCatalog.Interactive {
		t.Fatalf("/cron list interactive = false, want true")
	}
	listSummary := catalogSummaryText(listCatalog)
	if !strings.Contains(listSummary, "当前已加载 2 条任务。") || !strings.Contains(listSummary, "立即触发") {
		t.Fatalf("/cron list summary = %q, want task count + trigger hint", listSummary)
	}
	if len(listCatalog.Sections) != 1 || len(listCatalog.Sections[0].Entries) != 2 {
		t.Fatalf("/cron list sections = %#v, want one section with two entries", listCatalog.Sections)
	}
	firstEntry := listCatalog.Sections[0].Entries[0]
	secondEntry := listCatalog.Sections[0].Entries[1]
	if firstEntry.Title != "Git Sync" || !strings.Contains(firstEntry.Description, "下次 04-17 17:30") || !strings.Contains(firstEntry.Description, "来源：repo: github.com/kxn/codex-remote-feishu @ master") {
		t.Fatalf("unexpected first /cron list entry: %#v", firstEntry)
	}
	if len(firstEntry.Buttons) != 1 || firstEntry.Buttons[0].CommandText != "/cron run rec-2" {
		t.Fatalf("unexpected first /cron list buttons: %#v", firstEntry.Buttons)
	}
	if secondEntry.Title != "Nightly" || !strings.Contains(secondEntry.Description, "下次 04-18 11:00") || !strings.Contains(secondEntry.Description, "来源：/tmp/project") {
		t.Fatalf("unexpected second /cron list entry: %#v", secondEntry)
	}
	if len(secondEntry.Buttons) != 1 || secondEntry.Buttons[0].CommandText != "/cron run rec-1" {
		t.Fatalf("unexpected second /cron list buttons: %#v", secondEntry.Buttons)
	}
}

func TestCronCatalogHidesConfigEntryWhenWorkspaceSyncFails(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	api := &fakeCronBitableAPI{listRecordsErr: errors.New("bitable down")}
	setCronGatewayLookup(app, "gateway-1", "app-1")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronRuntime.loaded = true
	app.cronRuntime.bitableFactory = func(string) (feishu.BitableAPI, error) { return api, nil }
	app.cronRuntime.state = &cronrt.StateFile{
		SchemaVersion:    cronrt.StateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		GatewayID:        "gateway-1",
		OwnerGatewayID:   "gateway-1",
		OwnerAppID:       "app-1",
		OwnerBoundAt:     time.Now().UTC().Add(-time.Hour),
		Bitable: &cronrt.BitableState{
			AppToken: "app-cron",
			AppURL:   "https://example.feishu.cn/base/app-cron",
			Tables: cronrt.TableIDs{
				Tasks:      "tbl-tasks",
				Runs:       "tbl-runs",
				Workspaces: "tbl-workspaces",
			},
		},
	}

	menuEvents := app.handleCronDaemonCommand(control.DaemonCommand{
		Text:             "/cron",
		GatewayID:        "gateway-1",
		SurfaceSessionID: "surface-1",
	})
	if len(menuEvents) != 1 {
		t.Fatalf("menu events = %#v, want one catalog", menuEvents)
	}
	menuCatalog := catalogFromUIEvent(t, menuEvents[0])
	assertCatalogUsesPlainTextContracts(t, menuCatalog)
	menuSummary := catalogSummaryText(menuCatalog)
	if !strings.Contains(menuSummary, "工作区清单同步失败") {
		t.Fatalf("menu summary = %q, want failure reason", menuSummary)
	}
	menuButtons := collectCronCatalogButtons(menuCatalog)
	if !menuButtons["/cron edit"].Disabled {
		t.Fatalf("expected /cron edit to be disabled when workspace sync fails, got %#v", menuButtons["/cron edit"])
	}

	statusEvents := app.handleCronDaemonCommand(control.DaemonCommand{
		Text:             "/cron status",
		GatewayID:        "gateway-1",
		SurfaceSessionID: "surface-1",
	})
	if len(statusEvents) != 1 {
		t.Fatalf("status events = %#v, want one catalog", statusEvents)
	}
	statusCatalog := catalogFromUIEvent(t, statusEvents[0])
	assertCatalogUsesPlainTextContracts(t, statusCatalog)
	statusSummary := catalogSummaryText(statusCatalog)
	if strings.Contains(statusSummary, "[打开 Cron 配置表]") {
		t.Fatalf("status summary must not expose config link after sync failure: %q", statusSummary)
	}
	if !strings.Contains(statusSummary, "配置表：工作区清单未同步，暂不开放配置入口") {
		t.Fatalf("status summary = %q, want hidden-config wording", statusSummary)
	}
	if catalogHasOpenURLButton(statusCatalog, "打开 Cron 配置表", "https://example.feishu.cn/base/app-cron?table=tbl-tasks") {
		t.Fatalf("status catalog must not expose config url button after sync failure: %#v", statusCatalog.Sections)
	}

	editEvents := app.handleCronDaemonCommand(control.DaemonCommand{
		Text:             "/cron edit",
		GatewayID:        "gateway-1",
		SurfaceSessionID: "surface-1",
	})
	if len(editEvents) != 1 {
		t.Fatalf("edit events = %#v, want one catalog", editEvents)
	}
	editCatalog := catalogFromUIEvent(t, editEvents[0])
	assertCatalogUsesPlainTextContracts(t, editCatalog)
	editSummary := catalogSummaryText(editCatalog)
	if strings.Contains(editSummary, "[打开 Cron 配置表]") {
		t.Fatalf("edit summary must not expose config link after sync failure: %q", editSummary)
	}
	if !strings.Contains(editSummary, "工作区清单同步完成后才会开放配置入口") {
		t.Fatalf("edit summary = %q, want sync-first wording", editSummary)
	}
	if catalogHasOpenURLButton(editCatalog, "打开 Cron 配置表", "https://example.feishu.cn/base/app-cron?table=tbl-tasks") {
		t.Fatalf("edit catalog must not expose config url button after sync failure: %#v", editCatalog.Sections)
	}
}

func TestCronBitableTableURLOverridesPreviousTableContext(t *testing.T) {
	got := cronrt.BitableTableURL(
		"https://example.feishu.cn/base/app-cron?table=tbl-old&view=vew-old&record=rec-1&search=nightly",
		"tbl-runs",
	)
	want := "https://example.feishu.cn/base/app-cron?table=tbl-runs"
	if got != want {
		t.Fatalf("cronBitableTableURL() = %q, want %q", got, want)
	}
}

func TestParseCronCommandTextSupportsRunSubcommand(t *testing.T) {
	parsed, err := cronrt.ParseCommandText("/cron run rec-task-1")
	if err != nil {
		t.Fatalf("parseCronCommandText() error = %v, want nil", err)
	}
	if parsed.Mode != cronrt.CommandModeRun || parsed.JobRecordID != "rec-task-1" {
		t.Fatalf("parsed = %#v, want run/rec-task-1", parsed)
	}
}

func collectCronCatalogButtons(catalog *control.FeishuPageView) map[string]control.CommandCatalogButton {
	values := map[string]control.CommandCatalogButton{}
	if catalog == nil {
		return values
	}
	for _, section := range control.NormalizeFeishuPageView(*catalog).Sections {
		for _, entry := range section.Entries {
			for _, button := range entry.Buttons {
				values[button.CommandText] = button
			}
		}
	}
	return values
}

func catalogCommands(catalog *control.FeishuPageView) []string {
	values := []string{}
	if catalog == nil {
		return values
	}
	for _, section := range control.NormalizeFeishuPageView(*catalog).Sections {
		for _, entry := range section.Entries {
			values = append(values, entry.Commands...)
		}
	}
	return values
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
