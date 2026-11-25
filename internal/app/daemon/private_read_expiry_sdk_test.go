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

func TestPrivateReadExpiryNativePeer(t *testing.T) {
	if os.Getenv("PRIVATE_READ_EXPIRY_PEER") != "1" {
		return
	}
	cwd, _ := os.Getwd()
	count := 0
	turn := 0
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var frame struct {
			ID     string `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(scan.Bytes(), &frame) != nil {
			continue
		}
		switch frame.Method {
		case "initialize":
			fmt.Printf(`{"id":%q,"result":{}}`+"\n", frame.ID)
		case "thread/start", "thread/resume":
			fmt.Printf(`{"id":%q,"result":{"model":"fixture-model","thread":{"id":"graph-a","cwd":%q}}}`+"\n", frame.ID, cwd)
		case "turn/start":
			turn++
			id := fmt.Sprintf("turn-%d", turn)
			fmt.Printf(`{"id":%q,"result":{"turn":{"id":%q}}}`+"\n", frame.ID, id)
			fmt.Printf(`{"method":"turn/started","params":{"threadId":"graph-a","turn":{"id":%q}}}`+"\n", id)
			fmt.Printf(`{"method":"item/completed","params":{"threadId":"graph-a","turnId":%q,"item":{"id":"answer","type":"agentMessage","text":"done-graph-a"}}}`+"\n", id)
			fmt.Printf(`{"method":"turn/completed","params":{"threadId":"graph-a","turn":{"id":%q,"status":"completed"}}}`+"\n", id)
		case "thread/goal/get":
			count++
			if count == 1 {
				id := frame.ID
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
				fmt.Printf(`{"id":%q,"result":{"goal":null}}`+"\n", frame.ID)
			}
		case "thread/read":
			fmt.Printf(`{"id":%q,"result":{"thread":{"id":"graph-a","cwd":%q,"status":{"type":"idle"}}}}`+"\n", frame.ID, cwd)
		case "thread/list", "model/list":
			fmt.Printf(`{"id":%q,"result":{"data":[]}}`+"\n", frame.ID)
		}
	}
	os.Exit(0)
}

// The worker, not a direct runner call, observes the frozen deadline. The
// native response is intentionally held until after a second user query.
func TestPrivateSDKReadExpiryRetriesAndConsumesLateResult(t *testing.T) {
	a := filepath.Join(t.TempDir(), "workspace")
	b := filepath.Join(t.TempDir(), "other")
	if err := os.MkdirAll(a, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(b, 0700); err != nil {
		t.Fatal(err)
	}
	started, release := filepath.Join(t.TempDir(), "started"), filepath.Join(t.TempDir(), "release")
	g := newGraphEnv(t, a, b, func(app *App, _ *messageIDAssigningGateway) {
		exe, e := os.Executable()
		if e != nil {
			t.Fatal(e)
		}
		script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3-dev.1\\n'; exit; fi\nPRIVATE_READ_EXPIRY_PEER=1 exec %q -test.run '^TestPrivateReadExpiryNativePeer$'\n", exe)
		if e = os.WriteFile(app.headlessRuntime.CodexRealBinary, []byte(script), 0700); e != nil {
			t.Fatal(e)
		}
		app.headlessRuntime.BaseEnv = append(app.headlessRuntime.BaseEnv, "READ_EXPIRY_STARTED="+started, "READ_EXPIRY_RELEASE="+release)
	})
	u := g.user
	before := len(u.gateway.snapshotOperations())
	u.send(g.app, "first", "hi")
	u.waitVisible(g.app, before, "/list")
	u.chooseNew(g.app, "workspace")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "message", "hi")
	u.waitFinal(g.app, before, "message", "done-graph-a")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "goal-first", "/goal")
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		select {
		case <-u.ctx.Done():
			t.Fatal("first native goal query not observed")
		case <-time.After(20 * time.Millisecond):
		}
	}
	db, err := sql.Open("sqlite", filepath.Join(g.app.headlessRuntime.Paths.StateDir, "execution-owners.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	deadline := time.Now().Add(-time.Second).UnixNano()
	privateWrite, err := sql.Open("sqlite", filepath.Join(g.app.headlessRuntime.Paths.StateDir, "private-chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = privateWrite.Exec(`UPDATE private_read_observations SET expires_ns=?`, deadline); err != nil {
		t.Fatal(err)
	}
	privateWrite.Close()
	result, err := db.Exec(`UPDATE session_read_intents SET expires_ns=? WHERE session_id=?`, deadline, "graph-a")
	if err != nil {
		t.Fatal(err)
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		t.Fatalf("expected one frozen read intent, got %d", n)
	}
	u.waitVisible(g.app, before, "等待 Goal 查询结果超时")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "goal-retry", "/goal")
	u.waitVisible(g.app, before, "当前会话没有 Goal")
	before = len(u.gateway.snapshotOperations())
	if err = os.WriteFile(release, []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	privateDB, err := sql.Open("sqlite", "file:"+filepath.Join(g.app.headlessRuntime.Paths.StateDir, "private-chat.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer privateDB.Close()
	for {
		var count int
		err = privateDB.QueryRow(`SELECT count(*) FROM native_events WHERE json_extract(payload,'$.kind')='thread.goal.command.result' AND applied=1`).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count >= 2 {
			break
		}
		select {
		case <-u.ctx.Done():
			t.Fatal("late native result not consumed")
		case <-time.After(20 * time.Millisecond):
		}
	}
	for _, op := range u.gateway.snapshotOperations()[before:] {
		raw, _ := json.Marshal(op)
		if strings.Contains(string(raw), "当前会话没有 Goal") {
			t.Fatal("late first result repainted the retry")
		}
	}
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "after", "hi again")
	u.waitFinal(g.app, before, "after", "done-graph-a")
}
