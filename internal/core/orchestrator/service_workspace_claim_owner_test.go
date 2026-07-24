package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestWorkspaceClaimOwnerAllowsSameFeishuRoomSurfaces(t *testing.T) {
	svc := newWorkspaceClaimOwnerTestService(t)
	first := svc.root.Surfaces["feishu:app-1:chat:oc_room"]
	second := svc.root.Surfaces["feishu:app-2:chat:oc_room"]

	if !svc.claimWorkspace(first, "/data/dl/shared") {
		t.Fatal("expected first same-room surface to claim workspace")
	}
	if owner := svc.workspaceBusyOwnerForSurface(second, "/data/dl/shared"); owner != nil {
		t.Fatalf("same-room surface should not see busy owner, got %s", owner.SurfaceSessionID)
	}
	if !svc.claimWorkspace(second, "/data/dl/shared") {
		t.Fatal("expected second same-room surface to share workspace claim")
	}

	claim := svc.workspaceClaims["/data/dl/shared"]
	if claim == nil {
		t.Fatal("expected workspace claim")
	}
	if claim.OwnerScope != workspaceClaimOwnerRoom || claim.OwnerID != "feishu:chat:oc_room" {
		t.Fatalf("claim owner = %s/%s, want room feishu:chat:oc_room", claim.OwnerScope, claim.OwnerID)
	}
	if claim.SurfaceSessionID == "" {
		t.Fatal("expected display surface session id")
	}
}

func TestWorkspaceClaimOwnerRejectsDifferentFeishuRoomsAndPrivateSurfaces(t *testing.T) {
	svc := newWorkspaceClaimOwnerTestService(t)
	roomSurface := svc.root.Surfaces["feishu:app-1:chat:oc_room"]
	otherRoomSurface := svc.root.Surfaces["feishu:app-1:chat:oc_other"]
	privateSurface := svc.root.Surfaces["feishu:app-1:user:ou_1"]

	if !svc.claimWorkspace(roomSurface, "/data/dl/shared") {
		t.Fatal("expected room surface to claim workspace")
	}
	if owner := svc.workspaceBusyOwnerForSurface(otherRoomSurface, "/data/dl/shared"); owner == nil {
		t.Fatal("expected different room to see busy owner")
	}
	if svc.claimWorkspace(otherRoomSurface, "/data/dl/shared") {
		t.Fatal("expected different room claim to be rejected")
	}
	if owner := svc.workspaceBusyOwnerForSurface(privateSurface, "/data/dl/shared"); owner == nil {
		t.Fatal("expected private surface to see busy owner")
	}
	if svc.claimWorkspace(privateSurface, "/data/dl/shared") {
		t.Fatal("expected private surface claim to be rejected")
	}
}

func TestWorkspaceClaimOwnerReleaseOneSameRoomSurfaceKeepsClaimUntilLastRelease(t *testing.T) {
	svc := newWorkspaceClaimOwnerTestService(t)
	first := svc.root.Surfaces["feishu:app-1:chat:oc_room"]
	second := svc.root.Surfaces["feishu:app-2:chat:oc_room"]

	if !svc.claimWorkspace(first, "/data/dl/shared") || !svc.claimWorkspace(second, "/data/dl/shared") {
		t.Fatal("expected both same-room surfaces to share workspace claim")
	}
	svc.releaseSurfaceWorkspaceClaim(first)
	if first.ClaimedWorkspaceKey != "" {
		t.Fatalf("first surface workspace = %q, want empty", first.ClaimedWorkspaceKey)
	}
	if claim := svc.workspaceClaims["/data/dl/shared"]; claim == nil {
		t.Fatal("expected room claim to remain after first release")
	} else if claim.OwnerScope != workspaceClaimOwnerRoom || claim.OwnerID != "feishu:chat:oc_room" {
		t.Fatalf("claim owner after first release = %s/%s", claim.OwnerScope, claim.OwnerID)
	}
	if owner := svc.workspaceBusyOwnerForSurface(first, "/data/dl/shared"); owner != nil {
		t.Fatalf("released same-room surface should be allowed to rejoin, got busy owner %s", owner.SurfaceSessionID)
	}

	svc.releaseSurfaceWorkspaceClaim(second)
	if claim := svc.workspaceClaims["/data/dl/shared"]; claim != nil {
		t.Fatalf("expected workspace claim to clear after last room surface release, got %#v", claim)
	}
}

func TestWorkspaceClaimOwnerDoesNotShareInstanceOrThreadClaimsWithinRoom(t *testing.T) {
	svc := newWorkspaceClaimOwnerTestService(t)
	first := svc.root.Surfaces["feishu:app-1:chat:oc_room"]
	second := svc.root.Surfaces["feishu:app-2:chat:oc_room"]

	if !svc.claimWorkspace(first, "/data/dl/shared") || !svc.claimWorkspace(second, "/data/dl/shared") {
		t.Fatal("expected same-room workspace sharing")
	}
	first.AttachedInstanceID = "inst-1"
	if !svc.claimInstance(first, "inst-1") {
		t.Fatal("expected first surface to claim instance")
	}
	if svc.claimInstance(second, "inst-1") {
		t.Fatal("expected same-room instance claim to remain surface-exclusive")
	}
	first.SelectedThreadID = "thread-1"
	svc.bindThreadClaim(first, "inst-1", "thread-1")
	if owner := svc.threadClaimSurface("thread-1"); owner == nil || owner.SurfaceSessionID != first.SurfaceSessionID {
		t.Fatalf("expected first surface to own thread claim, got %#v", owner)
	}
	if svc.claimThread(second, svc.root.Instances["inst-1"], "thread-1") {
		t.Fatal("expected same-room thread claim to remain surface-exclusive")
	}
}

func newWorkspaceClaimOwnerTestService(t *testing.T) *Service {
	t.Helper()
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "shared",
		WorkspaceRoot: "/data/dl/shared",
		WorkspaceKey:  "/data/dl/shared",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "A", CWD: "/data/dl/shared", Loaded: true},
		},
	})
	for _, surface := range []struct {
		id      string
		gateway string
		chat    string
		actor   string
	}{
		{id: "feishu:app-1:chat:oc_room", gateway: "app-1", chat: "oc_room", actor: "ou_1"},
		{id: "feishu:app-2:chat:oc_room", gateway: "app-2", chat: "oc_room", actor: "ou_2"},
		{id: "feishu:app-1:chat:oc_other", gateway: "app-1", chat: "oc_other", actor: "ou_3"},
		{id: "feishu:app-1:user:ou_1", gateway: "app-1", chat: "ou_1", actor: "ou_1"},
	} {
		svc.MaterializeSurface(surface.id, surface.gateway, surface.chat, surface.actor)
		record := svc.root.Surfaces[surface.id]
		record.ProductMode = state.ProductModeNormal
	}
	return svc
}
