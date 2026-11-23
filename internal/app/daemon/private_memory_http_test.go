package daemon

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/toolservicecontract"
)

func TestPrivateMemoryMCPSessionAuthenticatedWriteReadRevokeAndDrain(t *testing.T) {
	ctx, root := t.Context(), t.TempDir()
	h := &privateNativeHost{hostID: "host", stateDir: root}
	var err error
	h.store, err = openPrivateNativeHostStore(filepath.Join(root, "host.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer h.store.Close()
	h.registry, err = executionowner.Open(filepath.Join(root, "owners.db"), h)
	if err != nil {
		t.Fatal(err)
	}
	defer h.registry.Close()
	h.outbound, err = executionowner.NewOutbound(h.registry)
	if err != nil {
		t.Fatal(err)
	}
	h.journal, err = privatechat.OpenJournal(filepath.Join(root, "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer h.journal.Close()
	core := privatechat.New(nil, orchestrator.Config{})
	h.processor, err = privatechat.NewProcessor(core, h.journal, privateHostTestDecoder{}, privatechat.CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	n := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	o := privatechat.Owner{GatewayID: "gateway", SurfaceID: "surface", ChatID: "chat", UserID: "user", TenantID: "tenant", AppID: "app"}
	rec := privateCarrierRecord{Snapshot: agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: n, CWD: root, JournalPath: filepath.Join(root, "wrapper.db")}, Surface: o, SurfaceGeneration: 1}
	secret, err := h.store.prepare(ctx, rec)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.registry.Register(ctx, privateRegistration(n)); err != nil {
		t.Fatal(err)
	}
	process, err := hostinventory.CaptureProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err = h.store.transition(ctx, n.InstanceID, "prepared", "sending", hostinventory.ProcessIdentity{}); err != nil {
		t.Fatal(err)
	}
	if err = h.store.transition(ctx, n.InstanceID, "sending", "running", process); err != nil {
		t.Fatal(err)
	}
	lease, err := h.lease(ctx, rec, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.processor.RegisterNativeHello(ctx, lease, agentproto.Hello{PrivateNativeProtocolVersion: 1, CarrierIncarnation: n.CarrierIncarnation, Instance: agentproto.InstanceHello{InstanceID: n.InstanceID, Backend: n.Backend, WorkspaceRoot: root, WorkspaceKey: root, Source: "headless", Managed: true}}); err != nil {
		t.Fatal(err)
	}
	inst := core.Service().Instance(n.InstanceID)
	inst.ObservedFocusedThreadID = "thread"
	inst.Threads["thread"] = &state.ThreadRecord{ThreadID: "thread", CWD: root}
	for i, kind := range []control.ActionKind{control.ActionAttachInstance, control.ActionTextMessage} {
		a := control.Action{Kind: kind, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID, InstanceID: n.InstanceID, MessageID: string(rune('a' + i)), Text: "remember my preferences"}
		body, _ := json.Marshal(a)
		seq, _, e := h.journal.Admit(ctx, privatechat.Ingress{Owner: o, Generation: 1, EventID: a.MessageID, Lane: "input", Payload: body, Sources: []string{a.MessageID}})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = h.processor.ApplyIngress(ctx, o, 1, seq); e != nil {
			t.Fatal(e)
		}
	}
	app := New(":0", ":0", &privatePublishGateway{}, agentproto.ServerIdentity{})
	app.privateTools = &privateToolRuntime{host: h}
	handler := app.newToolRuntimeHandler()
	token := toolservicecontract.PrivateBearer(secret)
	session, protocol := initializeToolMCPSessionWithCaller(t, handler, token, n.InstanceID)
	call := func(id int, method string, params map[string]any) string {
		t.Helper()
		response := performToolMCPRequest(t, handler, toolMCPRequestOptions{Token: token, SessionID: session, ProtocolVersion: protocol, CallerInstanceID: n.InstanceID, Body: toolMCPCallRequestBody(t, id, method, params)})
		if response.Code != http.StatusOK {
			t.Fatalf("MCP HTTP %d: %s", response.Code, response.Body.String())
		}
		return response.Body.String()
	}
	if body := call(2, "tools/list", nil); !strings.Contains(body, privateMemoryToolName) {
		t.Fatalf("memory not discoverable: %s", body)
	}
	tool := func(id int, args map[string]any) string {
		return call(id, "tools/call", map[string]any{"name": privateMemoryToolName, "arguments": args})
	}
	write := map[string]any{"action": "write", "id": "language", "operation_id": "save-language", "expected_revision": 0, "content": "回答使用中文", "applicability": "回复偏好"}
	if body := tool(3, write); strings.Contains(body, `"isError":true`) {
		t.Fatalf("authenticated write failed: %s", body)
	}
	if body := tool(4, map[string]any{"action": "read", "id": "language", "revision": 1}); !strings.Contains(body, "回答使用中文") {
		t.Fatalf("read failed: %s", body)
	}
	if body := tool(5, map[string]any{"action": "revoke", "id": "language", "operation_id": "forget-language", "expected_revision": 1}); strings.Contains(body, `"isError":true`) {
		t.Fatalf("revoke failed: %s", body)
	}
	if body := tool(6, map[string]any{"action": "read", "id": "language", "revision": 1}); !strings.Contains(body, `"isError":true`) || strings.Contains(body, "回答使用中文") {
		t.Fatalf("revoked content leaked: %s", body)
	}
	if err = h.outbound.Drain(ctx, privateRegistration(n)); err != nil {
		t.Fatal(err)
	}
	write["id"], write["operation_id"] = "after-drain", "after-drain"
	response := performToolMCPRequest(t, handler, toolMCPRequestOptions{Token: token, SessionID: session, ProtocolVersion: protocol, CallerInstanceID: n.InstanceID, Body: toolMCPCallRequestBody(t, 7, "tools/call", map[string]any{"name": privateMemoryToolName, "arguments": write})})
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"isError":true`) || !strings.Contains(response.Body.String(), "private_turn_unavailable") {
		t.Fatalf("existing MCP session bypassed outbound drain: %d %s", response.Code, response.Body.String())
	}
	if _, err = h.journal.ReadMemory(ctx, o, 1, "after-drain", 1); err == nil {
		t.Fatal("drained carrier wrote memory")
	}
}
