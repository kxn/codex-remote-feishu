package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestTriggerCronJobLaunchesImmediatelyWithoutChangingNextRun(t *testing.T) {
	workspace := t.TempDir()
	nextRunAt := time.Date(2026, 4, 18, 3, 0, 0, 0, time.UTC)
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	setCronGatewayLookup(app, "gateway-1", "app-1")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronLoaded = true
	app.cronState = &cronStateFile{
		GatewayID:      "gateway-1",
		OwnerGatewayID: "gateway-1",
		OwnerAppID:     "app-1",
		Bitable: &cronBitableState{
			AppToken: "app-1",
			Tables: cronTableIDs{
				Tasks: "tbl-tasks",
				Runs:  "tbl-runs",
			},
		},
		Jobs: []cronJobState{{
			RecordID:        "rec-task-1",
			Name:            "Nightly",
			ScheduleType:    cronScheduleTypeInterval,
			IntervalMinutes: 15,
			WorkspaceKey:    workspace,
			Prompt:          "check CI",
			TimeoutMinutes:  20,
			NextRunAt:       nextRunAt,
		}},
	}
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote",
		ConfigPath: "/tmp/config.json",
		Paths: relayruntime.Paths{
			StateDir: app.headlessRuntime.Paths.StateDir,
		},
	})
	var launches int
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		launches++
		return 4321, nil
	}

	event, err := app.triggerCronJob(control.DaemonCommand{SurfaceSessionID: "surface-1"}, "rec-task-1")
	if err != nil {
		t.Fatalf("triggerCronJob: %v", err)
	}
	if launches != 1 {
		t.Fatalf("launches = %d, want 1", launches)
	}
	if event == nil || event.Notice == nil || !strings.Contains(event.Notice.Text, "不会改动原有下次调度时间") {
		t.Fatalf("event = %#v, want success notice", event)
	}
	if got := app.cronState.Jobs[0].NextRunAt; !got.Equal(nextRunAt) {
		t.Fatalf("next run = %s, want unchanged %s", got, nextRunAt)
	}
	if len(app.cronRuns) != 1 {
		t.Fatalf("cronRuns = %#v, want one active run", app.cronRuns)
	}
}

func TestTriggerCronJobRespectsConcurrencyLimit(t *testing.T) {
	workspace := t.TempDir()
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	setCronGatewayLookup(app, "gateway-1", "app-1")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronLoaded = true
	app.cronState = &cronStateFile{
		GatewayID:      "gateway-1",
		OwnerGatewayID: "gateway-1",
		OwnerAppID:     "app-1",
		Bitable: &cronBitableState{
			AppToken: "app-1",
			Tables: cronTableIDs{
				Tasks: "tbl-tasks",
				Runs:  "tbl-runs",
			},
		},
		Jobs: []cronJobState{{
			RecordID:        "rec-task-1",
			Name:            "Nightly",
			ScheduleType:    cronScheduleTypeInterval,
			IntervalMinutes: 15,
			MaxConcurrency:  1,
			WorkspaceKey:    workspace,
			Prompt:          "check CI",
			TimeoutMinutes:  20,
			NextRunAt:       time.Now().Add(15 * time.Minute),
		}},
	}
	app.cronRuns["inst-running"] = &cronRunState{
		InstanceID:  "inst-running",
		JobRecordID: "rec-task-1",
		JobName:     "Nightly",
	}
	app.cronJobActiveRuns[cronJobActiveKey("rec-task-1", "Nightly")] = map[string]struct{}{"inst-running": {}}
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		t.Fatalf("triggerCronJob must not launch when concurrency is exhausted")
		return 0, nil
	}

	event, err := app.triggerCronJob(control.DaemonCommand{SurfaceSessionID: "surface-1"}, "rec-task-1")
	if err == nil || !strings.Contains(err.Error(), "并发上限") {
		t.Fatalf("triggerCronJob error = %v, want 并发上限", err)
	}
	if event != nil {
		t.Fatalf("event = %#v, want nil on concurrency rejection", event)
	}
}
