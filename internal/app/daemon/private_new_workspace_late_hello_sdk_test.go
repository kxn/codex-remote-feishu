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
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestPrivateSDKCancelledAuthenticatedHelloCannotReplaceNextWorkspace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	app, gateway := newApplicationRuntimeTestApp(t)
	r := app.applications
	cwd := t.TempDir()
	app.service.SetPersistedThreadCatalog(newWorkspaceJourneyCatalog{cwd: cwd})
	setTestWorkBotSettings(t, app, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendCodex, CodexProfileID: state.NativeCodexProfileID})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := app.headlessRuntime.Paths.StateDir
	native := filepath.Join(t.TempDir(), "codex")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3-dev.1\\n'; exit; fi\nPRIVATE_CLEAN_CODEX=1 exec %q -test.run '^TestPrivateCleanRestartCodexProcess$'\n", exe)
	if err = os.WriteFile(native, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(root, "codex-runtime")
	if err = os.MkdirAll(managed, 0700); err != nil {
		t.Fatal(err)
	}
	pin, _ := json.Marshal(map[string]string{"binary": native, "version": "0.159.3-dev.1"})
	if err = os.WriteFile(filepath.Join(managed, "exact-version.json"), pin, 0600); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(t.TempDir(), "trace")
	releaseTurn := filepath.Join(t.TempDir(), "turn")
	app.headlessRuntime.BaseEnv = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH"), "FIXTURE_TRACE=" + trace, "CLEAN_TURN_RELEASE=" + releaseTurn}
	app.headlessRuntime.CodexRealBinary = native
	type capturedHello struct {
		meta  relayws.ConnectionMeta
		hello agentproto.Hello
	}
	captured := make(chan capturedHello, 1)
	release := make(chan struct{})
	lateDone := make(chan error, 1)
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var helloMu sync.Mutex
	first := true
	app.relay = relayws.NewServer(relayws.ServerCallbacks{AuthorizeHello: app.authorizeApplicationHello, OnHello: func(c context.Context, m relayws.ConnectionMeta, h agentproto.Hello) {
		helloMu.Lock()
		isFirst := first
		if isFirst {
			first = false
		}
		helloMu.Unlock()
		if !isFirst {
			app.applicationHello(c, m, h)
			return
		}
		captured <- capturedHello{m, h}
		<-release
		e := r.native.acceptHello(context.Background(), m, h)
		if e == nil {
			app.wakePrivateApplication()
		}
		lateDone <- e
	}, OnEvents: app.applicationEvents, OnCommandAck: app.applicationAck, OnDisconnect: app.applicationDisconnect})
	app.sendAgentCommand = app.relay.SendCommand
	r.native.relay = app.relay
	app.relay.SetServerIdentity(agentproto.ServerIdentity{BinaryIdentity: agentproto.BinaryIdentity{Product: relayruntime.ProductName, Version: "fixture", BuildFingerprint: "fixture"}})
	server := httptest.NewServer(app.relay)
	defer server.Close()
	relayURL := "ws" + strings.TrimPrefix(server.URL, "http")
	var childrenMu sync.Mutex
	var children []*exec.Cmd
	var waits sync.WaitGroup
	r.native.startHeadless = func(v relayruntime.HeadlessLaunchOptions) (int, error) {
		child := exec.Command(exe, "-test.run=^TestPrivateOpenCodeRelayFixtureProcess$")
		child.Env = append(v.Env, "PRIVATE_OPENCODE_FIXTURE=wrapper", "FIXTURE_RELAY_URL="+relayURL, "FIXTURE_STATE_DIR="+root, "FIXTURE_CODEX_BIN="+native)
		child.Dir = v.WorkDir
		child.Stdout = io.Discard
		child.Stderr = os.Stderr
		if e := child.Start(); e != nil {
			return 0, e
		}
		childrenMu.Lock()
		children = append(children, child)
		waits.Add(1)
		childrenMu.Unlock()
		go func() { defer waits.Done(); _ = child.Wait() }()
		return child.Process.Pid, nil
	}
	defer func() {
		releaseOnce.Do(func() { close(release) })
		childrenMu.Lock()
		for _, c := range children {
			if c.Process != nil {
				_ = c.Process.Kill()
			}
		}
		childrenMu.Unlock()
		waits.Wait()
	}()
	errorsCh := make(chan error, 8)
	if err = r.startWorkers(ctx, errorsCh); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stop, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = r.stopWorkers(stop)
		_ = r.drainNativeDeliveries(stop)
	}()
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", ReceivedAt: time.Now()}
	send := func(id, text string) {
		t.Helper()
		raw.EventID = id
		raw.MessageID = id
		body, _ := json.Marshal(map[string]string{"text": text})
		raw.RawContent = string(body)
		raw.ReceivedAt = time.Now()
		if e := r.hook.Receive(ctx, raw); e != nil {
			t.Fatal(e)
		}
	}
	selectWorkspace := func(prefix, skip string, awaitReady bool) string {
		t.Helper()
		before := len(gateway.snapshotOperations())
		send(prefix+"-list", "/list")
		var card feishu.Operation
		for card.MessageID == "" {
			for _, op := range gateway.snapshotOperations() {
				if op.Kind != feishu.OperationSendCard || op.MessageID == "" || op.MessageID == skip {
					continue
				}
				f, e := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
				var d any
				if e == nil && json.Unmarshal(f, &d) == nil && findPrivateCallback(d, "target_picker_confirm") != nil {
					card = op
					break
				}
			}
			if card.MessageID != "" {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("picker not published")
			case <-time.After(20 * time.Millisecond):
			}
		}
		frozen, e := feishu.FreezePrivateOperation(card, app.daemonLifecycleID)
		if e != nil {
			t.Fatal(e)
		}
		var doc any
		if e = json.Unmarshal(frozen, &doc); e != nil {
			t.Fatal(e)
		}
		click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: card.MessageID, Kind: "card", ReceivedAt: time.Now()}
		if v := findPrivateCallback(doc, "target_picker_select_workspace"); v != nil {
			click.EventID = prefix + "-choose"
			click.Values = v
			click.Option = cwd
			response, e := r.hook.ReceiveInteractionResponse(ctx, click)
			if e != nil {
				t.Fatal(e)
			}
			if e = json.Unmarshal(response.RawBody, &doc); e != nil {
				t.Fatal(e)
			}
		}
		click.EventID = prefix + "-confirm"
		click.Values = findPrivateCallback(doc, "target_picker_confirm")
		click.Option = ""
		if click.Values == nil {
			t.Fatal("missing confirm")
		}
		response, e := r.hook.ReceiveInteractionResponse(ctx, click)
		if e != nil {
			t.Fatal(e)
		}
		if awaitReady && !strings.Contains(string(response.RawBody), "已进入新会话待命") {
			u := sdkJourneyUser{t: t, ctx: ctx, gateway: gateway, errors: errorsCh, cwd: cwd}
			u.waitVisible(app, before, "已进入新会话待命")
		}
		return card.MessageID
	}
	firstCard := selectWorkspace("first", "", false)
	var old capturedHello
	select {
	case old = <-captured:
	case <-ctx.Done():
		t.Fatal("first authenticated hello did not arrive")
	}
	if !old.meta.PrivateAuthenticated || old.meta.ConnectionID == 0 {
		t.Fatal("first hello lacked authentication")
	}
	u := sdkJourneyUser{t: t, ctx: ctx, gateway: gateway, errors: errorsCh, cwd: cwd}
	before := len(gateway.snapshotOperations())
	send("detach", "/detach")
	u.waitVisible(app, before, "已取消")
	selectWorkspace("second", firstCard, true)
	releaseOnce.Do(func() { close(release) })
	select {
	case <-lateDone:
	case <-ctx.Done():
		t.Fatal("old authenticated callback was not executed")
	}
	before = len(gateway.snapshotOperations())
	send("after-late", "hi")
	u.waitFinal(app, before, "after-late", "done")
	for {
		data, e := os.ReadFile(trace)
		if e == nil && strings.Contains(string(data), `"method":"turn/start"`) {
			break
		}
		select {
		case e := <-errorsCh:
			t.Fatal(e)
		case <-ctx.Done():
			t.Fatal("new session did not accept input")
		case <-time.After(20 * time.Millisecond):
		}
	}
	status, err := r.processor.ReadRuntimeStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	surface := gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user")
	for _, s := range status.Surfaces {
		if s.SurfaceSessionID == surface && (s.AttachedInstanceID == "" || s.AttachedInstanceID == old.hello.Instance.InstanceID || s.PendingHeadless != nil) {
			t.Fatalf("late hello changed the new session: %+v", s)
		}
	}
}
