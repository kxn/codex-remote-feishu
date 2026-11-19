package daemon

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type draftNavigationCatalog struct{ reads int }

func (c *draftNavigationCatalog) RecentThreads(int) ([]state.ThreadRecord, error) {
	c.reads++
	return nil, nil
}
func (c *draftNavigationCatalog) RecentWorkspaces(int) (map[string]time.Time, error) {
	c.reads++
	return nil, nil
}
func (c *draftNavigationCatalog) ThreadByID(string) (*state.ThreadRecord, error) {
	c.reads++
	return nil, nil
}
func (c *draftNavigationCatalog) ExplicitThreadNames([]string) (map[string]string, error) {
	c.reads++
	return nil, nil
}

func TestTargetPickerDraftNavigationDoesNotWaitForWorkspacePersistence(t *testing.T) {
	for _, kind := range []control.ActionKind{control.ActionTargetPickerSelectWorkspace, control.ActionTargetPickerSelectSession, control.ActionTargetPickerPage} {
		for _, oldLifecycle := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/old_lifecycle=%t", kind, oldLifecycle), func(t *testing.T) {
				gateway := &recordingGateway{}
				app := newTargetPickerPathReturnTestApp(t, gateway)
				catalog := &draftNavigationCatalog{}
				app.service.SetPersistedThreadCatalog(catalog)
				for i := 1; i <= 20; i++ {
					root := t.TempDir()
					id := fmt.Sprintf("inst-%d", i)
					thread := fmt.Sprintf("thread-%d", i)
					app.service.UpsertInstance(&state.InstanceRecord{InstanceID: id, WorkspaceRoot: root, WorkspaceKey: root, Source: "headless", Managed: true, Online: true, Threads: map[string]*state.ThreadRecord{thread: {ThreadID: thread, CWD: root, Loaded: true}}})
				}
				surface := app.service.Surface("surface-1")
				surface.AttachedInstanceID, surface.SelectedThreadID, surface.RouteMode = "inst-1", "thread-1", state.RouteModePinned
				beforeInstance, beforeThread := surface.AttachedInstanceID, surface.SelectedThreadID
				opened := handleGatewayActionForTest(context.Background(), app, control.Action{Kind: control.ActionListInstances, SurfaceSessionID: "surface-1", GatewayID: "app-1", ChatID: "chat-1", ActorUserID: "user-1", MessageID: "picker-card", Inbound: &control.ActionInboundMeta{CardDaemonLifecycleID: app.daemonLifecycleID}})
				if opened == nil || opened.ReplaceCurrentCard == nil {
					t.Fatal("expected a synchronous picker card")
				}
				pickerID := app.service.SurfaceUIRuntime("surface-1").ActiveTargetPickerID
				catalog.reads = 0
				provenance, ok := control.ResolveFeishuTextCommand(control.CatalogContext{Backend: agentproto.BackendCodex, ProductMode: string(state.ProductModeNormal)}, "/list")
				if !ok {
					t.Fatal("missing list command provenance")
				}
				writerEntered := make(chan struct{}, 1)
				releaseWriter := make(chan struct{})
				originalWrite := workspaceSurfaceContextWriteFile
				workspaceSurfaceContextWriteFile = func(string, workspaceSurfaceContextPayload) error {
					writerEntered <- struct{}{}
					<-releaseWriter
					return nil
				}
				done := make(chan *feishu.ActionResult, 1)
				defer func() { close(releaseWriter); <-done; workspaceSurfaceContextWriteFile = originalWrite }()
				action := control.Action{Kind: kind, SurfaceSessionID: "surface-1", GatewayID: "app-1", ChatID: "chat-1", ActorUserID: "user-1", MessageID: "picker-card", PickerID: pickerID, WorkspaceKey: app.service.Instance("inst-2").WorkspaceRoot, TargetPickerValue: "thread:thread-1", FieldName: frontstagecontract.CardTargetPickerWorkspaceFieldName, Cursor: 1, Inbound: &control.ActionInboundMeta{CardDaemonLifecycleID: app.daemonLifecycleID}}
				action.CatalogFamilyID, action.CatalogVariantID, action.CatalogBackend = provenance.FamilyID, provenance.VariantID, provenance.Backend
				if oldLifecycle {
					action.Inbound.CardDaemonLifecycleID = "previous-daemon"
				}
				go func() { done <- handleGatewayActionForTest(context.Background(), app, action) }()
				select {
				case result := <-done:
					done <- result
					if oldLifecycle {
						if result != nil || len(gateway.operations) == 0 {
							t.Fatal("old callback must only emit its rejection notice")
						}
					} else if result == nil || result.ReplaceCurrentCard == nil {
						t.Fatal("draft navigation must return a synchronous replacement")
					}
					if surface.AttachedInstanceID != beforeInstance || surface.SelectedThreadID != beforeThread {
						t.Fatal("draft selection changed the live route")
					}
					if catalog.reads != 0 {
						t.Fatalf("daemon navigation reentered catalog %d times", catalog.reads)
					}
				case <-writerEntered:
					t.Fatal("draft-only callback entered unrelated durable workspace I/O; a slow disk would block its ACK")
				case <-time.After(time.Second):
					t.Fatal("in-memory navigation exceeded callback test deadline")
				}
			})
		}
	}
}
