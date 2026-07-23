package orchestrator

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestEnsureSurfaceCreatesFeishuRoomContextForGroupChat(t *testing.T) {
	svc := NewService(nil, Config{}, nil)

	svc.ensureSurface(control.Action{
		SurfaceSessionID: "feishu:app-1:chat:oc_1",
		GatewayID:        "app-1",
		ChatID:           "oc_1",
		ActorUserID:      "ou_1",
	})

	room := svc.root.FeishuRoomContexts["feishu:chat:oc_1"]
	if room == nil {
		t.Fatalf("expected room context for group chat")
	}
	if room.RoomID != "feishu:chat:oc_1" {
		t.Fatalf("room id = %q, want feishu:chat:oc_1", room.RoomID)
	}
	if room.ChatID != "oc_1" {
		t.Fatalf("chat id = %q, want oc_1", room.ChatID)
	}
	if !room.GatewayIDs["app-1"] {
		t.Fatalf("expected gateway evidence app-1")
	}
	if !room.SurfaceSessionIDs["feishu:app-1:chat:oc_1"] {
		t.Fatalf("expected surface evidence")
	}
}

func TestEnsureSurfaceDoesNotCreateFeishuRoomContextForP2P(t *testing.T) {
	svc := NewService(nil, Config{}, nil)

	svc.ensureSurface(control.Action{
		SurfaceSessionID: "feishu:app-1:user:ou_1",
		GatewayID:        "app-1",
		ChatID:           "ou_1",
		ActorUserID:      "ou_1",
	})

	if len(svc.root.FeishuRoomContexts) != 0 {
		t.Fatalf("expected no room contexts for p2p, got %d", len(svc.root.FeishuRoomContexts))
	}
}

func TestEnsureSurfaceMapsSameChatAcrossGatewaysToSameFeishuRoomContext(t *testing.T) {
	svc := NewService(nil, Config{}, nil)

	svc.ensureSurface(control.Action{SurfaceSessionID: "feishu:app-1:chat:oc_1", GatewayID: "app-1", ChatID: "oc_1"})
	svc.ensureSurface(control.Action{SurfaceSessionID: "feishu:app-2:chat:oc_1", GatewayID: "app-2", ChatID: "oc_1"})

	if len(svc.root.FeishuRoomContexts) != 1 {
		t.Fatalf("room context count = %d, want 1", len(svc.root.FeishuRoomContexts))
	}
	room := svc.root.FeishuRoomContexts["feishu:chat:oc_1"]
	if room == nil {
		t.Fatalf("expected shared room context")
	}
	for _, gatewayID := range []string{"app-1", "app-2"} {
		if !room.GatewayIDs[gatewayID] {
			t.Fatalf("expected gateway evidence %s", gatewayID)
		}
	}
	for _, surfaceID := range []string{"feishu:app-1:chat:oc_1", "feishu:app-2:chat:oc_1"} {
		if !room.SurfaceSessionIDs[surfaceID] {
			t.Fatalf("expected surface evidence %s", surfaceID)
		}
	}
	surfaces := svc.feishuRoomSurfaces("feishu:chat:oc_1")
	if len(surfaces) != 2 {
		t.Fatalf("room surfaces = %d, want 2", len(surfaces))
	}
}

func TestEnsureSurfaceSeparatesDifferentFeishuRoomContexts(t *testing.T) {
	svc := NewService(nil, Config{}, nil)

	svc.ensureSurface(control.Action{SurfaceSessionID: "feishu:app-1:chat:oc_1", GatewayID: "app-1", ChatID: "oc_1"})
	svc.ensureSurface(control.Action{SurfaceSessionID: "feishu:app-1:chat:oc_2", GatewayID: "app-1", ChatID: "oc_2"})

	if len(svc.root.FeishuRoomContexts) != 2 {
		t.Fatalf("room context count = %d, want 2", len(svc.root.FeishuRoomContexts))
	}
	if svc.root.FeishuRoomContexts["feishu:chat:oc_1"] == nil {
		t.Fatalf("expected room for oc_1")
	}
	if svc.root.FeishuRoomContexts["feishu:chat:oc_2"] == nil {
		t.Fatalf("expected room for oc_2")
	}
}
