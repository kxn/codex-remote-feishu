package daemon

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	previewpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/preview"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

type workflowPreviewer struct{ calls, withDiff atomic.Int32 }

func (p *workflowPreviewer) RewriteFinalBlock(_ context.Context, req previewpkg.FinalBlockPreviewRequest) (previewpkg.FinalBlockPreviewResult, error) {
	p.calls.Add(1)
	result := previewpkg.FinalBlockPreviewResult{Block: req.Block}
	if req.TurnDiffSnapshot != nil {
		p.withDiff.Add(1)
		result.TurnDiffPreview = &control.TurnDiffPreview{URL: "https://preview.example/workflow-diff"}
	}
	return result, nil
}

// A private native protocol fixture; its responses go through the real wrapper,
// durable receiver, processor, publisher and the controlled SDK gateway.
func TestPrivateUserWorkflowNativeProcess(t *testing.T) {
	if os.Getenv("PRIVATE_WORKFLOW_NATIVE") != "1" {
		return
	}
	cwd, _ := os.Getwd()
	session := "graph-a"
	threadStarts := 0
	turn := 0
	var goal any
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
		if trace := os.Getenv("GRAPH_TRACE"); trace != "" {
			if out, err := os.OpenFile(trace, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); err == nil {
				fmt.Fprintln(out, f.Method)
				_ = out.Close()
			}
		}
		respond := func(result any) {
			raw, _ := json.Marshal(map[string]any{"id": f.ID, "result": result})
			fmt.Println(string(raw))
		}
		switch f.Method {
		case "initialize":
			respond(map[string]any{})
		case "thread/start":
			threadStarts++
			id := session
			if threadStarts > 1 {
				if f.Params["ephemeral"] != true || f.Params["cwd"] != cwd {
					fmt.Printf(`{"id":%q,"error":{"code":-32602,"message":"review child must be ephemeral with exact cwd"}}`+"\n", f.ID)
					continue
				}
				id = fmt.Sprintf("review-thread-%d", threadStarts)
			}
			respond(map[string]any{"model": "fixture-model", "thread": map[string]any{"id": id, "cwd": cwd}})
		case "thread/resume":
			id, _ := f.Params["threadId"].(string)
			if id != session {
				fmt.Printf(`{"id":%q,"error":{"code":-32000,"message":"no rollout found"}}`+"\n", f.ID)
				continue
			}
			respond(map[string]any{"model": "fixture-model", "thread": map[string]any{"id": id, "cwd": cwd}})
		case "turn/start":
			thread, _ := f.Params["threadId"].(string)
			turn++
			tid := fmt.Sprintf("turn-%d", turn)
			respond(map[string]any{"turn": map[string]any{"id": tid}})
			fmt.Printf(`{"method":"turn/started","params":{"threadId":%q,"turn":{"id":%q}}}`+"\n", thread, tid)
			fmt.Printf(`{"method":"turn/diff/updated","params":{"threadId":%q,"turnId":%q,"diff":"diff --git a/review.txt b/review.txt\n@@ -1 +1 @@\n-old\n+new\n"}}`+"\n", thread, tid)
			fmt.Printf(`{"method":"item/completed","params":{"threadId":%q,"turnId":%q,"item":{"id":"file-1","type":"fileChange","status":"completed","changes":[{"path":"review.txt","kind":{"type":"update"},"diff":"@@ -1 +1 @@\n-old\n+new"}]}}}`+"\n", thread, tid)
			fmt.Printf(`{"method":"item/completed","params":{"threadId":%q,"turnId":%q,"item":{"id":"answer","type":"agentMessage","text":"workflow final"}}}`+"\n", thread, tid)
			fmt.Printf(`{"method":"turn/completed","params":{"threadId":%q,"turn":{"id":%q,"status":"completed"}}}`+"\n", thread, tid)
		case "thread/goal/get":
			respond(map[string]any{"goal": goal})
		case "thread/goal/set":
			objective, _ := f.Params["objective"].(string)
			if objective == "" {
				objective = "fixture goal"
			}
			goal = map[string]any{"threadId": session, "objective": objective, "status": "active", "createdAt": 1710000000123, "updatedAt": 1710000000999, "timeUsedSeconds": 0, "tokensUsed": 0}
			respond(map[string]any{"goal": goal})
		case "thread/goal/clear":
			goal = nil
			respond(map[string]any{"cleared": true})
		case "thread/read":
			respond(map[string]any{"thread": map[string]any{"id": session, "cwd": cwd, "status": map[string]any{"type": "idle"}, "turns": []any{map[string]any{"id": "history-turn", "status": "completed", "items": []any{map[string]any{"id": "historic-user", "type": "user_message", "text": "fixture historical message"}, map[string]any{"id": "historic-answer", "type": "agent_message", "text": "fixture historical answer"}}}}}})
		case "thread/list", "model/list":
			respond(map[string]any{"data": []any{}})
		case "thread/compact/start":
			respond(map[string]any{})
			fmt.Printf(`{"method":"turn/started","params":{"threadId":%q,"turn":{"id":"compact-turn"}}}`+"\n", session)
			fmt.Printf(`{"method":"item/completed","params":{"threadId":%q,"turnId":"compact-turn","item":{"id":"compact","type":"contextCompaction","status":"completed"}}}`+"\n", session)
			fmt.Printf(`{"method":"turn/completed","params":{"threadId":%q,"turn":{"id":"compact-turn","status":"completed"}}}`+"\n", session)
		case "review/start":
			child, _ := f.Params["threadId"].(string)
			if f.Params["delivery"] != "inline" || !strings.HasPrefix(child, "review-thread-") {
				fmt.Printf(`{"id":%q,"error":{"code":-32602,"message":"review requires inline child"}}`+"\n", f.ID)
				continue
			}
			respond(map[string]any{"turn": map[string]any{"id": "review-turn"}, "reviewThreadId": child})
			fmt.Printf(`{"method":"turn/started","params":{"threadId":%q,"turn":{"id":"review-turn"}}}`+"\n", child)
			fmt.Printf(`{"method":"item/completed","params":{"threadId":%q,"turnId":"review-turn","item":{"id":"review","type":"agentMessage","text":"review complete"}}}`+"\n", child)
			fmt.Printf(`{"method":"turn/completed","params":{"threadId":%q,"turn":{"id":"review-turn","status":"completed"}}}`+"\n", child)
		case "mcpServer/oauth/login":
			fmt.Printf(`{"id":%q,"error":{"code":-32000,"message":"fixture oauth unavailable"}}`+"\n", f.ID)
		}
	}
	os.Exit(0)
}

func workflowWait(t *testing.T, g graphEnv, start int, label string, terms ...string) {
	t.Helper()
	deadline := time.After(9 * time.Second)
	for {
		for _, op := range g.user.gateway.snapshotOperations()[start:] {
			raw, e := feishu.FreezePrivateOperation(op, g.app.daemonLifecycleID)
			if e == nil {
				for _, term := range terms {
					if strings.Contains(string(raw), term) {
						return
					}
				}
			}
		}
		select {
		case <-deadline:
			trace, _ := os.ReadFile(g.trace)
			t.Logf("%s native methods: %s", label, trace)
			pending, _ := g.app.applications.journal.PendingHostEffects(g.user.ctx, 32)
			for _, item := range pending {
				t.Logf("%s pending effect=%s state=%s", label, item.Effect.Effect.ID, item.Effect.State)
			}
			db, e := sql.Open("sqlite", "file:"+filepath.Join(g.app.headlessRuntime.Paths.StateDir, "private-chat.db")+"?mode=ro")
			if e == nil {
				rows, e := db.Query(`SELECT d.lane,d.state,json_extract(n.payload,'$.kind') FROM dispatch d JOIN native_commands n ON n.dispatch_id=d.id ORDER BY d.rowid DESC LIMIT 10`)
				if e == nil {
					for rows.Next() {
						var lane, state, kind string
						_ = rows.Scan(&lane, &state, &kind)
						t.Logf("%s native dispatch lane=%s state=%s kind=%s", label, lane, state, kind)
					}
					_ = rows.Close()
				}
				_ = db.Close()
			}
			for _, op := range g.user.gateway.snapshotOperations()[start:] {
				raw, _ := feishu.FreezePrivateOperation(op, g.app.daemonLifecycleID)
				t.Logf("%s operation=%s title=%q card=%s", label, op.Kind, op.CardTitle, raw)
			}
			t.Fatalf("%s did not show expected result %v", label, terms)
		case <-g.user.ctx.Done():
			t.Fatalf("%s timed out", label)
		case <-time.After(15 * time.Millisecond):
		}
	}
}

func workflowOpenHistoryDetail(t *testing.T, g graphEnv, start int) {
	t.Helper()
	deadline := time.After(9 * time.Second)
	for {
		for _, op := range g.user.gateway.snapshotOperations()[start:] {
			if op.Kind != feishu.OperationUpdateCard {
				continue
			}
			raw, err := feishu.FreezePrivateOperation(op, g.app.daemonLifecycleID)
			if err != nil {
				continue
			}
			var doc any
			if json.Unmarshal(raw, &doc) != nil {
				continue
			}
			value := findPrivateCallback(doc, "history_detail")
			if value == nil {
				continue
			}
			message, err := g.app.applications.router.PublishedMessage(g.user.ctx, "tenant", "app", "private-chat", op.MessageID)
			if err != nil || message.CardRef != value["private_view"] {
				continue
			}
			click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "history-detail", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: op.MessageID, Kind: "card", Values: value, Option: "history-turn", ReceivedAt: time.Now()}
			before := len(g.user.gateway.snapshotOperations())
			response, err := g.app.applications.hook.ReceiveInteractionResponse(g.user.ctx, click)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(response.RawBody), "fixture historical message") {
				workflowWait(t, g, before, "history detail", "fixture historical message")
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("history detail callback was not ready")
		case <-g.user.ctx.Done():
			t.Fatal("history detail timed out")
		case <-time.After(15 * time.Millisecond):
		}
	}
}

func workflowReviewDiscardValue(v any) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		if x["kind"] == "page_local_action" && x["action_kind"] == "surface.button.review_discard" && x["private_view"] != nil {
			return x
		}
		for _, child := range x {
			if found := workflowReviewDiscardValue(child); found != nil {
				return found
			}
		}
	case []any:
		for _, child := range x {
			if found := workflowReviewDiscardValue(child); found != nil {
				return found
			}
		}
	}
	return nil
}

func workflowClickReviewDiscard(t *testing.T, g graphEnv, start int) {
	t.Helper()
	deadline := time.After(9 * time.Second)
	for {
		for _, op := range g.user.gateway.snapshotOperations()[start:] {
			raw, err := feishu.FreezePrivateOperation(op, g.app.daemonLifecycleID)
			if err != nil {
				continue
			}
			var doc any
			if json.Unmarshal(raw, &doc) != nil {
				continue
			}
			value := workflowReviewDiscardValue(doc)
			if value == nil {
				continue
			}
			message, err := g.app.applications.router.PublishedMessage(g.user.ctx, "tenant", "app", "private-chat", op.MessageID)
			if err != nil || message.CardRef != value["private_view"] {
				continue
			}
			before := len(g.user.gateway.snapshotOperations())
			response, err := g.app.applications.hook.ReceiveInteractionResponse(g.user.ctx, platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "review-discard", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: op.MessageID, Kind: "card", Values: value, ReceivedAt: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(response.RawBody), "已退出审阅") {
				workflowWait(t, g, before, "review exit", "已退出审阅")
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("review discard callback was not ready")
		case <-g.user.ctx.Done():
			t.Fatal("review discard timed out")
		case <-time.After(15 * time.Millisecond):
		}
	}
}

func TestPrivateSDKUserWorkflowHistoryGoalAndContinuation(t *testing.T) {
	originalDetect, originalApply := detectAutostart, applyAutostart
	t.Cleanup(func() { detectAutostart, applyAutostart = originalDetect, originalApply })
	var autostartCalls atomic.Int32
	detectAutostart = func(string) (install.AutostartStatus, error) {
		return install.AutostartStatus{Supported: true, CanApply: true, Status: "disabled"}, nil
	}
	applyAutostart = func(install.AutostartApplyOptions) (install.AutostartStatus, error) {
		autostartCalls.Add(1)
		return install.AutostartStatus{Supported: true, Configured: true, Enabled: true, Status: "enabled"}, nil
	}
	cwd := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-q", cwd)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(cwd, "review.txt"), []byte("changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var sentFiles <-chan string
	previewer := &workflowPreviewer{}
	g := newGraphEnv(t, cwd, filepath.Join(t.TempDir(), "other"), func(app *App, gateway *messageIDAssigningGateway) {
		sentFiles = setupAssembledSDKFileGateway(t, app, gateway)
		app.SetFinalBlockPreviewer(previewer)
	})
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3-dev.1\\n'; exit; fi\nPRIVATE_WORKFLOW_NATIVE=1 exec %q -test.run '^TestPrivateUserWorkflowNativeProcess$'\n", exe)
	if err = os.WriteFile(g.app.headlessRuntime.CodexRealBinary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	u := g.user
	before := len(u.gateway.snapshotOperations())
	u.send(g.app, "cold-hi", "hi")
	workflowWait(t, g, before, "cold guidance", "/list")
	u.chooseNew(g.app, "workspace")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "first", "first message")
	u.waitFinal(g.app, before, "first", "workflow final")
	workflowWait(t, g, before, "native diff preview", "https://preview.example/workflow-diff")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "history", "/history")
	workflowWait(t, g, before, "history list", "fixture historical me")
	workflowOpenHistoryDetail(t, g, before)
	for _, v := range []struct {
		id, text, label string
		terms           []string
	}{{"goal-read", "/goal", "goal query", []string{"当前会话没有 Goal"}}, {"goal-set", "/goal new fixture goal", "goal set", []string{"状态：active"}}, {"goal-clear", "/goal clear --confirm", "goal clear", []string{"Goal 已清除"}}} {
		before = len(u.gateway.snapshotOperations())
		u.send(g.app, v.id, v.text)
		workflowWait(t, g, before, v.label, v.terms...)
	}
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "compact", "/compact")
	workflowWait(t, g, before, "compact completed", "上下文已压缩")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "review", "/review uncommitted")
	u.waitFinal(g.app, before, "review", "review complete")
	workflowClickReviewDiscard(t, g, before)
	exerciseAssembledSDKSendFile(t, u.ctx, g.app, u.gateway, platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", ReceivedAt: time.Now()}, cwd, sentFiles)
	for _, page := range []struct{ id, command, title string }{{"admin", "/admin", "系统管理"}, {"debug", "/debug", "调试"}, {"upgrade", "/upgrade", "升级系统"}} {
		before = len(u.gateway.snapshotOperations())
		u.send(g.app, page.id, page.command)
		workflowWait(t, g, before, page.id+" navigation", page.title)
	}
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "admin-setting", "/admin autostart on")
	workflowWait(t, g, before, "admin autostart result", "自动启动")
	if autostartCalls.Load() != 1 {
		t.Fatalf("admin backend called %d times", autostartCalls.Load())
	}
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "mcp-fail", "/mcp docs")
	workflowWait(t, g, before, "MCP OAuth failure", "Codex 拒绝了这次 MCP 服务认证请求")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "mcp-retry", "/mcp docs")
	workflowWait(t, g, before, "MCP OAuth retry", "Codex 拒绝了这次 MCP 服务认证请求")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "after-goal", "continue after goal")
	u.waitFinal(g.app, before, "after-goal", "workflow final")
	trace, err := os.ReadFile(g.trace)
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"thread/read", "thread/goal/get", "thread/goal/set", "thread/goal/clear", "thread/compact/start", "mcpServer/oauth/login"} {
		if !strings.Contains(string(trace), method) {
			t.Fatalf("native request missing %s", method)
		}
	}
}
