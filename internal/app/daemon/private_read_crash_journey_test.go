package daemon

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

// This fixture restarts the actual private application and reconnects its
// existing wrapper. Only the exact registry crash boundary and deadline are
// fault-injected; all business routing and effects originate from SDK input.
func TestPrivateSDKReadCrashMissingRegistryIntent(t *testing.T) {
	old, gateway, lock := newApplicationRuntimeTestAppWithLock(t)
	cwd := t.TempDir()
	other := t.TempDir()
	root := old.headlessRuntime.Paths.StateDir
	old.service.SetPersistedThreadCatalog(graphCatalog{cwd, other})
	setTestWorkBotSettings(t, old, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendCodex, CodexProfileID: state.NativeCodexProfileID})
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "codex")
	started := filepath.Join(t.TempDir(), "started")
	release := filepath.Join(t.TempDir(), "release")
	trace := filepath.Join(t.TempDir(), "reads")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3-dev.1\\n'; exit; fi\nPRIVATE_READ_CRASH_PEER=1 exec %q -test.run '^TestPrivateReadCrashNativePeer$'\n", exe)
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
	old.headlessRuntime.BaseEnv = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH"), "READ_EXPIRY_STARTED=" + started, "READ_EXPIRY_RELEASE=" + release, "READ_CRASH_TRACE=" + trace}
	old.headlessRuntime.CodexRealBinary = bin
	old.relay.SetServerIdentity(agentproto.ServerIdentity{BinaryIdentity: agentproto.BinaryIdentity{Product: relayruntime.ProductName, Version: "fixture", BuildFingerprint: "fixture"}})
	server := httptest.NewServer(old.relay)
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	var child *exec.Cmd
	old.applications.native.startHeadless = func(v relayruntime.HeadlessLaunchOptions) (int, error) {
		child = exec.Command(exe, "-test.run=^TestPrivateOpenCodeRelayFixtureProcess$")
		child.Env = append(v.Env, "PRIVATE_OPENCODE_FIXTURE=wrapper", "FIXTURE_RELAY_URL="+url, "FIXTURE_STATE_DIR="+root, "FIXTURE_CODEX_BIN="+bin)
		child.Dir = v.WorkDir
		child.Stdout = io.Discard
		child.Stderr = os.Stderr
		if e := child.Start(); e != nil {
			return 0, e
		}
		go func() { _ = child.Wait() }()
		return child.Process.Pid, nil
	}
	t.Cleanup(func() {
		server.Close()
		if child != nil && child.Process != nil {
			_ = child.Process.Kill()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	errs := make(chan error, 16)
	if err = old.applications.startWorkers(ctx, errs); err != nil {
		t.Fatal(err)
	}
	user := sdkJourneyUser{t: t, ctx: ctx, gateway: gateway, errors: errs, cwd: cwd}
	n := len(gateway.snapshotOperations())
	user.send(old, "crash-cold", "hi")
	user.waitVisible(old, n, "/list")
	user.chooseNew(old, "crash-new")
	n = len(gateway.snapshotOperations())
	user.send(old, "crash-first", "hello")
	user.waitFinal(old, n, "crash-first", "done-graph-a")
	n = len(gateway.snapshotOperations())
	user.send(old, "crash-goal", "/goal")
	for {
		if _, e := os.Stat(started); e == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("query never reached native")
		case <-time.After(20 * time.Millisecond):
		}
	}
	// Crash injection: the native request may have been written, but its registry
	// intent transaction is absent. Freeze an accelerated, still-future deadline.
	privateDB, err := sql.Open("sqlite", filepath.Join(root, "private-chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	var dispatch string
	if err = privateDB.QueryRow(`SELECT d.id FROM dispatch d JOIN native_commands n ON n.dispatch_id=d.id WHERE json_extract(n.payload,'$.kind')='thread.goal.get' ORDER BY d.rowid DESC LIMIT 1`).Scan(&dispatch); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second).UnixNano()
	result, err := privateDB.Exec(`UPDATE private_read_observations SET expires_ns=? WHERE dispatch_id=?`, deadline, dispatch)
	if err != nil {
		t.Fatal(err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		t.Fatal("observation missing")
	}
	_ = privateDB.Close()
	registry, err := sql.Open("sqlite", filepath.Join(root, "execution-owners.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = registry.Exec(`DELETE FROM session_read_intents WHERE operation_id=?`, dispatch); err != nil {
		t.Fatal(err)
	}
	_ = registry.Close()
	r := old.applications
	if err = r.stopWorkers(ctx); err != nil {
		t.Fatal(err)
	}
	addr := server.Listener.Addr().String()
	if err = old.relay.Close(); err != nil {
		t.Fatal(err)
	}
	server.Close()
	if err = r.drainNativeDeliveries(ctx); err != nil {
		t.Fatal(err)
	}
	if err = r.close(); err != nil {
		t.Fatal(err)
	}
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
	next.relay.SetServerIdentity(agentproto.ServerIdentity{BinaryIdentity: agentproto.BinaryIdentity{Product: relayruntime.ProductName, Version: "fixture", BuildFingerprint: "fixture"}})
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	newServer := httptest.NewUnstartedServer(next.relay)
	newServer.Listener = listener
	newServer.Start()
	defer newServer.Close()
	t.Cleanup(func() {
		stop, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = next.applications.stopWorkers(stop)
		_ = next.applications.native.Shutdown(stop)
		_ = next.applications.drainNativeDeliveries(stop)
		_ = next.applications.close()
		_ = next.teamRuntime.store.Close()
	})
	if err = next.applications.startWorkers(ctx, errs); err != nil {
		t.Fatal(err)
	}
	user.waitVisible(next, n, "等待 Goal 查询结果超时")
	n = len(gateway.snapshotOperations())
	user.send(next, "crash-retry", "/goal")
	user.waitVisible(next, n, "当前会话没有 Goal")
	n = len(gateway.snapshotOperations())
	if err = os.WriteFile(release, []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(root, "private-chat.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for {
		var count int
		if err = db.QueryRow(`SELECT count(*) FROM native_events WHERE json_extract(payload,'$.kind')='thread.goal.command.result' AND applied=1`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count >= 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("late result not consumed")
		case <-time.After(20 * time.Millisecond):
		}
	}
	for _, op := range gateway.snapshotOperations()[n:] {
		raw, _ := json.Marshal(op)
		if strings.Contains(string(raw), "当前会话没有 Goal") {
			t.Fatal("old result overwrote retry")
		}
	}
	var state string
	if err = db.QueryRow(`SELECT state FROM dispatch WHERE id=?`, dispatch).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state == "failed" {
		t.Fatal("unknown native request was recorded as unsent")
	}
	data, err := os.ReadFile(trace)
	if err != nil || strings.Count(string(data), "goal-read\n") != 2 {
		t.Fatalf("native queries were retried unexpectedly: %q, %v", data, err)
	}
	n = len(gateway.snapshotOperations())
	user.send(next, "crash-next", "continue")
	user.waitFinal(next, n, "crash-next", "done-graph-a")
}

func TestPrivateReadCrashNativePeer(t *testing.T) {
	if os.Getenv("PRIVATE_READ_CRASH_PEER") != "1" {
		return
	}
	cwd, _ := os.Getwd()
	count, turn := 0, 0
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var f struct {
			ID     string `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(scan.Bytes(), &f) != nil {
			continue
		}
		switch f.Method {
		case "initialize":
			fmt.Printf(`{"id":%q,"result":{}}`+"\n", f.ID)
		case "thread/start", "thread/resume":
			fmt.Printf(`{"id":%q,"result":{"model":"fixture-model","thread":{"id":"graph-a","cwd":%q}}}`+"\n", f.ID, cwd)
		case "turn/start":
			turn++
			id := fmt.Sprintf("turn-%d", turn)
			fmt.Printf(`{"id":%q,"result":{"turn":{"id":%q}}}`+"\n", f.ID, id)
			fmt.Printf(`{"method":"turn/started","params":{"threadId":"graph-a","turn":{"id":%q}}}`+"\n", id)
			fmt.Printf(`{"method":"item/completed","params":{"threadId":"graph-a","turnId":%q,"item":{"id":"answer","type":"agentMessage","text":"done-graph-a"}}}`+"\n", id)
			fmt.Printf(`{"method":"turn/completed","params":{"threadId":"graph-a","turn":{"id":%q,"status":"completed"}}}`+"\n", id)
		case "thread/goal/get":
			if out, e := os.OpenFile(os.Getenv("READ_CRASH_TRACE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); e == nil {
				fmt.Fprintln(out, "goal-read")
				out.Close()
			}
			count++
			if count == 1 {
				id := f.ID
				_ = os.WriteFile(os.Getenv("READ_EXPIRY_STARTED"), []byte("started"), 0600)
				go func() {
					for {
						if _, e := os.Stat(os.Getenv("READ_EXPIRY_RELEASE")); e == nil {
							fmt.Printf(`{"id":%q,"result":{"goal":null}}`+"\n", id)
							return
						}
						time.Sleep(10 * time.Millisecond)
					}
				}()
			} else {
				fmt.Printf(`{"id":%q,"result":{"goal":null}}`+"\n", f.ID)
			}
		case "thread/read":
			fmt.Printf(`{"id":%q,"result":{"thread":{"id":"graph-a","cwd":%q,"status":{"type":"idle"}}}}`+"\n", f.ID, cwd)
		case "thread/list", "model/list":
			fmt.Printf(`{"id":%q,"result":{"data":[]}}`+"\n", f.ID)
		}
	}
	os.Exit(0)
}
