package daemon

import (
	"bufio"
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

	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestPrivateOpenCodeRelayFixtureProcess(t *testing.T) {
	switch os.Getenv("PRIVATE_OPENCODE_FIXTURE") {
	case "native":
		scanner := bufio.NewScanner(os.Stdin)
		var activeID any
		for scanner.Scan() {
			var f struct {
				ID     any    `json:"id"`
				Method string `json:"method"`
			}
			if json.Unmarshal(scanner.Bytes(), &f) != nil {
				continue
			}
			switch f.Method {
			case "initialize":
				fmt.Printf(`{"id":%q,"result":{"protocolVersion":1,"agentInfo":{"name":"OpenCode","version":"2.1.4"}}}`+"\n", f.ID)
			case "session/new":
				fmt.Printf(`{"id":%q,"result":{"sessionId":"session","configOptions":[{"id":"model","type":"select","category":"model","currentValue":"fixture/model","options":[{"value":"fixture/model","name":"Fixture Model"}]},{"id":"effort","type":"select","currentValue":"low","options":[{"value":"low","name":"Low"},{"value":"high","name":"High"}]}]}}`+"\n", f.ID)
			case "session/set_config_option":
				fmt.Printf(`{"id":%q,"result":{}}`+"\n", f.ID)
			case "session/prompt":
				activeID = f.ID
				fmt.Println(`{"method":"session/update","params":{"sessionId":"session","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"working"}}}}`)
			case "session/cancel":
				if activeID != nil {
					fmt.Printf(`{"id":%q,"result":{"stopReason":"cancelled"}}`+"\n", activeID)
					activeID = nil
				}
			}
		}
		os.Exit(0)
	case "codex":
		scanner := bufio.NewScanner(os.Stdin)
		turns := 0
		for scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			var f struct {
				ID     any    `json:"id"`
				Method string `json:"method"`
			}
			if json.Unmarshal(line, &f) != nil {
				continue
			}
			if f.Method == "turn/steer" || f.Method == "turn/start" || f.Method == "turn/interrupt" || f.Method == "thread/shellCommand" || f.Method == "thread/read" {
				if path := os.Getenv("FIXTURE_TRACE"); path != "" {
					file, _ := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
					if file != nil {
						_, _ = file.Write(append(line, '\n'))
						_ = file.Close()
					}
				}
			}
			switch f.Method {
			case "initialize":
				fmt.Printf(`{"id":%q,"result":{}}`+"\n", f.ID)
			case "thread/start", "thread/resume":
				cwd, _ := os.Getwd()
				fmt.Printf(`{"id":%q,"result":{"thread":{"id":"session","cwd":%q}}}`+"\n", f.ID, cwd)
			case "turn/start":
				turns++
				turn := fmt.Sprintf("turn-%d", turns)
				fmt.Printf(`{"id":%q,"result":{"turn":{"id":%q}}}`+"\n", f.ID, turn)
				fmt.Printf(`{"method":"turn/started","params":{"threadId":"session","turn":{"id":%q}}}`+"\n", turn)
				fmt.Printf(`{"method":"thread/tokenUsage/updated","params":{"threadId":"session","turnId":%q,"tokenUsage":{"last":{"totalTokens":200},"total":{"totalTokens":500},"modelContextWindow":1000}}}`+"\n", turn)
				if turns == 1 || turns > 2 {
					fmt.Printf(`{"method":"item/completed","params":{"threadId":"session","turnId":%q,"item":{"id":"answer","type":"agentMessage","text":"done"}}}`+"\n", turn)
					fmt.Printf(`{"method":"turn/completed","params":{"threadId":"session","turn":{"id":%q,"status":"completed"}}}`+"\n", turn)
				}
			case "thread/shellCommand":
				var command struct {
					Params struct {
						Command string `json:"command"`
					} `json:"params"`
				}
				_ = json.Unmarshal(line, &command)
				fmt.Printf(`{"id":%q,"result":{}}`+"\n", f.ID)
				item := map[string]any{"id": "shell-item", "type": "commandExecution", "source": "userShell", "command": command.Params.Command, "status": "completed", "exitCode": 0, "aggregatedOutput": "shell done"}
				raw, _ := json.Marshal(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "session", "turnId": "shell-turn", "item": item}})
				fmt.Println(string(raw))
			case "turn/steer":
				fmt.Printf(`{"id":%q,"result":{"turnId":"turn-2"}}`+"\n", f.ID)
			case "turn/interrupt":
				fmt.Printf(`{"id":%q,"result":{}}`+"\n", f.ID)
				fmt.Println(`{"method":"turn/completed","params":{"threadId":"session","turn":{"id":"turn-2","status":"interrupted"}}}`)
			case "thread/list":
				fmt.Printf(`{"id":%q,"result":{"data":[]}}`+"\n", f.ID)
			case "thread/read":
				cwd, _ := os.Getwd()
				fmt.Printf(`{"id":%q,"result":{"thread":{"id":"session","cwd":%q,"status":{"type":"idle"}}}}`+"\n", f.ID, cwd)
			case "thread/goal/get":
				fmt.Printf(`{"id":%q,"result":{"goal":null}}`+"\n", f.ID)
			case "model/list":
				fmt.Printf(`{"id":%q,"result":{"data":[{"id":"fixture-model","model":"fixture-model","displayName":"Fixture Model","supportedReasoningEfforts":[{"reasoningEffort":"high"}],"defaultReasoningEffort":"high"}]}}`+"\n", f.ID)
			}
		}
		os.Exit(0)
	case "wrapper":
		data, e := os.ReadFile(os.Getenv(agentproto.PrivateNativeLaunchEnv))
		if e != nil {
			os.Exit(71)
		}
		var snapshot agentproto.PrivateNativeLaunchSnapshot
		if json.Unmarshal(data, &snapshot) != nil {
			os.Exit(72)
		}
		mode := "opencode-acp"
		if snapshot.Owner.Backend == agentproto.BackendCodex {
			mode = "app-server"
		}
		cfg := wrapper.Config{PrivateNativeLaunch: &snapshot, RelayServerURL: os.Getenv("FIXTURE_RELAY_URL"), InstanceID: snapshot.Owner.InstanceID, WorkspaceRoot: snapshot.CWD, WorkspaceKey: snapshot.CWD, Backend: snapshot.Owner.Backend, CodexRealBinary: os.Getenv("FIXTURE_CODEX_BIN"), Managed: true, Source: "headless", Lifetime: "daemon-owned", Args: []string{mode}, Version: "fixture", BuildFingerprint: "fixture", RuntimePaths: relayruntime.Paths{StateDir: os.Getenv("FIXTURE_STATE_DIR"), ManagerLockFile: filepath.Join(os.Getenv("FIXTURE_STATE_DIR"), "manager.lock")}}
		app := wrapper.New(cfg)
		code, e := app.Run(context.Background(), os.Stdin, io.Discard, os.Stderr)
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
		}
		os.Exit(code)
	}
}
func TestPrivateOpenCodeRealRelayHostShutdownAndRetire(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(fmt.Sprint("active=", active), func(t *testing.T) { testPrivateOpenCodeRealRelayHost(t, active) })
	}
}
func testPrivateOpenCodeRealRelayHost(t *testing.T, active bool) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX child executable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	app := newManagedInstancesAdminTestApp(t)
	root := app.headlessRuntime.Paths.StateDir
	cwd := t.TempDir()
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	script := filepath.Join(t.TempDir(), "opencode")
	body := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf '2.1.4\\n'; exit; fi\nPRIVATE_OPENCODE_FIXTURE=native exec %q -test.run '^TestPrivateOpenCodeRelayFixtureProcess$'\n", exe)
	if e = os.WriteFile(script, []byte(body), 0700); e != nil {
		t.Fatal(e)
	}
	app.headlessRuntime.BaseEnv = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH"), "OPENCODE_BIN=" + script}
	h := &privateNativeHost{hostID: "host", stateDir: root}
	h.store, e = openPrivateNativeHostStore(filepath.Join(root, "host-fixture.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer h.store.Close()
	h.journal, e = privatechat.OpenJournal(filepath.Join(root, "private-fixture.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer h.journal.Close()
	h.registry, e = executionowner.Open(filepath.Join(root, "owners-fixture.db"), h)
	if e != nil {
		t.Fatal(e)
	}
	defer h.registry.Close()
	h.outbound, e = executionowner.NewOutbound(h.registry)
	if e != nil {
		t.Fatal(e)
	}
	private := privatechat.New(nil, orchestrator.Config{})
	h.processor, e = privatechat.NewProcessor(private, h.journal, privateActionDecoder{}, privatechat.CoreReducer{})
	if e != nil {
		t.Fatal(e)
	}
	failures := make(chan error, 16)
	connected := make(chan struct{}, 1)
	h.relay = relayws.NewServer(relayws.ServerCallbacks{AuthorizeHello: func(hello agentproto.Hello, credential string) error {
		if hello.Probe {
			return nil
		}
		return h.store.authorize(hello, credential)
	}, OnHello: func(c context.Context, m relayws.ConnectionMeta, hello agentproto.Hello) {
		if e := h.acceptHello(c, m, hello); e != nil {
			failures <- e
			return
		}
		select {
		case connected <- struct{}{}:
		default:
		}
	}, OnEvents: func(c context.Context, m relayws.ConnectionMeta, id string, events []agentproto.Event) {
		for _, event := range events {
			if e := h.receive(c, m, id, event); e != nil {
				failures <- e
			}
		}
	}, OnCommandAck: func(c context.Context, m relayws.ConnectionMeta, id string, ack agentproto.CommandAck) {
		if e := h.receiveAck(c, m, id, ack); e != nil {
			failures <- e
		}
	}, OnDisconnect: func(c context.Context, m relayws.ConnectionMeta, id string) { _ = h.disconnect(c, m, id) }})
	h.relay.SetServerIdentity(agentproto.ServerIdentity{BinaryIdentity: agentproto.BinaryIdentity{Product: relayruntime.ProductName, Version: "fixture", BuildFingerprint: "fixture"}})
	server := httptest.NewServer(h.relay)
	defer server.Close()
	relayURL := "ws" + strings.TrimPrefix(server.URL, "http")
	var child *exec.Cmd
	h.startHeadless = func(v relayruntime.HeadlessLaunchOptions) (int, error) {
		child = exec.Command(exe, "-test.run=^TestPrivateOpenCodeRelayFixtureProcess$")
		child.Env = append(v.Env, "PRIVATE_OPENCODE_FIXTURE=wrapper", "FIXTURE_RELAY_URL="+relayURL, "FIXTURE_STATE_DIR="+root)
		child.Dir = v.WorkDir
		child.Stdout = io.Discard
		child.Stderr = os.Stderr
		if e := child.Start(); e != nil {
			return 0, e
		}
		return child.Process.Pid, nil
	}
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendOpenCode}
	record := privateCarrierRecord{Snapshot: agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: filepath.Join(root, "native.db")}, Surface: privatechat.Owner{"gateway", "surface", "chat", "user", "tenant", "app"}, SurfaceGeneration: 1}
	if _, e = app.launchPrivateNative(ctx, h, privateNativeLaunchRequest{Record: record}); e != nil {
		t.Fatal(e)
	}
	defer func() {
		if child != nil {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	}()
	select {
	case <-connected:
	case e := <-failures:
		t.Fatal(e)
	case <-ctx.Done():
		t.Fatal("authenticated hello timed out")
	}
	if active {
		admit := func(id, lane string, action control.Action) []privatechat.EffectRecord {
			raw, e := json.Marshal(action)
			if e != nil {
				t.Fatal(e)
			}
			seq, _, e := h.journal.Admit(ctx, privatechat.Ingress{Owner: record.Surface, Generation: 1, EventID: id, Lane: lane, Payload: raw, Sources: []string{id}})
			if e != nil {
				t.Fatal(e)
			}
			effects, e := h.processor.ApplyIngress(ctx, record.Surface, 1, seq)
			if e != nil {
				t.Fatal(e)
			}
			return effects
		}
		admit("mode", "control", control.Action{Kind: control.ActionModeCommand, GatewayID: "gateway", SurfaceSessionID: "surface", ChatID: "chat", ActorUserID: "user", Text: "/mode opencode"})
		attached := admit("attach", "control", control.Action{Kind: control.ActionAttachInstance, GatewayID: "gateway", SurfaceSessionID: "surface", ChatID: "chat", ActorUserID: "user", InstanceID: "instance"})
		_ = attached
		effects := admit("message", "input", control.Action{Kind: control.ActionTextMessage, GatewayID: "gateway", SurfaceSessionID: "surface", ChatID: "chat", ActorUserID: "user", MessageID: "message", Text: "hello", Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}})
		var effectID string
		var command *agentproto.Command
		for _, effect := range effects {
			event, _, target, e := privatechat.DecodeStoredEffectTarget(effect.Effect.Body)
			if e == nil && event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend && target != nil && *target == owner.InstanceID {
				effectID = effect.Effect.ID
				command = event.Command
				break
			}
		}
		if command == nil {
			for _, effect := range append(attached, effects...) {
				ev, _, _, _ := privatechat.DecodeStoredEffectTarget(effect.Effect.Body)
				if ev.Notice != nil {
					t.Logf("notice: %s %s", ev.Notice.Code, ev.Notice.Text)
				}
			}
			t.Fatal("no frozen prompt effect")
		}
		saved, e := h.store.lookup(ctx, owner.InstanceID)
		if e != nil {
			t.Fatal(e)
		}
		binding := agentproto.NativeTurnBinding{Version: 1, Owner: saved.Snapshot.Owner, ExecutionID: "execution"}
		h.mu.Lock()
		connection := h.connections[owner.InstanceID]
		h.mu.Unlock()
		if e = h.sendEffect(ctx, relayws.ConnectionMeta{ConnectionID: connection, PrivateAuthenticated: true}, owner.InstanceID, effectID, binding, "", command.Kind); e != nil {
			t.Fatal(e)
		}
		lease, e := h.lease(ctx, saved, nil)
		if e != nil {
			t.Fatal(e)
		}
		for {
			turns, e := lease.ActiveAcceptedTurns(ctx)
			if e != nil {
				t.Fatal(e)
			}
			if len(turns) == 1 {
				break
			}
			select {
			case f := <-failures:
				t.Fatal(f)
			case <-ctx.Done():
				t.Fatal("native accepted turn missing")
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	if e = h.Shutdown(ctx); e != nil {
		select {
		case f := <-failures:
			t.Fatalf("shutdown %v, callback %v", e, f)
		default:
			t.Fatal(e)
		}
	}
	saved, e := h.store.lookup(ctx, owner.InstanceID)
	if e != nil || saved.State != "retired" {
		t.Fatalf("carrier not retired: %+v %v", saved, e)
	}
	if _, e = h.registry.Resolve(ctx, privateRegistration(saved.Snapshot.Owner).Carrier, owner.Epoch); e == nil {
		t.Fatal("retired carrier retained owner")
	}
}
