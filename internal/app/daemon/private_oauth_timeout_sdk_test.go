package daemon

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPrivateOAuthTimeoutNativePeer(t *testing.T) {
	if os.Getenv("PRIVATE_OAUTH_TIMEOUT_PEER") != "1" {
		return
	}
	cwd, _ := os.Getwd()
	turn := 0
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var f struct {
			ID     string         `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal(scan.Bytes(), &f) != nil {
			continue
		}
		switch f.Method {
		case "initialize":
			fmt.Printf(`{"id":%q,"result":{}}`+"\n", f.ID)
		case "thread/start", "thread/resume":
			fmt.Printf(`{"id":%q,"result":{"model":"fixture-model","thread":{"id":"timeout-thread","cwd":%q}}}`+"\n", f.ID, cwd)
		case "turn/start":
			turn++
			id := fmt.Sprintf("timeout-turn-%d", turn)
			fmt.Printf(`{"id":%q,"result":{"turn":{"id":%q}}}`+"\n", f.ID, id)
			fmt.Printf(`{"method":"turn/started","params":{"threadId":"timeout-thread","turn":{"id":%q}}}`+"\n", id)
			fmt.Printf(`{"method":"item/completed","params":{"threadId":"timeout-thread","turnId":%q,"item":{"id":"answer","type":"agentMessage","text":"oauth timeout dialogue complete"}}}`+"\n", id)
			fmt.Printf(`{"method":"turn/completed","params":{"threadId":"timeout-thread","turn":{"id":%q,"status":"completed"}}}`+"\n", id)
		case "mcpServer/oauth/login":
			if out, err := os.OpenFile(os.Getenv("PRIVATE_OAUTH_TRACE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); err == nil {
				fmt.Fprintln(out, "login")
				_ = out.Close()
			}
			expected := os.Getenv("PRIVATE_OAUTH_EXPECT_THREAD")
			actual, _ := f.Params["threadId"].(string)
			if actual != expected {
				os.Exit(55)
			}
			if err := os.WriteFile(os.Getenv("PRIVATE_OAUTH_STARTED"), []byte("started"), 0600); err != nil {
				os.Exit(56)
			}
			// Deliberately leave this exact request unanswered.
		case "thread/read":
			fmt.Printf(`{"id":%q,"result":{"thread":{"id":"timeout-thread","cwd":%q,"status":{"type":"idle"}}}}`+"\n", f.ID, cwd)
		case "thread/goal/get":
			fmt.Printf(`{"id":%q,"result":{"goal":null}}`+"\n", f.ID)
		case "thread/list", "model/list":
			fmt.Printf(`{"id":%q,"result":{"data":[]}}`+"\n", f.ID)
		}
	}
	os.Exit(0)
}

func TestPrivateSDKOAuthObservationTimeoutDetachAndContinue(t *testing.T) {
	for _, scoped := range []bool{false, true} {
		t.Run(fmt.Sprintf("thread-scoped=%t", scoped), func(t *testing.T) {
			f := newContinuityFixtureBare(t, nil)
			phaseStart := time.Now()
			phase := func(name string) {
				deadline, _ := f.ctx.Deadline()
				t.Logf("phase=%s elapsed=%s remaining=%s", name, time.Since(phaseStart), time.Until(deadline))
			}
			t.Cleanup(func() {
				if !t.Failed() {
					return
				}
				statusResult := make(chan error, 1)
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					defer cancel()
					_, e := f.app.applications.processor.ReadRuntimeStatus(ctx)
					statusResult <- e
				}()
				select {
				case e := <-statusResult:
					t.Logf("processor status error=%v", e)
				case <-time.After(time.Second):
					t.Log("processor status lock did not return within diagnostic bound")
				}
				runner := f.app.applications.runner
				if runner.effectMu.TryLock() {
					t.Logf("runner active=%v normal=%d/%d control=%d/%d publish=%d/%d", runner.running, len(runner.normalSlots), cap(runner.normalSlots), len(runner.controlSlots), cap(runner.controlSlots), len(runner.publishSlots), cap(runner.publishSlots))
					runner.effectMu.Unlock()
				} else {
					t.Log("runner effect lock busy")
				}
				db, e := sql.Open("sqlite", "file:"+filepath.Join(f.app.headlessRuntime.Paths.StateDir, "private-chat.db")+"?mode=ro&_pragma=busy_timeout(5000)")
				if e != nil {
					t.Logf("diagnostic open: %v", e)
					return
				}
				defer db.Close()
				artifact, e := os.MkdirTemp("/tmp", "private-oauth-timeout-")
				if e == nil {
					_ = os.Chmod(artifact, 0700)
					stack := make([]byte, 2<<20)
					n := runtime.Stack(stack, true)
					_ = os.WriteFile(filepath.Join(artifact, "goroutines.txt"), stack[:n], 0600)
					if _, e = db.Exec(`VACUUM INTO ?`, filepath.Join(artifact, "private-chat.db")); e == nil {
						t.Logf("consistent diagnostic snapshot: %s", artifact)
					}
				}
				rows, e := db.Query(`SELECT d.id,d.state,e.state,coalesce(json_extract(e.receipt,'$.kind'),''),coalesce(r.expires_ns,0),coalesce(a.revision,0) FROM dispatch d JOIN native_commands n ON n.dispatch_id=d.id JOIN private_effect e ON e.id=substr(d.id,1,length(d.id)-7) LEFT JOIN private_read_observations r ON r.dispatch_id=d.id LEFT JOIN private_effect_applied a ON a.effect_id=e.id WHERE json_extract(n.payload,'$.kind')='mcp.oauth_login.start'`)
				if e == nil {
					defer rows.Close()
					for rows.Next() {
						var id, ds, es, kind string
						var expiry, revision int64
						if rows.Scan(&id, &ds, &es, &kind, &expiry, &revision) == nil {
							t.Logf("oauth dispatch=%s state=%s effect=%s receipt=%s deadline=%d applied=%d", id, ds, es, kind, expiry, revision)
						}
					}
				}
				var cp string
				if e = db.QueryRow(`SELECT data FROM private_checkpoint ORDER BY revision DESC LIMIT 1`).Scan(&cp); e == nil {
					t.Logf("checkpoint=%s", cp)
				}
				passResult := make(chan error, 1)
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					defer cancel()
					passResult <- runner.pass(ctx)
				}()
				select {
				case e := <-passResult:
					t.Logf("diagnostic runner pass error=%v", e)
				case <-time.After(3 * time.Second):
					t.Log("diagnostic runner pass did not complete")
				}
			})
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			started := filepath.Join(t.TempDir(), "started")
			trace := filepath.Join(t.TempDir(), "trace")
			expected := ""
			if scoped {
				expected = "timeout-thread"
			}
			script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3-dev.1\\n'; exit; fi\nPRIVATE_OAUTH_TIMEOUT_PEER=1 exec %q -test.run '^TestPrivateOAuthTimeoutNativePeer$'\n", exe)
			if err = os.WriteFile(f.app.headlessRuntime.CodexRealBinary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			f.app.headlessRuntime.BaseEnv = append(f.app.headlessRuntime.BaseEnv, "PRIVATE_OAUTH_STARTED="+started, "PRIVATE_OAUTH_EXPECT_THREAD="+expected, "PRIVATE_OAUTH_TRACE="+trace)
			installFixtureReadDeadline(t, f.app, "mcp.oauth_login.start", 5*time.Second)
			u := f.user
			n := len(f.gateway.snapshotOperations())
			u.send(f.app, "oauth-timeout-cold", "hi")
			u.waitVisible(f.app, n, "/list")
			u.chooseNew(f.app, "oauth-timeout-new")
			phase("workspace-ready")
			if scoped {
				n = len(f.gateway.snapshotOperations())
				u.send(f.app, "oauth-timeout-first", "hello")
				u.waitFinal(f.app, n, "oauth-timeout-first", "oauth timeout dialogue complete")
			}
			n = len(f.gateway.snapshotOperations())
			u.send(f.app, "oauth-timeout-login", "/mcpoauth docs")
			for {
				if _, e := os.Stat(started); e == nil {
					break
				}
				select {
				case <-f.ctx.Done():
					t.Fatal("oauth request not observed")
				case <-time.After(20 * time.Millisecond):
				}
			}
			phase("native-login-received")
			conversation := len(f.gateway.snapshotOperations())
			u.send(f.app, "oauth-timeout-during", "continue while login waits")
			u.waitFinal(f.app, conversation, "oauth-timeout-during", "oauth timeout dialogue complete")
			phase("ordinary-final")
			u.waitVisible(f.app, n, "原请求结果尚未确认")
			phase("timeout-visible")
			n = len(f.gateway.snapshotOperations())
			u.send(f.app, "oauth-timeout-detach", "/detach")
			u.waitVisible(f.app, n, "已解除")
			u.chooseNew(f.app, "oauth-timeout-reselect")
			n = len(f.gateway.snapshotOperations())
			u.send(f.app, "oauth-timeout-after", "continue")
			u.waitFinal(f.app, n, "oauth-timeout-after", "oauth timeout dialogue complete")
			data, e := os.ReadFile(trace)
			if e != nil || strings.Count(string(data), "login\n") != 1 {
				t.Fatalf("original login was sent more than once: %q, %v", data, e)
			}
		})
	}
}
