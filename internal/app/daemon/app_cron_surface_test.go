package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestCronMenuCatalogUsesSteadyStateActions(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	setCronGatewayLookup(app, "gateway-1", "app-1")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronLoaded = true
	app.cronState = &cronStateFile{
		SchemaVersion:    cronStateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		GatewayID:        "gateway-1",
		OwnerGatewayID:   "gateway-1",
		OwnerAppID:       "app-1",
		OwnerBoundAt:     time.Now().UTC().Add(-time.Hour),
		Bitable: &cronBitableState{
			AppToken: "app-cron",
			AppURL:   "https://example.feishu.cn/base/app-cron",
		},
		Jobs: []cronJobState{{
			RecordID:        "rec-1",
			Name:            "Nightly",
			ScheduleType:    cronScheduleTypeInterval,
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
	if len(events) != 1 || events[0].FeishuDirectCommandCatalog == nil {
		t.Fatalf("events = %#v, want one command catalog", events)
	}
	catalog := events[0].FeishuDirectCommandCatalog
	if !strings.Contains(catalog.Summary, "当前状态：正常") {
		t.Fatalf("summary = %q, want healthy state", catalog.Summary)
	}
	if !strings.Contains(catalog.Summary, "当前已加载任务：1 条") {
		t.Fatalf("summary = %q, want loaded job count", catalog.Summary)
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
	setCronGatewayLookup(app, "gateway-1", "app-1")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronLoaded = true
	app.cronState = &cronStateFile{
		SchemaVersion:    cronStateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		GatewayID:        "gateway-1",
		OwnerGatewayID:   "gateway-1",
		OwnerAppID:       "app-1",
		OwnerBoundAt:     time.Now().UTC().Add(-time.Hour),
		Bitable: &cronBitableState{
			AppToken: "app-cron",
			AppURL:   "https://example.feishu.cn/base/app-cron",
		},
		LastWorkspaceSyncAt: time.Date(2026, 4, 17, 1, 2, 3, 0, time.UTC),
		LastReloadAt:        time.Date(2026, 4, 17, 2, 3, 4, 0, time.UTC),
		LastReloadSummary:   "已加载 2 条任务",
		Jobs: []cronJobState{
			{
				RecordID:        "rec-1",
				Name:            "Nightly",
				ScheduleType:    cronScheduleTypeInterval,
				IntervalMinutes: 15,
				WorkspaceKey:    "/tmp/project",
				NextRunAt:       time.Date(2026, 4, 18, 3, 0, 0, 0, time.UTC),
			},
			{
				RecordID:           "rec-2",
				Name:               "Git Sync",
				ScheduleType:       cronScheduleTypeDaily,
				DailyHour:          9,
				DailyMinute:        30,
				SourceType:         cronJobSourceGitRepo,
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
		if len(events) != 1 || events[0].FeishuDirectCommandCatalog == nil {
			t.Fatalf("%s events = %#v, want one command catalog", commandText, events)
		}
		catalog := events[0].FeishuDirectCommandCatalog
		if catalog.Title != wantTitle {
			t.Fatalf("%s title = %q, want %q", commandText, catalog.Title, wantTitle)
		}
		if catalog.Interactive {
			t.Fatalf("%s interactive = true, want false", commandText)
		}
		if len(catalog.Sections) != 0 {
			t.Fatalf("%s sections = %#v, want no follow-up menu sections", commandText, catalog.Sections)
		}
		if len(catalog.RelatedButtons) != 0 {
			t.Fatalf("%s related buttons = %#v, want none", commandText, catalog.RelatedButtons)
		}
		for _, fragment := range wantFragments {
			if !strings.Contains(catalog.Summary, fragment) {
				t.Fatalf("%s summary missing %q: %q", commandText, fragment, catalog.Summary)
			}
		}
	}

	assertCatalog("/cron status", "Cron 状态", "当前状态：正常", "当前已加载任务：2 条", "最近 reload 摘要：已加载 2 条任务")
	assertCatalog("/cron list", "Cron 任务", "`Nightly`", "来源：/tmp/project", "`Git Sync`", "来源：repo: github.com/kxn/codex-remote-feishu @ master")
	assertCatalog("/cron edit", "Cron 配置", "打开 Cron 配置表", "执行 `/cron reload` 生效")
}

func collectCronCatalogButtons(catalog *control.FeishuDirectCommandCatalog) map[string]control.CommandCatalogButton {
	values := map[string]control.CommandCatalogButton{}
	if catalog == nil {
		return values
	}
	for _, section := range catalog.Sections {
		for _, entry := range section.Entries {
			for _, button := range entry.Buttons {
				values[button.CommandText] = button
			}
		}
	}
	return values
}

func catalogCommands(catalog *control.FeishuDirectCommandCatalog) []string {
	values := []string{}
	if catalog == nil {
		return values
	}
	for _, section := range catalog.Sections {
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
