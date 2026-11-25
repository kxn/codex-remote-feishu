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

func TestPrivateGoalVerificationPeer(t *testing.T) {
	mode := os.Getenv("PRIVATE_GOAL_VERIFY_MODE")
	if mode == "" {
		return
	}
	cwd, _ := os.Getwd()
	turn, pauseCount, resumeCount := 0, 0, 0
	var goal map[string]any
	scan := bufio.NewScanner(os.Stdin)
	trace := func(line string) {
		if f, e := os.OpenFile(os.Getenv("PRIVATE_GOAL_VERIFY_TRACE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); e == nil {
			fmt.Fprintln(f, line)
			_ = f.Close()
		}
	}
	for scan.Scan() {
		var f struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal(scan.Bytes(), &f) != nil {
			continue
		}
		reply := func(v any) { b, _ := json.Marshal(map[string]any{"id": f.ID, "result": v}); fmt.Println(string(b)) }
		switch f.Method {
		case "initialize":
			reply(map[string]any{})
		case "thread/start", "thread/resume":
			reply(map[string]any{"model": "fixture-model", "thread": map[string]any{"id": "verify-thread", "cwd": cwd}})
		case "turn/start":
			if f.Params["threadId"] != "verify-thread" {
				os.Exit(51)
			}
			turn++
			id := fmt.Sprintf("verify-turn-%d", turn)
			trace("turn")
			reply(map[string]any{"turn": map[string]any{"id": id}})
			fmt.Printf(`{"method":"turn/started","params":{"threadId":"verify-thread","turn":{"id":%q}}}`+"\n", id)
			fmt.Printf(`{"method":"item/completed","params":{"threadId":"verify-thread","turnId":%q,"item":{"id":"answer","type":"agentMessage","text":"goal verification final"}}}`+"\n", id)
			fmt.Printf(`{"method":"turn/completed","params":{"threadId":"verify-thread","turn":{"id":%q,"status":"completed"}}}`+"\n", id)
		case "thread/goal/get":
			trace("get")
			if goal == nil {
				reply(map[string]any{"goal": nil})
			} else {
				reply(map[string]any{"goal": goal})
			}
		case "thread/goal/set":
			if f.Params["threadId"] != "verify-thread" {
				os.Exit(52)
			}
			if objective, ok := f.Params["objective"].(string); ok && objective != "" {
				goal = map[string]any{"threadId": "verify-thread", "objective": objective, "status": "active", "createdAt": int64(1710000000), "updatedAt": int64(1710000000), "timeUsedSeconds": int64(0), "tokensUsed": int64(0)}
				trace("new")
				reply(map[string]any{"goal": goal})
				continue
			}
			status, _ := f.Params["status"].(string)
			if goal == nil {
				os.Exit(53)
			}
			if status == "paused" {
				pauseCount++
				trace("pause")
				if pauseCount == 1 && mode == "pause-active" {
					reply(map[string]any{"goal": goal})
					continue
				}
				if pauseCount == 1 && mode == "pause-error" {
					fmt.Printf(`{"id":%q,"error":{"code":-32603,"message":"controlled pause failure"}}`+"\n", f.ID)
					continue
				}
				goal["status"] = "paused"
				reply(map[string]any{"goal": goal})
				continue
			}
			if status == "active" {
				resumeCount++
				trace("resume")
				if mode == "resume-terminal" && resumeCount == 1 {
					id := f.ID
					goal["status"] = "complete"
					data, _ := json.Marshal(map[string]any{"id": id, "result": map[string]any{"goal": goal}})
					_ = os.WriteFile(os.Getenv("PRIVATE_GOAL_VERIFY_STARTED"), []byte("started"), 0600)
					go func() {
						for {
							if _, e := os.Stat(os.Getenv("PRIVATE_GOAL_VERIFY_RELEASE")); e == nil {
								fmt.Println(string(data))
								return
							}
							time.Sleep(10 * time.Millisecond)
						}
					}()
					continue
				}
				goal["status"] = "active"
				reply(map[string]any{"goal": goal})
				continue
			}
			os.Exit(54)
		case "thread/read":
			trace("read")
			reply(map[string]any{"thread": map[string]any{"id": "verify-thread", "cwd": cwd, "status": map[string]any{"type": "idle"}}})
		case "thread/list", "model/list":
			reply(map[string]any{"data": []any{}})
		}
	}
	os.Exit(0)
}

func goalVerificationFixture(t *testing.T, mode string) (*continuityFixture, string, string, string) {
	t.Helper()
	f := newContinuityFixtureBare(t, nil)
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	trace, started, release := filepath.Join(t.TempDir(), "trace"), filepath.Join(t.TempDir(), "started"), filepath.Join(t.TempDir(), "release")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3-dev.1\\n'; exit; fi\nPRIVATE_GOAL_VERIFY_MODE=%s exec %q -test.run '^TestPrivateGoalVerificationPeer$'\n", mode, exe)
	if e = os.WriteFile(f.app.headlessRuntime.CodexRealBinary, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	f.app.headlessRuntime.BaseEnv = append(f.app.headlessRuntime.BaseEnv, "PRIVATE_GOAL_VERIFY_TRACE="+trace, "PRIVATE_GOAL_VERIFY_STARTED="+started, "PRIVATE_GOAL_VERIFY_RELEASE="+release)
	u := f.user
	n := len(f.gateway.snapshotOperations())
	u.send(f.app, "verify-cold", "hi")
	u.waitVisible(f.app, n, "/list")
	u.chooseNew(f.app, "verify-new")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "verify-initial", "hello")
	u.waitFinal(f.app, n, "verify-initial", "goal verification final")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "verify-goal", "/goal new fixture goal")
	u.waitVisible(f.app, n, "状态：active")
	return f, trace, started, release
}

func TestPrivateSDKGoalPauseUncertainManualRecovery(t *testing.T) {
	for _, mode := range []string{"pause-active", "pause-error"} {
		t.Run(mode, func(t *testing.T) {
			f, trace, _, _ := goalVerificationFixture(t, mode)
			u := f.user
			n := len(f.gateway.snapshotOperations())
			u.send(f.app, "verify-waiting", "original input")
			u.waitVisible(f.app, n, "/goal retry")
			n = len(f.gateway.snapshotOperations())
			u.send(f.app, "verify-check-active", "/goal retry")
			u.waitVisible(f.app, n, "/goal pause")
			data, e := os.ReadFile(trace)
			if e != nil || strings.Count(string(data), "pause\n") != 1 {
				t.Fatalf("automatically retried pause: %q %v", data, e)
			}
			n = len(f.gateway.snapshotOperations())
			u.send(f.app, "verify-user-pause", "/goal pause")
			u.waitVisible(f.app, n, "状态：paused")
			n = len(f.gateway.snapshotOperations())
			u.send(f.app, "verify-check-paused", "/goal retry")
			u.waitFinal(f.app, 0, "verify-waiting", "goal verification final")
			n = len(f.gateway.snapshotOperations())
			u.send(f.app, "verify-next", "next")
			u.waitFinal(f.app, n, "verify-next", "goal verification final")
		})
	}
}

func TestPrivateSDKGoalResumeTerminalDispatchesQueuedWithoutReactivation(t *testing.T) {
	f, trace, started, release := goalVerificationFixture(t, "resume-terminal")
	u := f.user
	n := len(f.gateway.snapshotOperations())
	u.send(f.app, "verify-first", "first after goal")
	u.waitFinal(f.app, n, "verify-first", "goal verification final")
	for {
		if _, e := os.Stat(started); e == nil {
			break
		}
		select {
		case <-f.ctx.Done():
			t.Fatal("resume request absent")
		case <-time.After(20 * time.Millisecond):
		}
	}
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "verify-queued", "next while resume pending")
	u.waitVisible(f.app, n, "OneSecond")
	db, e := sql.Open("sqlite", "file:"+filepath.Join(f.app.headlessRuntime.Paths.StateDir, "private-chat.db")+"?mode=ro&_pragma=busy_timeout(5000)")
	if e != nil {
		t.Fatal(e)
	}
	for {
		var count int
		e = db.QueryRow(`SELECT count(*) FROM ingress i JOIN private_applied a ON a.seq=i.seq WHERE i.event_id='verify-queued' AND a.canceled_by IS NULL`).Scan(&count)
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
			t.Fatal("queued input not applied before resume result")
		case <-time.After(20 * time.Millisecond):
		}
	}
	_ = db.Close()
	if e = os.WriteFile(release, []byte("release terminal result"), 0600); e != nil {
		t.Fatal(e)
	}
	u.waitFinal(f.app, n, "verify-queued", "goal verification final")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "verify-next", "next after terminal")
	u.waitFinal(f.app, n, "verify-next", "goal verification final")
	data, e := os.ReadFile(trace)
	if e != nil || strings.Count(string(data), "resume\n") != 1 {
		t.Fatalf("terminal goal was reactivated: %q %v", data, e)
	}
}
