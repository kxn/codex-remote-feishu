package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestApplySendCardRepliesToSourceMessage(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	var (
		replyMessageID string
		replyMsgType   string
		replyContent   string
		createCalled   bool
	)
	gateway.replyMessageFn = func(_ context.Context, messageID, msgType, content string) (*larkim.ReplyMessageResp, error) {
		replyMessageID = messageID
		replyMsgType = msgType
		replyContent = content
		return &larkim.ReplyMessageResp{
			ApiResp: &larkcore.ApiResp{},
			CodeError: larkcore.CodeError{
				Code: 0,
				Msg:  "ok",
			},
			Data: &larkim.ReplyMessageRespData{
				MessageId: stringRef("om-final-1"),
			},
		}, nil
	}
	gateway.createMessageFn = func(_ context.Context, _, _, _, _ string) (*larkim.CreateMessageResp, error) {
		createCalled = true
		return nil, nil
	}

	err := gateway.Apply(t.Context(), []Operation{{
		Kind:             OperationSendCard,
		SurfaceSessionID: "surface-1",
		ChatID:           "oc_1",
		ReceiveID:        "oc_1",
		ReceiveIDType:    "chat_id",
		ReplyToMessageID: "om-source-1",
		CardTitle:        "最后回复",
		CardBody:         "已完成修改。",
		CardThemeKey:     cardThemeFinal,
	}})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if createCalled {
		t.Fatalf("expected reply path without fallback create")
	}
	if replyMessageID != "om-source-1" || replyMsgType != "interactive" {
		t.Fatalf("unexpected reply request: message=%q type=%q", replyMessageID, replyMsgType)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(replyContent), &payload); err != nil {
		t.Fatalf("reply content is not valid json: %v", err)
	}
	header := payload["header"].(map[string]any)
	title := header["title"].(map[string]any)
	if title["content"] != "最后回复" {
		t.Fatalf("unexpected reply card title payload: %#v", payload)
	}
	if gateway.messages["om-final-1"] != "surface-1" {
		t.Fatalf("expected replied message to be tracked for surface callbacks, got %#v", gateway.messages)
	}
}

func TestApplySendCardFallsBackToCreateWhenReplyFails(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	var (
		replyCalls    int
		createCalls   int
		createMsgType string
		createContent string
	)
	gateway.replyMessageFn = func(_ context.Context, _, _, _ string) (*larkim.ReplyMessageResp, error) {
		replyCalls++
		return &larkim.ReplyMessageResp{
			ApiResp: &larkcore.ApiResp{},
			CodeError: larkcore.CodeError{
				Code: 230001,
				Msg:  "message not found",
			},
		}, nil
	}
	gateway.createMessageFn = func(_ context.Context, receiveIDType, receiveID, msgType, content string) (*larkim.CreateMessageResp, error) {
		createCalls++
		if receiveIDType != "chat_id" || receiveID != "oc_1" {
			t.Fatalf("unexpected fallback receive target: type=%q id=%q", receiveIDType, receiveID)
		}
		createMsgType = msgType
		createContent = content
		return &larkim.CreateMessageResp{
			ApiResp: &larkcore.ApiResp{},
			CodeError: larkcore.CodeError{
				Code: 0,
				Msg:  "ok",
			},
			Data: &larkim.CreateMessageRespData{
				MessageId: stringRef("om-final-2"),
			},
		}, nil
	}

	err := gateway.Apply(t.Context(), []Operation{{
		Kind:             OperationSendCard,
		SurfaceSessionID: "surface-1",
		ChatID:           "oc_1",
		ReceiveID:        "oc_1",
		ReceiveIDType:    "chat_id",
		ReplyToMessageID: "om-source-1",
		CardTitle:        "最后回复",
		CardBody:         "已完成修改。",
		CardThemeKey:     cardThemeFinal,
	}})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if replyCalls != 1 || createCalls != 1 {
		t.Fatalf("expected one reply attempt and one fallback create, got reply=%d create=%d", replyCalls, createCalls)
	}
	if createMsgType != "interactive" {
		t.Fatalf("unexpected fallback message type: %q", createMsgType)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(createContent), &payload); err != nil {
		t.Fatalf("fallback create content is not valid json: %v", err)
	}
	header := payload["header"].(map[string]any)
	title := header["title"].(map[string]any)
	if title["content"] != "最后回复" {
		t.Fatalf("unexpected fallback card payload: %#v", payload)
	}
	if gateway.messages["om-final-2"] != "surface-1" {
		t.Fatalf("expected fallback message to be tracked for surface callbacks, got %#v", gateway.messages)
	}
}

func TestMenuActionKindKnownValues(t *testing.T) {
	tests := map[string]control.ActionKind{
		"list":           control.ActionListInstances,
		"status":         control.ActionStatus,
		"stop":           control.ActionStop,
		"new":            control.ActionNewThread,
		"new_thread":     control.ActionNewThread,
		"newinstance":    control.ActionRemovedCommand,
		"new_instance":   control.ActionRemovedCommand,
		"killinstance":   control.ActionKillInstance,
		"kill_instance":  control.ActionKillInstance,
		"threads":        control.ActionShowThreads,
		"sessions":       control.ActionShowThreads,
		"use":            control.ActionShowThreads,
		"show_threads":   control.ActionShowThreads,
		"show_sessions":  control.ActionShowThreads,
		"useall":         control.ActionShowAllThreads,
		"threads_all":    control.ActionShowAllThreads,
		"accessfull":     control.ActionAccessCommand,
		"access_full":    control.ActionAccessCommand,
		"accessconfirm":  control.ActionAccessCommand,
		"access_confirm": control.ActionAccessCommand,
	}
	for key, want := range tests {
		got, ok := menuActionKind(key)
		if !ok || got != want {
			t.Fatalf("event key %q => (%q, %v), want (%q, true)", key, got, ok, want)
		}
	}
}

func TestMenuActionReasoningPresets(t *testing.T) {
	tests := map[string]string{
		"reason_low":    "/reasoning low",
		"reasonlow":     "/reasoning low",
		"reason_medium": "/reasoning medium",
		"reasonmedium":  "/reasoning medium",
		"reason_high":   "/reasoning high",
		"reasonhigh":    "/reasoning high",
		"reason_xhigh":  "/reasoning xhigh",
		"reasonxhigh":   "/reasoning xhigh",
	}
	for key, wantText := range tests {
		got, ok := menuAction(key)
		if !ok {
			t.Fatalf("expected menu action for %q", key)
		}
		if got.Kind != control.ActionReasoningCommand || got.Text != wantText {
			t.Fatalf("event key %q => %#v, want reasoning command %q", key, got, wantText)
		}
	}
}

func TestMenuActionDynamicModelPreset(t *testing.T) {
	tests := map[string]string{
		"model_gpt-5.4":       "/model gpt-5.4",
		"model_gpt-5.4-mini":  "/model gpt-5.4-mini",
		"model-gpt-5.4":       "/model gpt-5.4",
		" model_gpt-5.4 \n\t": "/model gpt-5.4",
	}
	for key, wantText := range tests {
		got, ok := menuAction(key)
		if !ok {
			t.Fatalf("expected dynamic model action for %q", key)
		}
		if got.Kind != control.ActionModelCommand || got.Text != wantText {
			t.Fatalf("event key %q => %#v, want model command %q", key, got, wantText)
		}
	}
}

func TestMenuActionAccessPresets(t *testing.T) {
	tests := map[string]string{
		"accessfull":     "/access full",
		"access_full":    "/access full",
		"accessFull":     "/access full",
		"accessconfirm":  "/access confirm",
		"access_confirm": "/access confirm",
		"accessConfirm":  "/access confirm",
	}
	for key, wantText := range tests {
		got, ok := menuAction(key)
		if !ok {
			t.Fatalf("expected menu action for %q", key)
		}
		if got.Kind != control.ActionAccessCommand || got.Text != wantText {
			t.Fatalf("event key %q => %#v, want access command %q", key, got, wantText)
		}
	}
}

func TestNormalizeMenuEventKey(t *testing.T) {
	tests := map[string]string{
		"access_full":      "accessfull",
		"access-full":      "accessfull",
		" accessFull \n":   "accessfull",
		"show_all_threads": "showallthreads",
		"approval_confirm": "approvalconfirm",
		"reason_high":      "reasonhigh",
		"reason_xhigh":     "reasonxhigh",
	}
	for input, want := range tests {
		if got := normalizeMenuEventKey(input); got != want {
			t.Fatalf("normalizeMenuEventKey(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestMenuActionKindUnknownValueIsIgnored(t *testing.T) {
	got, ok := menuActionKind("unexpected")
	if ok || got != "" {
		t.Fatalf("unexpected menu action result: (%q, %v)", got, ok)
	}
}

func TestResolveReceiveTarget(t *testing.T) {
	tests := []struct {
		name        string
		chatID      string
		actorUserID string
		wantID      string
		wantType    string
	}{
		{name: "chat id wins", chatID: "oc_1", actorUserID: "ou_1", wantID: "oc_1", wantType: "chat_id"},
		{name: "open id fallback", actorUserID: "ou_1", wantID: "ou_1", wantType: "open_id"},
		{name: "union id fallback", actorUserID: "on_1", wantID: "on_1", wantType: "union_id"},
		{name: "user id fallback", actorUserID: "user_1", wantID: "user_1", wantType: "user_id"},
	}
	for _, tt := range tests {
		gotID, gotType := ResolveReceiveTarget(tt.chatID, tt.actorUserID)
		if gotID != tt.wantID || gotType != tt.wantType {
			t.Fatalf("%s: got (%q, %q), want (%q, %q)", tt.name, gotID, gotType, tt.wantID, tt.wantType)
		}
	}
}

func TestSurfaceIDForInboundUsesUserScopeForP2P(t *testing.T) {
	got := surfaceIDForInbound("app-1", "oc_xxx", "p2p", "user-1")
	if got != "feishu:app-1:user:user-1" {
		t.Fatalf("unexpected p2p surface id: %q", got)
	}
}

func TestSurfaceIDForInboundUsesChatScopeForGroup(t *testing.T) {
	got := surfaceIDForInbound("app-1", "oc_xxx", "group", "user-1")
	if got != "feishu:app-1:chat:oc_xxx" {
		t.Fatalf("unexpected group surface id: %q", got)
	}
}

func TestParseSurfaceRefSupportsLegacyAndGatewayAwareFormats(t *testing.T) {
	newRef, ok := ParseSurfaceRef("feishu:app-1:chat:oc_1")
	if !ok {
		t.Fatal("expected gateway-aware surface id to parse")
	}
	if newRef.GatewayID != "app-1" || newRef.ScopeKind != ScopeKindChat || newRef.ScopeID != "oc_1" {
		t.Fatalf("unexpected new surface ref: %#v", newRef)
	}

	legacyRef, ok := ParseSurfaceRef("feishu:user:user-1")
	if !ok {
		t.Fatal("expected legacy surface id to parse")
	}
	if legacyRef.GatewayID != LegacyDefaultGatewayID || legacyRef.ScopeKind != ScopeKindUser || legacyRef.ScopeID != "user-1" {
		t.Fatalf("unexpected legacy surface ref: %#v", legacyRef)
	}
}

func TestParseTextActionRecognizesModelAndReasoningCommands(t *testing.T) {
	tests := map[string]control.ActionKind{
		"/model":          control.ActionModelCommand,
		"/model gpt-5.4":  control.ActionModelCommand,
		"/reasoning high": control.ActionReasoningCommand,
		"/effort medium":  control.ActionReasoningCommand,
		"/access":         control.ActionAccessCommand,
		"/access full":    control.ActionAccessCommand,
		"/approval":       control.ActionAccessCommand,
	}
	for input, want := range tests {
		action, handled := parseTextAction(input)
		if !handled {
			t.Fatalf("expected %q to be handled", input)
		}
		if action.Kind != want {
			t.Fatalf("input %q => kind %q, want %q", input, action.Kind, want)
		}
		if action.Text != input {
			t.Fatalf("input %q => text %q, want raw command", input, action.Text)
		}
	}
}

func TestParseTextActionRecognizesSessionCommands(t *testing.T) {
	tests := map[string]control.ActionKind{
		"/threads":      control.ActionShowThreads,
		"/use":          control.ActionShowThreads,
		"/sessions":     control.ActionShowThreads,
		"/useall":       control.ActionShowAllThreads,
		"/sessionsall":  control.ActionShowAllThreads,
		"/new":          control.ActionNewThread,
		"/newinstance":  control.ActionRemovedCommand,
		"/killinstance": control.ActionKillInstance,
	}
	for input, want := range tests {
		action, handled := parseTextAction(input)
		if !handled {
			t.Fatalf("expected %q to be handled", input)
		}
		if action.Kind != want {
			t.Fatalf("input %q => kind %q, want %q", input, action.Kind, want)
		}
	}
}

func TestParseTextActionRecognizesHelpAndMenuCommands(t *testing.T) {
	tests := map[string]control.ActionKind{
		"/help": control.ActionShowCommandHelp,
		"menu":  control.ActionShowCommandMenu,
		"/menu": control.ActionShowCommandMenu,
	}
	for input, want := range tests {
		action, handled := parseTextAction(input)
		if !handled {
			t.Fatalf("expected %q to be handled", input)
		}
		if action.Kind != want {
			t.Fatalf("input %q => kind %q, want %q", input, action.Kind, want)
		}
	}
}

func TestRemovedNewInstanceCommandPreservesCommandText(t *testing.T) {
	action, handled := parseTextAction("/newinstance")
	if !handled {
		t.Fatalf("expected /newinstance to be handled as removed command")
	}
	if action.Kind != control.ActionRemovedCommand || action.Text != "/newinstance" {
		t.Fatalf("unexpected removed command action: %#v", action)
	}

	menu, ok := menuAction("new_instance")
	if !ok {
		t.Fatalf("expected legacy new_instance menu to resolve to removed command")
	}
	if menu.Kind != control.ActionRemovedCommand || menu.Text != "new_instance" {
		t.Fatalf("unexpected removed menu action: %#v", menu)
	}
}

func TestParseMessageEventCommandPreservesGatewayID(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-2"})
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-msg-1"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("group"),
				MessageType: stringRef("text"),
				Content:     stringRef(`{"text":" /list "}`),
			},
		},
	}

	action, ok, err := gateway.parseMessageEvent(t.Context(), event)
	if err != nil {
		t.Fatalf("parseMessageEvent returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected command message to be handled")
	}
	if action.Kind != control.ActionListInstances {
		t.Fatalf("unexpected action kind: %#v", action)
	}
	if action.GatewayID != "app-2" {
		t.Fatalf("expected gateway id to be preserved, got %#v", action)
	}
	if action.SurfaceSessionID != "feishu:app-2:chat:oc_chat" {
		t.Fatalf("unexpected surface routing: %#v", action)
	}
	if action.ChatID != "oc_chat" || action.ActorUserID != "ou_user" || action.MessageID != "om-msg-1" {
		t.Fatalf("unexpected command routing payload: %#v", action)
	}
}

func TestCardTemplateUsesSemanticColors(t *testing.T) {
	tests := map[string]string{
		cardThemeInfo:     "grey",
		cardThemeSuccess:  "green",
		cardThemeApproval: "green",
		cardThemeFinal:    "blue",
		cardThemeError:    "red",
		"relay-error":     "red",
		"thread-1":        "grey",
	}
	for input, want := range tests {
		if got := cardTemplate(input, ""); got != want {
			t.Fatalf("cardTemplate(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestParseCardActionTriggerEventBuildsPromptSelectionAction(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-card-1", "feishu:app-1:user:user-1")
	userID := "user-1"
	event := &larkcallback.CardActionTriggerEvent{
		Event: &larkcallback.CardActionTriggerRequest{
			Operator: &larkcallback.Operator{UserID: &userID},
			Action: &larkcallback.CallBackAction{
				Value: map[string]interface{}{
					"kind":      "prompt_select",
					"prompt_id": "prompt-1",
					"option_id": "thread-1",
				},
			},
			Context: &larkcallback.Context{
				OpenChatID:    "oc_1",
				OpenMessageID: "om-card-1",
			},
		},
	}

	action, ok := gateway.parseCardActionTriggerEvent(event)
	if !ok {
		t.Fatal("expected card callback to be parsed")
	}
	if action.Kind != control.ActionSelectPrompt {
		t.Fatalf("unexpected action kind: %#v", action)
	}
	if action.GatewayID != "app-1" {
		t.Fatalf("unexpected gateway id: %#v", action)
	}
	if action.SurfaceSessionID != "feishu:app-1:user:user-1" || action.ChatID != "oc_1" || action.ActorUserID != "user-1" {
		t.Fatalf("unexpected action routing: %#v", action)
	}
	if action.PromptID != "prompt-1" || action.OptionID != "thread-1" {
		t.Fatalf("unexpected prompt selection payload: %#v", action)
	}
}

func TestParseCardActionTriggerEventBuildsDirectUseThreadAction(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-card-3", "feishu:app-1:user:user-1")
	userID := "user-1"
	event := &larkcallback.CardActionTriggerEvent{
		Event: &larkcallback.CardActionTriggerRequest{
			Operator: &larkcallback.Operator{UserID: &userID},
			Action: &larkcallback.CallBackAction{
				Value: map[string]interface{}{
					"kind":      "use_thread",
					"thread_id": "thread-1",
				},
			},
			Context: &larkcallback.Context{
				OpenChatID:    "oc_1",
				OpenMessageID: "om-card-3",
			},
		},
	}

	action, ok := gateway.parseCardActionTriggerEvent(event)
	if !ok {
		t.Fatal("expected card callback to be parsed")
	}
	if action.Kind != control.ActionUseThread {
		t.Fatalf("unexpected action kind: %#v", action)
	}
	if action.ThreadID != "thread-1" {
		t.Fatalf("unexpected direct thread payload: %#v", action)
	}
	if action.SurfaceSessionID != "feishu:app-1:user:user-1" || action.ChatID != "oc_1" || action.ActorUserID != "user-1" {
		t.Fatalf("unexpected action routing: %#v", action)
	}
}

func TestParseCardActionTriggerEventBuildsDirectAttachInstanceAction(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-card-4", "feishu:app-1:user:user-1")
	userID := "user-1"
	event := &larkcallback.CardActionTriggerEvent{
		Event: &larkcallback.CardActionTriggerRequest{
			Operator: &larkcallback.Operator{UserID: &userID},
			Action: &larkcallback.CallBackAction{
				Value: map[string]interface{}{
					"kind":        "attach_instance",
					"instance_id": "inst-1",
				},
			},
			Context: &larkcallback.Context{
				OpenChatID:    "oc_1",
				OpenMessageID: "om-card-4",
			},
		},
	}

	action, ok := gateway.parseCardActionTriggerEvent(event)
	if !ok {
		t.Fatal("expected card callback to be parsed")
	}
	if action.Kind != control.ActionAttachInstance || action.InstanceID != "inst-1" {
		t.Fatalf("unexpected attach action: %#v", action)
	}
	if action.SurfaceSessionID != "feishu:app-1:user:user-1" || action.ChatID != "oc_1" || action.ActorUserID != "user-1" {
		t.Fatalf("unexpected action routing: %#v", action)
	}
}

func TestParseCardActionTriggerEventBuildsRemovedResumeHeadlessAction(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-card-5", "feishu:app-1:user:user-1")
	userID := "user-1"
	event := &larkcallback.CardActionTriggerEvent{
		Event: &larkcallback.CardActionTriggerRequest{
			Operator: &larkcallback.Operator{UserID: &userID},
			Action: &larkcallback.CallBackAction{
				Value: map[string]interface{}{
					"kind":      "resume_headless_thread",
					"thread_id": "thread-1",
				},
			},
			Context: &larkcallback.Context{
				OpenChatID:    "oc_1",
				OpenMessageID: "om-card-5",
			},
		},
	}

	action, ok := gateway.parseCardActionTriggerEvent(event)
	if !ok {
		t.Fatal("expected card callback to be parsed")
	}
	if action.Kind != control.ActionRemovedCommand || action.Text != "resume_headless_thread" {
		t.Fatalf("unexpected removed-command action: %#v", action)
	}
	if action.SurfaceSessionID != "feishu:app-1:user:user-1" || action.ChatID != "oc_1" || action.ActorUserID != "user-1" {
		t.Fatalf("unexpected action routing: %#v", action)
	}
}

func TestParseCardActionTriggerEventBuildsKickActions(t *testing.T) {
	tests := []struct {
		name         string
		value        map[string]interface{}
		wantKind     control.ActionKind
		wantThreadID string
	}{
		{
			name: "confirm",
			value: map[string]interface{}{
				"kind":      "kick_thread_confirm",
				"thread_id": "thread-1",
			},
			wantKind:     control.ActionConfirmKickThread,
			wantThreadID: "thread-1",
		},
		{
			name: "cancel",
			value: map[string]interface{}{
				"kind":      "kick_thread_cancel",
				"thread_id": "thread-1",
			},
			wantKind:     control.ActionCancelKickThread,
			wantThreadID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
			gateway.recordSurfaceMessage("om-card-6", "feishu:app-1:user:user-1")
			userID := "user-1"
			event := &larkcallback.CardActionTriggerEvent{
				Event: &larkcallback.CardActionTriggerRequest{
					Operator: &larkcallback.Operator{UserID: &userID},
					Action:   &larkcallback.CallBackAction{Value: tt.value},
					Context: &larkcallback.Context{
						OpenChatID:    "oc_1",
						OpenMessageID: "om-card-6",
					},
				},
			}

			action, ok := gateway.parseCardActionTriggerEvent(event)
			if !ok {
				t.Fatal("expected card callback to be parsed")
			}
			if action.Kind != tt.wantKind || action.ThreadID != tt.wantThreadID {
				t.Fatalf("unexpected kick action: %#v", action)
			}
			if action.SurfaceSessionID != "feishu:app-1:user:user-1" || action.ChatID != "oc_1" || action.ActorUserID != "user-1" {
				t.Fatalf("unexpected action routing: %#v", action)
			}
		})
	}
}

func TestParseCardActionTriggerEventBuildsRequestRespondAction(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-card-2", "feishu:app-1:user:user-1")
	userID := "user-1"
	event := &larkcallback.CardActionTriggerEvent{
		Event: &larkcallback.CardActionTriggerRequest{
			Operator: &larkcallback.Operator{UserID: &userID},
			Action: &larkcallback.CallBackAction{
				Value: map[string]interface{}{
					"kind":              "request_respond",
					"request_id":        "req-1",
					"request_type":      "approval",
					"request_option_id": "acceptForSession",
				},
			},
			Context: &larkcallback.Context{
				OpenChatID:    "oc_1",
				OpenMessageID: "om-card-2",
			},
		},
	}

	action, ok := gateway.parseCardActionTriggerEvent(event)
	if !ok {
		t.Fatal("expected card callback to be parsed")
	}
	if action.Kind != control.ActionRespondRequest {
		t.Fatalf("unexpected action kind: %#v", action)
	}
	if action.RequestID != "req-1" || action.RequestType != "approval" || action.RequestOptionID != "acceptForSession" {
		t.Fatalf("unexpected request respond payload: %#v", action)
	}
}

func TestParseCardActionTriggerEventFallsBackToApprovedBool(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-card-3", "feishu:app-1:user:user-1")
	userID := "user-1"
	event := &larkcallback.CardActionTriggerEvent{
		Event: &larkcallback.CardActionTriggerRequest{
			Operator: &larkcallback.Operator{UserID: &userID},
			Action: &larkcallback.CallBackAction{
				Value: map[string]interface{}{
					"kind":         "request_respond",
					"request_id":   "req-legacy",
					"request_type": "approval",
					"approved":     false,
				},
			},
			Context: &larkcallback.Context{
				OpenChatID:    "oc_1",
				OpenMessageID: "om-card-3",
			},
		},
	}

	action, ok := gateway.parseCardActionTriggerEvent(event)
	if !ok {
		t.Fatal("expected legacy card callback to be parsed")
	}
	if action.RequestOptionID != "decline" || action.Approved {
		t.Fatalf("unexpected legacy request respond payload: %#v", action)
	}
}

func TestParseMessageRecalledEventBuildsRecallAction(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-msg-1", "feishu:app-1:user:user-1")
	event := &larkim.P2MessageRecalledV1{
		Event: &larkim.P2MessageRecalledV1Data{
			MessageId: stringRef("om-msg-1"),
			ChatId:    stringRef("oc_1"),
		},
	}

	action, ok := gateway.parseMessageRecalledEvent(event)
	if !ok {
		t.Fatal("expected recalled event to be parsed")
	}
	if action.Kind != control.ActionMessageRecalled {
		t.Fatalf("unexpected action kind: %#v", action)
	}
	if action.GatewayID != "app-1" {
		t.Fatalf("unexpected gateway id: %#v", action)
	}
	if action.SurfaceSessionID != "feishu:app-1:user:user-1" || action.TargetMessageID != "om-msg-1" || action.ChatID != "oc_1" {
		t.Fatalf("unexpected recalled action payload: %#v", action)
	}
}

func TestParseMessageRecalledEventIgnoresUnknownMessage(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	event := &larkim.P2MessageRecalledV1{
		Event: &larkim.P2MessageRecalledV1Data{
			MessageId: stringRef("om-missing"),
		},
	}

	if action, ok := gateway.parseMessageRecalledEvent(event); ok || action.Kind != "" {
		t.Fatalf("expected unknown recalled message to be ignored, got %#v", action)
	}
}

func TestParseMessageEventBuildsMixedInputsForPost(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.downloadImageFn = func(_ context.Context, messageID, imageKey string) (string, string, error) {
		if messageID != "om-post-1" || imageKey != "img-post-1" {
			t.Fatalf("unexpected post image download request: message=%s image=%s", messageID, imageKey)
		}
		return "/tmp/post-1.png", "image/png", nil
	}
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-post-1"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("group"),
				MessageType: stringRef("post"),
				Content:     stringRef(`{"title":"","content":[[{"tag":"img","image_key":"img-post-1"}],[{"tag":"text","text":"这是图文混合消息"}]]}`),
			},
		},
	}

	action, ok, err := gateway.parseMessageEvent(t.Context(), event)
	if err != nil {
		t.Fatalf("parseMessageEvent returned error: %v", err)
	}
	if !ok || action.Kind != control.ActionTextMessage {
		t.Fatalf("expected post message to become text action, got ok=%v action=%#v", ok, action)
	}
	if action.Text != "这是图文混合消息" {
		t.Fatalf("unexpected post text summary: %#v", action)
	}
	if len(action.Inputs) != 2 {
		t.Fatalf("expected image + text inputs, got %#v", action.Inputs)
	}
	if action.Inputs[0].Type != agentproto.InputLocalImage || action.Inputs[0].Path != "/tmp/post-1.png" {
		t.Fatalf("unexpected first post input: %#v", action.Inputs[0])
	}
	if action.Inputs[1].Type != agentproto.InputText || action.Inputs[1].Text != "这是图文混合消息" {
		t.Fatalf("unexpected second post input: %#v", action.Inputs[1])
	}
}

func TestParseMessageEventEnrichesReplyWithQuotedText(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.fetchMessageFn = func(_ context.Context, messageID string) (*gatewayMessage, error) {
		if messageID != "om-parent-1" {
			t.Fatalf("unexpected parent message lookup: %s", messageID)
		}
		return &gatewayMessage{
			MessageID:   messageID,
			MessageType: "text",
			Content:     `{"text":"原始消息"}`,
		}, nil
	}
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-reply-1"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("group"),
				MessageType: stringRef("text"),
				ParentId:    stringRef("om-parent-1"),
				Content:     stringRef(`{"text":"这是回复内容"}`),
			},
		},
	}

	action, ok, err := gateway.parseMessageEvent(t.Context(), event)
	if err != nil {
		t.Fatalf("parseMessageEvent returned error: %v", err)
	}
	if !ok || action.Kind != control.ActionTextMessage {
		t.Fatalf("expected reply text to be handled, got ok=%v action=%#v", ok, action)
	}
	if len(action.Inputs) != 2 {
		t.Fatalf("expected quoted text + current text inputs, got %#v", action.Inputs)
	}
	if action.Inputs[0].Type != agentproto.InputText || action.Inputs[0].Text != "<被引用内容>\n原始消息\n</被引用内容>" {
		t.Fatalf("unexpected quoted input: %#v", action.Inputs[0])
	}
	if action.Inputs[1].Type != agentproto.InputText || action.Inputs[1].Text != "这是回复内容" {
		t.Fatalf("unexpected current text input: %#v", action.Inputs[1])
	}
}

func TestParseMessageEventEnrichesReplyWithQuotedPost(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.fetchMessageFn = func(_ context.Context, messageID string) (*gatewayMessage, error) {
		if messageID != "om-parent-post-1" {
			t.Fatalf("unexpected parent post lookup: %s", messageID)
		}
		return &gatewayMessage{
			MessageID:   messageID,
			MessageType: "post",
			Content:     `{"title":"","content":[[{"tag":"img","image_key":"img-quoted-1"}],[{"tag":"text","text":"被引用的图文"}]]}`,
		}, nil
	}
	gateway.downloadImageFn = func(_ context.Context, messageID, imageKey string) (string, string, error) {
		if messageID != "om-parent-post-1" || imageKey != "img-quoted-1" {
			t.Fatalf("unexpected quoted post image download request: message=%s image=%s", messageID, imageKey)
		}
		return "/tmp/quoted-1.png", "image/png", nil
	}
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-reply-2"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("group"),
				MessageType: stringRef("text"),
				ParentId:    stringRef("om-parent-post-1"),
				Content:     stringRef(`{"text":"请继续处理"}`),
			},
		},
	}

	action, ok, err := gateway.parseMessageEvent(t.Context(), event)
	if err != nil {
		t.Fatalf("parseMessageEvent returned error: %v", err)
	}
	if !ok || action.Kind != control.ActionTextMessage {
		t.Fatalf("expected reply text to be handled, got ok=%v action=%#v", ok, action)
	}
	if len(action.Inputs) != 3 {
		t.Fatalf("expected quoted text + quoted image + current text, got %#v", action.Inputs)
	}
	if action.Inputs[0].Type != agentproto.InputText || action.Inputs[0].Text != "<被引用内容>\n被引用的图文\n</被引用内容>" {
		t.Fatalf("unexpected quoted text input: %#v", action.Inputs[0])
	}
	if action.Inputs[1].Type != agentproto.InputLocalImage || action.Inputs[1].Path != "/tmp/quoted-1.png" {
		t.Fatalf("unexpected quoted image input: %#v", action.Inputs[1])
	}
	if action.Inputs[2].Type != agentproto.InputText || action.Inputs[2].Text != "请继续处理" {
		t.Fatalf("unexpected current text input: %#v", action.Inputs[2])
	}
}

func TestParseMessageEventIgnoresQuoteFetchFailure(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.fetchMessageFn = func(_ context.Context, _ string) (*gatewayMessage, error) {
		return nil, errors.New("lark temporary error")
	}
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-reply-3"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("group"),
				MessageType: stringRef("text"),
				ParentId:    stringRef("om-parent-err"),
				Content:     stringRef(`{"text":"只保留当前消息"}`),
			},
		},
	}

	action, ok, err := gateway.parseMessageEvent(t.Context(), event)
	if err != nil {
		t.Fatalf("parseMessageEvent returned error: %v", err)
	}
	if !ok || len(action.Inputs) != 1 || action.Inputs[0].Text != "只保留当前消息" {
		t.Fatalf("expected current text to survive quote fetch failure, got ok=%v action=%#v", ok, action)
	}
}

func TestIgnoredMissingReactionError(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want bool
	}{
		{name: "english missing message", msg: "message not found", want: true},
		{name: "english recalled message", msg: "target message has been recalled", want: true},
		{name: "chinese missing message", msg: "目标消息不存在", want: true},
		{name: "reaction id not found", msg: "reaction not found", want: false},
		{name: "empty", msg: "", want: false},
	}
	for _, tt := range tests {
		if got := ignoredMissingReactionError(0, tt.msg); got != tt.want {
			t.Fatalf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func stringRef(value string) *string {
	return &value
}
