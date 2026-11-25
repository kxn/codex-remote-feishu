package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
	"io"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPrivateRequestSDKApprovalJourney(t *testing.T) {
	t.Run("confirmed", func(t *testing.T) { testPrivateRequestSDKApprovalJourney(t, false) })
	t.Run("unknown", func(t *testing.T) { testPrivateRequestSDKApprovalJourney(t, true) })
}

type uncertainRequestGateway struct {
	base      *messageIDAssigningGateway
	lifecycle string
}

func (g *uncertainRequestGateway) Start(ctx context.Context, handler feishu.ActionHandler) error {
	return g.base.Start(ctx, handler)
}
func (g *uncertainRequestGateway) Apply(ctx context.Context, ops []feishu.Operation) error {
	if err := g.base.Apply(ctx, ops); err != nil {
		return err
	}
	for _, op := range ops {
		body, _ := feishu.FreezePrivateOperation(op, g.lifecycle)
		if op.Kind == feishu.OperationSendCard && strings.Contains(string(body), "request-1") {
			return errors.New("fixture: delivery outcome unknown")
		}
	}
	return nil
}

func testPrivateRequestSDKApprovalJourney(t *testing.T, unknown bool) {
	backend := agentproto.BackendCodex
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable")
	}
	app, gateway, daemonLock := newApplicationRuntimeTestAppWithLock(t)
	if unknown {
		app.gateway = &uncertainRequestGateway{base: gateway, lifecycle: app.daemonLifecycleID}
	}
	r := app.applications
	_ = daemonLock
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	cwd := t.TempDir()
	root := app.headlessRuntime.Paths.StateDir
	binary := filepath.Join(t.TempDir(), "native-agent")
	nativeMode := "native"
	versionOutput := "2.1.4"
	if backend == agentproto.BackendCodex {
		nativeMode = "codex"
		versionOutput = "codex-cli 0.159.3-dev.1"
	}
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf '%s\\n'; exit; fi\nPRIVATE_OPENCODE_FIXTURE=%s exec %q -test.run '^TestPrivateOpenCodeRelayFixtureProcess$'\n", versionOutput, nativeMode, exe)
	if e = os.WriteFile(binary, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	trace := filepath.Join(t.TempDir(), "native-frames.jsonl")
	app.headlessRuntime.BaseEnv = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH"), "OPENCODE_BIN=" + binary, "FIXTURE_TRACE=" + trace, "PRIVATE_REQUEST_FIXTURE=1"}
	app.relay.SetServerIdentity(agentproto.ServerIdentity{BinaryIdentity: agentproto.BinaryIdentity{Product: relayruntime.ProductName, Version: "fixture", BuildFingerprint: "fixture"}})
	server := httptest.NewServer(app.relay)
	defer server.Close()
	relayURL := "ws" + strings.TrimPrefix(server.URL, "http")
	var child *exec.Cmd
	r.native.startHeadless = func(v relayruntime.HeadlessLaunchOptions) (int, error) {
		child = exec.Command(exe, "-test.run=^TestPrivateOpenCodeRelayFixtureProcess$")
		child.Env = append(v.Env, "PRIVATE_OPENCODE_FIXTURE=wrapper", "FIXTURE_RELAY_URL="+relayURL, "FIXTURE_STATE_DIR="+root, "FIXTURE_CODEX_BIN="+binary)
		child.Dir = v.WorkDir
		child.Stdout = io.Discard
		child.Stderr = os.Stderr
		if e := child.Start(); e != nil {
			return 0, e
		}
		return child.Process.Pid, nil
	}
	defer func() {
		if child != nil {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	}()
	errorsCh := make(chan error, 8)
	if e = r.startWorkers(ctx, errorsCh); e != nil {
		t.Fatal(e)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = r.stopWorkers(stopCtx)
		if child != nil {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
		_ = r.drainNativeDeliveries(stopCtx)
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
	send("mode", "/mode "+string(backend))
	surface := gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user")
	o := privatechat.Owner{GatewayID: "gateway", SurfaceID: surface, ChatID: "private-chat", UserID: "user", TenantID: "tenant", AppID: "app"}
	var bindingOwnerID string
	var epoch int64
	for {
		b, e := r.ingress.privatePublicationBindingFor(ctx, o, 1)
		if e == nil {
			bindingOwnerID = b.Owner.ID
			epoch = b.Owner.Epoch
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(e)
		case <-time.After(10 * time.Millisecond):
		}
	}
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: bindingOwnerID, Epoch: epoch, CarrierIncarnation: "journey-inc", InstanceID: "journey-instance", HostID: r.native.hostID, InstanceGeneration: 1, ProcessGeneration: 1, Backend: backend}
	rec := privateCarrierRecord{Snapshot: agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: filepath.Join(root, "journey-native.db")}, Surface: o, SurfaceGeneration: epoch}
	request := privateNativeLaunchRequest{Record: rec}
	if backend == agentproto.BackendCodex {
		request.CodexProfileID = "cp_native"
	}
	if _, e = app.launchPrivateNative(ctx, r.native, request); e != nil {
		t.Fatal(e)
	}
	for {
		r.native.mu.Lock()
		n := r.native.connections[owner.InstanceID]
		r.native.mu.Unlock()
		if n != 0 {
			break
		}
		select {
		case err := <-errorsCh:
			t.Fatal(err)
		case <-ctx.Done():
			t.Fatal("native hello timed out")
		case <-time.After(10 * time.Millisecond):
		}
	}
	send("list", "/list")
	deadline := time.Now().Add(5 * time.Second)
	var callback map[string]any
	var message string
	for time.Now().Before(deadline) && callback == nil {
		for _, op := range gateway.snapshotOperations() {
			if op.Kind != feishu.OperationSendCard || op.MessageID == "" {
				continue
			}
			frozen, e := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
			if e != nil {
				continue
			}
			var document any
			if json.Unmarshal(frozen, &document) != nil {
				continue
			}
			var visit func(any)
			visit = func(v any) {
				switch value := v.(type) {
				case map[string]any:
					if value["private_view"] != nil && value["kind"] == "target_picker_confirm" {
						callback = value
						message = op.MessageID
					}
					for _, item := range value {
						visit(item)
					}
				case []any:
					for _, item := range value {
						visit(item)
					}
				}
			}
			visit(document)
		}
		if callback == nil {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if callback == nil {
		for _, op := range gateway.snapshotOperations() {
			t.Logf("operation %s title=%q body=%q elements=%v", op.Kind, op.CardTitle, op.CardBody, op.CardElements)
		}
		t.Fatal("SDK /list did not render stamped instance selection")
	}
	for {
		if _, err := r.router.PublishedMessage(ctx, "tenant", "app", "private-chat", message); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("instance selection receipt was not registered")
		case <-time.After(10 * time.Millisecond):
		}
	}
	click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "attach-click", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: message, Kind: "card", Values: callback, ReceivedAt: time.Now()}
	if _, e = r.hook.ReceiveInteractionResponse(ctx, click); e != nil {
		t.Fatal(e)
	}
	// The instance selector can launch an asynchronous attachment; do not
	// race the test's first prompt against the still-pending selection.
	for {
		cp, err := r.journal.LoadCheckpoint(ctx, o, epoch)
		if err == nil && cp.Core.AttachedInstanceID == owner.InstanceID && (cp.Core.RouteMode == "new_thread_ready" || cp.Core.RouteMode == "pinned") {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("instance selection did not complete")
		case <-time.After(10 * time.Millisecond):
		}
	}
	send("ask", "request approval")
	var requestCard string
	var responseValue map[string]any
	for responseValue == nil {
		for _, op := range gateway.snapshotOperations() {
			if op.Kind != feishu.OperationSendCard || op.MessageID == "" {
				continue
			}
			frozen, err := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
			if err != nil {
				continue
			}
			var doc any
			if json.Unmarshal(frozen, &doc) != nil {
				continue
			}
			var visit func(any)
			visit = func(v any) {
				switch x := v.(type) {
				case map[string]any:
					if x["kind"] == "request_respond" && x["private_view"] != nil && responseValue == nil {
						responseValue = x
						requestCard = op.MessageID
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
			visit(doc)
		}
		if responseValue == nil {
			select {
			case <-ctx.Done():
				cp, _ := r.journal.LoadCheckpoint(context.Background(), o, epoch)
				inputs, _ := r.journal.PendingIngressWork(context.Background(), 32)
				effects, _ := r.journal.PendingHostEffects(context.Background(), 32)
				traceBody, _ := os.ReadFile(trace)
				var kinds []string
				for _, h := range cp.Core.Holds {
					kinds = append(kinds, h.Kind)
				}
				t.Logf("request wait route=%s selected=%q active=%q holds=%v ingress=%d effects=%d operations=%d nativeTurnStarts=%d", cp.Core.RouteMode, cp.Core.SelectedThreadID, cp.Core.ActiveQueueItemID, kinds, len(inputs), len(effects), len(gateway.snapshotOperations()), strings.Count(string(traceBody), `"method":"turn/start"`))
				for _, item := range effects {
					ev, _ := privatechat.DecodeEffectEvent(item.Effect.Effect.Body)
					t.Logf("pending effect kind=%s state=%s command=%+v notice=%+v", ev.Kind, item.Effect.State, ev.Command, ev.Notice)
				}
				for _, op := range gateway.snapshotOperations() {
					t.Logf("operation kind=%s title=%q body=%q", op.Kind, op.CardTitle, op.CardBody)
				}
				t.Fatal("request card with actionable approval was not published")
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	// Ordinary traffic and menu navigation must leave the already published
	// request actionable rather than creating another authority card.
	send("pending-text", "queued while awaiting approval")
	send("pending-menu", "/menu")
	for !unknown {
		if _, err := r.router.PublishedMessage(ctx, "tenant", "app", "private-chat", requestCard); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("request card receipt not registered")
		case <-time.After(10 * time.Millisecond):
		}
	}

	oldPID := child.Process.Pid
	beforeCards := len(gateway.snapshotOperations())
	app, r = restartRequestSDKDaemon(t, ctx, app, gateway, daemonLock, server, o, epoch, owner.InstanceID)
	if child.Process.Pid != oldPID {
		t.Fatal("native process changed during daemon restart")
	}
	if unknown {
		db, err := sql.Open("sqlite", filepath.Join(app.headlessRuntime.Paths.StateDir, "private-chat.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var unknownCount int
		if err := db.QueryRow(`SELECT count(*) FROM private_effect WHERE state='unknown' AND json_extract(body,'$.event.RequestView.RequestID')='request-1'`).Scan(&unknownCount); err != nil || unknownCount != 1 {
			t.Fatalf("request delivery is not unknown: count=%d err=%v", unknownCount, err)
		}
		deadline := time.After(2 * time.Second)
		for {
			select {
			case <-deadline:
				for _, op := range gateway.snapshotOperations()[beforeCards:] {
					body, _ := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
					if (op.Kind == feishu.OperationSendCard || op.Kind == feishu.OperationUpdateCard) && strings.Contains(string(body), "request-1") {
						t.Fatal("unknown request card sent a second time")
					}
				}
				return
			case <-ctx.Done():
				t.Fatal("unknown restart timed out")
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	var newValue map[string]any
	var newCard string
	for {
		for _, op := range gateway.snapshotOperations()[beforeCards:] {
			if (op.Kind != feishu.OperationSendCard && op.Kind != feishu.OperationUpdateCard) || op.MessageID == "" {
				continue
			}
			raw, err := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
			if err != nil {
				continue
			}
			var doc any
			if json.Unmarshal(raw, &doc) != nil {
				continue
			}
			var visit func(any)
			visit = func(v any) {
				switch x := v.(type) {
				case map[string]any:
					if x["kind"] == "request_respond" && x["private_view"] != nil && newValue == nil {
						newValue = x
						newCard = op.MessageID
					}
					for _, c := range x {
						visit(c)
					}
				case []any:
					for _, c := range x {
						visit(c)
					}
				}
			}
			visit(doc)
		}
		if newValue != nil {
			break
		}
		select {
		case <-ctx.Done():
			cp, _ := r.journal.LoadCheckpoint(context.Background(), o, epoch)
			dispatches, _ := r.journal.ListDispatches(context.Background(), o, epoch, "", 64)
			effects, _ := r.journal.PendingHostEffects(context.Background(), 64)
			var kinds []string
			for _, h := range cp.Core.Holds {
				kinds = append(kinds, h.Kind)
			}
			var ds []string
			for _, d := range dispatches {
				ds = append(ds, d.Dispatch.Lane+":"+d.State)
			}
			db, _ := sql.Open("sqlite", filepath.Join(app.headlessRuntime.Paths.StateDir, "private-chat.db"))
			if db != nil {
				defer db.Close()
				rows, err := db.Query(`SELECT sequence,applied,json_extract(payload,'$.kind'),json_extract(payload,'$.runtimeInspection.pendingRequests.complete'),json_extract(payload,'$.runtimeInspection.pendingRequests.eventSequence'),json_extract(payload,'$.commandId'),json_extract(payload,'$.requestId'),json_extract(payload,'$.threadId'),json_extract(payload,'$.turnId'),json_type(payload,'$.nativeBinding'),json_array_length(payload,'$.runtimeInspection.pendingRequests.requests') FROM native_events WHERE incarnation=? ORDER BY sequence`, owner.CarrierIncarnation)
				if err == nil {
					defer rows.Close()
					for rows.Next() {
						var seq, applied int
						var kind, complete, watermark, command, request, thread, turn, binding, requests any
						_ = rows.Scan(&seq, &applied, &kind, &complete, &watermark, &command, &request, &thread, &turn, &binding, &requests)
						t.Logf("native seq=%d applied=%d kind=%v complete=%v watermark=%v command=%v request=%v thread=%v turn=%v binding=%v requests=%v", seq, applied, kind, complete, watermark, command, request, thread, turn, binding, requests)
					}
				}
			}
			t.Logf("request recovery holds=%v dispatches=%v pendingEffects=%d", kinds, ds, len(effects))
			t.Fatal("same-carrier request was not re-presented after restart")
		case <-time.After(10 * time.Millisecond):
		}
	}
	for {
		if published, err := r.router.PublishedMessage(ctx, "tenant", "app", "private-chat", newCard); err == nil && published.CardRef == newValue["private_view"] {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("replayed card receipt not registered")
		case <-time.After(10 * time.Millisecond):
		}
	}
	requestCard, responseValue = newCard, newValue
	approval := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "approval-click", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: requestCard, Kind: "card", Values: responseValue, ReceivedAt: time.Now()}
	approvalResponse, err := r.hook.ReceiveInteractionResponse(ctx, approval)
	if err != nil {
		t.Fatal(err)
	}
	for {
		found := false
		for _, op := range gateway.snapshotOperations() {
			b, _ := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
			if strings.Contains(string(b), "approval complete") {
				found = true
				break
			}
		}
		if found {
			break
		}
		select {
		case <-ctx.Done():
			cp, _ := r.journal.LoadCheckpoint(context.Background(), o, epoch)
			pending, _ := r.journal.PendingHostEffects(context.Background(), 32)
			for _, item := range pending {
				ev, _ := privatechat.DecodeEffectEvent(item.Effect.Effect.Body)
				kind := ""
				if ev.Command != nil {
					kind = string(ev.Command.Kind)
				}
				t.Logf("pending kind=%s command=%s state=%s", ev.Kind, kind, item.Effect.State)
			}
			var holds []string
			for _, h := range cp.Core.Holds {
				holds = append(holds, h.Kind)
			}
			t.Logf("approval response=%+v route=%s active=%s holds=%v", approvalResponse, cp.Core.RouteMode, cp.Core.ActiveQueueItemID, holds)
			for _, op := range gateway.snapshotOperations() {
				t.Logf("operation kind=%s title=%q body=%q", op.Kind, op.CardTitle, op.CardBody)
			}
			t.Fatal("approval did not yield final response")
		case <-time.After(10 * time.Millisecond):
		}
	}
	send("after-approval", "second turn")
	for {
		found := false
		for _, op := range gateway.snapshotOperations() {
			b, _ := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
			if strings.Contains(string(b), "done") {
				found = true
				break
			}
		}
		if found {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("subsequent turn did not finish")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func restartRequestSDKDaemon(t *testing.T, ctx context.Context, old *App, gateway *messageIDAssigningGateway, lock *relayruntime.FileLock, server *httptest.Server, owner privatechat.Owner, epoch int64, instance string) (*App, *applicationRuntime) {
	t.Helper()
	r := old.applications
	if err := r.platformDeliveries.drain(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.stopWorkers(ctx); err != nil {
		t.Fatal(err)
	}
	addr := server.Listener.Addr().String()
	if err := old.relay.Close(); err != nil {
		t.Fatal(err)
	}
	server.Close()
	if err := r.drainNativeDeliveries(ctx); err != nil {
		t.Fatal(err)
	}
	cp, err := r.journal.LoadCheckpoint(ctx, owner, epoch)
	if err != nil {
		t.Fatal(err)
	}
	if cp.Core.AttachedInstanceID != instance {
		t.Fatal("original request carrier missing")
	}
	found := false
	for _, h := range cp.Core.Holds {
		if h.Kind == "requests" && h.InstanceID == instance && strings.Contains(string(h.Frozen), "request-1") {
			found = true
		}
	}
	if !found {
		t.Fatal("original request hold was not persisted")
	}
	if err = r.close(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := captureTeamLegacyFiles(old.headlessRuntime.Paths.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	app := New("127.0.0.1:0", "127.0.0.1:0", gateway, agentproto.ServerIdentity{})
	if err = app.prepareApplicationCore(); err != nil {
		t.Fatal(err)
	}
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		t.Error("unexpected replacement native process")
		return 0, context.Canceled
	}
	app.SetHeadlessRuntime(old.headlessRuntime)
	app.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: old.headlessRuntime.Paths.ConfigFile, Services: defaultFeishuServices()})
	if err = app.configureWorkRuntime(ctx, teamPrivateCommands{app: app}, app.authorizeTeamControl); err != nil {
		t.Fatal(err)
	}
	if err = app.activateWorkRuntime(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if err = app.configureApplicationIngress(ctx, feishu.NewMultiGatewayController(), lock); err != nil {
		t.Fatal(err)
	}
	r2 := app.applications
	app.relay.SetServerIdentity(agentproto.ServerIdentity{BinaryIdentity: agentproto.BinaryIdentity{Product: relayruntime.ProductName, Version: "fixture", BuildFingerprint: "fixture"}})
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	server2 := httptest.NewUnstartedServer(app.relay)
	server2.Listener = listener
	server2.Start()
	t.Cleanup(func() { server2.Close(); _ = r2.close(); _ = app.teamRuntime.store.Close() })
	errs := make(chan error, 8)
	if err = r2.startWorkers(ctx, errs); err != nil {
		t.Fatal(err)
	}
	for {
		r2.native.mu.Lock()
		connection := r2.native.connections[instance]
		r2.native.mu.Unlock()
		if connection != 0 {
			break
		}
		select {
		case err := <-errs:
			t.Fatal(err)
		case <-ctx.Done():
			t.Fatal("native wrapper did not reconnect")
		case <-time.After(10 * time.Millisecond):
		}
	}
	return app, r2
}
