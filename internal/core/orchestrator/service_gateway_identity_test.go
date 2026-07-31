package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPurgeGatewayIdentityStatePreservesRuntimeOwnedByAnotherSurface(t *testing.T) {
	now := time.Now().UTC()
	svc := newServiceForTest(&now)
	oldSurfaceID := "feishu:app-old:chat:oc_room"
	otherSurfaceID := "feishu:app-other:chat:oc_room"
	svc.MaterializeSurface(oldSurfaceID, "app-old", "oc_room", "ou_old")
	svc.MaterializeSurface(otherSurfaceID, "app-other", "oc_room", "ou_other")
	oldSurface := svc.root.Surfaces[oldSurfaceID]
	otherSurface := svc.root.Surfaces[otherSurfaceID]
	oldSurface.AttachedInstanceID = "inst-shared"
	otherSurface.AttachedInstanceID = "inst-shared"

	svc.turns.activeRemote["inst-shared"] = &remoteTurnBinding{
		InstanceID:       "inst-shared",
		SurfaceSessionID: otherSurfaceID,
		ThreadID:         "thread-other",
		TurnID:           "turn-other",
	}
	renderKey := turnRenderKey("inst-shared", "thread-other", "turn-other")
	svc.progress.pendingTurnText[renderKey] = &completedTextItem{
		InstanceID: "inst-shared",
		ThreadID:   "thread-other",
		TurnID:     "turn-other",
	}
	svc.itemBuffers["item-other"] = &itemBuffer{
		InstanceID: "inst-shared",
		ThreadID:   "thread-other",
		TurnID:     "turn-other",
		ItemID:     "item-other",
	}

	svc.PurgeGatewayIdentityState("app-old")

	if binding := svc.turns.activeRemote["inst-shared"]; binding == nil || binding.SurfaceSessionID != otherSurfaceID {
		t.Fatalf("other surface active binding was removed: %#v", binding)
	}
	if svc.progress.pendingTurnText[renderKey] == nil {
		t.Fatal("other surface pending turn text was removed")
	}
	if svc.itemBuffers["item-other"] == nil {
		t.Fatal("other surface item buffer was removed")
	}
}

func TestPurgeGatewayIdentityStateClearsBotRuntimeAndPreservesRoomWorkspace(t *testing.T) {
	now := time.Now().UTC()
	svc := newServiceForTest(&now)
	oldSurfaceID := "feishu:app-old:chat:oc_room"
	otherSurfaceID := "feishu:app-other:chat:oc_room"
	svc.MaterializeSurface(oldSurfaceID, "app-old", "oc_room", "ou_old")
	svc.MaterializeSurface(otherSurfaceID, "app-other", "oc_room", "ou_other")
	svc.MaterializeBotCapabilitySettings([]state.BotCapabilitySettingsRecord{
		{GatewayID: "app-old", ProductMode: state.ProductModeNormal},
		{GatewayID: "app-other", ProductMode: state.ProductModeNormal},
	})
	svc.MaterializeFeishuRoomState([]state.FeishuRoomStateRecord{{
		RoomID:           "feishu:chat:oc_room",
		ChatID:           "oc_room",
		WorkspaceKey:     "/data/workspaces/shared",
		PrimaryGatewayID: "app-old",
	}})
	svc.ensureSurfaceUIRuntime(svc.root.Surfaces[oldSurfaceID]).ActiveTargetPicker = &activeTargetPickerRecord{PickerID: "old-picker"}
	svc.ensureSurfaceUIRuntime(svc.root.Surfaces[otherSurfaceID]).ActiveTargetPicker = &activeTargetPickerRecord{PickerID: "other-picker"}
	oldNoticeKey := activeNoticeCooldownKey("runtime", oldSurfaceID, "inst-old", "thread-old", "notice")
	otherNoticeKey := activeNoticeCooldownKey("runtime", otherSurfaceID, "inst-other", "thread-other", "notice")
	svc.activeNoticeCooldowns[oldNoticeKey] = now.Add(time.Minute)
	svc.activeNoticeCooldowns[otherNoticeKey] = now.Add(time.Minute)

	removed := svc.PurgeGatewayIdentityState("app-old")

	if len(removed) != 1 || removed[0] != oldSurfaceID {
		t.Fatalf("removed surfaces = %#v, want only %s", removed, oldSurfaceID)
	}
	if svc.root.Surfaces[oldSurfaceID] != nil || svc.root.Surfaces[otherSurfaceID] == nil {
		t.Fatalf("surfaces after purge = %#v", svc.root.Surfaces)
	}
	if svc.surfaceUIRuntime[oldSurfaceID] != nil || svc.surfaceUIRuntime[otherSurfaceID] == nil {
		t.Fatalf("surface UI runtime after purge = %#v", svc.surfaceUIRuntime)
	}
	if _, ok := svc.activeNoticeCooldowns[oldNoticeKey]; ok {
		t.Fatal("old surface notice cooldown survived identity purge")
	}
	if _, ok := svc.activeNoticeCooldowns[otherNoticeKey]; !ok {
		t.Fatal("other surface notice cooldown was removed")
	}
	if _, ok := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-old")]; ok {
		t.Fatal("old bot capability settings survived identity purge")
	}
	if _, ok := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-other")]; !ok {
		t.Fatal("other bot capability settings were removed")
	}
	room := svc.root.FeishuRoomContexts["feishu:chat:oc_room"]
	if room == nil || room.WorkspaceKey != "/data/workspaces/shared" || room.PrimaryGatewayID != "" {
		t.Fatalf("room after purge = %#v", room)
	}
	if room.SurfaceSessionIDs[oldSurfaceID] || !room.SurfaceSessionIDs[otherSurfaceID] {
		t.Fatalf("room surfaces after purge = %#v", room.SurfaceSessionIDs)
	}
}
