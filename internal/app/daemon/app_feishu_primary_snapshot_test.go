package daemon

import (
	"sync"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestFeishuPrimaryGatewayHookTracksRoomPrimarySnapshot(t *testing.T) {
	app, _ := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	lookup := app.gatewayRuntimeHooks().PrimaryGatewayForChat
	if lookup == nil {
		t.Fatal("expected primary lookup hook")
	}

	app.mu.Lock()
	app.service.MaterializeFeishuRoomState([]state.FeishuRoomStateRecord{{
		RoomID:           "feishu:chat:oc_room",
		ChatID:           "oc_room",
		WorkspaceKey:     "/data/workspaces/shared",
		PrimaryGatewayID: "app-1",
	}})
	app.syncFeishuRoomStateLocked()
	app.mu.Unlock()
	if got := lookup("oc_room"); got != "app-1" {
		t.Fatalf("primary lookup after set = %q, want app-1", got)
	}

	app.mu.Lock()
	app.service.MaterializeFeishuRoomState([]state.FeishuRoomStateRecord{{
		RoomID:           "feishu:chat:oc_room",
		ChatID:           "oc_room",
		WorkspaceKey:     "/data/workspaces/shared",
		PrimaryGatewayID: "app-2",
	}})
	app.syncFeishuRoomStateLocked()
	app.mu.Unlock()
	if got := lookup("feishu:chat:oc_room"); got != "app-2" {
		t.Fatalf("primary lookup after switch = %q, want app-2", got)
	}

	app.mu.Lock()
	app.service.MaterializeFeishuRoomState([]state.FeishuRoomStateRecord{{
		RoomID:       "feishu:chat:oc_room",
		ChatID:       "oc_room",
		WorkspaceKey: "/data/workspaces/shared",
	}})
	app.syncFeishuRoomStateLocked()
	app.mu.Unlock()
	if got := lookup("oc_room"); got != "" {
		t.Fatalf("primary lookup after clear = %q, want empty", got)
	}
}

func TestFeishuPrimaryGatewayHookDoesNotRaceWithRoomPrimaryUpdates(t *testing.T) {
	app, _ := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	lookup := app.gatewayRuntimeHooks().PrimaryGatewayForChat
	if lookup == nil {
		t.Fatal("expected primary lookup hook")
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = lookup("oc_room")
			}
		}
	}()

	for i := 0; i < 1000; i++ {
		primary := "app-1"
		if i%2 == 0 {
			primary = "app-2"
		}
		app.mu.Lock()
		app.service.MaterializeFeishuRoomState([]state.FeishuRoomStateRecord{{
			RoomID:           "feishu:chat:oc_room",
			ChatID:           "oc_room",
			WorkspaceKey:     "/data/workspaces/shared",
			PrimaryGatewayID: primary,
		}})
		app.refreshFeishuPrimaryGatewaySnapshotLocked()
		app.mu.Unlock()
	}
	close(stop)
	wg.Wait()
}
