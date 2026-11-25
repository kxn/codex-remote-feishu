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
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type continuityFixture struct {
	app     *App
	user    sdkJourneyUser
	gateway *messageIDAssigningGateway
	ctx     context.Context
	trace   string
	cwd     string
}

func newContinuityFixture(t *testing.T) *continuityFixture {
	t.Helper()
	f := newContinuityFixtureBare(t, nil)
	before := len(f.gateway.snapshotOperations())
	f.user.send(f.app, "cold-hi", "hi")
	f.user.waitVisible(f.app, before, "/list")
	f.user.chooseNew(f.app, "initial")
	return f
}

func newContinuityFixtureBare(t *testing.T, setup func(*App)) *continuityFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX wrapper")
	}
	app, gateway := newApplicationRuntimeTestApp(t)
	cwd := t.TempDir()
	app.service.SetPersistedThreadCatalog(newWorkspaceJourneyCatalog{cwd: cwd})
	setTestWorkBotSettings(t, app, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendCodex, CodexProfileID: state.NativeCodexProfileID})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	root := app.headlessRuntime.Paths.StateDir
	native := filepath.Join(t.TempDir(), "codex")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3-dev.1\\n'; exit; fi\nPRIVATE_CONTINUITY_NATIVE=1 exec %q -test.run '^TestPrivateContinuityNativeProcess$'\n", exe)
	if e = os.WriteFile(native, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	managed := filepath.Join(root, "codex-runtime")
	if e = os.MkdirAll(managed, 0700); e != nil {
		t.Fatal(e)
	}
	pin, _ := json.Marshal(map[string]string{"binary": native, "version": "0.159.3-dev.1"})
	if e = os.WriteFile(filepath.Join(managed, "exact-version.json"), pin, 0600); e != nil {
		t.Fatal(e)
	}
	trace := filepath.Join(t.TempDir(), "trace")
	app.headlessRuntime.BaseEnv = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH"), "FIXTURE_TRACE=" + trace}
	app.headlessRuntime.CodexRealBinary = native
	app.relay.SetServerIdentity(agentproto.ServerIdentity{BinaryIdentity: agentproto.BinaryIdentity{Product: relayruntime.ProductName, Version: "fixture", BuildFingerprint: "fixture"}})
	server := httptest.NewServer(app.relay)
	t.Cleanup(server.Close)
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	var mu sync.Mutex
	var children []*exec.Cmd
	var waits sync.WaitGroup
	app.applications.native.startHeadless = func(v relayruntime.HeadlessLaunchOptions) (int, error) {
		c := exec.Command(exe, "-test.run=^TestPrivateOpenCodeRelayFixtureProcess$")
		c.Env = append(v.Env, "PRIVATE_OPENCODE_FIXTURE=wrapper", "FIXTURE_RELAY_URL="+url, "FIXTURE_STATE_DIR="+root, "FIXTURE_CODEX_BIN="+native)
		c.Dir = v.WorkDir
		c.Stdout = io.Discard
		c.Stderr = os.Stderr
		if e := c.Start(); e != nil {
			return 0, e
		}
		mu.Lock()
		children = append(children, c)
		waits.Add(1)
		mu.Unlock()
		go func() { defer waits.Done(); _ = c.Wait() }()
		return c.Process.Pid, nil
	}
	t.Cleanup(func() {
		mu.Lock()
		for _, c := range children {
			if c.Process != nil {
				_ = c.Process.Kill()
			}
		}
		mu.Unlock()
		waits.Wait()
	})
	errs := make(chan error, 16)
	if setup != nil {
		setup(app)
	}
	if e = app.applications.startWorkers(ctx, errs); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		stop, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = app.applications.stopWorkers(stop)
		_ = app.applications.drainNativeDeliveries(stop)
	})
	u := sdkJourneyUser{t: t, ctx: ctx, gateway: gateway, errors: errs, cwd: cwd}
	return &continuityFixture{app: app, user: u, gateway: gateway, ctx: ctx, trace: trace, cwd: cwd}
}
func TestPrivateContinuityNativeProcess(t *testing.T) {
	if os.Getenv("PRIVATE_CONTINUITY_NATIVE") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	session := 0
	thread := ""
	turn := 0
	currentTurn := ""
	request := false
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		var req struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(line, &req) != nil {
			continue
		}
		if req.Method == "" && req.ID == "request-1" && request {
			if f, e := os.OpenFile(os.Getenv("FIXTURE_TRACE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); e == nil {
				_, _ = f.Write(append(line, '\n'))
				_ = f.Close()
			}
			fmt.Printf(`{"method":"serverRequest/resolved","params":{"requestId":"request-1","threadId":%q,"turnId":%q}}`+"\n", thread, currentTurn)
			fmt.Printf(`{"method":"item/completed","params":{"threadId":%q,"turnId":%q,"item":{"id":"approval","type":"agentMessage","text":"approval ended"}}}`+"\n", thread, currentTurn)
			fmt.Printf(`{"method":"turn/completed","params":{"threadId":%q,"turn":{"id":%q,"status":"completed"}}}`+"\n", thread, currentTurn)
			request = false
			continue
		}
		if req.Method == "turn/start" || req.Method == "turn/interrupt" || req.Method == "turn/steer" || req.Method == "thread/shellCommand" {
			if f, e := os.OpenFile(os.Getenv("FIXTURE_TRACE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); e == nil {
				_, _ = f.Write(append(line, '\n'))
				_ = f.Close()
			}
		}
		switch req.Method {
		case "initialize":
			fmt.Printf(`{"id":%q,"result":{}}`+"\n", req.ID)
		case "thread/start":
			session++
			thread = fmt.Sprintf("session-%d", session)
			cwd, _ := os.Getwd()
			fmt.Printf(`{"id":%q,"result":{"model":"fixture-model","thread":{"id":%q,"cwd":%q}}}`+"\n", req.ID, thread, cwd)
		case "thread/resume":
			cwd, _ := os.Getwd()
			fmt.Printf(`{"id":%q,"result":{"model":"fixture-model","thread":{"id":%q,"cwd":%q}}}`+"\n", req.ID, thread, cwd)
		case "turn/start":
			turn++
			currentTurn = fmt.Sprintf("turn-%d", turn)
			fmt.Printf(`{"id":%q,"result":{"turn":{"id":%q}}}`+"\n", req.ID, currentTurn)
			fmt.Printf(`{"method":"turn/started","params":{"threadId":%q,"turn":{"id":%q}}}`+"\n", thread, currentTurn)
			if strings.Contains(string(line), "ask-approval") {
				request = true
				fmt.Printf(`{"id":"request-1","method":"item/commandExecution/requestApproval","params":{"threadId":%q,"turnId":%q,"itemId":"cmd-1","reason":"fixture approval","command":"true","availableDecisions":["accept","decline","cancel"]}}`+"\n", thread, currentTurn)
				continue
			}
			if strings.Contains(string(line), "busy-turn") {
				fmt.Printf(`{"method":"item/completed","params":{"threadId":%q,"turnId":%q,"item":{"id":"progress","type":"agentMessage","text":"busy-visible"}}}`+"\n", thread, currentTurn)
				continue
			}
			fmt.Printf(`{"method":"item/completed","params":{"threadId":%q,"turnId":%q,"item":{"id":"answer","type":"agentMessage","text":"continuity-final"}}}`+"\n", thread, currentTurn)
			fmt.Printf(`{"method":"turn/completed","params":{"threadId":%q,"turn":{"id":%q,"status":"completed"}}}`+"\n", thread, currentTurn)
		case "turn/interrupt":
			fmt.Printf(`{"id":%q,"result":{}}`+"\n", req.ID)
			fmt.Printf(`{"method":"turn/completed","params":{"threadId":%q,"turn":{"id":%q,"status":"interrupted"}}}`+"\n", thread, currentTurn)
		case "turn/steer":
			fmt.Printf(`{"id":%q,"result":{"turnId":%q}}`+"\n", req.ID, currentTurn)
		case "thread/shellCommand":
			fmt.Printf(`{"id":%q,"result":{}}`+"\n", req.ID)
			fmt.Printf(`{"method":"item/completed","params":{"threadId":%q,"turnId":%q,"item":{"id":"shell-item","type":"commandExecution","source":"userShell","command":"echo shell","status":"completed","exitCode":0,"aggregatedOutput":"shell done"}}}`+"\n", thread, currentTurn)
		case "thread/read":
			cwd, _ := os.Getwd()
			fmt.Printf(`{"id":%q,"result":{"thread":{"id":%q,"cwd":%q,"status":{"type":"idle"}}}}`+"\n", req.ID, thread, cwd)
		case "thread/goal/get":
			fmt.Printf(`{"id":%q,"result":{"goal":null}}`+"\n", req.ID)
		case "thread/list", "model/list":
			fmt.Printf(`{"id":%q,"result":{"data":[]}}`+"\n", req.ID)
		}
	}
	os.Exit(0)
}
