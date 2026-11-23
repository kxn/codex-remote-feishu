package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func privateJourneyString(s string) *string { return &s }
func privateJourneySDKText(id, text string) *larkim.P2MessageReceiveV1 {
	content, _ := json.Marshal(map[string]string{"text": text})
	return &larkim.P2MessageReceiveV1{Event: &larkim.P2MessageReceiveV1Data{Sender: &larkim.EventSender{SenderId: &larkim.UserId{OpenId: privateJourneyString("ou_user")}}, Message: &larkim.EventMessage{MessageId: privateJourneyString(id), ChatId: privateJourneyString("oc_chat"), ChatType: privateJourneyString("p2p"), MessageType: privateJourneyString("text"), Content: privateJourneyString(string(content))}}}
}

// This tests the legacy daemon handler using SDK event parsing and controlled
// transport/native stubs. It is not the team-enabled production gateway route.
func TestPrivateLegacySDKJourneyBaseline(t *testing.T) {
	ctx := context.Background()
	gateway := &messageIDAssigningGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	var commands []agentproto.Command
	app.sendAgentCommand = func(_ string, c agentproto.Command) error { commands = append(commands, c); return nil }
	surface := gatewaypkg.SurfaceIDForInbound("app-1", "oc_chat", "p2p", "ou_user")
	app.service.UpsertInstance(&state.InstanceRecord{InstanceID: "inst", Online: true, WorkspaceRoot: "/tmp/workspace", WorkspaceKey: "/tmp/workspace", ObservedFocusedThreadID: "thread", Threads: map[string]*state.ThreadRecord{"thread": {ThreadID: "thread", CWD: "/tmp/workspace"}}})
	app.HandleAction(ctx, control.Action{Kind: control.ActionAttachInstance, GatewayID: "app-1", SurfaceSessionID: surface, ChatID: "oc_chat", ActorUserID: "ou_user", InstanceID: "inst"})
	messages := map[string]string{}
	env := gatewaypkg.InboundEnv{GatewayID: "app-1", ParseTextActionWithoutCatalog: control.ParseFeishuTextActionWithoutCatalog, RecordSurfaceMessage: func(id, surface string) { messages[id] = surface }, LookupSurfaceMessage: func(id string) string { return messages[id] }, DownloadImage: func(context.Context, string, string) (string, string, error) {
		return "/tmp/queued.png", "image/png", nil
	}}
	send := func(id, text string) {
		t.Helper()
		err := gatewaypkg.HandleInboundMessageEvent(ctx, env, privateJourneySDKText(id, text), nil, func(ctx context.Context, a control.Action) error { app.HandleAction(ctx, a); return nil })
		if err != nil {
			t.Fatal(err)
		}
	}
	commands = nil
	send("om-user", "你好")
	if len(commands) != 1 || commands[0].Kind != agentproto.CommandPromptSend || commands[0].Target.ThreadID != "thread" {
		t.Fatalf("native command: %#v", commands)
	}
	app.onCommandAck(ctx, "inst", agentproto.CommandAck{CommandID: commands[0].CommandID, Accepted: true})
	app.onEvents(ctx, "inst", []agentproto.Event{{Kind: agentproto.EventTurnStarted, ThreadID: "thread", TurnID: "turn", Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown}}})
	image := privateJourneySDKText("om-image", "")
	image.Event.Message.MessageType = privateJourneyString("image")
	image.Event.Message.Content = privateJourneyString(`{"image_key":"img-key"}`)
	if err := gatewaypkg.HandleInboundMessageEvent(ctx, env, image, nil, func(ctx context.Context, a control.Action) error { app.HandleAction(ctx, a); return nil }); err != nil {
		t.Fatal(err)
	}
	send("om-queued", "补充信息")
	reaction := &larkim.P2MessageReactionCreatedV1{Event: &larkim.P2MessageReactionCreatedV1Data{MessageId: privateJourneyString("om-queued"), ReactionType: &larkim.Emoji{EmojiType: privateJourneyString("ThumbsUp")}, UserId: &larkim.UserId{OpenId: privateJourneyString("ou_user")}}}
	if err := gatewaypkg.HandleInboundMessageReactionCreatedEvent(ctx, env, reaction, nil, func(ctx context.Context, a control.Action) error { app.HandleAction(ctx, a); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 || commands[1].Kind != agentproto.CommandTurnSteer || len(commands[1].Prompt.Inputs) != 2 || commands[1].Prompt.Inputs[0].Type != agentproto.InputLocalImage || commands[1].Prompt.Inputs[1].Text != "补充信息" {
		t.Fatalf("SDK image + reaction steer: %#v", commands)
	}
	app.onCommandAck(ctx, "inst", agentproto.CommandAck{CommandID: commands[1].CommandID, Accepted: true})
	app.onEvents(ctx, "inst", []agentproto.Event{{Kind: agentproto.EventItemCompleted, ThreadID: "thread", TurnID: "turn", ItemID: "answer", ItemKind: "agent_message", Metadata: map[string]any{"text": "已完成"}}, {Kind: agentproto.EventTurnCompleted, ThreadID: "thread", TurnID: "turn", Status: "completed", Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown}}})
	found := false
	for _, op := range gateway.snapshotOperations() {
		if op.Kind == feishu.OperationSendCard && strings.Contains(op.CardBody, "已完成") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing projected final answer: %#v", gateway.snapshotOperations())
	}
	beforeMenu := len(gateway.snapshotOperations())
	send("om-menu", "/menu maintenance")
	menuCardID := ""
	for _, op := range gateway.snapshotOperations()[beforeMenu:] {
		if op.Kind == feishu.OperationSendCard && op.MessageID != "" && op.SurfaceSessionID == surface {
			menuCardID = op.MessageID
		}
	}
	if menuCardID == "" {
		t.Fatal("menu did not produce a sent card ID")
	}
	callback := &larkcallback.CardActionTriggerEvent{Event: &larkcallback.CardActionTriggerRequest{Operator: &larkcallback.Operator{OpenID: "ou_user"}, Context: &larkcallback.Context{OpenChatID: "oc_chat", OpenMessageID: "om-menu-card"}, Action: &larkcallback.CallBackAction{Value: map[string]interface{}{"kind": "page_local_action", "action_kind": string(control.ActionShowCommandMenu), "action_arg": "/menu", "daemon_lifecycle_id": app.daemonLifecycleID}}}}
	callback.Event.Context.OpenMessageID = menuCardID
	parsed, ok := gatewaypkg.ParseCardActionTriggerEvent(gatewaypkg.RoutingEnv{GatewayID: "app-1", SurfaceForCardAction: func(gatewaypkg.CardActionSurfaceLookup) string { return surface }}, callback)
	if !ok {
		t.Fatal("SDK menu callback did not parse")
	}
	result := app.HandleGatewayAction(ctx, parsed)
	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("menu return did not replace current card: %#v", result)
	}
}

// The active team setup fences this old handler. This test does not bypass it
// and must not be read as proof that privatechat is connected to production.
func TestPrivateLegacyHandlerRemainsFencedWhenTeamOwnsIngress(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.teamRuntime = &teamRuntime{}
	app.teamRuntime.legacyFenced.Store(true)
	env := gatewaypkg.InboundEnv{GatewayID: "app-1", ParseTextActionWithoutCatalog: control.ParseFeishuTextActionWithoutCatalog, RecordSurfaceMessage: func(string, string) {}}
	err := gatewaypkg.HandleInboundMessageEvent(context.Background(), env, privateJourneySDKText("om-fenced", "你好"), nil, func(ctx context.Context, a control.Action) error { app.HandleAction(ctx, a); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(app.service.Surfaces()) != 0 || len(gateway.operations) != 0 {
		t.Fatalf("legacy handler escaped fence: surfaces=%#v operations=%#v", app.service.Surfaces(), gateway.operations)
	}
}

// Exercises the assembled production hook, durable private application and
// publisher. Native execution is covered separately by the translator/receipt
// journey; this test does not fabricate an authenticated wrapper.
func TestPrivateAssembledSDKMenuAndCallbackJourney(t *testing.T) {
	app, gateway := newApplicationRuntimeTestApp(t)
	r := app.applications
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "journey-menu", MessageID: "journey-input", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"/menu"}`, ReceivedAt: time.Now()}
	if err := r.hook.Receive(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	errors := make(chan error, 4)
	if err := r.startWorkers(t.Context(), errors); err != nil {
		t.Fatal(err)
	}
	ops := gateway.waitForOperationCount(1, 3*time.Second)
	if len(ops) < 1 || ops[0].Kind != feishu.OperationSendCard || ops[0].MessageID == "" {
		t.Fatalf("menu: %#v", ops)
	}
	cardID := ops[0].MessageID
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := r.router.PublishedMessage(t.Context(), "tenant", "app", "private-chat", cardID); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("menu publication was not durably registered")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var document any
	frozen, err := feishu.FreezePrivateOperation(ops[0], app.daemonLifecycleID)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(frozen, &document); err != nil {
		t.Fatal(err)
	}
	var callback map[string]any
	var find func(any)
	find = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if v["private_view"] != nil && v["kind"] != nil && callback == nil {
				callback = v
			}
			for _, child := range v {
				find(child)
			}
		case []any:
			for _, child := range v {
				find(child)
			}
		}
	}
	find(document)
	if callback == nil {
		t.Fatal("rendered menu has no stamped callback")
	}
	click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "journey-click", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: cardID, Kind: "card", Values: callback, ReceivedAt: time.Now()}
	first, err := r.hook.ReceiveInteractionResponse(t.Context(), click)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.RawBody) == 0 {
		t.Fatal("callback did not return same-card response")
	}
	again, err := r.hook.ReceiveInteractionResponse(t.Context(), click)
	if err != nil || string(again.RawBody) != string(first.RawBody) {
		t.Fatalf("callback replay: %v", err)
	}
	observation := platformingress.PlatformIngressEvent{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "journey-reaction", Kind: "reaction.created", MessageID: "journey-input", OperatorType: "user", OperatorIDs: platformingress.SenderIDs{OpenID: "user"}, Emoji: "ThumbsUp", ReceivedAt: time.Now()}
	if err = r.hook.ReceivePlatformEvent(t.Context(), observation); err != nil {
		t.Fatalf("reaction SDK entry: %v", err)
	}
	if err = r.hook.ReceivePlatformEvent(t.Context(), observation); err != nil {
		t.Fatalf("reaction replay: %v", err)
	}
	text := raw
	text.EventID, text.MessageID, text.RawContent = "journey-text", "journey-text-message", `{"text":"继续"}`
	if err = r.hook.Receive(t.Context(), text); err != nil {
		t.Fatalf("text SDK entry: %v", err)
	}
	if err = r.hook.Receive(t.Context(), text); err != nil {
		t.Fatalf("text replay: %v", err)
	}
	// Authenticate a foreign actor against the current offered view, rather
	// than merely proving an already superseded token is expired.
	callback = nil
	if err = json.Unmarshal(first.RawBody, &document); err != nil {
		t.Fatal(err)
	}
	find(document)
	if callback == nil {
		t.Fatal("replacement card lost private callback identity")
	}
	foreign := click
	foreign.EventID = "foreign-click"
	foreign.Values = callback
	foreign.SenderIDs.OpenID = "other-user"
	if _, err = r.hook.ReceiveInteractionResponse(t.Context(), foreign); err == nil {
		t.Fatal("foreign owner used private card")
	}
	select {
	case err := <-errors:
		t.Fatal(err)
	default:
	}
}
