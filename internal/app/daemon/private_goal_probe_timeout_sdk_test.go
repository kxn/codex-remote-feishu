package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPrivateSDKGoalProbeTimeoutRetryDispatchesOriginalInput(t *testing.T) {
	for _, ackFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("ack-first=%t", ackFirst), func(t *testing.T) { runGoalProbeTimeoutJourney(t, ackFirst) })
	}
}

func runGoalProbeTimeoutJourney(t *testing.T, ackFirst bool) {
	f := newContinuityFixtureBare(t, nil)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	started, release, trace := filepath.Join(t.TempDir(), "started"), filepath.Join(t.TempDir(), "release"), filepath.Join(t.TempDir(), "trace")
	methods := filepath.Join(t.TempDir(), "methods")
	resumeStarted, resumeRelease, resumeSent := filepath.Join(t.TempDir(), "resume-started"), filepath.Join(t.TempDir(), "resume-release"), filepath.Join(t.TempDir(), "resume-sent")
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		b, _ := os.ReadFile(methods)
		t.Logf("native methods: %s", b)
		status, e := f.app.applications.processor.ReadRuntimeStatus(context.Background())
		statusJSON, _ := json.Marshal(status)
		t.Logf("runtime: %s %v", statusJSON, e)
		db, e := sql.Open("sqlite", "file:"+filepath.Join(f.app.headlessRuntime.Paths.StateDir, "private-chat.db")+"?mode=ro")
		if e != nil {
			return
		}
		defer db.Close()
		artifact, e := os.MkdirTemp("/tmp", "private-goal-probe-")
		if e == nil {
			_ = os.Chmod(artifact, 0700)
			if _, e = db.Exec(`VACUUM INTO ?`, filepath.Join(artifact, "private-chat.db")); e == nil {
				t.Logf("consistent diagnostic snapshot: %s", artifact)
			} else {
				t.Logf("snapshot error: %v", e)
			}
		}
		rows, e := db.Query(`SELECT json_extract(n.payload,'$.kind'),d.state FROM dispatch d JOIN native_commands n ON n.dispatch_id=d.id ORDER BY d.rowid`)
		if e == nil {
			defer rows.Close()
			for rows.Next() {
				var kind, state string
				if rows.Scan(&kind, &state) == nil {
					t.Logf("dispatch kind=%s state=%s", kind, state)
				}
			}
		}
		rows, e = db.Query(`SELECT sequence,json_extract(payload,'$.kind'),coalesce(json_extract(payload,'$.commandId'),''),applied FROM native_events ORDER BY sequence`)
		if e == nil {
			defer rows.Close()
			for rows.Next() {
				var seq, applied int
				var kind, command string
				if rows.Scan(&seq, &kind, &command, &applied) == nil {
					t.Logf("native seq=%d kind=%s command=%s applied=%d", seq, kind, command, applied)
				}
			}
		}
		rows, e = db.Query(`SELECT event_id,lane,seq IN (SELECT seq FROM private_applied) FROM ingress ORDER BY seq`)
		if e == nil {
			defer rows.Close()
			for rows.Next() {
				var id, lane string
				var applied int
				if rows.Scan(&id, &lane, &applied) == nil {
					t.Logf("ingress id=%s lane=%s applied=%d", id, lane, applied)
				}
			}
		}
	})
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3-dev.1\\n'; exit; fi\nPRIVATE_GOAL_STRICT=1 PRIVATE_GOAL_PROBE=1 exec %q -test.run '^TestPrivateGoalStrictNativePeer$'\n", exe)
	if err = os.WriteFile(f.app.headlessRuntime.CodexRealBinary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	f.app.headlessRuntime.BaseEnv = append(f.app.headlessRuntime.BaseEnv, "PRIVATE_GOAL_PROBE_STARTED="+started, "PRIVATE_GOAL_PROBE_RELEASE="+release, "PRIVATE_GOAL_PROBE_TRACE="+trace, "PRIVATE_GOAL_PROBE_METHODS="+methods, "PRIVATE_GOAL_RESUME_STARTED="+resumeStarted, "PRIVATE_GOAL_RESUME_RELEASE="+resumeRelease, "PRIVATE_GOAL_RESUME_SENT="+resumeSent)
	u := f.user
	n := len(f.gateway.snapshotOperations())
	u.send(f.app, "probe-cold", "hi")
	u.waitVisible(f.app, n, "/list")
	u.chooseNew(f.app, "probe-new")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "probe-establish", "hello")
	u.waitFinal(f.app, n, "probe-establish", "goal dialogue complete")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "probe-goal", "/goal new fixture goal")
	u.waitVisible(f.app, n, "状态：active")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "probe-waiting", "original user request")
	for {
		if _, e := os.Stat(started); e == nil {
			break
		}
		select {
		case <-f.ctx.Done():
			t.Fatal("goal probe did not reach native")
		case <-time.After(20 * time.Millisecond):
		}
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(f.app.headlessRuntime.Paths.StateDir, "private-chat.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	var dispatch string
	if err = db.QueryRow(`SELECT d.id FROM dispatch d JOIN native_commands n ON n.dispatch_id=d.id WHERE json_extract(n.payload,'$.kind')='thread.read' ORDER BY d.rowid DESC LIMIT 1`).Scan(&dispatch); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(-time.Second).UnixNano()
	result, err := db.Exec(`UPDATE private_read_observations SET expires_ns=? WHERE dispatch_id=?`, deadline, dispatch)
	_ = db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		t.Fatal("probe observation absent")
	}
	reg, err := sql.Open("sqlite", "file:"+filepath.Join(f.app.headlessRuntime.Paths.StateDir, "execution-owners.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	result, err = reg.Exec(`UPDATE session_read_intents SET expires_ns=? WHERE operation_id=?`, deadline, dispatch)
	_ = reg.Close()
	if err != nil {
		t.Fatal(err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		t.Fatal("probe intent absent")
	}
	u.waitVisible(f.app, n, "/goal retry")
	before := len(f.gateway.snapshotOperations())
	u.send(f.app, "probe-retry", "/goal retry")
	u.waitFinal(f.app, n, "probe-waiting", "goal dialogue complete")
	if err = os.WriteFile(release, []byte("release old response"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(trace)
	if err != nil || strings.Count(string(data), "read\n") != 2 {
		t.Fatalf("expected one original and one explicit fresh probe: %q %v", data, err)
	}
	for {
		if _, e := os.Stat(resumeStarted); e == nil {
			break
		}
		select {
		case <-f.ctx.Done():
			t.Fatal("goal resume not observed")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if ackFirst {
		if err = os.WriteFile(resumeRelease, []byte("release"), 0600); err != nil {
			t.Fatal(err)
		}
		for {
			if _, e := os.Stat(resumeSent); e == nil {
				break
			}
			select {
			case <-f.ctx.Done():
				t.Fatal("goal resume response absent")
			case <-time.After(20 * time.Millisecond):
			}
		}
		db, e := sql.Open("sqlite", "file:"+filepath.Join(f.app.headlessRuntime.Paths.StateDir, "private-chat.db")+"?mode=ro&_pragma=busy_timeout(5000)")
		if e != nil {
			t.Fatal(e)
		}
		for {
			var count int
			e = db.QueryRow(`SELECT count(*) FROM native_events e JOIN native_commands n ON n.command_id=e.command_id WHERE e.applied=1 AND json_extract(n.payload,'$.kind')='thread.goal.set' AND json_extract(n.payload,'$.goal.status')='active' AND json_extract(e.payload,'$.threadGoal.status')='active'`).Scan(&count)
			if e != nil {
				_ = db.Close()
				t.Fatal(e)
			}
			if count == 1 {
				break
			}
			select {
			case <-f.ctx.Done():
				_ = db.Close()
				t.Fatal("resume ACK not projected")
			case <-time.After(20 * time.Millisecond):
			}
		}
		_ = db.Close()
	}
	u.send(f.app, "probe-after", "next")
	if !ackFirst {
		u.waitVisible(f.app, before, "OneSecond")
		db, e := sql.Open("sqlite", "file:"+filepath.Join(f.app.headlessRuntime.Paths.StateDir, "private-chat.db")+"?mode=ro&_pragma=busy_timeout(5000)")
		if e != nil {
			t.Fatal(e)
		}
		for {
			var count int
			e = db.QueryRow(`SELECT count(*) FROM ingress i JOIN private_applied a ON a.seq=i.seq WHERE i.event_id='probe-after' AND a.canceled_by IS NULL`).Scan(&count)
			if e != nil {
				_ = db.Close()
				t.Fatal(e)
			}
			if count == 1 {
				break
			}
			select {
			case <-f.ctx.Done():
				_ = db.Close()
				t.Fatal("queued input not projected before resume ACK")
			case <-time.After(20 * time.Millisecond):
			}
		}
		_ = db.Close()
		if err = os.WriteFile(resumeRelease, []byte("release"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	u.waitFinal(f.app, before, "probe-after", "goal dialogue complete")
}
