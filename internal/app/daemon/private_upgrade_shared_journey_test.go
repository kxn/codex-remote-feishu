package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/codexupgrade"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workupgrade"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type sharedUpgradeJourneyAuthority struct{}
type sharedUpgradeJourneyRelease struct{}

func (sharedUpgradeJourneyRelease) WithMaintenanceRelease(_ context.Context, _ executionowner.Maintenance, _ string, commit func() error) error {
	return commit()
}

func (sharedUpgradeJourneyAuthority) WithPrivateUpgradeRequest(_ context.Context, _ workupgrade.PrivateRequest, commit func() error) error {
	return commit()
}

func TestSharedCodexUpgradeRestartsAuthenticatedPrivateCarrier(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	app, gateway, _ := newApplicationRuntimeTestAppWithLock(t)
	r := app.applications
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root, cwd := app.headlessRuntime.Paths.StateDir, t.TempDir()
	binary := filepath.Join(t.TempDir(), "codex")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3\\n'; exit; fi\nPRIVATE_OPENCODE_FIXTURE=codex exec %q -test.run '^TestPrivateOpenCodeRelayFixtureProcess$'\n", exe)
	if err = os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(root, "codex-runtime"), 0700); err != nil {
		t.Fatal(err)
	}
	cache, _ := json.Marshal(map[string]any{"latest": "0.159.3", "checkedAt": time.Now().UTC()})
	if err = os.WriteFile(filepath.Join(root, "codex-runtime", "check.json"), cache, 0600); err != nil {
		t.Fatal(err)
	}
	app.headlessRuntime.BaseEnv = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH"), "FIXTURE_TRACE=" + filepath.Join(t.TempDir(), "trace")}
	app.relay.SetServerIdentity(agentproto.ServerIdentity{BinaryIdentity: agentproto.BinaryIdentity{Product: relayruntime.ProductName, Version: "fixture", BuildFingerprint: "fixture"}})
	server := httptest.NewServer(app.relay)
	defer server.Close()
	relayURL := "ws" + strings.TrimPrefix(server.URL, "http")
	var children []*exec.Cmd
	r.native.startHeadless = func(v relayruntime.HeadlessLaunchOptions) (int, error) {
		child := exec.Command(exe, "-test.run=^TestPrivateOpenCodeRelayFixtureProcess$")
		child.Env = append(v.Env, "PRIVATE_OPENCODE_FIXTURE=wrapper", "FIXTURE_RELAY_URL="+relayURL, "FIXTURE_STATE_DIR="+root, "FIXTURE_CODEX_BIN="+binary)
		child.Dir = v.WorkDir
		child.Stdout = io.Discard
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			return 0, err
		}
		children = append(children, child)
		return child.Process.Pid, nil
	}
	defer func() {
		for _, child := range children {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	}()
	errorsCh := make(chan error, 8)
	if err = r.startWorkers(ctx, errorsCh); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stop, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = r.stopWorkers(stop)
		_ = r.drainNativeDeliveries(stop)
	}()
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", ReceivedAt: time.Now()}
	send := func(id, text string) {
		t.Helper()
		v := raw
		v.EventID = id
		v.MessageID = id
		v.RawContent = fmt.Sprintf(`{"text":%q}`, text)
		if e := r.hook.Receive(ctx, v); e != nil {
			t.Fatal(e)
		}
	}
	send("mode", "/mode codex")
	surface := gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user")
	o := privatechat.Owner{GatewayID: "gateway", SurfaceID: surface, ChatID: "private-chat", UserID: "user", TenantID: "tenant", AppID: "app"}
	var ownerID string
	var epoch int64
	for ownerID == "" {
		binding, e := r.ingress.privatePublicationBindingFor(ctx, o, 1)
		if e == nil {
			ownerID, epoch = binding.Owner.ID, binding.Owner.Epoch
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(e)
		case <-time.After(10 * time.Millisecond):
		}
	}
	old := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: ownerID, Epoch: epoch, CarrierIncarnation: "upgrade-old", InstanceID: "upgrade-old-instance", HostID: r.native.hostID, InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex}
	rec := privateCarrierRecord{Snapshot: agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: old, CWD: cwd, JournalPath: filepath.Join(root, "upgrade-old.db")}, Surface: o, SurfaceGeneration: epoch}
	if _, err = app.launchPrivateNative(ctx, r.native, privateNativeLaunchRequest{Record: rec}); err != nil {
		t.Fatal(err)
	}
	for {
		r.native.mu.Lock()
		connected := r.native.connections[old.InstanceID] != 0
		r.native.mu.Unlock()
		if connected {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("old hello timeout")
		case <-time.After(10 * time.Millisecond):
		}
	}
	send("list", "/list")
	var callback map[string]any
	var cardID string
	for callback == nil {
		for _, op := range gateway.snapshotOperations() {
			if op.Kind != feishu.OperationSendCard || op.MessageID == "" {
				continue
			}
			encoded, e := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
			if e != nil {
				continue
			}
			var document any
			if json.Unmarshal(encoded, &document) != nil {
				continue
			}
			var visit func(any)
			visit = func(v any) {
				switch x := v.(type) {
				case map[string]any:
					if x["private_view"] != nil && x["kind"] == "target_picker_confirm" {
						callback = x
						cardID = op.MessageID
					}
					for _, child := range x {
						visit(child)
					}
				case []any:
					for _, child := range x {
						visit(child)
					}
				}
			}
			visit(document)
		}
		if callback != nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("attach card timeout")
		case <-time.After(10 * time.Millisecond):
		}
	}
	for {
		if _, err := r.router.PublishedMessage(ctx, "tenant", "app", "private-chat", cardID); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("attach card publication was not recorded")
		case <-time.After(10 * time.Millisecond):
		}
	}
	click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "attach", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: cardID, Kind: "card", Values: callback, ReceivedAt: time.Now()}
	if _, err = r.hook.ReceiveInteractionResponse(ctx, click); err != nil {
		t.Fatal(err)
	}
	send("seed", "establish session")
	for {
		cp, e := r.journal.LoadCheckpoint(ctx, o, epoch)
		if e == nil && cp.Core.SelectedThreadID == "session" && cp.Core.ActiveQueueItemID == "" {
			break
		}
		select {
		case e := <-errorsCh:
			t.Fatal(e)
		case <-ctx.Done():
			t.Fatal("session timeout")
		case <-time.After(10 * time.Millisecond):
		}
	}
	// Exercise the same maintenance lifecycle with an actual wrapper. The
	// installer is isolated, but both stop and hello proofs are real.
	m, err := r.registry.BeginMaintenance(ctx, "codex", "private-wrapper-upgrade", "0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	shared := &sharedCodexMaintenance{app: app, registry: r.registry, private: r.native, store: app.teamRuntime.store, teamOwners: r.teamOwners}
	if err = shared.pausePrivateUpgradeCarriers(ctx, m); err != nil {
		t.Fatal(err)
	}
	for {
		err = r.native.drainPrivateUpgradeCarriers(ctx, m)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("drain: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}
	if len(children) != 1 {
		t.Fatalf("unexpected old carrier processes: %d", len(children))
	}
	if err = r.registry.MarkMaintenanceQuiescent(ctx, m, "sources", maintenanceFixtureProof{}); err != nil {
		t.Fatal(err)
	}
	if err = r.registry.MarkMaintenanceInstalling(ctx, m.Backend, m.ID, m.Epoch); err != nil {
		t.Fatal(err)
	}
	if err = r.registry.MarkMaintenanceInstalled(ctx, m, "installed", maintenanceFixtureProof{}); err != nil {
		t.Fatal(err)
	}
	if err = shared.preparePrivateRecoveries(ctx, m); err != nil {
		t.Fatal(err)
	}
	var replacement executionowner.Registration
	for {
		list, e := r.registry.MaintenanceRecoveries(ctx, m, "", 64)
		if e != nil {
			t.Fatal(e)
		}
		if len(list) == 1 && list[0].Replacement != nil {
			replacement = *list[0].Replacement
			err = r.registry.MarkMaintenanceRecovered(ctx, m, list[0].Old, &replacement, "private-ready:"+m.ID+":"+replacement.Carrier.Incarnation, r.native)
			if err == nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = r.stopWorkers(stopCtx)
			stopCancel()
			pending, e := r.journal.PendingHostEffects(context.Background(), 64)
			for _, work := range pending {
				event, _, _ := privatechat.DecodeStoredEffect(work.Effect.Effect.Body)
				t.Logf("pending state=%s command=%+v", work.Effect.State, event.DaemonCommand)
				if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandStartHeadless {
					t.Logf("headless retry result: %v", r.runner.runEffect(context.Background(), work))
				}
			}
			t.Fatalf("replacement hello: %v; pending=%d query=%v children=%d", err, len(pending), e, len(children))
		case <-time.After(20 * time.Millisecond):
		}
	}
	if err = r.registry.ReleaseMaintenance(ctx, m, "release", sharedUpgradeJourneyRelease{}); err != nil {
		t.Fatal(err)
	}
	plan, ok, err := r.journal.UpgradePauseForCarrier(ctx, m.ID, old.CarrierIncarnation)
	if err != nil || !ok {
		t.Fatalf("paused plan: %v %v", ok, err)
	}
	if _, err = r.processor.ResumeUpgradeRecovery(ctx, plan, replacement.Carrier.InstanceID, r.native); err != nil {
		status, _ := r.processor.ReadRuntimeStatus(ctx)
		for _, s := range status.Surfaces {
			t.Logf("surface=%+v", s)
		}
		for _, i := range status.Instances {
			t.Logf("instance=%+v", i)
		}
		t.Fatal(err)
	}
	cp, err := r.journal.LoadCheckpoint(ctx, o, epoch)
	if err != nil || cp.Core.SelectedThreadID != "session" {
		t.Fatalf("selected session after restart: %s %v", cp.Core.SelectedThreadID, err)
	}
}

// The real tracked installer launches an isolated fake npm executable. The
// shared adapter and both durable stores are production implementations.
func TestSharedCodexUpgradeRunsTrackedInstallUnderBackendFence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	app, _ := newApplicationRuntimeTestApp(t)
	r := app.applications
	root := t.TempDir()
	marker, log, npm := filepath.Join(root, "version"), filepath.Join(root, "installs"), filepath.Join(root, "npm")
	triple := "x86_64-unknown-linux-musl"
	if runtime.GOARCH == "arm64" {
		triple = "aarch64-unknown-linux-musl"
	}
	if runtime.GOOS == "darwin" {
		triple = strings.ReplaceAll(triple, "unknown-linux-musl", "apple-darwin")
	}
	script := fmt.Sprintf("#!/bin/sh\nset -eu\nprintf 'install\\n' >> %q\nfor item in \"$@\"; do target=${item##*@}; done\nif [ \"$2\" = '--prefix' ]; then\n binary=\"$3/node_modules/@openai/codex/vendor/%s/bin/codex\"\n mkdir -p \"$(dirname \"$binary\")\"\n printf '#!/bin/sh\\nprintf \\\"codex-cli %%s\\\\n\\\"\\n' \"$target\" > \"$binary\"\n chmod 755 \"$binary\"\nelse\n printf '%%s' \"$target\" > %q\nfi\n", log, triple, marker)
	if err := os.WriteFile(npm, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	inspect := func(context.Context, codexupgrade.InspectOptions) codexupgrade.Installation {
		version := "0.1.0"
		if raw, e := os.ReadFile(marker); e == nil {
			version = string(raw)
		}
		return codexupgrade.Installation{ConfiguredBinary: "codex", EffectiveBinary: "codex", ResolvedBinary: "codex", SourceKind: codexupgrade.SourceStandaloneNPM, PackageVersion: version, NPMCommand: npm, PackageRoot: filepath.Join(root, "global")}
	}
	c, err := workupgrade.New(app.teamRuntime.store, workupgrade.Config{HostID: app.teamRuntime.codexUpgradeHostID, StateDir: root, ConfiguredBinary: "codex", Inspect: inspect, Drain: func(context.Context, work.WorkerSession) error { return fmt.Errorf("unexpected team carrier") }, Restart: func(context.Context, string, string) error { return fmt.Errorf("unexpected team carrier") }})
	if err != nil {
		t.Fatal(err)
	}
	c.SetMaintenance(&sharedCodexMaintenance{app: app, registry: r.registry, private: r.native, store: app.teamRuntime.store, team: c, teamOwners: r.teamOwners})
	source := workstore.NativeUpgradePrivateSource{Tenant: "tenant", AppID: "app", Gateway: "gateway", Surface: "surface", Chat: "chat", User: "user", Generation: 1, EffectID: "intent"}
	req := workupgrade.PrivateRequest{ID: "journey", TargetVersion: "0.2.0", InstallationFingerprint: workupgrade.InstallationFingerprint(inspect(t.Context(), codexupgrade.InspectOptions{})), Source: source}
	if _, err = c.RequestPrivate(t.Context(), req, sharedUpgradeJourneyAuthority{}); err != nil {
		t.Fatal(err)
	}
	if err = c.Run(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	status, err := c.Status(t.Context(), req.ID)
	if err != nil || status.Stage != "completed" {
		t.Fatalf("shared result: %s %v", status.Stage, err)
	}
	gate, err := r.registry.MaintenanceStatus(t.Context(), "codex")
	if err != nil || gate.Phase != "released" {
		t.Fatalf("backend gate: %+v %v", gate, err)
	}
	raw, err := os.ReadFile(log)
	if err != nil || strings.Count(string(raw), "install\n") != 2 {
		t.Fatalf("tracked install count: %q %v", raw, err)
	}
}
