package daemon

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

// This peer enforces the Codex 0.171 review/start delivery and thread contract.
func TestPrivateStrictReviewNativeProcess(t *testing.T) {
	if os.Getenv("PRIVATE_STRICT_REVIEW") != "1" {
		return
	}
	cwd, _ := os.Getwd()
	seq := 0
	turn := 0
	reviews := 0
	failedOnce := false
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		var f struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal(line, &f) != nil {
			continue
		}
		if trace := os.Getenv("GRAPH_TRACE"); trace != "" {
			if out, e := os.OpenFile(trace, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); e == nil {
				fmt.Fprintln(out, string(line))
				out.Close()
			}
		}
		reply := func(result any) {
			b, _ := json.Marshal(map[string]any{"id": f.ID, "result": result})
			fmt.Println(string(b))
		}
		fail := func(message string) {
			b, _ := json.Marshal(map[string]any{"id": f.ID, "error": map[string]any{"code": -32602, "message": message}})
			fmt.Println(string(b))
		}
		unsupported := func(message string) {
			b, _ := json.Marshal(map[string]any{"id": f.ID, "error": map[string]any{"code": -32600, "message": message}})
			fmt.Println(string(b))
		}
		delayedFailure := func() {
			path := os.Getenv("GRAPH_RELEASE")
			_ = os.WriteFile(path+".review-entered", []byte("entered"), 0600)
			for {
				if _, err := os.Stat(path); err == nil {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			fail("controlled review rejection")
		}
		switch f.Method {
		case "initialize":
			reply(map[string]any{})
		case "thread/start":
			seq++
			id := fmt.Sprintf("review-thread-%d", seq)
			if seq > 1 && !failedOnce && os.Getenv("STRICT_REVIEW_FAIL") == "thread/start" {
				failedOnce = true
				delayedFailure()
				continue
			}
			if seq > 1 && (f.Params["ephemeral"] != true || f.Params["cwd"] != cwd) {
				fail("review child requires ephemeral and exact cwd")
				continue
			}
			if seq > 1 && os.Getenv("STRICT_REVIEW_EARLY") == "1" {
				fmt.Printf(`{"method":"thread/started","params":{"thread":{"id":%q,"cwd":%q,"source":"vscode"}}}`+"\n", id, cwd)
			}
			reply(map[string]any{"model": "fixture-model", "thread": map[string]any{"id": id, "cwd": cwd}})
		case "thread/resume":
			id, _ := f.Params["threadId"].(string)
			if id != "review-thread-1" {
				fail("no rollout found for ephemeral child")
				continue
			}
			reply(map[string]any{"model": "fixture-model", "thread": map[string]any{"id": id, "cwd": cwd}})
		case "turn/start":
			turn++
			id, _ := f.Params["threadId"].(string)
			tid := fmt.Sprintf("turn-%d", turn)
			reply(map[string]any{"turn": map[string]any{"id": tid}})
			fmt.Printf(`{"method":"turn/started","params":{"threadId":%q,"turn":{"id":%q}}}`+"\n", id, tid)
			fmt.Printf(`{"method":"turn/diff/updated","params":{"threadId":%q,"turnId":%q,"diff":"diff --git a/review.txt b/review.txt\n@@ -1 +1 @@\n-old\n+new\n"}}`+"\n", id, tid)
			fmt.Printf(`{"method":"item/completed","params":{"threadId":%q,"turnId":%q,"item":{"id":"file-1","type":"fileChange","status":"completed","changes":[{"path":"review.txt","kind":{"type":"update"},"diff":"@@ -1 +1 @@\n-old\n+new"}]}}}`+"\n", id, tid)
			fmt.Printf(`{"method":"item/completed","params":{"threadId":%q,"turnId":%q,"item":{"id":"answer","type":"agentMessage","text":"strict final"}}}`+"\n", id, tid)
			fmt.Printf(`{"method":"turn/completed","params":{"threadId":%q,"turn":{"id":%q,"status":"completed"}}}`+"\n", id, tid)
		case "review/start":
			reviews++
			reviewTurn := fmt.Sprintf("review-turn-%d", reviews)
			id, _ := f.Params["threadId"].(string)
			if !failedOnce && os.Getenv("STRICT_REVIEW_FAIL") == "review/start" {
				failedOnce = true
				delayedFailure()
				continue
			}
			target, ok := f.Params["target"].(map[string]any)
			if f.Params["delivery"] != "inline" || !strings.HasPrefix(id, "review-thread-") || id == "review-thread-1" || !ok || (target["type"] != "uncommittedChanges" && target["type"] != "commit") {
				fail("expected dedicated child with inline delivery")
				continue
			}
			if os.Getenv("STRICT_REVIEW_EARLY") != "1" {
				reply(map[string]any{"reviewThreadId": id, "turn": map[string]any{"id": reviewTurn}})
			}
			// Codex 0.171 also emits a child-internal turn whose rootTurnId
			// names the review turn. It must not become the user-facing turn.
			fmt.Printf(`{"method":"turn/started","params":{"threadId":%q,"turn":{"id":%q,"rootTurnId":%q,"status":"inProgress"}}}`+"\n", id, reviewTurn+"-internal", reviewTurn)
			if os.Getenv("STRICT_REVIEW_LIVE") == "1" {
				for {
					if _, err := os.Stat(os.Getenv("GRAPH_RELEASE") + ".live"); err == nil {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				fmt.Printf(`{"method":"item/agentMessage/delta","params":{"threadId":%q,"turnId":%q,"itemId":"review-live","delta":"review partial"}}`+"\n", id, reviewTurn+"-internal")
			}
			if os.Getenv("STRICT_REVIEW_REAL") != "1" {
				fmt.Printf(`{"method":"turn/started","params":{"threadId":%q,"turn":{"id":%q}}}`+"\n", id, reviewTurn)
			}
			if os.Getenv("STRICT_REVIEW_PERMISSION") == "1" && reviews == 1 {
				grant := map[string]any{"network": map[string]any{"enabled": true}, "fileSystem": map[string]any{"read": []any{cwd}}}
				request, _ := json.Marshal(map[string]any{"id": "review-perm", "method": "item/permissions/requestApproval", "params": map[string]any{"threadId": id, "turnId": reviewTurn + "-internal", "itemId": "review-perm-item", "cwd": cwd, "startedAtMs": int64(1790000000000), "permissions": grant}})
				fmt.Println(string(request))
				valid := false
				for scanner.Scan() {
					var response struct {
						ID     any            `json:"id"`
						Result map[string]any `json:"result"`
					}
					if json.Unmarshal(scanner.Bytes(), &response) == nil && response.ID == "review-perm" {
						perms, _ := response.Result["permissions"].(map[string]any)
						valid = response.Result["scope"] == "turn" && reflect.DeepEqual(perms, grant)
						break
					}
				}
				if !valid {
					os.Exit(52)
				}
				if out, e := os.OpenFile(os.Getenv("GRAPH_TRACE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); e == nil {
					fmt.Fprintln(out, "REVIEW_PERMISSION_TURN_ACCEPTED")
					out.Close()
				}
				fmt.Printf(`{"method":"serverRequest/resolved","params":{"requestId":"review-perm","threadId":%q,"turnId":%q}}`+"\n", id, reviewTurn+"-internal")
			}
			fmt.Printf(`{"method":"item/completed","params":{"threadId":%q,"turnId":%q,"item":{"id":"review","type":"agentMessage","text":"strict review final"}}}`+"\n", id, reviewTurn)
			if os.Getenv("STRICT_REVIEW_EARLY") == "1" {
				path := os.Getenv("GRAPH_RELEASE")
				_ = os.WriteFile(path+".early-review", []byte("ready"), 0600)
				for {
					if _, err := os.Stat(path); err == nil {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				reply(map[string]any{"reviewThreadId": id, "turn": map[string]any{"id": reviewTurn}})
			}
			fmt.Printf(`{"method":"turn/completed","params":{"threadId":%q,"turn":{"id":%q,"status":"completed"}}}`+"\n", id, reviewTurn)
		case "thread/read":
			id, _ := f.Params["threadId"].(string)
			if id == "review-thread-1" && f.Params["includeTurns"] == true {
				fmt.Println(`{"method":"deprecationNotice","params":{"summary":"Full-history hydration is deprecated for paginated threads; omit ` + "`includeTurns`" + ` or set it to ` + "`false`" + `, then page with ` + "`thread/turns/list`" + ` and ` + "`thread/items/list`" + `.","details":null}}`)
			}
			if id != "review-thread-1" && f.Params["includeTurns"] != false {
				unsupported("ephemeral threads do not support includeTurns")
				continue
			}
			reply(map[string]any{"thread": map[string]any{"id": id, "cwd": cwd, "status": map[string]any{"type": "idle"}}})
		case "thread/goal/get":
			id, _ := f.Params["threadId"].(string)
			if id != "review-thread-1" {
				unsupported("ephemeral thread does not support goals: " + id)
				continue
			}
			reply(map[string]any{"goal": nil})
		case "thread/list", "model/list":
			reply(map[string]any{"data": []any{}})
		}
	}
	os.Exit(0)
}
func findReviewAction(v any, action string) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		if x["action_kind"] == action && x["private_view"] != nil {
			return x
		}
		for _, z := range x {
			if hit := findReviewAction(z, action); hit != nil {
				return hit
			}
		}
	case []any:
		for _, z := range x {
			if hit := findReviewAction(z, action); hit != nil {
				return hit
			}
		}
	}
	return nil
}
func clickReviewAction(t *testing.T, g graphEnv, from int, action, id string) (string, []byte) {
	t.Helper()
	for {
		for _, op := range g.user.gateway.snapshotOperations()[from:] {
			body, e := feishu.FreezePrivateOperation(op, g.app.daemonLifecycleID)
			var doc any
			if e != nil || json.Unmarshal(body, &doc) != nil {
				continue
			}
			value := findReviewAction(doc, action)
			if value == nil {
				continue
			}
			response, e := g.app.applications.hook.ReceiveInteractionResponse(g.user.ctx, platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: id, SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: op.MessageID, Kind: "card", Values: value, ReceivedAt: time.Now()})
			if e != nil {
				t.Fatal(e)
			}
			return op.MessageID, response.RawBody
		}
		select {
		case <-g.user.ctx.Done():
			t.Fatalf("review button %s not visible", action)
		case <-time.After(20 * time.Millisecond):
		}
	}
}
func TestPrivateSDKStrictInlineReviewJourney(t *testing.T) {
	for _, tc := range []struct {
		name             string
		early, realShape bool
	}{{"ordered", false, false}, {"early", true, false}, {"real-shape", false, true}} {
		t.Run(tc.name, func(t *testing.T) { testPrivateSDKStrictInlineReviewJourney(t, tc.early, tc.realShape) })
	}
}
func testPrivateSDKStrictInlineReviewJourney(t *testing.T, early, realShape bool, live ...bool) {
	cwd := t.TempDir()
	prepareRealReviewRepository(t, cwd)
	if e := os.WriteFile(filepath.Join(cwd, "review.txt"), []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	g := newGraphEnv(t, cwd, t.TempDir(), func(a *App, _ *messageIDAssigningGateway) { a.SetFinalBlockPreviewer(&workflowPreviewer{}) })
	t.Cleanup(func() { preserveReviewEvidenceOnFailure(t, g.app) })
	if early {
		g.app.headlessRuntime.BaseEnv = append(g.app.headlessRuntime.BaseEnv, "STRICT_REVIEW_EARLY=1")
	}
	if realShape {
		g.app.headlessRuntime.BaseEnv = append(g.app.headlessRuntime.BaseEnv, "STRICT_REVIEW_REAL=1")
	}
	if len(live) > 1 && live[1] {
		g.app.headlessRuntime.BaseEnv = append(g.app.headlessRuntime.BaseEnv, "STRICT_REVIEW_PERMISSION=1")
	}
	if len(live) > 0 && live[0] {
		g.app.headlessRuntime.BaseEnv = append(g.app.headlessRuntime.BaseEnv, "STRICT_REVIEW_LIVE=1")
		go func() {
			db, err := sql.Open("sqlite", "file:"+filepath.Join(g.app.headlessRuntime.Paths.StateDir, "private-chat.db")+"?mode=ro")
			if err != nil {
				return
			}
			defer db.Close()
			for {
				var n int
				_ = db.QueryRow(`SELECT count(*) FROM native_events WHERE applied=1 AND json_extract(payload,'$.kind')='turn.started' AND json_extract(payload,'$.threadId')='review-thread-2' AND json_extract(payload,'$.turnId')='review-turn-1-internal'`).Scan(&n)
				if n > 0 {
					_ = os.WriteFile(g.release+".live", []byte("ready"), 0600)
					return
				}
				select {
				case <-g.user.ctx.Done():
					return
				case <-time.After(20 * time.Millisecond):
				}
			}
		}()
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3-dev.1\\n'; exit; fi\nPRIVATE_STRICT_REVIEW=1 exec %q -test.run '^TestPrivateStrictReviewNativeProcess$'\n", exe)
	if e = os.WriteFile(g.app.headlessRuntime.CodexRealBinary, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	u := g.user
	n := len(u.gateway.snapshotOperations())
	u.send(g.app, "cold", "hi")
	u.waitVisible(g.app, n, "/list")
	if len(live) > 1 && live[1] {
		n = len(u.gateway.snapshotOperations())
		u.send(g.app, "review-full-access", "/access full")
		u.waitVisible(g.app, n, "已更新当前会话的执行权限模式")
	}
	u.chooseNew(g.app, "initial")
	n = len(u.gateway.snapshotOperations())
	u.send(g.app, "parent-first", "make a change")
	u.waitFinal(g.app, n, "parent-first", "strict final")
	beforeReview := len(u.gateway.snapshotOperations())
	card, _ := clickReviewAction(t, g, n, "surface.button.review_start", "review")
	if early {
		for {
			if _, err := os.Stat(g.release + ".early-review"); err == nil {
				break
			}
			select {
			case <-u.ctx.Done():
				t.Fatal("early native events not emitted")
			case <-time.After(20 * time.Millisecond):
			}
		}
		db, err := sql.Open("sqlite", "file:"+filepath.Join(g.app.headlessRuntime.Paths.StateDir, "private-chat.db")+"?mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		for {
			var pending, applied, accepted int
			err = db.QueryRow(`SELECT count(*),coalesce(sum(applied),0) FROM native_events WHERE json_extract(payload,'$.kind')='item.completed' AND json_extract(payload,'$.threadId')='review-thread-2' AND json_extract(payload,'$.turnId')='review-turn-1'`).Scan(&pending, &applied)
			if err == nil {
				err = db.QueryRow(`SELECT count(*) FROM native_events WHERE json_extract(payload,'$.action')='review.start' AND json_extract(payload,'$.status')='accepted'`).Scan(&accepted)
			}
			if err != nil {
				db.Close()
				t.Fatal(err)
			}
			if pending > 0 {
				if applied != 0 || accepted != 0 {
					db.Close()
					t.Fatalf("early item applied=%d accepted=%d", applied, accepted)
				}
				break
			}
			select {
			case <-u.ctx.Done():
				db.Close()
				t.Fatal("early item was not durably captured")
			case <-time.After(20 * time.Millisecond):
			}
		}
		db.Close()
		for _, op := range u.gateway.snapshotOperations()[beforeReview:] {
			if strings.Contains(op.FinalSourceBody(), "strict review final") {
				t.Fatal("review final emitted before accepted receipt")
			}
		}
		if err := os.WriteFile(g.release, []byte("release"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	u.waitFinal(g.app, beforeReview, card, "strict review final")
	if len(live) > 1 && live[1] {
		trace, err := os.ReadFile(g.trace)
		if err != nil || !strings.Contains(string(trace), "REVIEW_PERMISSION_TURN_ACCEPTED") {
			t.Fatalf("review B permission response missing: %v", err)
		}
	}
	if len(live) > 0 && live[0] {
		db, err := sql.Open("sqlite", "file:"+filepath.Join(g.app.headlessRuntime.Paths.StateDir, "private-chat.db")+"?mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var count int
		if err = db.QueryRow(`SELECT count(*) FROM private_live_applied`).Scan(&count); err != nil || count < 1 {
			t.Fatalf("review live was not projected: count=%d err=%v", count, err)
		}
	}
	n = len(u.gateway.snapshotOperations())
	cardStart := beforeReview
	if !realShape {
		_, response := clickReviewAction(t, g, beforeReview, "surface.button.review_follow_up", "ask-review")
		if !strings.Contains(string(response), "请发送一条文字追问") {
			u.waitVisible(g.app, n, "请发送一条文字追问")
		}
		n = len(u.gateway.snapshotOperations())
		u.send(g.app, "follow-review", "please explain")
		u.waitFinal(g.app, n, "follow-review", "strict final")
		cardStart = n
	}
	_, response := clickReviewAction(t, g, cardStart, "surface.button.review_discard", "exit-review")
	if !strings.Contains(string(response), "已退出审阅") {
		u.waitVisible(g.app, n, "已退出审阅")
	}
	n = len(u.gateway.snapshotOperations())
	u.send(g.app, "parent-after", "continue parent")
	u.waitFinal(g.app, n, "parent-after", "strict final")
	followSource := "follow-review"
	if realShape {
		followSource = ""
	}
	assertReviewNativeOwnership(t, u.ctx, g.app, "parent-first", "parent-after", followSource)
	git := exec.Command("git", "rev-parse", "HEAD")
	git.Dir = cwd
	sha, err := git.Output()
	if err != nil {
		t.Fatal(err)
	}
	n = len(u.gateway.snapshotOperations())
	u.send(g.app, "commit-review", "/review commit "+strings.TrimSpace(string(sha)))
	u.waitFinal(g.app, n, "commit-review", "strict review final")
	applyCard, _ := clickReviewAction(t, g, n, "surface.button.review_apply", "apply-review")
	u.waitFinal(g.app, n, applyCard, "strict final")
	// Replaying the same SDK event and clicking the now-expired card again
	// cannot create a second parent modification turn.
	clickReviewAction(t, g, n, "surface.button.review_apply", "apply-review")
	clickReviewAction(t, g, n, "surface.button.review_apply", "stale-apply-review")
	afterApply := len(u.gateway.snapshotOperations())
	u.send(g.app, "parent-after-apply", "continue after applying review")
	u.waitFinal(g.app, afterApply, "parent-after-apply", "strict final")
	trace, err := os.ReadFile(g.trace)
	if err != nil || !strings.Contains(string(trace), `"type":"commit"`) || strings.Count(string(trace), "请根据以下审阅意见继续修改") != 1 {
		t.Fatalf("commit review/apply frame missing: %v", err)
	}
	if realShape {
		controlledReviewShutdown(t, g)
	}
}

func TestPrivateSDKReviewLiveThenFinalAndNext(t *testing.T) {
	testPrivateSDKStrictInlineReviewJourney(t, false, false, true)
}

func TestPrivateSDKReviewSubturnPermissionAutoApproval(t *testing.T) {
	testPrivateSDKStrictInlineReviewJourney(t, false, false, false, true)
}
