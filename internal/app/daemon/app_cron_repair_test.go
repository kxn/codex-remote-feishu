package daemon

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	cronrt "github.com/kxn/codex-remote-feishu/internal/app/cronruntime"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestCronRepairTakesOverBindingWhenOwnerUnavailable(t *testing.T) {
	newAPI := newFlakyCronBootstrapBitableAPI()
	newAPI.failCreateField = false

	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	setCronGatewayLookup(app, "gateway-2", "app-2")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronRuntime.loaded = true
	app.cronRuntime.state = &cronrt.StateFile{
		SchemaVersion:    cronrt.StateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		OwnerGatewayID:   "gateway-1",
		OwnerAppID:       "app-1",
		OwnerBoundAt:     time.Now().UTC().Add(-time.Hour),
		Bitable: &cronrt.BitableState{
			AppToken: "app-old",
			AppURL:   "https://example.feishu.cn/base/app-old",
			Tables: cronrt.TableIDs{
				Tasks:      "tbl-tasks-old",
				Workspaces: "tbl-workspaces-old",
				Runs:       "tbl-runs-old",
				Meta:       "tbl-meta-old",
			},
		},
		Jobs: []cronrt.JobState{{
			RecordID:        "rec-stale",
			Name:            "Stale",
			ScheduleType:    cronrt.ScheduleTypeInterval,
			IntervalMinutes: 15,
			WorkspaceKey:    "/tmp/project",
			NextRunAt:       time.Now().Add(15 * time.Minute),
		}},
		LastReloadAt:      time.Now().UTC().Add(-time.Minute),
		LastReloadSummary: "旧 owner 下的 stale jobs",
	}
	app.cronRuntime.bitableFactory = func(gatewayID string) (feishu.BitableAPI, error) {
		if gatewayID != "gateway-2" {
			return nil, fmt.Errorf("unexpected gateway %q", gatewayID)
		}
		return newAPI, nil
	}

	summary, err := app.repairCronBitableNow(control.DaemonCommand{
		Text:             "/cron repair",
		GatewayID:        "gateway-2",
		SurfaceSessionID: "surface-2",
	})
	if err != nil {
		t.Fatalf("repairCronBitableNow: %v", err)
	}
	if !strings.Contains(summary, "接管 Cron 配置") {
		t.Fatalf("summary = %q, want takeover wording", summary)
	}
	if app.cronRuntime.state.OwnerGatewayID != "gateway-2" || app.cronRuntime.state.OwnerAppID != "app-2" {
		t.Fatalf("owner state = %#v, want gateway-2/app-2", app.cronRuntime.state)
	}
	if app.cronRuntime.state.Bitable == nil || app.cronRuntime.state.Bitable.AppToken == "" || app.cronRuntime.state.Bitable.AppToken == "app-old" {
		t.Fatalf("binding after takeover = %#v, want fresh binding", app.cronRuntime.state.Bitable)
	}
	if len(app.cronRuntime.state.Jobs) != 0 {
		t.Fatalf("jobs after takeover = %#v, want cleared jobs", app.cronRuntime.state.Jobs)
	}
	if !app.cronRuntime.state.LastReloadAt.IsZero() || app.cronRuntime.state.LastReloadSummary != "" {
		t.Fatalf("reload state after takeover = (%v, %q), want cleared", app.cronRuntime.state.LastReloadAt, app.cronRuntime.state.LastReloadSummary)
	}
}
