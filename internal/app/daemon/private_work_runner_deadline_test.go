package daemon

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/ingressmapper"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateRunnerDetectsExactExitedLaunchWithoutHello(t *testing.T) {
	app, _ := newApplicationRuntimeTestApp(t)
	r := app.applications
	ctx := context.Background()
	now := time.Now().UTC()
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "source", MessageID: "message", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"hi"}`, ReceivedAt: now}
	event, e := ingressmapper.Message(raw, 1)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = r.router.Admit(ctx, event, applicationOwnerResolver{router: r.router}); e != nil {
		t.Fatal(e)
	}
	binding, e := r.router.EnsureObservedPrivateBinding(ctx, event)
	if e != nil {
		t.Fatal(e)
	}
	o := privatechat.Owner{GatewayID: "gateway", SurfaceID: gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user"), ChatID: "private-chat", UserID: "user", TenantID: "tenant", AppID: "app"}
	native := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: binding.Owner.ID, Epoch: binding.Owner.Epoch, CarrierIncarnation: "test-inc", InstanceID: "test-instance", HostID: r.native.hostID, InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "test-history"}
	pending := state.HeadlessLaunchRecord{InstanceID: native.InstanceID, Backend: native.Backend, WorkspaceKey: t.TempDir(), ThreadCWD: t.TempDir(), RequestedAt: now, ExpiresAt: now.Add(time.Minute), Purpose: state.HeadlessLaunchPurposeFreshWorkspace}
	seed := privatechat.New(nil, orchestrator.Config{})
	if e = seed.RegisterOwner(o); e != nil {
		t.Fatal(e)
	}
	seed.Service().Surface(o.SurfaceID).PendingHeadless = &pending
	core, e := seed.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if e != nil {
		t.Fatal(e)
	}

	if e = r.journal.CommitCheckpoint(ctx, privatechat.Checkpoint{Owner: o, Generation: binding.Owner.Epoch, Revision: 1, Core: core}, 0, nil); e != nil {
		t.Fatal(e)
	}
	rec := privateCarrierRecord{Snapshot: agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: native, CWD: pending.ThreadCWD, JournalPath: filepath.Join(app.headlessRuntime.Paths.StateDir, "journal.db")}, Surface: o, SurfaceGeneration: binding.Owner.Epoch}
	if _, e = r.native.store.prepare(ctx, rec); e != nil {
		t.Fatal(e)
	}
	if e = r.native.store.transition(ctx, native.InstanceID, "prepared", "sending", hostinventory.ProcessIdentity{}); e != nil {
		t.Fatal(e)
	}
	child := exec.Command("sh", "-c", "sleep 0.2")
	if e = child.Start(); e != nil {
		t.Fatal(e)
	}
	identity, e := hostinventory.CaptureProcess(child.Process.Pid)
	if e != nil {
		_ = child.Wait()
		t.Fatal(e)
	}
	if e = r.native.store.transition(ctx, native.InstanceID, "sending", "running", identity); e != nil {
		t.Fatal(e)
	}
	if e = child.Wait(); e != nil {
		t.Fatal(e)
	}
	rec, e = r.native.store.lookup(ctx, native.InstanceID)
	if e != nil {
		t.Fatal(e)
	}
	r.runner.observeExitedPendingCarrier(ctx, rec)
	cp, e := r.journal.LoadCheckpoint(ctx, o, binding.Owner.Epoch)
	if e != nil {
		t.Fatal(e)
	}
	for _, h := range cp.Core.Holds {
		if h.Kind == "headless_launch" {
			t.Fatal("dead process left startup held")
		}
	}
	effects, e := r.journal.PendingHostEffects(ctx, 128)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, v := range effects {
		ev, _, e := privatechat.DecodeStoredEffect(v.Effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if ev.DaemonCommand != nil && ev.DaemonCommand.Kind == control.DaemonCommandKillHeadless && ev.DaemonCommand.InstanceID == native.InstanceID {
			found = true
		}
	}
	if !found {
		t.Fatal("exact carrier stop was not persisted")
	}
	rec, e = r.native.store.lookup(ctx, native.InstanceID)
	if e != nil || rec.State != "running" {
		t.Fatal("root PID exit was incorrectly promoted to carrier retirement")
	}
}
