package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

// Uses required fields from the 0.171 ThreadGoal schema and rejects a request
// that targets any thread other than the one opened through the SDK journey.
func TestPrivateGoalStrictNativePeer(t *testing.T) {
	if os.Getenv("PRIVATE_GOAL_STRICT") != "1" {
		return
	}
	cwd, _ := os.Getwd()
	turn := 0
	var goal any
	probeWaiting, probeSeen := false, false
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var f struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal(scan.Bytes(), &f) != nil {
			continue
		}
		if path := os.Getenv("PRIVATE_GOAL_PROBE_METHODS"); path != "" {
			if out, e := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); e == nil {
				fmt.Fprintf(out, "method=%s id=%v status=%v thread=%v\n", f.Method, f.ID, f.Params["status"], f.Params["threadId"])
				_ = out.Close()
			}
		}
		reply := func(result any) {
			b, _ := json.Marshal(map[string]any{"id": f.ID, "result": result})
			fmt.Println(string(b))
		}
		switch f.Method {
		case "initialize":
			reply(map[string]any{})
		case "thread/start", "thread/resume":
			reply(map[string]any{"model": "fixture-model", "thread": map[string]any{"id": "goal-thread", "cwd": cwd}})
		case "turn/start":
			if f.Params["threadId"] != "goal-thread" {
				os.Exit(51)
			}
			turn++
			if os.Getenv("PRIVATE_PLAN_STRICT") == "1" && (turn == 2 || turn == 3) {
				mode, ok := f.Params["collaborationMode"].(map[string]any)
				settings, settingsOK := mode["settings"].(map[string]any)
				model, modelOK := settings["model"].(string)
				if turn == 2 && (!ok || !settingsOK || !modelOK || model == "" || mode["mode"] != "plan") {
					os.Exit(55)
				}
				if turn == 3 && ok && (!settingsOK || !modelOK || model == "" || mode["mode"] == "plan") {
					os.Exit(55)
				}
			}
			id := fmt.Sprintf("goal-turn-%d", turn)
			reply(map[string]any{"turn": map[string]any{"id": id}})
			fmt.Printf(`{"method":"turn/started","params":{"threadId":"goal-thread","turn":{"id":%q}}}`+"\n", id)
			if os.Getenv("PRIVATE_PLAN_STRICT") == "1" && turn == 2 {
				fmt.Printf(`{"method":"item/plan/delta","params":{"threadId":"goal-thread","turnId":%q,"itemId":"plan-1","delta":"Implement the requested change."}}`+"\n", id)
				fmt.Printf(`{"method":"item/completed","params":{"threadId":"goal-thread","turnId":%q,"item":{"id":"plan-1","type":"plan","text":"Implement the requested change."}}}`+"\n", id)
				fmt.Printf(`{"method":"turn/completed","params":{"threadId":"goal-thread","turn":{"id":%q,"status":"completed"}}}`+"\n", id)
				continue
			}
			fmt.Printf(`{"method":"item/completed","params":{"threadId":"goal-thread","turnId":%q,"item":{"id":"answer","type":"agentMessage","text":"goal dialogue complete"}}}`+"\n", id)
			fmt.Printf(`{"method":"turn/completed","params":{"threadId":"goal-thread","turn":{"id":%q,"status":"completed"}}}`+"\n", id)
		case "thread/goal/get":
			if f.Params["threadId"] != "goal-thread" {
				os.Exit(52)
			}
			reply(map[string]any{"goal": goal})
		case "thread/goal/set":
			objective, ok := f.Params["objective"].(string)
			if status, isStatus := f.Params["status"].(string); os.Getenv("PRIVATE_GOAL_PROBE") == "1" && isStatus {
				current, exists := goal.(map[string]any)
				if !exists || f.Params["threadId"] != "goal-thread" {
					os.Exit(53)
				}
				current["status"] = status
				probeWaiting = status == "paused"
				if status == "active" && os.Getenv("PRIVATE_GOAL_RESUME_RELEASE") != "" {
					id := f.ID
					data, _ := json.Marshal(map[string]any{"id": id, "result": map[string]any{"goal": current}})
					_ = os.WriteFile(os.Getenv("PRIVATE_GOAL_RESUME_STARTED"), []byte("started"), 0600)
					go func() {
						for {
							if _, e := os.Stat(os.Getenv("PRIVATE_GOAL_RESUME_RELEASE")); e == nil {
								fmt.Println(string(data))
								_ = os.WriteFile(os.Getenv("PRIVATE_GOAL_RESUME_SENT"), []byte("sent"), 0600)
								return
							}
							time.Sleep(10 * time.Millisecond)
						}
					}()
					continue
				}
				reply(map[string]any{"goal": current})
				continue
			}
			if !ok || objective == "" || f.Params["threadId"] != "goal-thread" {
				os.Exit(53)
			}
			goal = map[string]any{"threadId": "goal-thread", "objective": objective, "status": "active", "createdAt": int64(1710000000), "updatedAt": int64(1710000000), "timeUsedSeconds": int64(0), "tokensUsed": int64(0)}
			reply(map[string]any{"goal": goal})
		case "thread/goal/clear":
			if f.Params["threadId"] != "goal-thread" {
				os.Exit(54)
			}
			goal = nil
			reply(map[string]any{"cleared": true})
		case "thread/read":
			if os.Getenv("PRIVATE_GOAL_PROBE") == "1" && probeWaiting {
				if out, e := os.OpenFile(os.Getenv("PRIVATE_GOAL_PROBE_TRACE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); e == nil {
					fmt.Fprintln(out, "read")
					_ = out.Close()
				}
			}
			if os.Getenv("PRIVATE_GOAL_PROBE") == "1" && probeWaiting && !probeSeen {
				probeSeen = true
				id := f.ID
				_ = os.WriteFile(os.Getenv("PRIVATE_GOAL_PROBE_STARTED"), []byte("started"), 0600)
				go func() {
					for {
						if _, e := os.Stat(os.Getenv("PRIVATE_GOAL_PROBE_RELEASE")); e == nil {
							b, _ := json.Marshal(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{"id": "goal-thread", "cwd": cwd, "status": map[string]any{"type": "idle"}}}})
							fmt.Println(string(b))
							return
						}
						time.Sleep(10 * time.Millisecond)
					}
				}()
				continue
			}
			reply(map[string]any{"thread": map[string]any{"id": "goal-thread", "cwd": cwd, "status": map[string]any{"type": "idle"}}})
		case "thread/list", "model/list":
			reply(map[string]any{"data": []any{}})
		}
	}
	os.Exit(0)
}

func TestPrivateSDKGoalStrictSchemaAndNextTurn(t *testing.T) {
	f := newContinuityFixtureBare(t, nil)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3-dev.1\\n'; exit; fi\nPRIVATE_GOAL_STRICT=1 exec %q -test.run '^TestPrivateGoalStrictNativePeer$'\n", exe)
	if err = os.WriteFile(f.app.headlessRuntime.CodexRealBinary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	u := f.user
	n := len(f.gateway.snapshotOperations())
	u.send(f.app, "goal-cold", "hi")
	u.waitVisible(f.app, n, "/list")
	u.chooseNew(f.app, "goal-new")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "goal-first", "hello")
	u.waitFinal(f.app, n, "goal-first", "goal dialogue complete")
	for _, step := range []struct{ id, text, want string }{{"goal-get-empty", "/goal", "当前会话没有 Goal"}, {"goal-set", "/goal new fixture goal", "状态：active"}, {"goal-get", "/goal", "fixture goal"}, {"goal-clear", "/goal clear --confirm", "Goal 已清除"}, {"goal-get-cleared", "/goal", "当前会话没有 Goal"}} {
		n = len(f.gateway.snapshotOperations())
		u.send(f.app, step.id, step.text)
		u.waitVisible(f.app, n, step.want)
	}
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "goal-next", "continue")
	u.waitFinal(f.app, n, "goal-next", "goal dialogue complete")
}
