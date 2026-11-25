package daemon

import (
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// Saturate only newly scheduled work. Existing jobs keep their captured slot
// channels and can finish while SDK ingress continues to be reduced.
func blockPreparedFixtureEffects(r *privateWorkRunner) func() {
	r.effectMu.Lock()
	if r.running == nil {
		r.running = make(map[string]struct{})
		r.normalSlots = make(chan struct{}, 8)
		r.controlSlots = make(chan struct{}, 4)
		r.publishSlots = make(chan struct{}, 8)
	}
	normal, publish := r.normalSlots, r.publishSlots
	blockedNormal, blockedPublish := make(chan struct{}, cap(normal)), make(chan struct{}, cap(publish))
	for i := 0; i < cap(blockedNormal); i++ {
		blockedNormal <- struct{}{}
	}
	for i := 0; i < cap(blockedPublish); i++ {
		blockedPublish <- struct{}{}
	}
	r.normalSlots, r.publishSlots = blockedNormal, blockedPublish
	r.effectMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() { r.effectMu.Lock(); r.normalSlots, r.publishSlots = normal, publish; r.effectMu.Unlock() })
	}
}

func TestPreparedFixtureSlotSwapLetsOlderEffectFinish(t *testing.T) {
	r := &privateWorkRunner{}
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	r.startEffect("old", "", false, false, func() { close(started); <-release; close(finished) })
	<-started
	restore := blockPreparedFixtureEffects(r)
	defer restore()
	called := make(chan struct{}, 1)
	r.startEffect("blocked", "", false, false, func() { called <- struct{}{} })
	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("older effect blocked by slot swap")
	}
	r.effectWG.Wait()
	select {
	case <-called:
		t.Fatal("saturated slot admitted a new effect")
	default:
	}
}

// Freeze a real SDK-created effect at the private-commit/registry-commit crash
// boundary, then let the ordinary worker recover it without a transport replay.
func TestPrivateSDKPreparedReadRecoversOrExpiresBeforeSend(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(map[bool]string{false: "recover", true: "expired"}[expired], func(t *testing.T) {
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
			u.send(g.app, "first", "hi")
			u.waitVisible(g.app, before, "/list")
			u.chooseNew(g.app, "workspace")
			before = len(u.gateway.snapshotOperations())
			u.send(g.app, "message", "hi")
			u.waitFinal(g.app, before, "message", "done-graph-a")
			r := g.app.applications
			restoreEffects := blockPreparedFixtureEffects(r.runner)
			defer restoreEffects()
			before = len(u.gateway.snapshotOperations())
			u.send(g.app, "goal", "/goal")
			db, err := sql.Open("sqlite", filepath.Join(g.app.headlessRuntime.Paths.StateDir, "private-chat.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var id string
			for {
				err = db.QueryRow(`SELECT id FROM private_effect WHERE CAST(body AS TEXT) LIKE '%thread.goal.get%' ORDER BY revision DESC LIMIT 1`).Scan(&id)
				if err == nil {
					break
				}
				if err != sql.ErrNoRows {
					t.Fatal(err)
				}
				select {
				case <-u.ctx.Done():
					t.Fatal("SDK goal effect missing")
				case <-time.After(20 * time.Millisecond):
				}
			}
			recs, err := r.native.store.byState(u.ctx, "running")
			if err != nil || len(recs) != 1 {
				t.Fatalf("carrier: %v %#v", err, recs)
			}
			lease, err := r.native.lease(u.ctx, recs[0], nil)
			if err != nil {
				t.Fatal(err)
			}
			binding := agentproto.NativeTurnBinding{Version: agentproto.PrivateNativeProtocolVersion, Owner: recs[0].Snapshot.Owner}
			dispatch, _, err := lease.PrepareEffectDispatch(u.ctx, id, binding)
			if err != nil {
				t.Fatal(err)
			}
			if expired {
				if _, err = db.Exec(`UPDATE private_read_observations SET expires_ns=? WHERE dispatch_id=?`, time.Now().Add(-time.Second).UnixNano(), dispatch.ID); err != nil {
					t.Fatal(err)
				}
			}
			restoreEffects()
			if expired {
				u.waitVisible(g.app, before, "会话查询在发送前已超时")
			} else {
				u.waitVisible(g.app, before, "当前会话没有 Goal")
			}
			var state string
			if err = db.QueryRow(`SELECT state FROM dispatch WHERE id=?`, dispatch.ID).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if expired && state != "failed" || !expired && state != "accepted" {
				t.Fatalf("dispatch state=%s", state)
			}
			wrapper, err := sql.Open("sqlite", "file:"+recs[0].Snapshot.JournalPath+"?mode=ro")
			if err != nil {
				t.Fatal(err)
			}
			defer wrapper.Close()
			var count int
			if err = wrapper.QueryRow(`SELECT count(*) FROM commands WHERE json_extract(payload,'$.kind')='thread.goal.get'`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if expired && count != 0 || !expired && count != 1 {
				t.Fatalf("native sends=%d", count)
			}
		})
	}
}
