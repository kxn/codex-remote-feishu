package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestTransitionSurfaceRouteCoreMaintainsClaimsAcrossStates(t *testing.T) {
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
	inst := svc.root.Instances["inst-1"]

	if !svc.transitionSurfaceRouteCore(surface, inst, surfaceRouteCoreState{
		AttachedInstanceID: "inst-1",
		WorkspaceKey:       "/data/dl/droid",
		RouteMode:          state.RouteModePinned,
		SelectedThreadID:   "thread-1",
		ThreadClaimPolicy:  surfaceRouteThreadClaimVisible,
	}) {
		t.Fatal("expected pinned route transition to succeed")
	}
	if surface.AttachedInstanceID != "inst-1" || surface.SelectedThreadID != "thread-1" || surface.RouteMode != state.RouteModePinned {
		t.Fatalf("unexpected pinned route state: %#v", surface)
	}
	if claim := svc.workspaceClaims["/data/dl/droid"]; claim == nil || claim.SurfaceSessionID != "surface-1" {
		t.Fatalf("expected workspace claim for surface-1, got %#v", claim)
	}
	if claim := svc.instanceClaims["inst-1"]; claim == nil || claim.SurfaceSessionID != "surface-1" {
		t.Fatalf("expected instance claim for surface-1, got %#v", claim)
	}
	if claim := svc.threadClaims["thread-1"]; claim == nil || claim.SurfaceSessionID != "surface-1" {
		t.Fatalf("expected thread claim for surface-1, got %#v", claim)
	}

	if !svc.transitionSurfaceRouteCore(surface, inst, surfaceRouteCoreState{
		AttachedInstanceID:   "inst-1",
		RouteMode:            state.RouteModeNewThreadReady,
		PreparedThreadCWD:    "/data/dl/droid",
		PreparedFromThreadID: "thread-1",
	}) {
		t.Fatal("expected new-thread-ready transition to succeed")
	}
	if surface.RouteMode != state.RouteModeNewThreadReady || surface.SelectedThreadID != "" || surface.PreparedThreadCWD != "/data/dl/droid" || surface.PreparedFromThreadID != "thread-1" {
		t.Fatalf("unexpected new-thread-ready state: %#v", surface)
	}
	if claim := svc.threadClaims["thread-1"]; claim != nil {
		t.Fatalf("expected thread claim to be released, got %#v", claim)
	}
	if claim := svc.instanceClaims["inst-1"]; claim == nil || claim.SurfaceSessionID != "surface-1" {
		t.Fatalf("expected instance claim to remain, got %#v", claim)
	}
	if claim := svc.workspaceClaims["/data/dl/droid"]; claim == nil || claim.SurfaceSessionID != "surface-1" {
		t.Fatalf("expected workspace claim to remain, got %#v", claim)
	}

	if !svc.transitionSurfaceRouteCore(surface, nil, surfaceRouteCoreState{WorkspaceKey: "/data/dl/droid"}) {
		t.Fatal("expected detached transition with workspace memory to succeed")
	}
	if surface.AttachedInstanceID != "" || surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("unexpected detached route state: %#v", surface)
	}
	if surface.PreparedThreadCWD != "" || surface.PreparedFromThreadID != "" {
		t.Fatalf("expected prepared-new-thread state to be cleared, got %#v", surface)
	}
	if surface.ClaimedWorkspaceKey != "/data/dl/droid" {
		t.Fatalf("expected detached workspace memory, got %q", surface.ClaimedWorkspaceKey)
	}
	if claim := svc.workspaceClaims["/data/dl/droid"]; claim != nil {
		t.Fatalf("expected active workspace claim to be released on detach, got %#v", claim)
	}
	if claim := svc.instanceClaims["inst-1"]; claim != nil {
		t.Fatalf("expected active instance claim to be released on detach, got %#v", claim)
	}
}

func TestTransitionSurfaceRouteCoreRejectsConflictingAttachWithoutMutation(t *testing.T) {
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid-1",
		WorkspaceRoot: "/data/dl/droid-1",
		WorkspaceKey:  "/data/dl/droid-1",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "A", CWD: "/data/dl/droid-1", Loaded: true},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-2",
		DisplayName:   "droid-2",
		WorkspaceRoot: "/data/dl/droid-2",
		WorkspaceKey:  "/data/dl/droid-2",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "B", CWD: "/data/dl/droid-2", Loaded: true},
		},
	})
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	svc.MaterializeSurface("surface-2", "app-1", "chat-2", "user-2")
	first := svc.root.Surfaces["surface-1"]
	second := svc.root.Surfaces["surface-2"]

	if !svc.transitionSurfaceRouteCore(first, svc.root.Instances["inst-1"], surfaceRouteCoreState{
		AttachedInstanceID: "inst-1",
		WorkspaceKey:       "/data/dl/droid-1",
		RouteMode:          state.RouteModePinned,
		SelectedThreadID:   "thread-1",
		ThreadClaimPolicy:  surfaceRouteThreadClaimVisible,
	}) {
		t.Fatal("expected first surface attach to succeed")
	}
	if !svc.transitionSurfaceRouteCore(second, svc.root.Instances["inst-2"], surfaceRouteCoreState{
		AttachedInstanceID: "inst-2",
		WorkspaceKey:       "/data/dl/droid-2",
		RouteMode:          state.RouteModePinned,
		SelectedThreadID:   "thread-2",
		ThreadClaimPolicy:  surfaceRouteThreadClaimVisible,
	}) {
		t.Fatal("expected second surface attach to succeed")
	}

	if svc.transitionSurfaceRouteCore(second, svc.root.Instances["inst-1"], surfaceRouteCoreState{
		AttachedInstanceID: "inst-1",
		WorkspaceKey:       "/data/dl/droid-1",
		RouteMode:          state.RouteModeUnbound,
	}) {
		t.Fatal("expected conflicting attach to fail")
	}

	if second.AttachedInstanceID != "inst-2" || second.SelectedThreadID != "thread-2" || second.RouteMode != state.RouteModePinned {
		t.Fatalf("surface-2 mutated on failed attach: %#v", second)
	}
	if claim := svc.workspaceClaims["/data/dl/droid-2"]; claim == nil || claim.SurfaceSessionID != "surface-2" {
		t.Fatalf("expected workspace-2 claim to stay on surface-2, got %#v", claim)
	}
	if claim := svc.instanceClaims["inst-2"]; claim == nil || claim.SurfaceSessionID != "surface-2" {
		t.Fatalf("expected instance-2 claim to stay on surface-2, got %#v", claim)
	}
	if claim := svc.threadClaims["thread-2"]; claim == nil || claim.SurfaceSessionID != "surface-2" {
		t.Fatalf("expected thread-2 claim to stay on surface-2, got %#v", claim)
	}
}
