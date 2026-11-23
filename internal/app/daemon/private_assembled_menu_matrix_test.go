package daemon

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

func assembledMenuValue(v any, argument string) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		if x["kind"] == "page_local_action" && x["action_arg"] == argument && x["private_view"] != nil {
			return x
		}
		for _, v := range x {
			if result := assembledMenuValue(v, argument); result != nil {
				return result
			}
		}
	case []any:
		for _, v := range x {
			if result := assembledMenuValue(v, argument); result != nil {
				return result
			}
		}
	}
	return nil
}

func assembledMenuBack(v any) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		if x["kind"] == "page_local_action" && x["action_kind"] == "surface.command.menu" && (x["action_arg"] == nil || x["action_arg"] == "") && x["private_view"] != nil {
			return x
		}
		for _, child := range x {
			if hit := assembledMenuBack(child); hit != nil {
				return hit
			}
		}
	case []any:
		for _, child := range x {
			if hit := assembledMenuBack(child); hit != nil {
				return hit
			}
		}
	}
	return nil
}

func assembledActionKind(v any, kind string) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		if x["kind"] == "page_local_action" && x["action_kind"] == kind && x["private_view"] != nil {
			return x
		}
		for _, child := range x {
			if hit := assembledActionKind(child, kind); hit != nil {
				return hit
			}
		}
	case []any:
		for _, child := range x {
			if hit := assembledActionKind(child, kind); hit != nil {
				return hit
			}
		}
	}
	return nil
}
func TestPrivateAssembledSDKMenuMatrix(t *testing.T) {
	app, gateway := newApplicationRuntimeTestApp(t)
	r := app.applications
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "matrix-menu", MessageID: "matrix-source", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"/menu"}`, ReceivedAt: time.Now()}
	if err := r.hook.Receive(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	errors := make(chan error, 4)
	if err := r.startWorkers(t.Context(), errors); err != nil {
		t.Fatal(err)
	}
	ops := gateway.waitForOperationCount(1, 5*time.Second)
	if len(ops) == 0 {
		t.Fatal("menu missing")
	}
	var card feishu.Operation
	for _, op := range ops {
		if op.Kind == feishu.OperationSendCard && op.MessageID != "" {
			card = op
			break
		}
	}
	if card.MessageID == "" {
		t.Fatal("menu receipt missing")
	}
	for deadline := time.Now().Add(5 * time.Second); ; {
		if _, err := r.router.PublishedMessage(t.Context(), "tenant", "app", "private-chat", card.MessageID); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("menu receipt not registered")
		}
		time.Sleep(10 * time.Millisecond)
	}
	frozen, err := feishu.FreezePrivateOperation(card, app.daemonLifecycleID)
	if err != nil {
		t.Fatal(err)
	}
	var root any
	if err = json.Unmarshal(frozen, &root); err != nil {
		t.Fatal(err)
	}
	for i, tc := range []struct{ argument, title string }{{"current_work", "基本命令"}, {"send_settings", "参数设置"}, {"common_tools", "常用工具"}} {
		value := assembledMenuValue(root, tc.argument)
		if value == nil {
			t.Fatalf("menu %s callback missing: %s", tc.argument, frozen)
		}
		click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: fmt.Sprintf("matrix-%d", i), SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: card.MessageID, Kind: "card", Values: value, ReceivedAt: time.Now()}
		response, err := r.hook.ReceiveInteractionResponse(t.Context(), click)
		if err != nil {
			t.Fatalf("%s: %v", tc.argument, err)
		}
		if !strings.Contains(string(response.RawBody), tc.title) {
			t.Fatalf("%s lacks %q: %s", tc.argument, tc.title, response.RawBody)
		}
		again, err := r.hook.ReceiveInteractionResponse(t.Context(), click)
		if err != nil || string(again.RawBody) != string(response.RawBody) {
			t.Fatalf("%s replay: %v", tc.argument, err)
		}
		var page any
		if err := json.Unmarshal(response.RawBody, &page); err != nil {
			t.Fatal(err)
		}
		back := assembledMenuBack(page)
		if back == nil {
			t.Fatalf("%s missing stamped breadcrumb: %s", tc.argument, response.RawBody)
		}
		click.EventID = fmt.Sprintf("matrix-back-%d", i)
		click.Values = back
		returned, err := r.hook.ReceiveInteractionResponse(t.Context(), click)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(returned.RawBody, &root); err != nil {
			t.Fatalf("back response: %s: %v", returned.RawBody, err)
		}
		// The corresponding typed command enters through the same SDK ingress
		// and produces the same page title for this owner.
		before := len(gateway.snapshotOperations())
		message := raw
		message.EventID = fmt.Sprintf("matrix-slash-%d", i)
		message.MessageID = message.EventID
		message.RawContent = fmt.Sprintf(`{"text":%q}`, "/menu "+tc.argument)
		if err = r.hook.Receive(t.Context(), message); err != nil {
			t.Fatal(err)
		}
		matched := false
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
			for _, op := range gateway.snapshotOperations()[before:] {
				if op.Kind == feishu.OperationSendCard {
					body, e := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
					if e == nil && strings.Contains(string(body), tc.title) {
						matched = true
						break
					}
				}
			}
			if matched {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !matched {
			t.Fatalf("slash /menu %s missing same %q page", tc.argument, tc.title)
		}
	}
	for i, tc := range []struct{ kind, expected string }{{"surface.command.workspace", "工作区与会话"}, {"surface.command.admin", "系统管理"}} {
		before := len(gateway.snapshotOperations())
		message := raw
		message.EventID = fmt.Sprintf("root-reopen-%d", i)
		message.MessageID = message.EventID
		if err = r.hook.Receive(t.Context(), message); err != nil {
			t.Fatal(err)
		}
		var op feishu.Operation
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
			for _, candidate := range gateway.snapshotOperations()[before:] {
				if candidate.Kind == feishu.OperationSendCard && candidate.MessageID != "" {
					op = candidate
					break
				}
			}
			if op.MessageID != "" {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if op.MessageID == "" {
			t.Fatal("reopened root missing")
		}
		for deadline := time.Now().Add(5 * time.Second); ; {
			if _, e := r.router.PublishedMessage(t.Context(), "tenant", "app", "private-chat", op.MessageID); e == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("root receipt missing")
			}
			time.Sleep(10 * time.Millisecond)
		}
		frozen, e := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
		if e != nil {
			t.Fatal(e)
		}
		var doc any
		if e = json.Unmarshal(frozen, &doc); e != nil {
			t.Fatal(e)
		}
		value := assembledActionKind(doc, tc.kind)
		if value == nil {
			t.Fatalf("%s missing", tc.kind)
		}
		click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: fmt.Sprintf("root-action-%d", i), SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: op.MessageID, Kind: "card", Values: value, ReceivedAt: time.Now()}
		response, e := r.hook.ReceiveInteractionResponse(t.Context(), click)
		if e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(string(response.RawBody), tc.expected) {
			t.Fatalf("%s missing %q: %s", tc.kind, tc.expected, response.RawBody)
		}
	}
	owner := privatechat.Owner{TenantID: "tenant", AppID: "app", GatewayID: "gateway", ChatID: "private-chat", UserID: "user", SurfaceID: gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user")}
	for i, command := range []string{"/autowhip on", "/autocontinue on", "/reasoning high", "/plan on", "/verbose verbose"} {
		message := raw
		message.EventID = fmt.Sprintf("matrix-setting-%d", i)
		message.MessageID = message.EventID
		message.RawContent = fmt.Sprintf(`{"text":%q}`, command)
		if err = r.hook.Receive(t.Context(), message); err != nil {
			t.Fatal(err)
		}
	}
	for deadline := time.Now().Add(5 * time.Second); ; {
		checkpoint, e := r.journal.LoadCheckpoint(t.Context(), owner, 1)
		if e == nil && checkpoint.Core.AutoWhip.Enabled && checkpoint.Core.AutoContinue.Enabled && checkpoint.Core.PromptOverride.ReasoningEffort == "high" && checkpoint.Core.PlanMode == "on" && checkpoint.Core.Verbosity == "verbose" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("settings were not persisted: %+v (%v)", checkpoint.Core, e)
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case err := <-errors:
		t.Fatal(err)
	default:
	}
}

func TestPrivateAssembledSDKSlashBaselineMatrix(t *testing.T) {
	app, gateway := newApplicationRuntimeTestApp(t)
	r := app.applications
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", ReceivedAt: time.Now()}
	errs := make(chan error, 4)
	if err := r.startWorkers(t.Context(), errs); err != nil {
		t.Fatal(err)
	}
	for i, command := range []string{"/menu", "/status", "/workspace", "/list", "/history", "/mode", "/model", "/reasoning", "/access", "/plan", "/verbose", "/autowhip", "/autocontinue", "/help", "/review", "/sendfile", "/stop", "/new", "/admin", "/debug", "/upgrade"} {
		before := len(gateway.snapshotOperations())
		m := raw
		m.EventID = fmt.Sprintf("slash-baseline-%d", i)
		m.MessageID = m.EventID
		m.RawContent = fmt.Sprintf(`{"text":%q}`, command)
		if err := r.hook.Receive(t.Context(), m); err != nil {
			t.Fatalf("%s admission: %v", command, err)
		}
		var op feishu.Operation
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
			ops := gateway.snapshotOperations()
			if len(ops) > before {
				op = ops[before]
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if op.Kind == "" {
			t.Fatalf("%s produced no visible response", command)
		}
		if op.SurfaceSessionID != gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user") || op.GatewayID != "gateway" || op.ChatID != "private-chat" {
			t.Fatalf("%s crossed owner: %+v", command, op)
		}
		select {
		case err := <-errs:
			t.Fatal(err)
		default:
		}
	}
}
