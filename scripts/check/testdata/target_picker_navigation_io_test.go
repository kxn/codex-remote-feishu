package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// This test is compiled only by the host-resolver overlay selftest. It observes
// the actual resolution boundary rather than a catalog mock or call-site string.
func TestTargetPickerNavigationHostIOBoundary(t *testing.T) {
	for _, attached := range []bool{false, true} {
		for _, source := range []control.ActionKind{control.ActionListInstances, control.ActionShowThreads} {
			name := string(source)
			if attached {
				name += "/attached"
			} else {
				name += "/detached"
			}
			t.Run(name, func(t *testing.T) {
				now := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
				svc := newServiceForTest(&now)
				cwd := t.TempDir()
				svc.UpsertInstance(&state.InstanceRecord{InstanceID: "instance", WorkspaceRoot: cwd, WorkspaceKey: cwd, Online: true, Threads: map[string]*state.ThreadRecord{"thread": {ThreadID: "thread", CWD: cwd, Loaded: true, LastUsedAt: now}}})
				svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
				surface := svc.root.Surfaces["surface-1"]
				if attached {
					svc.useThread(surface, "thread", true)
					if surface.SelectedThreadID != "thread" {
						t.Fatal("fixture could not attach")
					}
				}
				picker := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{Kind: source, SurfaceSessionID: surface.SurfaceSessionID, ChatID: surface.ChatID, ActorUserID: "user-1"}))
				selectedWorkspace := picker.SelectedWorkspaceKey
				if selectedWorkspace == "" {
					t.Fatal("fixture has no workspace option")
				}
				state.TargetPickerHostResolutionReads = 0
				state.TargetPickerHostResolutionForbidden = true
				defer func() { state.TargetPickerHostResolutionForbidden = false }()
				for _, action := range []control.Action{
					{Kind: control.ActionTargetPickerSelectWorkspace, WorkspaceKey: selectedWorkspace},
					{Kind: control.ActionTargetPickerSelectSession, TargetPickerValue: targetPickerThreadValue("thread")},
					{Kind: control.ActionTargetPickerPage, FieldName: frontstagecontract.CardTargetPickerSessionFieldName},
					{Kind: control.ActionTargetPickerPage, FieldName: frontstagecontract.CardTargetPickerWorkspaceFieldName},
					{Kind: control.ActionTargetPickerSelectWorkspace, WorkspaceKey: "/not-an-emitted-workspace"},
					{Kind: control.ActionTargetPickerSelectSession, TargetPickerValue: "thread:not-an-emitted-thread"},
				} {
					action.SurfaceSessionID, action.ActorUserID, action.PickerID = surface.SurfaceSessionID, "user-1", picker.PickerID
					action.CatalogFamilyID, action.CatalogVariantID, action.CatalogBackend = picker.CatalogFamilyID, picker.CatalogVariantID, picker.CatalogBackend
					action.Inbound = &control.ActionInboundMeta{CardCallback: true, CardDaemonLifecycleID: "test-daemon"}
					events := svc.ApplySurfaceAction(action)
					if len(events) != 1 || events[0].TargetPickerView == nil {
						t.Fatalf("navigation lost its card: %#v", events)
					}
				}
				if state.TargetPickerHostResolutionReads != 0 {
					t.Fatal("navigation accessed the host filesystem")
				}
				wrongActor := control.Action{Kind: control.ActionTargetPickerSelectWorkspace, SurfaceSessionID: surface.SurfaceSessionID, ActorUserID: "other-user", PickerID: picker.PickerID, WorkspaceKey: selectedWorkspace}
				if events := svc.ApplySurfaceAction(wrongActor); len(events) == 0 || events[0].TargetPickerView != nil {
					t.Fatal("navigation bypassed owner admission")
				}
				assertRejected := func(id string) {
					t.Helper()
					for _, kind := range []control.ActionKind{control.ActionTargetPickerSelectWorkspace, control.ActionTargetPickerSelectSession, control.ActionTargetPickerPage} {
						events := svc.ApplySurfaceAction(control.Action{Kind: kind, SurfaceSessionID: surface.SurfaceSessionID, ActorUserID: "user-1", PickerID: id, WorkspaceKey: selectedWorkspace, TargetPickerValue: targetPickerThreadValue("thread"), FieldName: frontstagecontract.CardTargetPickerSessionFieldName,
							CatalogFamilyID: picker.CatalogFamilyID, CatalogVariantID: picker.CatalogVariantID, CatalogBackend: picker.CatalogBackend,
							Inbound: &control.ActionInboundMeta{CardCallback: true, CardDaemonLifecycleID: "test-daemon"}})
						if len(events) == 0 || events[0].TargetPickerView != nil {
							t.Fatalf("%s bypassed stale/expired owner admission", kind)
						}
					}
				}
				assertRejected("old-picker-id")
				state.TargetPickerHostResolutionForbidden = false
				svc.ApplySurfaceAction(control.Action{Kind: control.ActionTargetPickerConfirm, SurfaceSessionID: surface.SurfaceSessionID, ActorUserID: "user-1", PickerID: picker.PickerID, WorkspaceKey: selectedWorkspace, TargetPickerValue: targetPickerThreadValue("thread")})
				if state.TargetPickerHostResolutionReads == 0 {
					t.Fatal("confirmation must retain live path validation")
				}
				state.TargetPickerHostResolutionForbidden = true
				assertRejected(picker.PickerID) // replay after confirmation cleared runtime
				state.TargetPickerHostResolutionForbidden = false
				picker = singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{Kind: source, SurfaceSessionID: surface.SurfaceSessionID, ChatID: surface.ChatID, ActorUserID: "user-1"}))
				now = now.Add(11 * time.Minute)
				state.TargetPickerHostResolutionForbidden = true
				assertRejected(picker.PickerID)
				svc.clearTargetPickerRuntime(surface)
				assertRejected(picker.PickerID) // retry after expired runtime was pruned
			})
		}
	}
}
