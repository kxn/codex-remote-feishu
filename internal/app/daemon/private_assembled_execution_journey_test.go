package daemon

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
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

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

// Exercises the assembled SDK hook with a real private wrapper and controlled
// native ACP subprocess. Platform delivery is the in-memory receipt gateway.
func TestPrivateAssembledSDKExecutionJourney(t *testing.T) {
	for _, backend := range []agentproto.Backend{agentproto.BackendOpenCode, agentproto.BackendCodex} {
		t.Run(string(backend), func(t *testing.T) { testPrivateAssembledSDKExecutionJourney(t, backend) })
	}
}
func testPrivateAssembledSDKExecutionJourney(t *testing.T, backend agentproto.Backend) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable")
	}
	app, gateway, daemonLock := newApplicationRuntimeTestAppWithLock(t)
	r := app.applications
	var sentFiles <-chan string
	if backend == agentproto.BackendCodex {
		sentFiles = setupAssembledSDKFileGateway(t, app, gateway)
	}
	_ = daemonLock
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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
	app.headlessRuntime.BaseEnv = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH"), "OPENCODE_BIN=" + binary, "FIXTURE_TRACE=" + trace}
	imagePath := filepath.Join(t.TempDir(), "image.png")
	imageData, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/a9sAAAAASUVORK5CYII=")
	if e = os.WriteFile(imagePath, imageData, 0600); e != nil {
		t.Fatal(e)
	}
	r.ingress.stager = controlledSDKImageStager{path: imagePath}
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
	if backend == agentproto.BackendCodex {
		exerciseAssembledSDKSendFile(t, ctx, app, gateway, raw, cwd, sentFiles)
		before := len(gateway.snapshotOperations())
		send("model-menu", "/model")
		foundModel := false
		modelCardID := ""
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
			for _, op := range gateway.snapshotOperations()[before:] {
				if modelCardID == "" && op.Kind == feishu.OperationSendCard && op.MessageID != "" {
					modelCardID = op.MessageID
				}
				body, e := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
				if e == nil && op.Kind == feishu.OperationUpdateCard && op.MessageID == modelCardID && modelCardID != "" && strings.Contains(string(body), "Fixture Model") {
					foundModel = true
					break
				}
			}
			if foundModel {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !foundModel {
			t.Logf("model picker operations: %+v", gateway.snapshotOperations()[before:])
			t.Fatal("native model catalog missing from SDK model picker")
		}
		exerciseAssembledSDKModelChoice(t, ctx, app, gateway, before)
		send("seed", "establish session")
		for {
			checkpoint, e := r.journal.LoadCheckpoint(ctx, o, epoch)
			if e == nil && checkpoint.Core.SelectedThreadID == "session" && checkpoint.Core.ActiveQueueItemID == "" {
				break
			}
			select {
			case err := <-errorsCh:
				t.Fatal(err)
			case <-ctx.Done():
				inputs, _ := r.journal.PendingIngressWork(context.Background(), 32)
				effects, _ := r.journal.PendingHostEffects(context.Background(), 32)
				checkpoint, _ := r.journal.LoadCheckpoint(context.Background(), o, epoch)
				t.Logf("timeout diagnostics: ingress=%+v effects=%+v checkpoint=%+v", inputs, effects, checkpoint.Core)
				t.Fatal("initial session did not finish")
			case <-time.After(10 * time.Millisecond):
			}
		}
		status, e := r.processor.ReadRuntimeStatus(ctx)
		if e != nil {
			t.Fatal(e)
		}
		usageSeen := false
		for _, inst := range status.Instances {
			if inst.InstanceID == owner.InstanceID {
				thread := inst.Threads["session"]
				usageSeen = thread != nil && thread.TokenUsage != nil && thread.TokenUsage.Total.TotalTokens == 500
			}
		}
		if !usageSeen {
			t.Fatal("native token usage did not reach private core")
		}

	}
	send("message", "hello")
	for {
		saved, e := r.native.store.lookup(ctx, owner.InstanceID)
		if e != nil {
			t.Fatal(e)
		}
		lease, e := r.native.lease(ctx, saved, nil)
		if e == nil {
			turns, e := lease.ActiveAcceptedTurns(ctx)
			if e != nil {
				t.Fatal(e)
			}
			if len(turns) > 0 {
				break
			}
		}
		select {
		case err := <-errorsCh:
			t.Fatal(err)
		case <-ctx.Done():
			inputs, _ := r.journal.PendingIngressWork(context.Background(), 32)
			effects, _ := r.journal.PendingHostEffects(context.Background(), 32)
			checkpoint, _ := r.journal.LoadCheckpoint(context.Background(), o, epoch)
			t.Logf("timeout diagnostics: ingress=%+v effects=%+v checkpoint=%+v", inputs, effects, checkpoint.Core)
			t.Fatal("SDK prompt never reached native accepted turn")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if backend == agentproto.BackendCodex {
		before := len(gateway.snapshotOperations())
		send("working-menu", "/menu current_work")
		var workingMenu []byte
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
			for _, op := range gateway.snapshotOperations()[before:] {
				if op.Kind == feishu.OperationSendCard {
					candidate, _ := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
					if strings.Contains(string(candidate), "基本命令") {
						workingMenu = candidate
						break
					}
				}
			}
			if len(workingMenu) > 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if len(workingMenu) == 0 || !strings.Contains(string(workingMenu), "surface.menu.stop") || !strings.Contains(string(workingMenu), "surface.menu.new_thread") {
			pending, _ := r.journal.PendingIngressWork(context.Background(), 100)
			t.Logf("working menu diagnostics: operations=%+v pending=%+v", gateway.snapshotOperations()[before:], pending)
			t.Fatalf("working menu lacks stop/new: %s", workingMenu)
		}
		// Wait for the accepted first turn to be applied before staging the
		// image and text; the reaction then steers the exact queued inputs.
		for {
			saved, e := r.native.store.lookup(ctx, owner.InstanceID)
			if e != nil {
				t.Fatal(e)
			}
			lease, e := r.native.lease(ctx, saved, nil)
			if e != nil {
				t.Fatal(e)
			}
			events, e := lease.PendingNativeEvents(ctx, 0, 128)
			if e != nil {
				t.Fatal(e)
			}
			if len(events) == 0 {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("native projection did not drain")
			case <-time.After(10 * time.Millisecond):
			}
		}
		image := raw
		image.EventID = "image"
		image.MessageID = "image"
		image.MessageType = "image"
		image.RawContent = `{"image_key":"fixture-image"}`
		if e = r.hook.Receive(ctx, image); e != nil {
			t.Fatal(e)
		}
		for {
			record, e := r.router.Get(ctx, ingressowner.Key{Tenant: "tenant", AppID: "app", EventID: "image"})
			if e == nil && record.State == ingressowner.Accepted {
				break
			}
			select {
			case err := <-errorsCh:
				t.Fatal(err)
			case <-ctx.Done():
				t.Fatal("image staging did not complete")
			case <-time.After(10 * time.Millisecond):
			}
		}
		send("description", "describe this image")
		for {
			checkpoint, e := r.journal.LoadCheckpoint(ctx, o, epoch)
			if e == nil {
				found := false
				for _, item := range checkpoint.Core.QueueItems {
					if item != nil && item.SourceMessageID == "description" {
						found = true
					}
				}
				if found {
					break
				}
			}
			select {
			case err := <-errorsCh:
				t.Fatal(err)
			case <-ctx.Done():
				cp, _ := r.journal.LoadCheckpoint(context.Background(), o, epoch)
				for id, item := range cp.Core.QueueItems {
					if item != nil {
						t.Logf("queue %s source=%q sources=%v state=%s", id, item.SourceMessageID, item.SourceMessageIDs, item.Status)
					}
				}
				t.Logf("staged images=%d", len(cp.Core.StagedImages))
				pending, _ := r.journal.PendingIngressWork(context.Background(), 128)
				t.Logf("pending ingress=%+v", pending)
				for _, op := range gateway.snapshotOperations() {
					if op.CardTitle == "系统提示" {
						t.Logf("notice %s", op.CardBody)
					}
				}
				t.Fatal("description was not queued")
			case <-time.After(10 * time.Millisecond):
			}
		}
		reaction := platformingress.PlatformIngressEvent{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "reaction", Kind: "reaction.created", MessageID: "description", OperatorType: "user", OperatorIDs: platformingress.SenderIDs{OpenID: "user"}, Emoji: "ThumbsUp", ReceivedAt: time.Now()}
		if e = r.hook.ReceivePlatformEvent(ctx, reaction); e != nil {
			t.Fatal(e)
		}
		if e = r.hook.ReceivePlatformEvent(ctx, reaction); e != nil {
			t.Fatal(e)
		}
		for {
			traceData, _ := os.ReadFile(trace)
			if bytes.Contains(traceData, []byte(`"method":"turn/steer"`)) {
				var found bool
				for _, line := range bytes.Split(traceData, []byte("\n")) {
					var f struct {
						Method string `json:"method"`
						Params struct {
							ThreadID       string `json:"threadId"`
							ExpectedTurnID string `json:"expectedTurnId"`
							Input          []struct {
								Type string `json:"type"`
								Path string `json:"path"`
								Text string `json:"text"`
							} `json:"input"`
						} `json:"params"`
					}
					if json.Unmarshal(line, &f) != nil || f.Method != "turn/steer" {
						continue
					}
					found = true
					if f.Params.ThreadID != "session" || f.Params.ExpectedTurnID != "turn-2" || len(f.Params.Input) != 2 || f.Params.Input[0].Type != "localImage" || f.Params.Input[0].Path != imagePath || f.Params.Input[1].Type != "text" || f.Params.Input[1].Text != "describe this image" {
						t.Fatalf("steer inputs not exact: %+v", f.Params)
					}
				}
				if !found {
					t.Fatal("steer frame missing")
				}
				break
			}
			select {
			case err := <-errorsCh:
				t.Fatal(err)
			case <-ctx.Done():
				t.Fatal("reaction did not send native steer")
			case <-time.After(10 * time.Millisecond):
			}
		}
		// A native acceptance must finish the original queue/reaction state,
		// not merely write a turn/steer frame to the subprocess.
		for {
			checkpoint, e := r.journal.LoadCheckpoint(ctx, o, epoch)
			steered := false
			if e == nil {
				for _, item := range checkpoint.Core.QueueItems {
					if item != nil && item.SourceMessageID == "description" && string(item.Status) == "steered" {
						steered = true
					}
				}
			}
			if steered {
				break
			}
			select {
			case err := <-errorsCh:
				t.Fatal(err)
			case <-ctx.Done():
				t.Fatal("native steer acceptance did not finish queued input")
			case <-time.After(10 * time.Millisecond):
			}
		}
		send("shell-input", "queued for shell")
		for {
			checkpoint, e := r.journal.LoadCheckpoint(ctx, o, epoch)
			queued := false
			if e == nil {
				for _, item := range checkpoint.Core.QueueItems {
					if item != nil && item.SourceMessageID == "shell-input" && string(item.Status) == "queued" {
						queued = true
					}
				}
			}
			if queued {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("shell input not queued")
			case <-time.After(10 * time.Millisecond):
			}
		}
		applause := platformingress.PlatformIngressEvent{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "applause", Kind: "reaction.created", MessageID: "shell-input", OperatorType: "user", OperatorIDs: platformingress.SenderIDs{OpenID: "user"}, Emoji: "APPLAUSE", ReceivedAt: time.Now()}
		if e = r.hook.ReceivePlatformEvent(ctx, applause); e != nil {
			t.Fatal(e)
		}
		for {
			checkpoint, e := r.journal.LoadCheckpoint(ctx, o, epoch)
			commanded := false
			if e == nil {
				for _, item := range checkpoint.Core.QueueItems {
					if item != nil && item.SourceMessageID == "shell-input" && string(item.Status) == "shell-commanded" {
						commanded = true
					}
				}
			}
			if commanded {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("native shell acknowledgment did not finish reaction")
			case <-time.After(10 * time.Millisecond):
			}
		}
		send("stop", "/stop")
		for {
			traceData, _ := os.ReadFile(trace)
			if bytes.Contains(traceData, []byte(`"method":"turn/interrupt"`)) {
				break
			}
			select {
			case err := <-errorsCh:
				t.Fatal(err)
			case <-ctx.Done():
				t.Fatal("stop did not send native interrupt")
			case <-time.After(10 * time.Millisecond):
			}
		}
		send("next", "next request")
		for {
			traceData, _ := os.ReadFile(trace)
			if bytes.Count(traceData, []byte(`"method":"turn/start"`)) >= 3 {
				if bytes.Count(traceData, []byte(`"method":"turn/steer"`)) != 1 {
					t.Fatal("duplicate reaction resent the native steer")
				}
				break
			}
			select {
			case err := <-errorsCh:
				t.Fatal(err)
			case <-ctx.Done():
				t.Fatal("next message did not start after stop")
			case <-time.After(10 * time.Millisecond):
			}
		}
		for {
			checkpoint, e := r.journal.LoadCheckpoint(ctx, o, epoch)
			if e == nil {
				done := false
				for _, item := range checkpoint.Core.QueueItems {
					if item != nil && item.SourceMessageID == "next" && item.Status == "completed" {
						done = true
					}
				}
				if done && checkpoint.Core.ActiveQueueItemID == "" {
					break
				}
			}
			select {
			case err := <-errorsCh:
				t.Fatal(err)
			case <-ctx.Done():
				t.Fatal("next turn did not complete before restart")
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	if backend == agentproto.BackendCodex {
		testPrivateSDKRestart(t, ctx, app, gateway, daemonLock, raw, o, epoch, trace, server, owner.InstanceID)
	} else {
		send("opencode-stop", "/stop")
		exerciseAssembledSDKOpenCodeReasoning(t, ctx, app, gateway)
		if e = r.native.Shutdown(ctx); e != nil {
			t.Fatal(e)
		}
	}
}

func testPrivateSDKRestart(t *testing.T, ctx context.Context, old *App, gateway *messageIDAssigningGateway, lock *relayruntime.FileLock, raw platformingress.RawIngressEnvelope, owner privatechat.Owner, epoch int64, trace string, oldServer *httptest.Server, instanceID string) {
	t.Helper()
	r := old.applications
	if e := r.platformDeliveries.drain(ctx); e != nil {
		t.Fatal(e)
	}
	if e := r.stopWorkers(ctx); e != nil {
		t.Fatal(e)
	}
	beforeTrace, _ := os.ReadFile(trace)
	priorReads := bytes.Count(beforeTrace, []byte(`"method":"thread/read"`))
	addr := oldServer.Listener.Addr().String()
	if e := old.relay.Close(); e != nil {
		t.Fatal(e)
	}
	oldServer.Close()
	if e := r.drainNativeDeliveries(ctx); e != nil {
		t.Fatal(e)
	}
	before, e := r.journal.LoadCheckpoint(ctx, owner, epoch)
	if e != nil {
		t.Fatal(e)
	}
	if before.Core.Navigation == nil {
		t.Fatalf("confirmed navigation snapshot missing before restart: backend=%s route=%s history=%+v profiles=%q/%q/%q", before.Core.Backend, before.Core.RouteMode, before.Core.HistoricalInstance, before.Core.CodexProfileID, before.Core.ClaudeProfileID, before.Core.OpenCodeProfileID)
	}
	var beforeKinds []string
	for _, h := range before.Core.Holds {
		beforeKinds = append(beforeKinds, h.Kind)
	}
	t.Logf("before restart: route=%s attached=%t selected=%t active=%t holds=%v", before.Core.RouteMode, before.Core.AttachedInstanceID != "", before.Core.SelectedThreadID != "", before.Core.ActiveQueueItemID != "", beforeKinds)
	if e = r.close(); e != nil {
		t.Fatal(e)
	}
	snapshot, e := captureTeamLegacyFiles(old.headlessRuntime.Paths.StateDir)
	if e != nil {
		t.Fatal(e)
	}
	app := New("127.0.0.1:0", "127.0.0.1:0", gateway, agentproto.ServerIdentity{})
	if e = app.prepareApplicationCore(); e != nil {
		t.Fatal(e)
	}
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		t.Error("unexpected resend or restart of completed native work")
		return 0, context.Canceled
	}
	app.SetHeadlessRuntime(old.headlessRuntime)
	app.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: old.headlessRuntime.Paths.ConfigFile, Services: defaultFeishuServices()})
	if e = app.configureWorkRuntime(ctx, teamPrivateCommands{app: app}, app.authorizeTeamControl); e != nil {
		t.Fatal(e)
	}
	if e = app.activateWorkRuntime(ctx, snapshot); e != nil {
		t.Fatal(e)
	}
	controller := feishu.NewMultiGatewayController()
	if e = app.configureApplicationIngress(ctx, controller, lock); e != nil {
		t.Fatal(e)
	}
	r2 := app.applications
	app.relay.SetServerIdentity(agentproto.ServerIdentity{BinaryIdentity: agentproto.BinaryIdentity{Product: relayruntime.ProductName, Version: "fixture", BuildFingerprint: "fixture"}})
	listener, e := net.Listen("tcp", addr)
	if e != nil {
		t.Fatal(e)
	}
	relayServer := httptest.NewUnstartedServer(app.relay)
	relayServer.Listener = listener
	relayServer.Start()
	defer relayServer.Close()
	defer func() {
		finish, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = r2.platformDeliveries.drain(finish)
		_ = r2.stopWorkers(finish)
		_ = r2.native.Shutdown(finish)
		_ = r2.drainNativeDeliveries(finish)
		_ = r2.close()
		_ = app.teamRuntime.store.Close()
	}()
	errs := make(chan error, 8)
	if e = r2.startWorkers(ctx, errs); e != nil {
		t.Fatal(e)
	}
	after, e := r2.journal.LoadCheckpoint(ctx, owner, epoch)
	if e != nil {
		t.Fatal(e)
	}
	if after.Revision < before.Revision {
		t.Fatal("checkpoint regressed across restart")
	}
	for {
		r2.native.mu.Lock()
		connection := r2.native.connections[instanceID]
		r2.native.mu.Unlock()
		if connection != 0 {
			break
		}
		select {
		case err := <-errs:
			t.Fatal(err)
		case <-ctx.Done():
			t.Fatal("existing wrapper failed to reconnect")
		case <-time.After(10 * time.Millisecond):
		}
	}
	afterTrace, _ := os.ReadFile(trace)
	if bytes.Count(afterTrace, []byte(`"method":"thread/read"`)) != priorReads {
		t.Fatal("confirmed navigation unexpectedly used fallback query")
	}
	resumed := raw
	resumed.EventID = "resumed-after-restart"
	resumed.MessageID = resumed.EventID
	resumed.RawContent = `{"text":"continue same session"}`
	if e = r2.hook.Receive(ctx, resumed); e != nil {
		t.Fatal(e)
	}
	for {
		data, _ := os.ReadFile(trace)
		if bytes.Count(data, []byte(`"method":"turn/start"`)) >= 4 {
			break
		}
		select {
		case err := <-errs:
			t.Fatal(err)
		case <-ctx.Done():
			cp, _ := r2.journal.LoadCheckpoint(context.Background(), owner, epoch)
			var kinds []string
			for _, h := range cp.Core.Holds {
				kinds = append(kinds, h.Kind)
			}
			pending, _ := r2.journal.PendingIngressWorkPage(context.Background(), 128, 0)
			db, x := sql.Open("sqlite", filepath.Join(app.headlessRuntime.Paths.StateDir, "private-chat.db"))
			if x == nil {
				rows, x := db.Query(`SELECT body FROM private_effect ORDER BY revision DESC,ordinal DESC LIMIT 20`)
				if x == nil {
					for rows.Next() {
						var body []byte
						if rows.Scan(&body) == nil {
							ev, _, d := privatechat.DecodeStoredEffect(body)
							if d == nil && ev.Notice != nil {
								t.Logf("recent notice %s", ev.Notice.Code)
							}
							if d == nil && ev.Command != nil {
								t.Logf("recent command %s", ev.Command.Kind)
							}
						}
					}
					rows.Close()
				}
				db.Close()
			}
			t.Logf("contract refresh pending=%v", cp.Core.ContractRefreshPending)

			t.Fatalf("reconnected wrapper did not accept new input: revision=%d route=%s attached=%t selected=%t active=%t queued=%d holds=%v pending=%d", cp.Revision, cp.Core.RouteMode, cp.Core.AttachedInstanceID != "", cp.Core.SelectedThreadID != "", cp.Core.ActiveQueueItemID != "", len(cp.Core.QueuedQueueItemIDs), kinds, len(pending))
		case <-time.After(10 * time.Millisecond):
		}
	}
	duplicate := raw
	duplicate.EventID = "next"
	duplicate.MessageID = "next"
	duplicate.RawContent = `{"text":"next request"}`
	if e = r2.hook.Receive(ctx, duplicate); e != nil {
		t.Fatal(e)
	}
	status := raw
	status.EventID = "after-restart"
	status.MessageID = "after-restart"
	status.RawContent = `{"text":"/status"}`
	if e = r2.hook.Receive(ctx, status); e != nil {
		t.Fatal(e)
	}
	for {
		record, e := r2.router.Get(ctx, ingressowner.Key{Tenant: "tenant", AppID: "app", EventID: "after-restart"})
		if e == nil && record.State == ingressowner.Accepted {
			break
		}
		select {
		case err := <-errs:
			t.Fatal(err)
		case <-ctx.Done():
			t.Fatal("restarted SDK ingress did not recover")
		case <-time.After(10 * time.Millisecond):
		}
	}
	traceData, _ := os.ReadFile(trace)
	if bytes.Count(traceData, []byte(`"method":"turn/start"`)) != 4 || bytes.Count(traceData, []byte(`"method":"turn/steer"`)) != 1 {
		t.Fatal("restart replayed a native message")
	}
}
