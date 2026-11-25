package daemon

import (
	"bufio"
	"context"
	"database/sql"
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
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type graphCatalog struct{ a, b string }

func (c graphCatalog) RecentThreads(int) ([]state.ThreadRecord, error) {
	return []state.ThreadRecord{{ThreadID: "graph-a", CWD: c.a, Name: "A session"}, {ThreadID: "graph-b", CWD: c.b, Name: "B session"}}, nil
}
func (c graphCatalog) RecentWorkspaces(int) (map[string]time.Time, error) {
	return map[string]time.Time{c.a: time.Now(), c.b: time.Now()}, nil
}
func (c graphCatalog) ThreadByID(id string) (*state.ThreadRecord, error) {
	for _, x := range []struct{ id, cwd string }{{"graph-a", c.a}, {"graph-b", c.b}} {
		if id == x.id {
			return &state.ThreadRecord{ThreadID: id, CWD: x.cwd, Name: id}, nil
		}
	}
	return nil, nil
}

type graphEnv struct {
	app     *App
	user    sdkJourneyUser
	trace   string
	release string
}

type graphSetup func(*App, *messageIDAssigningGateway)

func newGraphEnv(t *testing.T, a, b string, setup ...graphSetup) graphEnv {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX controlled wrapper")
	}
	app, gateway := newApplicationRuntimeTestApp(t)
	r := app.applications
	app.service.SetPersistedThreadCatalog(graphCatalog{a, b})
	root := app.headlessRuntime.Paths.StateDir
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "codex")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3-dev.1\\n'; exit; fi\nPRIVATE_GRAPH_NATIVE=1 exec %q -test.run '^TestPrivateGraphNativeProcess$'\n", exe)
	if err = os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(root, "codex-runtime")
	if err = os.MkdirAll(managed, 0700); err != nil {
		t.Fatal(err)
	}
	pin, _ := json.Marshal(map[string]string{"binary": bin, "version": "0.159.3-dev.1"})
	if err = os.WriteFile(filepath.Join(managed, "exact-version.json"), pin, 0600); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(t.TempDir(), "trace")
	release := filepath.Join(t.TempDir(), "release")
	app.headlessRuntime.BaseEnv = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH"), "GRAPH_TRACE=" + trace, "GRAPH_A=" + a, "GRAPH_B=" + b, "GRAPH_RELEASE=" + release}
	app.headlessRuntime.CodexRealBinary = bin
	app.relay.SetServerIdentity(agentproto.ServerIdentity{BinaryIdentity: agentproto.BinaryIdentity{Product: relayruntime.ProductName, Version: "fixture", BuildFingerprint: "fixture"}})
	server := httptest.NewServer(app.relay)
	t.Cleanup(server.Close)
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	var mu sync.Mutex
	var children []*exec.Cmd
	var wg sync.WaitGroup
	r.native.startHeadless = func(v relayruntime.HeadlessLaunchOptions) (int, error) {
		child := exec.Command(exe, "-test.run=^TestPrivateOpenCodeRelayFixtureProcess$")
		child.Env = append(v.Env, "PRIVATE_OPENCODE_FIXTURE=wrapper", "FIXTURE_RELAY_URL="+url, "FIXTURE_STATE_DIR="+root, "FIXTURE_CODEX_BIN="+bin)
		child.Dir = v.WorkDir
		child.Stdout = io.Discard
		child.Stderr = os.Stderr
		if e := child.Start(); e != nil {
			return 0, e
		}
		mu.Lock()
		children = append(children, child)
		wg.Add(1)
		mu.Unlock()
		go func() { defer wg.Done(); _ = child.Wait() }()
		return child.Process.Pid, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	errs := make(chan error, 8)
	for _, fn := range setup {
		fn(app, gateway)
	}
	if err = r.startWorkers(ctx, errs); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		stop, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = r.stopWorkers(stop)
		mu.Lock()
		for _, child := range children {
			if child.Process != nil {
				_ = child.Process.Kill()
			}
		}
		mu.Unlock()
		wg.Wait()
		_ = r.drainNativeDeliveries(stop)
	})
	return graphEnv{app: app, user: sdkJourneyUser{t: t, ctx: ctx, gateway: gateway, errors: errs, cwd: a}, trace: trace, release: release}
}

func TestPrivateGraphNativeProcess(t *testing.T) {
	if os.Getenv("PRIVATE_GRAPH_NATIVE") != "1" {
		return
	}
	cwd, _ := os.Getwd()
	session := "graph-a"
	if cwd == os.Getenv("GRAPH_B") {
		session = "graph-b"
	}
	turn := 0
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		line := append([]byte(nil), scan.Bytes()...)
		var f struct {
			ID     any             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(line, &f) != nil {
			continue
		}
		if f.Method == "turn/start" || f.Method == "thread/start" || f.Method == "thread/resume" {
			if out, e := os.OpenFile(os.Getenv("GRAPH_TRACE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); e == nil {
				fmt.Fprintf(out, "%s %s\n", cwd, line)
				out.Close()
			}
		}
		switch f.Method {
		case "initialize":
			fmt.Printf(`{"id":%q,"result":{}}`+"\n", f.ID)
		case "thread/start", "thread/resume":
			fmt.Printf(`{"id":%q,"result":{"model":"fixture-model","thread":{"id":%q,"cwd":%q}}}`+"\n", f.ID, session, cwd)
		case "turn/start":
			turn++
			id := fmt.Sprintf("turn-%d", turn)
			fmt.Printf(`{"id":%q,"result":{"turn":{"id":%q}}}`+"\n", f.ID, id)
			fmt.Printf(`{"method":"turn/started","params":{"threadId":%q,"turn":{"id":%q}}}`+"\n", session, id)
			finish := func() {
				fmt.Printf(`{"method":"item/completed","params":{"threadId":%q,"turnId":%q,"item":{"id":"answer","type":"agentMessage","text":%q}}}`+"\n", session, id, "done-"+session)
				fmt.Printf(`{"method":"turn/completed","params":{"threadId":%q,"turn":{"id":%q,"status":"completed"}}}`+"\n", session, id)
			}
			if strings.Contains(string(f.Params), "graph-busy") {
				fmt.Printf(`{"method":"item/agentMessage/delta","params":{"threadId":%q,"turnId":%q,"itemId":"answer","delta":"graph-working"}}`+"\n", session, id)
				go func() {
					for {
						if _, e := os.Stat(os.Getenv("GRAPH_RELEASE")); e == nil {
							finish()
							return
						}
						time.Sleep(10 * time.Millisecond)
					}
				}()
			} else {
				finish()
			}
		case "thread/read":
			fmt.Printf(`{"id":%q,"result":{"thread":{"id":%q,"cwd":%q,"status":{"type":"idle"}}}}`+"\n", f.ID, session, cwd)
		case "thread/list":
			fmt.Printf(`{"id":%q,"result":{"data":[]}}`+"\n", f.ID)
		case "model/list":
			fmt.Printf(`{"id":%q,"result":{"data":[]}}`+"\n", f.ID)
		case "thread/goal/get":
			fmt.Printf(`{"id":%q,"result":{"goal":null}}`+"\n", f.ID)
		case "turn/interrupt":
			fmt.Printf(`{"id":%q,"result":{}}`+"\n", f.ID)
		}
	}
	os.Exit(0)
}

func graphChooseExisting(u sdkJourneyUser, app *App, prefix, cwd, thread string) {
	u.t.Helper()
	before := len(u.gateway.snapshotOperations())
	u.send(app, prefix+"-list", "/list")
	var card feishu.Operation
	var doc any
	for card.MessageID == "" {
		for _, op := range u.gateway.snapshotOperations()[before:] {
			if op.Kind != feishu.OperationSendCard || op.MessageID == "" {
				continue
			}
			raw, e := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
			if e == nil && json.Unmarshal(raw, &doc) == nil && findPrivateCallback(doc, "target_picker_confirm") != nil {
				card = op
				break
			}
		}
		if card.MessageID == "" {
			select {
			case <-u.ctx.Done():
				u.t.Fatal("session picker absent")
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: card.MessageID, Kind: "card", ReceivedAt: time.Now()}
	for _, step := range []struct{ kind, option string }{{"target_picker_select_workspace", cwd}, {"target_picker_select_session", "thread:" + thread}} {
		value := findPrivateCallback(doc, step.kind)
		if value == nil {
			u.t.Fatalf("missing %s", step.kind)
		}
		click.EventID = prefix + step.kind
		click.Values = value
		click.Option = step.option
		response, e := app.applications.hook.ReceiveInteractionResponse(u.ctx, click)
		if e != nil {
			u.t.Fatal(e)
		}
		if e = json.Unmarshal(response.RawBody, &doc); e != nil {
			u.t.Fatalf("%s response=%s: %v", step.kind, response.RawBody, e)
		}
	}
	click.EventID = prefix + "-confirm"
	click.Values = findPrivateCallback(doc, "target_picker_confirm")
	click.Option = ""
	if click.Values == nil {
		u.t.Fatal("confirm missing")
	}
	response, e := app.applications.hook.ReceiveInteractionResponse(u.ctx, click)
	if e != nil {
		u.t.Fatal(e)
	}
	if strings.Contains(string(response.RawBody), "已切换会话") {
		return
	}
	u.waitVisible(app, before, "已切换会话")
}

func TestPrivateSDKUserGraphWorkspaceAToBAndBack(t *testing.T) {
	a := filepath.Join(t.TempDir(), "a")
	b := filepath.Join(t.TempDir(), "b")
	if e := os.MkdirAll(a, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(b, 0700); e != nil {
		t.Fatal(e)
	}
	g := newGraphEnv(t, a, b)
	u := g.user
	before := len(u.gateway.snapshotOperations())
	u.send(g.app, "first-hi", "hi")
	u.waitVisible(g.app, before, "/list")
	u.chooseNew(g.app, "choose-a")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "a-first", "hello A")
	u.waitFinal(g.app, before, "a-first", "done-graph-a")
	u.cwd = b
	u.chooseNew(g.app, "choose-b")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "b-first", "hello B")
	u.waitFinal(g.app, before, "b-first", "done-graph-b")
	graphChooseExisting(u, g.app, "return-a", a, "graph-a")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "a-again", "hello A again")
	u.waitFinal(g.app, before, "a-again", "done-graph-a")
	trace, e := os.ReadFile(g.trace)
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range []string{"hello A", "hello B", "hello A again", a, b, "graph-a", "graph-b"} {
		if !strings.Contains(string(trace), v) {
			t.Fatalf("native trace missing %q", v)
		}
	}
	for _, expected := range []struct{ text, cwd, session string }{{"hello A", a, "graph-a"}, {"hello B", b, "graph-b"}, {"hello A again", a, "graph-a"}} {
		found := false
		for _, line := range strings.Split(string(trace), "\n") {
			if strings.HasPrefix(line, expected.cwd+" ") && strings.Contains(line, `"threadId":"`+expected.session+`"`) && strings.Contains(line, expected.text) && !(expected.text == "hello A" && strings.Contains(line, "hello A again")) {
				found = true
			}
		}
		if !found {
			t.Fatalf("native request text=%q did not use cwd=%q and thread=%q", expected.text, expected.cwd, expected.session)
		}
	}
	// Inspect durable provenance only after all three visible final replies;
	// it does not gate the next user action in the journey above.
	db, e := sql.Open("sqlite", "file:"+filepath.Join(g.app.headlessRuntime.Paths.StateDir, "private-chat.db")+"?mode=ro")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	for _, expected := range []struct{ source, session string }{{"a-first", "graph-a"}, {"b-first", "graph-b"}, {"a-again", "graph-a"}} {
		var command string
		e = db.QueryRowContext(u.ctx, `SELECT d.command_id FROM dispatch d JOIN dispatch_input di ON di.dispatch_id=d.id JOIN ingress i ON i.seq=di.seq WHERE i.event_id=?`, expected.source).Scan(&command)
		if e != nil || command == "" {
			t.Fatalf("source %s dispatch: %v", expected.source, e)
		}
		var session string
		e = db.QueryRowContext(u.ctx, `SELECT json_extract(payload,'$.threadId') FROM native_events WHERE command_id=? AND json_extract(payload,'$.kind')='native.turn.binding.observed' AND json_extract(payload,'$.status')='accepted'`, command).Scan(&session)
		if e != nil || session != expected.session {
			t.Fatalf("source %s session=%q want=%q: %v", expected.source, session, expected.session, e)
		}
	}
}
