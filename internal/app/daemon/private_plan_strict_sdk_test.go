package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

func findExecutePlan(v any) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		if x["kind"] == "plan_proposal" && x["option_id"] == "execute" && x["private_view"] != nil {
			return x
		}
		for _, v := range x {
			if r := findExecutePlan(v); r != nil {
				return r
			}
		}
	case []any:
		for _, v := range x {
			if r := findExecutePlan(v); r != nil {
				return r
			}
		}
	}
	return nil
}

func TestPrivateSDKPlanExecuteAndNextTurn(t *testing.T) {
	f := newContinuityFixtureBare(t, nil)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3-dev.1\\n'; exit; fi\nPRIVATE_GOAL_STRICT=1 PRIVATE_PLAN_STRICT=1 exec %q -test.run '^TestPrivateGoalStrictNativePeer$'\n", exe)
	if err = os.WriteFile(f.app.headlessRuntime.CodexRealBinary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	u := f.user
	n := len(f.gateway.snapshotOperations())
	u.send(f.app, "plan-cold", "hi")
	u.waitVisible(f.app, n, "/list")
	u.chooseNew(f.app, "plan-new")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "plan-first", "hello")
	u.waitFinal(f.app, n, "plan-first", "goal dialogue complete")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "plan-on", "/plan on")
	u.waitVisible(f.app, n, "Plan mode")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "plan-prompt", "prepare a plan")
	u.waitVisible(f.app, n, "提案计划")
	var value map[string]any
	var message string
	for value == nil {
		for _, op := range f.gateway.snapshotOperations()[n:] {
			if op.MessageID == "" {
				continue
			}
			raw, e := feishu.FreezePrivateOperation(op, f.app.daemonLifecycleID)
			var doc any
			if e == nil && json.Unmarshal(raw, &doc) == nil {
				if value = findExecutePlan(doc); value != nil {
					message = op.MessageID
					break
				}
			}
		}
		if value == nil {
			select {
			case <-f.ctx.Done():
				t.Fatal("plan execute button missing")
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	before := len(f.gateway.snapshotOperations())
	click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "plan-execute", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: message, Kind: "card", Values: value, ReceivedAt: time.Now()}
	if _, err = f.app.applications.hook.ReceiveInteractionResponse(f.ctx, click); err != nil {
		t.Fatal(err)
	}
	u.waitFinal(f.app, before, message, "goal dialogue complete")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "plan-next", "continue")
	u.waitFinal(f.app, n, "plan-next", "goal dialogue complete")
}
