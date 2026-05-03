package daemon

import (
	"strings"
	"testing"
	"time"

	cronrt "github.com/kxn/codex-remote-feishu/internal/app/cronruntime"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestCronRuntimeDirectOwnerSurfaceContracts(t *testing.T) {
	now := time.Date(2026, 4, 23, 1, 2, 3, 0, time.UTC)
	state := &cronrt.StateFile{
		SchemaVersion:    cronrt.StateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		GatewayID:        "gateway-1",
		OwnerGatewayID:   "gateway-1",
		OwnerAppID:       "app-1",
		OwnerBoundAt:     now.Add(-time.Hour),
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
			NextRunAt:       now.Add(15 * time.Minute),
		}},
	}
	ownerView := cronrt.OwnerView{Status: cronrt.OwnerStatusHealthy}

	if got := cronrt.PrimaryMenuCommand(state, ownerView); got != "/cron edit" {
		t.Fatalf("PrimaryMenuCommand() = %q, want /cron edit", got)
	}
	if button, ok := cronrt.RunActionButton("rec-1"); !ok {
		t.Fatal("RunActionButton() = false, want callback button")
	} else if button.Kind != control.CommandCatalogButtonCallbackAction || button.CommandID != control.FeishuCommandCron {
		t.Fatalf("RunActionButton() = %#v, want cron callback button", button)
	}

	catalog := cronrt.BuildListPageView(state, ownerView, "")
	assertCatalogUsesPlainTextContracts(t, &catalog)
	if catalog.Title != "Cron 任务" {
		t.Fatalf("BuildListPageView() title = %q, want %q", catalog.Title, "Cron 任务")
	}
	summary := catalogSummaryText(&catalog)
	if !strings.Contains(summary, "当前已加载 1 条任务。") || !strings.Contains(summary, "立即触发") {
		t.Fatalf("BuildListPageView() summary = %q, want count + trigger hint", summary)
	}
	if len(catalog.Sections) != 1 || len(catalog.Sections[0].Entries) != 1 {
		t.Fatalf("BuildListPageView() sections = %#v, want one entry", catalog.Sections)
	}
	entry := catalog.Sections[0].Entries[0]
	if entry.Title != "Nightly" {
		t.Fatalf("BuildListPageView() entry title = %q, want %q", entry.Title, "Nightly")
	}
	if len(entry.Buttons) != 1 || entry.Buttons[0].Kind != control.CommandCatalogButtonCallbackAction {
		t.Fatalf("BuildListPageView() buttons = %#v, want callback action button", entry.Buttons)
	}
	value := entry.Buttons[0].CallbackValue
	if value["kind"] != "page_local_action" || value["action_kind"] != string(control.ActionCronCommand) || value["action_arg"] != "run rec-1" {
		t.Fatalf("BuildListPageView() callback = %#v, want cron run page_local_action", value)
	}
}

func TestCronRuntimeDirectOwnerStateHelpers(t *testing.T) {
	boundAt := time.Date(2026, 4, 23, 3, 4, 5, 0, time.UTC)
	state := &cronrt.StateFile{
		OwnerGatewayID: "gateway-1",
		OwnerAppID:     "app-1",
		OwnerBoundAt:   boundAt,
		Jobs: []cronrt.JobState{{
			RecordID:        "rec-1",
			Name:            "Nightly",
			ScheduleType:    cronrt.ScheduleTypeInterval,
			IntervalMinutes: 15,
			WorkspaceKey:    "/tmp/project",
		}},
	}

	binding := cronrt.OwnerBindingFromState(state)
	if binding == nil || binding.GatewayID != "gateway-1" || binding.AppID != "app-1" || !binding.BoundAt.Equal(boundAt) {
		t.Fatalf("OwnerBindingFromState() = %#v, want gateway/app/boundAt copied", binding)
	}

	backfilled, changed := cronrt.OwnerBindingBackfill(&cronrt.OwnerBinding{GatewayID: "gateway-1"}, cronrt.GatewayIdentity{
		GatewayID: "gateway-1",
		AppID:     "app-1",
	})
	if !changed || backfilled == nil || backfilled.AppID != "app-1" || backfilled.BoundAt.IsZero() {
		t.Fatalf("OwnerBindingBackfill() = (%#v, %v), want app id + bound time", backfilled, changed)
	}

	cloned := cronrt.CloneState(state)
	if cloned == state || len(cloned.Jobs) != 1 || cloned.Jobs[0].RecordID != "rec-1" {
		t.Fatalf("CloneState() = %#v, want deep copy of job list", cloned)
	}

	target := &cronrt.StateFile{}
	cronrt.ApplyOwnerBinding(target, binding)
	if target.OwnerGatewayID != "gateway-1" || target.OwnerAppID != "app-1" || !target.OwnerBoundAt.Equal(boundAt) {
		t.Fatalf("ApplyOwnerBinding() = %#v, want gateway/app/boundAt written back", target)
	}

	item := cronrt.ReloadTaskItemFromJob(state.Jobs[0])
	if item.RecordID != "rec-1" || item.Name != "Nightly" || item.SourceSummary != "/tmp/project" {
		t.Fatalf("ReloadTaskItemFromJob() = %#v, want record/name/workspace source", item)
	}
	if item.MaxConcurrency != cronrt.DefaultConcurrencyCap {
		t.Fatalf("ReloadTaskItemFromJob() max concurrency = %d, want default %d", item.MaxConcurrency, cronrt.DefaultConcurrencyCap)
	}
}
