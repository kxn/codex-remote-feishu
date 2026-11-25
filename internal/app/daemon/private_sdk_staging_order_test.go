package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/ingressmapper"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/app/privatestaging"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type blockedSDKImage struct {
	started, release chan struct{}
	path             string
}
type cleanupSDKGateway struct {
	*messageIDAssigningGateway
	base    string
	enabled atomic.Bool
}

func (g *cleanupSDKGateway) PrivateStagingBase(_, _ string) (string, error) {
	if !g.enabled.Load() {
		return "", fmt.Errorf("cleanup paused")
	}
	return g.base, nil
}

type sdkStageAuthority struct{}

func (sdkStageAuthority) WithNativeLease(_ context.Context, _ agentproto.NativeOwner, _ privatechat.Owner, _ int64, fn func() error) error {
	return fn()
}

func (s *blockedSDKImage) StagePrivateMessage(ctx context.Context, raw platformingress.RawIngressEnvelope, _ string) (control.Action, bool, error) {
	close(s.started)
	select {
	case <-s.release:
	case <-ctx.Done():
		return control.Action{}, false, ctx.Err()
	}
	return control.Action{Kind: control.ActionImageMessage, GatewayID: raw.GatewayRef, SurfaceSessionID: gatewaypkg.SurfaceIDForInbound(raw.GatewayRef, "", "p2p", raw.SenderIDs.OpenID), ChatID: raw.ChatID, ActorUserID: raw.SenderIDs.OpenID, MessageID: raw.MessageID, LocalPath: s.path, MIMEType: "image/png"}, true, nil
}

// The SDK hook, neutral router, owner journal and processor are production;
// the downloaded image and initial already-prepared session are controlled.
func TestPrivateSDKOldImageCannotCrossNewOrDetach(t *testing.T) {
	for _, command := range []string{"/new", "/detach"} {
		t.Run(command, func(t *testing.T) {
			a, gateway := newApplicationRuntimeTestApp(t)
			base := t.TempDir()
			managed, err := privatestaging.Open(base, privatestaging.Owner{Tenant: "tenant", AppID: "app", Gateway: "gateway", EventID: "old-image", MessageID: "old-image"})
			if err != nil {
				t.Fatal(err)
			}
			path, err := managed.Create(strings.NewReader("image"), ".png")
			if err != nil {
				t.Fatal(err)
			}
			if command == "/detach" {
				path = filepath.Join(t.TempDir(), "foreign.png")
				if err = os.WriteFile(path, []byte("foreign"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			provider := &cleanupSDKGateway{messageIDAssigningGateway: gateway, base: base}
			a.gateway = provider
			r := a.applications
			ctx := t.Context()
			raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "old-image", MessageID: "old-image", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "image", RawContent: `{"image_key":"fixture"}`, ReceivedAt: time.Now()}
			e, err := ingressmapper.Message(raw, 1)
			if err != nil {
				t.Fatal(err)
			}
			record, _, err := r.router.Admit(ctx, e, r.ingress.resolver)
			if err != nil || record.Owner == nil {
				t.Fatalf("owner: %v", err)
			}
			o := privatechat.Owner{GatewayID: "gateway", AppID: "app", TenantID: "tenant", ChatID: "private-chat", UserID: "user", SurfaceID: gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user")}
			seed := privatechat.New(nil, orchestrator.Config{})
			if err = seed.RegisterOwner(o); err != nil {
				t.Fatal(err)
			}
			cp, err := seed.Service().ExportPrivateCheckpoint(o.SurfaceID)
			if err != nil {
				t.Fatal(err)
			}
			cp.RouteMode = state.RouteModeNewThreadReady
			cp.PreparedThreadCWD = t.TempDir()
			if err = r.journal.CommitCheckpoint(ctx, privatechat.Checkpoint{Owner: o, Generation: record.Owner.Epoch, Revision: 1, Core: cp}, 0, nil); err != nil {
				t.Fatal(err)
			}
			native := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: record.Owner.ID, Epoch: record.Owner.Epoch, CarrierIncarnation: "fixture-inc", InstanceID: "fixture-instance", HostID: "fixture", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "fixture-history"}
			lease, err := r.journal.OpenNativeLease(ctx, sdkStageAuthority{}, native, o, record.Owner.Epoch)
			if err != nil {
				t.Fatal(err)
			}
			hello := agentproto.Hello{PrivateNativeProtocolVersion: agentproto.PrivateNativeProtocolVersion, CarrierIncarnation: native.CarrierIncarnation, Instance: agentproto.InstanceHello{InstanceID: native.InstanceID, Backend: agentproto.BackendCodex, WorkspaceRoot: cp.PreparedThreadCWD, WorkspaceKey: cp.PreparedThreadCWD, Source: "headless", Managed: true}}
			if err = r.processor.RegisterNativeHello(ctx, lease, hello); err != nil {
				t.Fatal(err)
			}
			inst := r.core.Service().Instance(native.InstanceID)
			inst.ObservedFocusedThreadID = "thread"
			inst.Threads["thread"] = &state.ThreadRecord{ThreadID: "thread", CWD: cp.PreparedThreadCWD}
			_, err = r.core.Action(control.Action{Kind: control.ActionAttachInstance, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID, InstanceID: native.InstanceID})
			if err != nil {
				t.Fatal(err)
			}
			stager := &blockedSDKImage{started: make(chan struct{}), release: make(chan struct{}), path: path}
			r.ingress.stager = stager
			if err = r.hook.Receive(ctx, raw); err != nil {
				t.Fatal(err)
			}
			if err = r.startWorkers(ctx, make(chan error, 8)); err != nil {
				t.Fatal(err)
			}
			select {
			case <-stager.started:
			case <-time.After(5 * time.Second):
				t.Fatal("download not started")
			}
			msg := raw
			msg.EventID = "control"
			msg.MessageID = "control"
			msg.MessageType = "text"
			msg.RawContent = fmt.Sprintf(`{"text":%q}`, command)
			if err = r.hook.Receive(ctx, msg); err != nil {
				close(stager.release)
				t.Fatal(err)
			}
			for deadline := time.Now().Add(5 * time.Second); ; {
				current, e := r.journal.LoadCheckpoint(ctx, o, record.Owner.Epoch)
				if e == nil && current.Revision > 1 {
					break
				}
				if time.Now().After(deadline) {
					close(stager.release)
					t.Fatal("control not applied")
				}
				time.Sleep(10 * time.Millisecond)
			}
			// Detach marks the controlled old carrier stopping. This local cleanup
			// fixture checks the canceled source while detached; the separate cold
			// SDK delayed-image journey verifies selecting a real new carrier before
			// the download returns.

			close(stager.release)
			for deadline := time.Now().Add(5 * time.Second); ; {
				staged, stageErr := r.router.Get(ctx, e.Key)
				pending, pendingErr := r.journal.PendingIngressWork(ctx, 100)
				current, e := r.journal.LoadCheckpoint(ctx, o, record.Owner.Epoch)
				if stageErr == nil && staged.State == "accepted" && pendingErr == nil && len(pending) == 0 && e == nil && current.Revision > 2 {
					t.Logf("control=%s route=%s images=%+v", command, current.Core.RouteMode, current.Core.StagedImages)
					for _, img := range current.Core.StagedImages {
						if img.SourceMessageID == "old-image" && img.State == state.ImageStaged {
							t.Fatalf("%s allowed pre-control image into new draft", command)
						}
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("image did not settle")
				}
				time.Sleep(10 * time.Millisecond)
			}
			work, err := r.journal.PendingStagingCleanup(ctx, 100)
			if err != nil || len(work) != 1 || work[0].IngressSequence == 0 {
				t.Fatalf("canceled cleanup missing: %+v %v", work, err)
			}
			stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err = r.stopWorkers(stop); err != nil {
				t.Fatal(err)
			}
			if err = r.drainNativeDeliveries(stop); err != nil {
				t.Fatal(err)
			}
			if err = r.close(); err != nil {
				t.Fatal(err)
			}
			j, err := privatechat.OpenJournal(filepath.Join(a.headlessRuntime.Paths.StateDir, "private-chat.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer j.Close()
			provider.enabled.Store(true)
			if err = runPrivateStagingCleanup(stop, a, j); err != nil {
				t.Fatal(err)
			}
			if err = runPrivateStagingCleanup(stop, a, j); err != nil {
				t.Fatal(err)
			}
			_, statErr := os.Stat(path)
			if command == "/new" && !os.IsNotExist(statErr) {
				t.Fatalf("managed source not cleaned: %v", statErr)
			}
			if command == "/detach" && statErr != nil {
				t.Fatalf("foreign source was removed: %v", statErr)
			}
		})
	}
}
