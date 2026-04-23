package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	larkbitable "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	cronrt "github.com/kxn/codex-remote-feishu/internal/app/cronruntime"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestLoadCronStateLockedHardMigratesLegacyGatewayOwner(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	setCronGatewayLookup(app, "gateway-1", "app-1")
	app.headlessRuntime.Paths.StateDir = t.TempDir()

	rawState := cronrt.StateFile{
		SchemaVersion:    cronrt.StateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		GatewayID:        "gateway-1",
		Bitable: &cronrt.BitableState{
			AppToken: "app-cron",
		},
	}
	payload, err := json.Marshal(rawState)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(app.headlessRuntime.Paths.StateDir, "cron-state.json"), payload, 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	app.mu.Lock()
	stateValue, err := app.loadCronStateLocked(false)
	app.mu.Unlock()
	if err != nil {
		t.Fatalf("loadCronStateLocked: %v", err)
	}
	if stateValue.OwnerGatewayID != "gateway-1" || stateValue.OwnerAppID != "app-1" {
		t.Fatalf("owner state = %#v, want migrated gateway-1/app-1", stateValue)
	}
	if stateValue.OwnerBoundAt.IsZero() {
		t.Fatalf("owner bound time not migrated: %#v", stateValue)
	}

	persistedRaw, err := os.ReadFile(filepath.Join(app.headlessRuntime.Paths.StateDir, "cron-state.json"))
	if err != nil {
		t.Fatalf("read persisted state: %v", err)
	}
	var persisted cronrt.StateFile
	if err := json.Unmarshal(persistedRaw, &persisted); err != nil {
		t.Fatalf("unmarshal persisted state: %v", err)
	}
	if persisted.OwnerGatewayID != "gateway-1" || persisted.OwnerAppID != "app-1" || persisted.OwnerBoundAt.IsZero() {
		t.Fatalf("persisted owner state = %#v, want migrated owner binding", persisted)
	}
}

func TestInspectCronOwnerViewTreatsLegacyGatewayStateAsHealthy(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	setCronGatewayLookup(app, "gateway-1", "app-1")

	view := app.inspectCronOwnerView(&cronrt.StateFile{
		SchemaVersion:    cronrt.StateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		GatewayID:        "gateway-1",
		Bitable: &cronrt.BitableState{
			AppToken: "app-cron",
		},
	})
	if view.Status != cronrt.OwnerStatusHealthy {
		t.Fatalf("owner view status = %q, want %q", view.Status, cronrt.OwnerStatusHealthy)
	}
	if view.NeedsRepair {
		t.Fatalf("owner view unexpectedly needs repair: %#v", view)
	}
	if view.StatusLabel != "正常" {
		t.Fatalf("status label = %q, want 正常", view.StatusLabel)
	}
}

func TestReloadCronJobsNowHardMigratesLegacyGatewayWithoutLegacyNotice(t *testing.T) {
	api := &fakeCronBitableAPI{
		recordsByTable: map[string][]*larkbitable.AppTableRecord{
			"tbl-workspaces": {},
			"tbl-tasks":      {},
		},
	}
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	setCronGatewayLookup(app, "gateway-1", "app-1")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronRuntime.loaded = true
	app.cronRuntime.state = &cronrt.StateFile{
		SchemaVersion:    cronrt.StateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		GatewayID:        "gateway-1",
		Bitable: &cronrt.BitableState{
			AppToken: "app-cron",
			Tables: cronrt.TableIDs{
				Tasks:      "tbl-tasks",
				Workspaces: "tbl-workspaces",
			},
		},
	}
	app.cronRuntime.bitableFactory = func(gatewayID string) (feishu.BitableAPI, error) {
		if gatewayID != "gateway-1" {
			t.Fatalf("unexpected gateway %q", gatewayID)
		}
		return api, nil
	}

	summary, err := app.reloadCronJobsNow(control.DaemonCommand{GatewayID: "gateway-1", SurfaceSessionID: "surface-1"})
	if err != nil {
		t.Fatalf("reloadCronJobsNow: %v", err)
	}
	if strings.Contains(summary, "回填正式 owner") {
		t.Fatalf("summary = %q, want no legacy backfill wording", summary)
	}
	if app.cronRuntime.state.OwnerGatewayID != "gateway-1" || app.cronRuntime.state.OwnerAppID != "app-1" {
		t.Fatalf("owner state = %#v, want hard-migrated owner binding", app.cronRuntime.state)
	}
}
