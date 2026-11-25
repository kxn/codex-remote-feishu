package daemon

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
)

func TestPrivateSDKSelectedSessionCannotCompactAfterForeignClaim(t *testing.T) {
	a := filepath.Join(t.TempDir(), "workspace")
	b := filepath.Join(t.TempDir(), "other")
	if err := os.MkdirAll(a, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(b, 0700); err != nil {
		t.Fatal(err)
	}
	g := newGraphEnv(t, a, b)
	u := g.user
	before := len(u.gateway.snapshotOperations())
	u.send(g.app, "hello", "hi")
	u.waitVisible(g.app, before, "/list")
	u.chooseNew(g.app, "workspace")
	graphChooseExisting(u, g.app, "select", a, "graph-a")
	recs, err := g.app.applications.native.store.byState(u.ctx, "running")
	if err != nil || len(recs) != 1 {
		t.Fatalf("carriers=%d err=%v", len(recs), err)
	}
	n := recs[0].Snapshot.Owner
	foreign := executionowner.Registration{Carrier: executionowner.Carrier{InstanceID: "foreign-instance", Incarnation: "foreign-incarnation", Backend: string(n.Backend), Namespace: n.HistoryNamespace, Generation: 1}, Owner: executionowner.Owner{Application: executionowner.Team, ID: "foreign-owner", Epoch: 1}}
	registry := g.app.applications.native.registry
	if err = registry.Register(u.ctx, foreign); err != nil {
		t.Fatal(err)
	}
	if err = registry.BindSession(u.ctx, foreign, "graph-a"); err != nil {
		t.Fatalf("selected historical session already claimed: %v", err)
	}
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "compact-foreign", "/compact")
	db, err := sql.Open("sqlite", "file:"+filepath.Join(g.app.headlessRuntime.Paths.StateDir, "private-chat.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		var state string
		err = db.QueryRow(`SELECT d.state FROM native_commands n JOIN dispatch d ON d.id=n.dispatch_id WHERE json_extract(n.payload,'$.kind')='thread.compact.start'`).Scan(&state)
		if err != nil && err != sql.ErrNoRows {
			t.Fatal(err)
		}
		if state == "failed" {
			break
		}
		if state == "sending" || state == "accepted" || state == "unknown" {
			t.Fatalf("foreign session reached native compact dispatch: %s", state)
		}
		select {
		case <-deadline.C:
			t.Fatal("foreign session failure was not settled")
		case <-time.After(20 * time.Millisecond):
		}
	}
	u.waitVisible(g.app, before, "该会话当前由其他运行环境管理")
	childDB, err := sql.Open("sqlite", "file:"+recs[0].Snapshot.JournalPath+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer childDB.Close()
	var sent int
	if err = childDB.QueryRow(`SELECT count(*) FROM commands WHERE json_extract(payload,'$.kind')='thread.compact.start'`).Scan(&sent); err != nil || sent != 0 {
		t.Fatalf("foreign command sent=%d err=%v", sent, err)
	}
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "goal-foreign", "/goal")
	u.waitVisible(g.app, before, "该会话当前由其他运行环境管理")
	if err = childDB.QueryRow(`SELECT count(*) FROM commands WHERE json_extract(payload,'$.kind')='thread.goal.get'`).Scan(&sent); err != nil || sent != 0 {
		t.Fatalf("foreign goal sent=%d err=%v", sent, err)
	}
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "oauth-foreign", "/mcpoauth docs")
	u.waitVisible(g.app, before, "该会话当前由其他运行环境管理")
	if err = childDB.QueryRow(`SELECT count(*) FROM commands WHERE json_extract(payload,'$.kind')='mcp.oauth_login.start'`).Scan(&sent); err != nil || sent != 0 {
		t.Fatalf("foreign thread OAuth sent=%d err=%v", sent, err)
	}
	u.cwd = b
	u.chooseNew(g.app, "safe-workspace")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "safe-message", "hello B")
	u.waitFinal(g.app, before, "safe-message", "done-graph-b")
}
