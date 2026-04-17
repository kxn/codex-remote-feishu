package daemon

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestCronRepairTakesOverBindingWhenOwnerUnavailable(t *testing.T) {
	newAPI := newFlakyCronBootstrapBitableAPI()
	newAPI.failCreateField = false

	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	setCronGatewayLookup(app, "gateway-2", "app-2")
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
			AppToken: "app-old",
			AppURL:   "https://example.feishu.cn/base/app-old",
			Tables: cronTableIDs{
				Tasks:      "tbl-tasks-old",
				Workspaces: "tbl-workspaces-old",
				Runs:       "tbl-runs-old",
				Meta:       "tbl-meta-old",
			},
		},
		Jobs: []cronJobState{{
			RecordID:        "rec-stale",
			Name:            "Stale",
			ScheduleType:    cronScheduleTypeInterval,
			IntervalMinutes: 15,
			WorkspaceKey:    "/tmp/project",
			NextRunAt:       time.Now().Add(15 * time.Minute),
		}},
		LastReloadAt:      time.Now().UTC().Add(-time.Minute),
		LastReloadSummary: "旧 owner 下的 stale jobs",
	}
	app.cronBitableFactory = func(gatewayID string) (feishu.BitableAPI, error) {
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
	if app.cronState.OwnerGatewayID != "gateway-2" || app.cronState.OwnerAppID != "app-2" {
		t.Fatalf("owner state = %#v, want gateway-2/app-2", app.cronState)
	}
	if app.cronState.Bitable == nil || app.cronState.Bitable.AppToken == "" || app.cronState.Bitable.AppToken == "app-old" {
		t.Fatalf("binding after takeover = %#v, want fresh binding", app.cronState.Bitable)
	}
	if len(app.cronState.Jobs) != 0 {
		t.Fatalf("jobs after takeover = %#v, want cleared jobs", app.cronState.Jobs)
	}
	if !app.cronState.LastReloadAt.IsZero() || app.cronState.LastReloadSummary != "" {
		t.Fatalf("reload state after takeover = (%v, %q), want cleared", app.cronState.LastReloadAt, app.cronState.LastReloadSummary)
	}
}
