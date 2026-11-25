package daemon

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
			u := f.user
			n := len(f.gateway.snapshotOperations())
			u.send(f.app, "oauth-timeout-cold", "hi")
			u.waitVisible(f.app, n, "/list")
			u.chooseNew(f.app, "oauth-timeout-new")
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
			conversation := len(f.gateway.snapshotOperations())
			u.send(f.app, "oauth-timeout-during", "continue while login waits")
			u.waitFinal(f.app, conversation, "oauth-timeout-during", "oauth timeout dialogue complete")
			db, err := sql.Open("sqlite", "file:"+filepath.Join(f.app.headlessRuntime.Paths.StateDir, "private-chat.db")+"?_pragma=busy_timeout(5000)")
			if err != nil {
				t.Fatal(err)
			}
			var dispatch string
			if err = db.QueryRow(`SELECT d.id FROM dispatch d JOIN native_commands n ON n.dispatch_id=d.id WHERE json_extract(n.payload,'$.kind')='mcp.oauth_login.start' ORDER BY d.rowid DESC LIMIT 1`).Scan(&dispatch); err != nil {
				t.Fatal(err)
			}
			expires := time.Now().Add(-time.Second).UnixNano()
			result, err := db.Exec(`UPDATE private_read_observations SET expires_ns=? WHERE dispatch_id=?`, expires, dispatch)
			_ = db.Close()
			if err != nil {
				t.Fatal(err)
			}
			if count, _ := result.RowsAffected(); count != 1 {
				t.Fatal("exact OAuth observation missing")
			}
			if scoped {
				registry, e := sql.Open("sqlite", "file:"+filepath.Join(f.app.headlessRuntime.Paths.StateDir, "execution-owners.db")+"?_pragma=busy_timeout(5000)")
				if e != nil {
					t.Fatal(e)
				}
				result, e = registry.Exec(`UPDATE session_read_intents SET expires_ns=? WHERE operation_id=?`, expires, dispatch)
				_ = registry.Close()
				if e != nil {
					t.Fatal(e)
				}
				if count, _ := result.RowsAffected(); count != 1 {
					t.Fatal("exact OAuth intent missing")
				}
			}
			u.waitVisible(f.app, n, "原请求结果尚未确认")
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
