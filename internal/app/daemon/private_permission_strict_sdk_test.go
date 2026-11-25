package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPrivatePermissionStrictNativePeer(t *testing.T) {
	mode := os.Getenv("AUDIT_PERMISSION_MODE")
	if mode == "" {
		return
	}
	cwd, _ := os.Getwd()
	turn := 0
	pending := false
	sc := bufio.NewScanner(os.Stdin)
	trace := func(s string) {
		f, e := os.OpenFile(os.Getenv("FIXTURE_TRACE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e == nil {
			fmt.Fprintln(f, s)
			f.Close()
		}
	}
	grant := map[string]any{"network": map[string]any{"enabled": true}, "fileSystem": map[string]any{"read": []any{"/tmp/audit-read"}, "write": []any{"/tmp/audit-write"}}}
	for sc.Scan() {
		var f struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Result map[string]any `json:"result"`
		}
		if json.Unmarshal(sc.Bytes(), &f) != nil {
			continue
		}
		if f.Method == "" && f.ID == "perm-1" && pending {
			permissions, ok := f.Result["permissions"].(map[string]any)
			valid := ok && f.Result["decision"] == nil && (mode == "decline" && len(permissions) == 0 && f.Result["scope"] == "turn" || mode == "accept" && reflect.DeepEqual(permissions, grant) && (f.Result["scope"] == "turn" || f.Result["scope"] == "session"))
			if !valid {
				trace("BAD_RESPONSE " + string(sc.Bytes()))
				os.Exit(52)
			}
			trace("VALID_RESPONSE " + mode)
			pending = false
			fmt.Println(`{"method":"serverRequest/resolved","params":{"requestId":"perm-1","threadId":"audit-thread","turnId":"turn-1"}}`)
			fmt.Println(`{"method":"item/completed","params":{"threadId":"audit-thread","turnId":"turn-1","item":{"id":"answer","type":"agentMessage","text":"permission complete"}}}`)
			fmt.Println(`{"method":"turn/completed","params":{"threadId":"audit-thread","turn":{"id":"turn-1","status":"completed"}}}`)
			continue
		}
		switch f.Method {
		case "initialize":
			fmt.Printf(`{"id":%q,"result":{}}`+"\n", f.ID)
		case "thread/start", "thread/resume":
			fmt.Printf(`{"id":%q,"result":{"model":"fixture-model","thread":{"id":"audit-thread","cwd":%q}}}`+"\n", f.ID, cwd)
		case "turn/start":
			turn++
			id := fmt.Sprintf("turn-%d", turn)
			fmt.Printf(`{"id":%q,"result":{"turn":{"id":%q}}}`+"\n", f.ID, id)
			fmt.Printf(`{"method":"turn/started","params":{"threadId":"audit-thread","turn":{"id":%q}}}`+"\n", id)
			if turn == 1 {
				pending = true
				b, _ := json.Marshal(map[string]any{"id": "perm-1", "method": "item/permissions/requestApproval", "params": map[string]any{"threadId": "audit-thread", "turnId": id, "itemId": "perm-item", "cwd": cwd, "startedAtMs": int64(1790000000000), "reason": "fixture permission", "permissions": grant}})
				fmt.Println(string(b))
				continue
			}
			fmt.Printf(`{"method":"item/completed","params":{"threadId":"audit-thread","turnId":%q,"item":{"id":"next","type":"agentMessage","text":"next complete"}}}`+"\n", id)
			fmt.Printf(`{"method":"turn/completed","params":{"threadId":"audit-thread","turn":{"id":%q,"status":"completed"}}}`+"\n", id)
		case "thread/read":
			fmt.Printf(`{"id":%q,"result":{"thread":{"id":"audit-thread","cwd":%q,"status":{"type":"idle"}}}}`+"\n", f.ID, cwd)
		case "thread/goal/get":
			fmt.Printf(`{"id":%q,"result":{"goal":null}}`+"\n", f.ID)
		case "thread/list", "model/list":
			fmt.Printf(`{"id":%q,"result":{"data":[]}}`+"\n", f.ID)
		}
	}
	os.Exit(0)
}
func auditPermissionValue(v any, option string) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		if x["kind"] == "request_respond" && x["request_option_id"] == option && x["private_view"] != nil {
			return x
		}
		for _, z := range x {
			if a := auditPermissionValue(z, option); a != nil {
				return a
			}
		}
	case []any:
		for _, z := range x {
			if a := auditPermissionValue(z, option); a != nil {
				return a
			}
		}
	}
	return nil
}
func TestPrivateSDKPermissionStrictApproval(t *testing.T) {
	for _, mode := range []string{"accept", "decline"} {
		t.Run(mode, func(t *testing.T) {
			f := newContinuityFixtureBare(t, nil)
			exe, e := os.Executable()
			if e != nil {
				t.Fatal(e)
			}
			script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3-dev.1\\n'; exit; fi\nAUDIT_PERMISSION_MODE=%s exec %q -test.run '^TestPrivatePermissionStrictNativePeer$'\n", mode, exe)
			if e = os.WriteFile(f.app.headlessRuntime.CodexRealBinary, []byte(script), 0700); e != nil {
				t.Fatal(e)
			}
			u := f.user
			n := len(f.gateway.snapshotOperations())
			u.send(f.app, "cold", "hi")
			u.waitVisible(f.app, n, "/list")
			u.chooseNew(f.app, "initial")
			n = len(f.gateway.snapshotOperations())
			u.send(f.app, "permission-input", "request permission")
			var card feishu.Operation
			var value map[string]any
			for value == nil {
				for _, op := range f.gateway.snapshotOperations()[n:] {
					if op.MessageID == "" {
						continue
					}
					raw, e := feishu.FreezePrivateOperation(op, f.app.daemonLifecycleID)
					var doc any
					if e == nil && json.Unmarshal(raw, &doc) == nil {
						if value = auditPermissionValue(doc, mode); value != nil {
							card = op
							break
						}
					}
				}
				if value == nil {
					select {
					case <-f.ctx.Done():
						t.Fatal("permission card absent")
					case <-time.After(20 * time.Millisecond):
					}
				}
			}
			click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "permission-click", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: card.MessageID, Kind: "card", Values: value, ReceivedAt: time.Now()}
			foreign := click
			foreign.EventID = "foreign-permission-click"
			foreign.SenderIDs.OpenID = "other-user"
			_, _ = f.app.applications.hook.ReceiveInteractionResponse(f.ctx, foreign)
			if data, _ := os.ReadFile(f.trace); strings.Contains(string(data), "VALID_RESPONSE") || strings.Contains(string(data), "BAD_RESPONSE") {
				t.Fatal("foreign user reached the native permission request")
			}
			if _, e = f.app.applications.hook.ReceiveInteractionResponse(f.ctx, click); e != nil {
				t.Fatal(e)
			}
			for {
				data, _ := os.ReadFile(f.trace)
				if strings.Contains(string(data), "BAD_RESPONSE") {
					t.Fatalf("native rejected permission response: %s", data)
				}
				if strings.Contains(string(data), "VALID_RESPONSE") {
					break
				}
				select {
				case <-f.ctx.Done():
					t.Fatal("native did not receive valid permission response")
				case <-time.After(20 * time.Millisecond):
				}
			}
			u.waitFinal(f.app, n, "permission-input", "permission complete")
			_, _ = f.app.applications.hook.ReceiveInteractionResponse(f.ctx, click)
			n = len(f.gateway.snapshotOperations())
			u.send(f.app, "next", "next")
			u.waitFinal(f.app, n, "next", "next complete")
			if data, _ := os.ReadFile(f.trace); strings.Count(string(data), "VALID_RESPONSE ") != 1 {
				t.Fatalf("permission response was delivered more than once: %s", data)
			}
		})
	}
}
