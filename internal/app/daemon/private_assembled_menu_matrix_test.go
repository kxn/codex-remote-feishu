package daemon

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/ingressmapper"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
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
	testPrivateAssembledSDKSlashBaselineMatrix(t, false)
}
func TestPrivateAssembledSDKSlashBoundMatrix(t *testing.T) {
	testPrivateAssembledSDKSlashBaselineMatrix(t, true)
}
func testPrivateAssembledSDKSlashBaselineMatrix(t *testing.T, bound bool) {
	app, gateway := newApplicationRuntimeTestApp(t)
	r := app.applications
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", ReceivedAt: time.Now()}
	owner := privatechat.Owner{GatewayID: "gateway", AppID: "app", TenantID: "tenant", SurfaceID: gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user"), ChatID: "private-chat", UserID: "user"}
	if bound {
		witness := raw
		witness.EventID = "bound-witness"
		witness.MessageID = "bound-witness"
		witness.RawContent = `{"text":"/menu"}`
		e, err := ingressmapper.Message(witness, 1)
		if err != nil {
			t.Fatal(err)
		}
		record, _, err := r.router.Admit(t.Context(), e, r.ingress.resolver)
		if err != nil || record.Owner == nil {
			t.Fatalf("private owner: %v", err)
		}
		native := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: record.Owner.ID, Epoch: record.Owner.Epoch, CarrierIncarnation: "matrix-inc", InstanceID: "matrix-instance", HostID: "fixture", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "fixture-history"}
		lease, err := r.journal.OpenNativeLease(t.Context(), sdkStageAuthority{}, native, owner, record.Owner.Epoch)
		if err != nil {
			t.Fatal(err)
		}
		cwd := t.TempDir()
		hello := agentproto.Hello{PrivateNativeProtocolVersion: agentproto.PrivateNativeProtocolVersion, CarrierIncarnation: native.CarrierIncarnation, Instance: agentproto.InstanceHello{InstanceID: native.InstanceID, Backend: agentproto.BackendCodex, WorkspaceRoot: cwd, WorkspaceKey: cwd, Source: "headless", Managed: true}}
		if err = r.processor.RegisterNativeHello(t.Context(), lease, hello); err != nil {
			t.Fatal(err)
		}
		inst := r.core.Service().Instance(native.InstanceID)
		inst.ObservedFocusedThreadID = "matrix-thread"
		inst.Threads["matrix-thread"] = &state.ThreadRecord{ThreadID: "matrix-thread", CWD: cwd, WorkspaceKey: cwd}
		if _, err = r.core.Action(control.Action{Kind: control.ActionAttachInstance, GatewayID: owner.GatewayID, SurfaceSessionID: owner.SurfaceID, ChatID: owner.ChatID, ActorUserID: owner.UserID, InstanceID: native.InstanceID}); err != nil {
			t.Fatal(err)
		}
	}
	errs := make(chan error, 4)
	if err := r.startWorkers(t.Context(), errs); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(app.headlessRuntime.Paths.StateDir, "private-chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cases := []struct{ command, title, body string }{
		{"/menu", "命令菜单", ""}, {"/status", "当前状态", ""}, {"/workspace", "工作区与会话", ""}, {"/list", "切换工作区与会话", ""},
		{"/history", "系统提示", "当前还没有接管在线实例"}, {"/mode", "切换模式", ""}, {"/model", "使用模型", ""},
		{"/reasoning", "推理强度", ""}, {"/access", "执行权限", ""}, {"/plan", "Plan 模式", ""}, {"/verbose", "提示详细程度", ""},
		{"/autowhip", "AutoWhip", ""}, {"/autocontinue", "自动继续", ""}, {"/help", "命令帮助", ""}, {"/review", "审阅代码变更", ""},
		{"/sendfile", "当前不能发送文件", ""}, {"/stop", "没有正在运行的推理", "没有正在运行"}, {"/new", "系统提示", "没有接管任何工作区"},
		{"/admin", "系统管理", ""}, {"/debug", "调试", ""}, {"/upgrade", "升级系统", ""},
		{"/compact", "系统提示", "没有接管任何工作区"}, {"/steerall", "系统提示", "没有可并入"}, {"/goal", "Goal 目标", ""},
		{"/codexprofile", "切换 Codex Profile", ""}, {"/claudeprofile", "系统提示", "不在 Claude 模式"}, {"/opencodeprofile", "系统提示", "不在 OpenCode 模式"},
		{"/workspace list", "切换工作区与会话", ""}, {"/workspace new", "新建工作区", ""}, {"/workspace new dir", "从目录新建工作区", ""},
		{"/workspace new git", "从 GIT URL 新建工作区", ""}, {"/workspace new worktree", "从 Worktree 新建工作区", ""},
		{"/workspace detach", "系统提示", "没有已接管"}, {"/use", "切换工作区与会话", ""}, {"/useall", "切换工作区与会话", ""},
		{"/detach", "系统提示", "没有已接管"}, {"/mcpoauth", "MCP 服务认证", "请指定"}, {"/project", "项目资料与分享", ""},
	}
	if bound {
		// These navigation/parameter pages have no native dependency. Commands
		// requiring live native receipts use the real wrapper journeys instead.
		filtered := cases[:0]
		for _, tc := range cases {
			switch tc.command {
			case "/history", "/sendfile", "/new", "/compact", "/goal", "/workspace detach", "/detach":
				continue
			}
			filtered = append(filtered, tc)
		}
		cases = filtered
	}
	for i, tc := range cases {
		command := tc.command
		m := raw
		m.EventID = fmt.Sprintf("slash-baseline-%d", i)
		m.MessageID = m.EventID
		m.RawContent = fmt.Sprintf(`{"text":%q}`, command)
		if err := r.hook.Receive(t.Context(), m); err != nil {
			t.Fatalf("%s admission: %v", command, err)
		}
		var op feishu.Operation
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
			// Follow committed ingress→effect→host receipt→effect revisions;
			// a previous asynchronous card cannot satisfy this command.
			rows, e := db.QueryContext(t.Context(), `WITH RECURSIVE lineage(revision) AS (
 SELECT a.revision FROM ingress i JOIN private_applied a ON a.seq=i.seq WHERE i.tenant=? AND i.app_id=? AND i.event_id=? AND i.generation=1
 UNION SELECT applied.revision FROM private_effect source JOIN lineage l ON l.revision=source.revision JOIN private_effect_applied applied ON applied.effect_id=source.id WHERE source.tenant=? AND source.app_id=? AND source.gateway=? AND source.surface=? AND source.chat=? AND source.user_id=? AND source.generation=1)
 SELECT e.id FROM private_effect e JOIN lineage l ON l.revision=e.revision WHERE e.tenant=? AND e.app_id=? AND e.gateway=? AND e.surface=? AND e.chat=? AND e.user_id=? AND e.generation=1`, "tenant", "app", m.EventID, "tenant", "app", "gateway", owner.SurfaceID, owner.ChatID, owner.UserID, "tenant", "app", "gateway", owner.SurfaceID, owner.ChatID, owner.UserID)
			if e != nil {
				t.Fatal(e)
			}
			var ids []string
			for rows.Next() {
				var id string
				if e = rows.Scan(&id); e != nil {
					t.Fatal(e)
				}
				ids = append(ids, id)
			}
			if e = rows.Err(); e != nil {
				t.Fatal(e)
			}
			rows.Close()
			for _, id := range ids {
				pub, e := r.journal.LoadPublication(t.Context(), owner, 1, id)
				if e != nil || pub.EffectState != "confirmed" {
					continue
				}
				for _, item := range pub.Operations {
					if item.State != "confirmed" {
						continue
					}
					planned, e := feishu.ThawPrivateOperation(item.Body)
					if e != nil || planned.Kind != feishu.OperationSendCard || planned.CardTitle != tc.title || (tc.body != "" && !strings.Contains(planned.CardBody, tc.body)) {
						continue
					}
					var receipt feishu.PrivateOperationReceipt
					if json.Unmarshal(item.Receipt, &receipt) != nil || receipt.MessageID == "" {
						continue
					}
					for _, actual := range gateway.snapshotOperations() {
						if actual.Kind == feishu.OperationSendCard && actual.MessageID == receipt.MessageID && actual.CardTitle == tc.title {
							op = actual
							break
						}
					}
				}
			}
			if op.Kind != "" {
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
