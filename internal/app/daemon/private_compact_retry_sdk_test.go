package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

func TestPrivateCompactRetryNativePeer(t *testing.T) {
	if os.Getenv("PRIVATE_COMPACT_PEER") != "1" {
		return
	}
	cwd, _ := os.Getwd()
	starts, turns := 0, 0
	failed := false
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
		reply := func(v any) { b, _ := json.Marshal(map[string]any{"id": f.ID, "result": v}); fmt.Println(string(b)) }
		switch f.Method {
		case "initialize":
			reply(map[string]any{})
		case "thread/start":
			starts++
			id := fmt.Sprintf("compact-thread-%d", starts)
			reply(map[string]any{"model": "fixture-model", "thread": map[string]any{"id": id, "cwd": cwd}})
			fmt.Printf(`{"method":"thread/started","params":{"thread":{"id":%q,"cwd":%q,"source":"cli"}}}`+"\n", id, cwd)
		case "thread/resume":
			id, _ := f.Params["threadId"].(string)
			if id == "" {
				os.Exit(51)
			}
			if _, err := os.Stat(os.Getenv("PRIVATE_COMPACT_FAIL_ARM")); err == nil && !failed {
				failed = true
				fmt.Printf(`{"id":%q,"error":{"code":-32603,"message":"controlled resume failure"}}`+"\n", f.ID)
				continue
			}
			reply(map[string]any{"model": "fixture-model", "thread": map[string]any{"id": id, "cwd": cwd}})
		case "turn/start":
			id, _ := f.Params["threadId"].(string)
			if id == "" {
				os.Exit(52)
			}
			turns++
			turn := fmt.Sprintf("compact-turn-%d", turns)
			reply(map[string]any{"turn": map[string]any{"id": turn}})
			fmt.Printf(`{"method":"turn/started","params":{"threadId":%q,"turn":{"id":%q}}}`+"\n", id, turn)
			fmt.Printf(`{"method":"item/completed","params":{"threadId":%q,"turnId":%q,"item":{"id":"answer","type":"agentMessage","text":"compact dialogue complete"}}}`+"\n", id, turn)
			fmt.Printf(`{"method":"turn/completed","params":{"threadId":%q,"turn":{"id":%q,"status":"completed"}}}`+"\n", id, turn)
		case "thread/compact/start":
			id, _ := f.Params["threadId"].(string)
			if id != "compact-thread-1" || !failed {
				os.Exit(53)
			}
			reply(map[string]any{})
			fmt.Printf(`{"method":"turn/started","params":{"threadId":%q,"turn":{"id":"compact-op"}}}`+"\n", id)
			fmt.Printf(`{"method":"item/completed","params":{"threadId":%q,"turnId":"compact-op","item":{"id":"compact","type":"contextCompaction","status":"completed"}}}`+"\n", id)
			fmt.Printf(`{"method":"turn/completed","params":{"threadId":%q,"turn":{"id":"compact-op","status":"completed"}}}`+"\n", id)
		case "thread/goal/get":
			reply(map[string]any{"goal": nil})
		case "thread/read":
			id, _ := f.Params["threadId"].(string)
			reply(map[string]any{"thread": map[string]any{"id": id, "cwd": cwd, "status": map[string]any{"type": "idle"}}})
		case "thread/list", "model/list":
			reply(map[string]any{"data": []any{}})
		}
	}
	os.Exit(0)
}

func compactChooseFirstSession(t *testing.T, f *continuityFixture) {
	t.Helper()
	u := f.user
	n := len(f.gateway.snapshotOperations())
	u.send(f.app, "compact-use", "/use")
	var card feishu.Operation
	var doc any
	for card.MessageID == "" {
		for _, op := range f.gateway.snapshotOperations()[n:] {
			if op.MessageID == "" {
				continue
			}
			raw, e := feishu.FreezePrivateOperation(op, f.app.daemonLifecycleID)
			var candidate any
			if e == nil && json.Unmarshal(raw, &candidate) == nil && findPrivateCallback(candidate, "target_picker_select_session") != nil {
				card = op
				doc = candidate
				break
			}
		}
		if card.MessageID == "" {
			select {
			case <-f.ctx.Done():
				t.Fatal("session picker absent")
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "compact-pick", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: card.MessageID, Kind: "card", Values: findPrivateCallback(doc, "target_picker_select_session"), Option: "thread:compact-thread-1", ReceivedAt: time.Now()}
	response, e := f.app.applications.hook.ReceiveInteractionResponse(f.ctx, click)
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(response.RawBody, &doc); e != nil {
		t.Fatal(e)
	}
	click.EventID = "compact-confirm"
	click.Option = ""
	click.Values = findPrivateCallback(doc, "target_picker_confirm")
	if click.Values == nil {
		t.Fatal("session confirm absent")
	}
	response, e = f.app.applications.hook.ReceiveInteractionResponse(f.ctx, click)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(response.RawBody), "已切换会话") {
		u.waitVisible(f.app, n, "已切换会话")
	}
}

func TestPrivateSDKCompactResumeFailureRetryAndNextTurn(t *testing.T) {
	f := newContinuityFixtureBare(t, nil)
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	arm := f.cwd + "/compact-fail-arm"
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3-dev.1\\n'; exit; fi\nPRIVATE_COMPACT_PEER=1 PRIVATE_COMPACT_FAIL_ARM=%q exec %q -test.run '^TestPrivateCompactRetryNativePeer$'\n", arm, exe)
	if e = os.WriteFile(f.app.headlessRuntime.CodexRealBinary, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	u := f.user
	n := len(f.gateway.snapshotOperations())
	u.send(f.app, "compact-cold", "hi")
	u.waitVisible(f.app, n, "/list")
	u.chooseNew(f.app, "compact-new")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "compact-first", "first")
	u.waitFinal(f.app, n, "compact-first", "compact dialogue complete")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "compact-new2", "/new")
	u.waitVisible(f.app, n, "下一条文本")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "compact-second", "second")
	u.waitFinal(f.app, n, "compact-second", "compact dialogue complete")
	compactChooseFirstSession(t, f)
	if e = os.WriteFile(arm, []byte("fail next native resume"), 0600); e != nil {
		t.Fatal(e)
	}
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "compact-failed", "/compact")
	u.waitVisible(f.app, n, "上下文压缩失败")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "compact-retry", "/compact")
	u.waitVisible(f.app, n, "上下文已压缩")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "compact-after", "continue")
	u.waitFinal(f.app, n, "compact-after", "compact dialogue complete")
}
