package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestBuildRestartRootPageOnlyExposesChildEntry(t *testing.T) {
	catalog := buildRestartRootPageView(restartChildAvailability{Available: true}, "", "")
	if !catalog.Interactive {
		t.Fatalf("expected interactive restart catalog, got %#v", catalog)
	}
	assertCatalogUsesPlainTextContracts(t, &catalog)
	if len(catalog.Sections) != 1 {
		t.Fatalf("expected a single restart menu section, got %#v", catalog.Sections)
	}
	buttons := catalog.Sections[0].Entries[0].Buttons
	if len(buttons) != 1 || buttons[0].CommandText != "/restart child" {
		t.Fatalf("expected restart root page to expose only /restart child, got %#v", buttons)
	}
	if buttons[0].Disabled {
		t.Fatalf("expected enabled child restart button, got %#v", buttons[0])
	}
}

func TestRestartRootPageDisablesChildWhenSurfaceNotAttached(t *testing.T) {
	gateway := newLifecycleGateway()
	app, _ := newUpgradeTestApp(t, gateway)
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")

	events := app.handleDaemonCommand(control.DaemonCommand{
		Kind:             control.DaemonCommandRestart,
		SurfaceSessionID: "surface-1",
		Text:             "/restart",
	})
	page := catalogFromUIEvent(t, events[0])
	if len(page.Sections) != 1 || len(page.Sections[0].Entries) != 1 || len(page.Sections[0].Entries[0].Buttons) != 1 {
		t.Fatalf("unexpected restart root page: %#v", page)
	}
	button := page.Sections[0].Entries[0].Buttons[0]
	if !button.Disabled {
		t.Fatalf("expected detached restart button to be disabled, got %#v", button)
	}
	if !strings.Contains(catalogSummaryText(page), "请先 /list 或 /workspace 重新接入") && !strings.Contains(page.StatusText, "请先 /list 或 /workspace 重新接入") {
		t.Fatalf("expected detached restart guidance, got %#v", page)
	}
}

func TestParseRestartCommandTextRecognizesChild(t *testing.T) {
	parsed, err := parseRestartCommandText("/restart child")
	if err != nil {
		t.Fatalf("parseRestartCommandText: %v", err)
	}
	if parsed.Mode != restartCommandChild {
		t.Fatalf("mode = %q, want %q", parsed.Mode, restartCommandChild)
	}
}

func TestHandleRestartChildCommandRejectsBusyInstance(t *testing.T) {
	gateway := newLifecycleGateway()
	app, _ := newUpgradeTestApp(t, gateway)
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := app.service.Surface("surface-1")
	surface.AttachedInstanceID = "inst-1"
	surface.ActiveQueueItemID = "queue-1"
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Online:        true,
	})

	events := app.handleDaemonCommand(control.DaemonCommand{
		Kind:             control.DaemonCommandRestart,
		SurfaceSessionID: "surface-1",
		Text:             "/restart child",
	})
	if len(events) != 1 || events[0].Notice == nil {
		t.Fatalf("expected busy restart notice, got %#v", events)
	}
	if events[0].Notice.Code != "restart_child_busy" {
		t.Fatalf("notice code = %q, want restart_child_busy", events[0].Notice.Code)
	}
	if !strings.Contains(events[0].Notice.Text, "/stop") {
		t.Fatalf("expected busy notice to mention /stop, got %#v", events[0].Notice)
	}
}

func TestRestartChildCommandEmitsPreparingNoticeThenSuccess(t *testing.T) {
	gateway := newLifecycleGateway()
	app, _ := newUpgradeTestApp(t, gateway)
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := app.service.Surface("surface-1")
	surface.AttachedInstanceID = "inst-1"
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Online:        true,
	})

	started := make(chan struct{})
	release := make(chan struct{})
	app.sendAgentCommand = func(instanceID string, command agentproto.Command) error {
		go func() {
			close(started)
			<-release
			app.onCommandAck(context.Background(), instanceID, agentproto.CommandAck{
				CommandID: command.CommandID,
				Accepted:  true,
			})
			app.onEvents(context.Background(), instanceID, []agentproto.Event{
				agentproto.NewChildRestartUpdatedEvent(command.CommandID, "thread-1", agentproto.ChildRestartStatusSucceeded, nil),
			})
		}()
		return nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionRestartCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/restart child",
	})

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for restart child dispatch")
	}

	ops := gateway.snapshotOperations()
	if len(ops) == 0 {
		t.Fatal("expected preparing notice operation")
	}
	last := ops[len(ops)-1]
	if last.CardTitle != "Restart" || !strings.Contains(last.CardBody, "正在重启 droid 的 provider child") {
		t.Fatalf("expected preparing notice, got %#v", last)
	}

	close(release)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ops = gateway.snapshotOperations()
		for _, op := range ops {
			if op.CardTitle == "Restart" && strings.Contains(op.CardBody, "已重启 droid 的 provider child") {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for restart success notice, got %#v", gateway.snapshotOperations())
}
