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
	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestPrivateSDKInterruptedShutdownThenSameWorkspaceNewSession(t *testing.T) {
	testPrivateSDKInterruptedShutdownRecovery(t, false, false, false)
}
func TestPrivateSDKInterruptedShutdownThenDirectMessageRecovery(t *testing.T) {
	testPrivateSDKInterruptedShutdownRecovery(t, true, false, false)
}
func TestPrivateSDKInterruptedUnknownTurnCanDetachAndStartNew(t *testing.T) {
	testPrivateSDKInterruptedShutdownRecovery(t, false, true, false)
}
func TestPrivateSDKCleanShutdownThenDirectMessageRecovery(t *testing.T) {
	testPrivateSDKInterruptedShutdownRecovery(t, true, false, true)
}
func testPrivateSDKInterruptedShutdownRecovery(t *testing.T, direct, unknown, clean bool) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX controlled native")
	}
	old, gateway, lock := newApplicationRuntimeTestAppWithLock(t)
	cwd := t.TempDir()
	old.service.SetPersistedThreadCatalog(existingWorkspaceJourneyCatalog{cwd: cwd})
	setTestWorkBotSettings(t, old, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendCodex, CodexProfileID: state.NativeCodexProfileID})
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := old.headlessRuntime.Paths.StateDir
	native := filepath.Join(t.TempDir(), "codex")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3-dev.1\\n'; exit; fi\nPRIVATE_DRAIN_CODEX=1 exec %q -test.run '^TestPrivateDrainControlledNativeProcess$'\n", exe)
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
	gate := filepath.Join(t.TempDir(), "inspection-gate")
	old.headlessRuntime.BaseEnv = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH"), "FIXTURE_TRACE=" + trace, "INSPECTION_GATE=" + gate}
	old.headlessRuntime.CodexRealBinary = native
	var mu sync.Mutex
	var children []*exec.Cmd
	var waits sync.WaitGroup
	startServer := func(app *App) *httptest.Server {
		app.relay.SetServerIdentity(agentproto.ServerIdentity{BinaryIdentity: agentproto.BinaryIdentity{Product: relayruntime.ProductName, Version: "fixture", BuildFingerprint: "fixture"}})
		server := httptest.NewServer(app.relay)
		url := "ws" + strings.TrimPrefix(server.URL, "http")
		app.applications.native.startHeadless = func(v relayruntime.HeadlessLaunchOptions) (int, error) {
			c := exec.Command(exe, "-test.run=^TestPrivateOpenCodeRelayFixtureProcess$")
			mu.Lock()
			ordinal := len(children)
			mu.Unlock()
			c.Env = append(v.Env, "PRIVATE_OPENCODE_FIXTURE=wrapper", "FIXTURE_RELAY_URL="+url, "FIXTURE_STATE_DIR="+root, "FIXTURE_CODEX_BIN="+native, fmt.Sprintf("FIXTURE_NATIVE_ORDINAL=%d", ordinal))
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
		return server
	}
	defer func() {
		_ = os.WriteFile(gate+".release", []byte("release"), 0600)
		mu.Lock()
		for _, c := range children {
			if c.Process != nil {
				_ = c.Process.Kill()
			}
		}
		mu.Unlock()
		waits.Wait()
	}()
	server := startServer(old)
	defer server.Close()
	r := old.applications
	errs := make(chan error, 16)
	if err = r.startWorkers(ctx, errs); err != nil {
		t.Fatal(err)
	}
	user := sdkJourneyUser{t: t, ctx: ctx, gateway: gateway, errors: errs, cwd: cwd}
	n0 := len(gateway.snapshotOperations())
	user.send(old, "cold-hi", "hi")
	user.waitVisible(old, n0, "/list")
	user.chooseNew(old, "first")
	n := len(gateway.snapshotOperations())
	if unknown {
		user.send(old, "unknown-active", "unknown-active")
		for {
			if _, e := os.Stat(gate + ".accepted"); e == nil {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("native did not accept controlled unknown turn")
			case <-time.After(20 * time.Millisecond):
			}
		}
	} else {
		user.send(old, "first-hi", "hi")
		user.waitFinal(old, n, "first-hi", "done")
		if !clean {
			n = len(gateway.snapshotOperations())
			user.send(old, "first-new", "/new")
			user.waitVisible(old, n, "下一条文本")
		}
	}
	var oldCarrier privateCarrierRecord
	if clean {
		shutdownCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err = old.shutdownApplications(shutdownCtx); err != nil {
			t.Fatalf("clean shutdown: %v", err)
		}
	} else {
		if err = os.WriteFile(gate, []byte("hold"), 0600); err != nil {
			t.Fatal(err)
		}
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer shutdownCancel()
		stopped := make(chan error, 1)
		go func() { stopped <- old.shutdownApplications(shutdownCtx) }()
		for !unknown {
			if _, e := os.Stat(gate + ".started"); e == nil {
				break
			}
			select {
			case e := <-stopped:
				t.Fatalf("shutdown returned before inspection gated: %v", e)
			case <-ctx.Done():
				t.Fatal("inspection did not reach controlled native")
			case <-time.After(20 * time.Millisecond):
			}
		}
		if err = <-stopped; err == nil {
			t.Fatal("gated shutdown unexpectedly succeeded")
		}
		db, e := sql.Open("sqlite", "file:"+filepath.Join(root, "private-carriers.db")+"?mode=ro")
		if e != nil {
			t.Fatal(e)
		}
		var draining int
		e = db.QueryRow(`SELECT count(*) FROM carriers WHERE state='draining'`).Scan(&draining)
		var frozen []byte
		if e == nil && draining > 0 {
			e = db.QueryRow(`SELECT record FROM carriers WHERE state='draining' LIMIT 1`).Scan(&frozen)
		}
		db.Close()
		if e != nil || draining == 0 {
			t.Fatalf("shutdown did not preserve draining carrier: %d %v", draining, e)
		}
		if err = os.WriteFile(gate+".release", []byte("release"), 0600); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		firstChild := children[0]
		mu.Unlock()
		_ = firstChild.Process.Kill()
		waits.Wait()
		if e = json.Unmarshal(frozen, &oldCarrier); e != nil {
			t.Fatal(e)
		}
		logControlledDrainInventory(t, oldCarrier.Snapshot.JournalPath, oldCarrier.Process)
	}
	server.Close()
	snapshot, err := captureTeamLegacyFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	next := New("127.0.0.1:0", "127.0.0.1:0", gateway, agentproto.ServerIdentity{})
	if err = next.prepareApplicationCore(); err != nil {
		t.Fatal(err)
	}
	next.SetHeadlessRuntime(old.headlessRuntime)
	next.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: old.headlessRuntime.Paths.ConfigFile, Services: defaultFeishuServices()})
	if err = next.configureWorkRuntime(ctx, teamPrivateCommands{app: next}, next.authorizeTeamControl); err != nil {
		t.Fatal(err)
	}
	if err = next.activateWorkRuntime(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if err = next.configureApplicationIngress(ctx, feishu.NewMultiGatewayController(), lock); err != nil {
		t.Fatal(err)
	}
	next.service.SetPersistedThreadCatalog(existingWorkspaceJourneyCatalog{cwd: cwd})
	server2 := startServer(next)
	defer server2.Close()
	if err = next.applications.startWorkers(ctx, errs); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stop, c := context.WithTimeout(context.Background(), 8*time.Second)
		defer c()
		_ = next.shutdownApplications(stop)
	}()
	if unknown {
		n = len(gateway.snapshotOperations())
		user.send(next, "detach-unknown", "/detach")
		user.waitVisible(next, n, "已退出旧会话")
	}
	if !direct {
		user.chooseNew(next, "second")
	}
	n = len(gateway.snapshotOperations())
	user.send(next, "after-ready-hi", "hi")
	user.waitFinal(next, n, "after-ready-hi", "done")
	n = len(gateway.snapshotOperations())
	user.send(next, "after-ready-next", "second turn")
	user.waitFinal(next, n, "after-ready-next", "done")
	if clean {
		data, e := os.ReadFile(trace)
		if e != nil {
			t.Fatal(e)
		}
		var sessions []string
		for _, line := range strings.Split(string(data), "\n") {
			var frame struct {
				Method string `json:"method"`
				Params struct {
					ThreadID string `json:"threadId"`
				} `json:"params"`
			}
			if json.Unmarshal([]byte(line), &frame) == nil && frame.Method == "turn/start" {
				sessions = append(sessions, frame.Params.ThreadID)
			}
		}
		if len(sessions) != 3 || sessions[0] == "" || sessions[1] != sessions[0] || sessions[2] != sessions[0] {
			t.Fatalf("clean restart did not continue the same session: %v", sessions)
		}
	}
	if unknown {
		data, e := os.ReadFile(trace)
		if e != nil {
			t.Fatal(e)
		}
		if strings.Count(string(data), "unknown-active") != 1 {
			t.Fatalf("unknown old input was replayed: %s", data)
		}
		owner := privatechat.Owner{GatewayID: "gateway", AppID: "app", TenantID: "tenant", ChatID: "private-chat", UserID: "user", SurfaceID: gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user")}
		cp, e := next.applications.journal.LoadCheckpoint(ctx, owner, 1)
		if e != nil {
			t.Fatal(e)
		}
		preserved := false
		for _, detached := range cp.Core.DetachedExecutions {
			if detached.InstanceID != oldCarrier.Snapshot.Owner.InstanceID {
				continue
			}
			if item := cp.Core.QueueItems[detached.ActiveQueueItemID]; item != nil && item.SourceMessageID == "unknown-active" {
				preserved = true
			}
		}
		if !preserved {
			t.Fatal("unknown old input did not remain attached to its detached execution")
		}
		journal, e := sql.Open("sqlite", "file:"+filepath.Join(root, "private-chat.db")+"?mode=ro")
		if e != nil {
			t.Fatal(e)
		}
		defer journal.Close()
		for {
			var applied int
			e = journal.QueryRowContext(ctx, `SELECT count(*) FROM private_effect e JOIN private_effect_applied a ON a.effect_id=e.id WHERE json_extract(e.body,'$.event.DaemonCommand.Kind')='headless.kill' AND json_extract(e.body,'$.event.DaemonCommand.InstanceID')=?`, oldCarrier.Snapshot.Owner.InstanceID).Scan(&applied)
			if e != nil {
				t.Fatal(e)
			}
			if applied > 0 {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("old carrier kill receipt was not applied")
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
}

func TestPrivateDrainControlledNativeProcess(t *testing.T) {
	if os.Getenv("PRIVATE_DRAIN_CODEX") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	turn := 0
	thread := "session-" + os.Getenv("FIXTURE_NATIVE_ORDINAL")
	for scanner.Scan() {
		line := scanner.Bytes()
		var req struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
			Params struct {
				ThreadID string `json:"threadId"`
			} `json:"params"`
		}
		if json.Unmarshal(line, &req) != nil {
			continue
		}
		switch req.Method {
		case "initialize":
			fmt.Printf(`{"id":%q,"result":{}}`+"\n", req.ID)
		case "thread/start", "thread/resume":
			if req.Method == "thread/resume" && req.Params.ThreadID != "" {
				thread = req.Params.ThreadID
			}
			cwd, _ := os.Getwd()
			fmt.Printf(`{"id":%q,"result":{"model":"fixture-model","thread":{"id":%q,"cwd":%q}}}`+"\n", req.ID, thread, cwd)
		case "turn/start":
			if f, e := os.OpenFile(os.Getenv("FIXTURE_TRACE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); e == nil {
				_, _ = f.Write(append(append([]byte(nil), line...), '\n'))
				_ = f.Close()
			}
			turn++
			id := fmt.Sprintf("turn-%d", turn)
			fmt.Printf(`{"id":%q,"result":{"turn":{"id":%q}}}`+"\n", req.ID, id)
			fmt.Printf(`{"method":"turn/started","params":{"threadId":%q,"turn":{"id":%q}}}`+"\n", thread, id)
			if strings.Contains(string(line), "unknown-active") {
				_ = os.WriteFile(os.Getenv("INSPECTION_GATE")+".accepted", []byte("accepted"), 0600)
				continue
			}
			fmt.Printf(`{"method":"item/completed","params":{"threadId":%q,"turnId":%q,"item":{"id":"answer","type":"agentMessage","text":"done"}}}`+"\n", thread, id)
			fmt.Printf(`{"method":"turn/completed","params":{"threadId":%q,"turn":{"id":%q,"status":"completed"}}}`+"\n", thread, id)
		case "thread/read":
			gate := os.Getenv("INSPECTION_GATE")
			if _, e := os.Stat(gate); e == nil {
				_ = os.WriteFile(gate+".started", []byte("started"), 0600)
				for {
					if _, e = os.Stat(gate + ".release"); e == nil {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			cwd, _ := os.Getwd()
			fmt.Printf(`{"id":%q,"result":{"thread":{"id":%q,"cwd":%q,"status":{"type":"idle"}}}}`+"\n", req.ID, thread, cwd)
		case "thread/goal/get":
			fmt.Printf(`{"id":%q,"result":{"goal":null}}`+"\n", req.ID)
		case "thread/list", "model/list":
			fmt.Printf(`{"id":%q,"result":{"data":[]}}`+"\n", req.ID)
		case "turn/interrupt":
			fmt.Printf(`{"id":%q,"result":{}}`+"\n", req.ID)
		}
	}
	os.Exit(0)
}
