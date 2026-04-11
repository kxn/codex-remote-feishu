package feishu

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkimv2 "github.com/larksuite/oapi-sdk-go/v3/service/im/v2"
)

func TestApplySetTimeSensitive(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	var (
		gotUserIDType    string
		gotTimeSensitive bool
		gotUserIDs       []string
	)
	gateway.botTimeSensitiveFn = func(_ context.Context, userIDType string, timeSensitive bool, userIDs []string) (*larkimv2.BotTimeSentiveFeedCardResp, error) {
		gotUserIDType = userIDType
		gotTimeSensitive = timeSensitive
		gotUserIDs = append([]string(nil), userIDs...)
		return &larkimv2.BotTimeSentiveFeedCardResp{
			ApiResp: &larkcore.ApiResp{},
			CodeError: larkcore.CodeError{
				Code: 0,
				Msg:  "ok",
			},
		}, nil
	}

	err := gateway.Apply(t.Context(), []Operation{{
		Kind:          OperationSetTimeSensitive,
		GatewayID:     "app-1",
		ReceiveID:     "ou_user-1",
		ReceiveIDType: "open_id",
		TimeSensitive: true,
	}})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if gotUserIDType != "open_id" {
		t.Fatalf("user id type = %q, want open_id", gotUserIDType)
	}
	if !gotTimeSensitive {
		t.Fatalf("time sensitive = false, want true")
	}
	if len(gotUserIDs) != 1 || gotUserIDs[0] != "ou_user-1" {
		t.Fatalf("user ids = %#v, want [ou_user-1]", gotUserIDs)
	}
}

func TestApplySendCardRepliesToSourceMessageWithV2EnvelopeByDefault(t *testing.T) {
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
		CardTitle:        "最后答复：处理一下",
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
	if payload["schema"] != "2.0" {
		t.Fatalf("expected normal send card to default to v2 envelope, got %#v", payload)
	}
	header := payload["header"].(map[string]any)
	title := header["title"].(map[string]any)
	if title["content"] != "最后答复：处理一下" {
		t.Fatalf("unexpected reply card title payload: %#v", payload)
	}
	if gateway.messages["om-final-1"] != "surface-1" {
		t.Fatalf("expected replied message to be tracked for surface callbacks, got %#v", gateway.messages)
	}
}

func TestApplySendCardFallsBackToCreateWithV2EnvelopeByDefault(t *testing.T) {
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
		CardTitle:        "最后答复",
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
	if payload["schema"] != "2.0" {
		t.Fatalf("expected fallback send card to default to v2 envelope, got %#v", payload)
	}
	header := payload["header"].(map[string]any)
	title := header["title"].(map[string]any)
	if title["content"] != "最后答复" {
		t.Fatalf("unexpected fallback card payload: %#v", payload)
	}
	if gateway.messages["om-final-2"] != "surface-1" {
		t.Fatalf("expected fallback message to be tracked for surface callbacks, got %#v", gateway.messages)
	}
}

func TestApplySendCardCanStillForceLegacyEnvelope(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	var createContent string
	gateway.createMessageFn = func(_ context.Context, receiveIDType, receiveID, msgType, content string) (*larkim.CreateMessageResp, error) {
		if receiveIDType != "chat_id" || receiveID != "oc_1" {
			t.Fatalf("unexpected receive target: type=%q id=%q", receiveIDType, receiveID)
		}
		if msgType != "interactive" {
			t.Fatalf("unexpected message type: %q", msgType)
		}
		createContent = content
		return &larkim.CreateMessageResp{
			ApiResp: &larkcore.ApiResp{},
			CodeError: larkcore.CodeError{
				Code: 0,
				Msg:  "ok",
			},
			Data: &larkim.CreateMessageRespData{
				MessageId: stringRef("om-v2-1"),
			},
		}, nil
	}

	err := gateway.Apply(t.Context(), []Operation{{
		Kind:             OperationSendCard,
		SurfaceSessionID: "surface-1",
		ChatID:           "oc_1",
		ReceiveID:        "oc_1",
		ReceiveIDType:    "chat_id",
		CardTitle:        "系统提示",
		CardBody:         "已完成切换。",
		CardThemeKey:     cardThemeInfo,
		cardEnvelope:     cardEnvelopeLegacy,
		card:             legacyCardDocument("系统提示", "已完成切换。", cardThemeInfo, nil),
	}})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(createContent), &payload); err != nil {
		t.Fatalf("create content is not valid json: %v", err)
	}
	if _, ok := payload["schema"]; ok {
		t.Fatalf("expected explicit legacy override to keep legacy envelope, got %#v", payload)
	}
	elements, _ := payload["elements"].([]interface{})
	if len(elements) != 1 {
		t.Fatalf("expected one markdown element in legacy body, got %#v", payload)
	}
}

func TestMenuActionKindKnownValues(t *testing.T) {
	tests := map[string]control.ActionKind{
		"menu":             control.ActionShowCommandMenu,
		"list":             control.ActionListInstances,
		"status":           control.ActionStatus,
		"stop":             control.ActionStop,
		"new":              control.ActionNewThread,
		"new_thread":       control.ActionNewThread,
		"newinstance":      control.ActionRemovedCommand,
		"new_instance":     control.ActionRemovedCommand,
		"killinstance":     control.ActionRemovedCommand,
		"kill_instance":    control.ActionRemovedCommand,
		"threads":          control.ActionShowThreads,
		"sessions":         control.ActionShowThreads,
		"use":              control.ActionShowThreads,
		"show_threads":     control.ActionShowThreads,
		"show_sessions":    control.ActionShowThreads,
		"useall":           control.ActionShowAllThreads,
		"threads_all":      control.ActionShowAllThreads,
		"reasoning":        control.ActionReasoningCommand,
		"model":            control.ActionModelCommand,
		"access":           control.ActionAccessCommand,
		"mode":             control.ActionModeCommand,
		"autowhip":         control.ActionAutoContinueCommand,
		"autocontinue":     control.ActionAutoContinueCommand,
		"help":             control.ActionShowCommandHelp,
		"debug":            control.ActionDebugCommand,
		"accessfull":       control.ActionAccessCommand,
		"access_full":      control.ActionAccessCommand,
		"accessconfirm":    control.ActionAccessCommand,
		"access_confirm":   control.ActionAccessCommand,
		"approval_confirm": control.ActionAccessCommand,
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
		"reasoning_low":    "/reasoning low",
		"reason_low":       "/reasoning low",
		"reasonlow":        "/reasoning low",
		"reasoning_medium": "/reasoning medium",
		"reason_medium":    "/reasoning medium",
		"reasonmedium":     "/reasoning medium",
		"reasoning_high":   "/reasoning high",
		"reason_high":      "/reasoning high",
		"reasonhigh":       "/reasoning high",
		"reasoning_xhigh":  "/reasoning xhigh",
		"reason_xhigh":     "/reasoning xhigh",
		"reasonxhigh":      "/reasoning xhigh",
		"reasoning_clear":  "/reasoning clear",
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
		"reasoning_high":   "reasoninghigh",
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

func TestSurfaceForCardActionPrefersRecordedMessageSurface(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-card-1", "feishu:app-1:user:ou_user")

	got := gateway.surfaceForCardAction("om-card-1", "oc_1", "user_1")
	if got != "feishu:app-1:user:ou_user" {
		t.Fatalf("expected recorded surface to win, got %q", got)
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
		"/model":           control.ActionModelCommand,
		"/model gpt-5.4":   control.ActionModelCommand,
		"/reasoning high":  control.ActionReasoningCommand,
		"/effort medium":   control.ActionReasoningCommand,
		"/access":          control.ActionAccessCommand,
		"/access full":     control.ActionAccessCommand,
		"/approval":        control.ActionAccessCommand,
		"/autowhip":        control.ActionAutoContinueCommand,
		"/autowhip on":     control.ActionAutoContinueCommand,
		"/autocontinue":    control.ActionAutoContinueCommand,
		"/autocontinue on": control.ActionAutoContinueCommand,
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
		"/killinstance": control.ActionRemovedCommand,
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
	if action, handled := parseTextAction("menu"); handled {
		t.Fatalf("expected bare menu text to be ignored, got %#v", action)
	}
}

func TestRemovedLegacyCommandsPreserveCommandText(t *testing.T) {
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

	kill, handled := parseTextAction("/killinstance")
	if !handled {
		t.Fatalf("expected /killinstance to be handled as removed command")
	}
	if kill.Kind != control.ActionRemovedCommand || kill.Text != "/killinstance" {
		t.Fatalf("unexpected removed killinstance action: %#v", kill)
	}

	killMenu, ok := menuAction("kill_instance")
	if !ok {
		t.Fatalf("expected legacy kill_instance menu to resolve to removed command")
	}
	if killMenu.Kind != control.ActionRemovedCommand || killMenu.Text != "kill_instance" {
		t.Fatalf("unexpected removed kill menu action: %#v", killMenu)
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

func TestParseCardActionTriggerEventPrefersRecordedSurfaceAndOpenID(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-card-open", "feishu:app-1:user:ou_user")
	userID := "user-1"
	event := &larkcallback.CardActionTriggerEvent{
		Event: &larkcallback.CardActionTriggerRequest{
			Operator: &larkcallback.Operator{
				UserID: &userID,
				OpenID: "ou_user",
			},
			Action: &larkcallback.CallBackAction{
				Value: map[string]interface{}{
					"kind":      "use_thread",
					"thread_id": "thread-1",
				},
			},
			Context: &larkcallback.Context{
				OpenChatID:    "oc_1",
				OpenMessageID: "om-card-open",
			},
		},
	}

	action, ok := gateway.parseCardActionTriggerEvent(event)
	if !ok {
		t.Fatal("expected card callback to be parsed")
	}
	if action.SurfaceSessionID != "feishu:app-1:user:ou_user" {
		t.Fatalf("unexpected card callback surface: %#v", action)
	}
	if action.ActorUserID != "ou_user" {
		t.Fatalf("expected callback actor to prefer open id, got %#v", action)
	}
}

func TestParseCardActionTriggerEventCarriesInboundMeta(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-card-meta", "feishu:app-1:user:user-1")
	userID := "user-1"
	event := &larkcallback.CardActionTriggerEvent{
		EventV2Base: &larkevent.EventV2Base{
			Header: &larkevent.EventHeader{
				EventID:    "evt-card-1",
				EventType:  "card.action.trigger",
				CreateTime: "1710000000000",
			},
		},
		EventReq: &larkevent.EventReq{
			Header: map[string][]string{
				larkcore.HttpHeaderKeyRequestId: {"req-card-1"},
			},
		},
		Event: &larkcallback.CardActionTriggerRequest{
			Operator: &larkcallback.Operator{UserID: &userID},
			Action: &larkcallback.CallBackAction{
				Value: map[string]interface{}{
					"kind":                "use_thread",
					"thread_id":           "thread-1",
					"daemon_lifecycle_id": "life-1",
				},
			},
			Context: &larkcallback.Context{
				OpenChatID:    "oc_1",
				OpenMessageID: "om-card-meta",
			},
		},
	}

	action, ok := gateway.parseCardActionTriggerEvent(event)
	if !ok {
		t.Fatal("expected card callback to be parsed")
	}
	if action.Inbound == nil {
		t.Fatalf("expected inbound meta, got %#v", action)
	}
	if action.Inbound.EventID != "evt-card-1" || action.Inbound.EventType != "card.action.trigger" || action.Inbound.RequestID != "req-card-1" {
		t.Fatalf("unexpected card inbound meta: %#v", action.Inbound)
	}
	if action.Inbound.OpenMessageID != "om-card-meta" || action.Inbound.CardDaemonLifecycleID != "life-1" {
		t.Fatalf("unexpected card inbound payload: %#v", action.Inbound)
	}
	if !action.Inbound.EventCreateTime.Equal(time.UnixMilli(1710000000000).UTC()) {
		t.Fatalf("unexpected event create time: %#v", action.Inbound)
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

func TestParseCardActionTriggerEventBuildsShowWorkspaceThreadsAction(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-card-workspace", "feishu:app-1:user:user-1")
	userID := "user-1"
	event := &larkcallback.CardActionTriggerEvent{
		Event: &larkcallback.CardActionTriggerRequest{
			Operator: &larkcallback.Operator{UserID: &userID},
			Action: &larkcallback.CallBackAction{
				Value: map[string]interface{}{
					"kind":          "show_workspace_threads",
					"workspace_key": "/data/dl/web",
				},
			},
			Context: &larkcallback.Context{
				OpenChatID:    "oc_1",
				OpenMessageID: "om-card-workspace",
			},
		},
	}

	action, ok := gateway.parseCardActionTriggerEvent(event)
	if !ok {
		t.Fatal("expected card callback to be parsed")
	}
	if action.Kind != control.ActionShowWorkspaceThreads || action.WorkspaceKey != "/data/dl/web" {
		t.Fatalf("unexpected workspace threads action: %#v", action)
	}
}

func TestParseCardActionTriggerEventBuildsWorkspaceListNavigationActions(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-card-workspaces", "feishu:app-1:user:user-1")
	userID := "user-1"
	tests := []struct {
		name string
		kind string
		want control.ActionKind
	}{
		{name: "show all", kind: "show_all_workspaces", want: control.ActionShowAllWorkspaces},
		{name: "show recent", kind: "show_recent_workspaces", want: control.ActionShowRecentWorkspaces},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event := &larkcallback.CardActionTriggerEvent{
				Event: &larkcallback.CardActionTriggerRequest{
					Operator: &larkcallback.Operator{UserID: &userID},
					Action: &larkcallback.CallBackAction{
						Value: map[string]interface{}{
							"kind": tc.kind,
						},
					},
					Context: &larkcallback.Context{
						OpenChatID:    "oc_1",
						OpenMessageID: "om-card-workspaces",
					},
				},
			}

			action, ok := gateway.parseCardActionTriggerEvent(event)
			if !ok {
				t.Fatal("expected card callback to be parsed")
			}
			if action.Kind != tc.want {
				t.Fatalf("unexpected workspace list navigation action: %#v", action)
			}
		})
	}
}

func TestParseCardActionTriggerEventBuildsThreadWorkspaceNavigationActions(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-card-thread-workspaces", "feishu:app-1:user:user-1")
	userID := "user-1"
	tests := []struct {
		name string
		kind string
		want control.ActionKind
	}{
		{name: "show all", kind: "show_all_thread_workspaces", want: control.ActionShowAllThreadWorkspaces},
		{name: "show recent", kind: "show_recent_thread_workspaces", want: control.ActionShowRecentThreadWorkspaces},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event := &larkcallback.CardActionTriggerEvent{
				Event: &larkcallback.CardActionTriggerRequest{
					Operator: &larkcallback.Operator{UserID: &userID},
					Action: &larkcallback.CallBackAction{
						Value: map[string]interface{}{
							"kind": tc.kind,
						},
					},
					Context: &larkcallback.Context{
						OpenChatID:    "oc_1",
						OpenMessageID: "om-card-thread-workspaces",
					},
				},
			}

			action, ok := gateway.parseCardActionTriggerEvent(event)
			if !ok {
				t.Fatal("expected card callback to be parsed")
			}
			if action.Kind != tc.want {
				t.Fatalf("unexpected thread workspace navigation action: %#v", action)
			}
		})
	}
}

func TestParseCardActionTriggerEventBuildsRunCommandAction(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-card-5", "feishu:app-1:user:user-1")
	userID := "user-1"
	event := &larkcallback.CardActionTriggerEvent{
		Event: &larkcallback.CardActionTriggerRequest{
			Operator: &larkcallback.Operator{UserID: &userID},
			Action: &larkcallback.CallBackAction{
				Value: map[string]interface{}{
					"kind":         "run_command",
					"command_text": "/help",
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
		t.Fatal("expected run_command callback to be parsed")
	}
	if action.Kind != control.ActionShowCommandHelp {
		t.Fatalf("unexpected action kind: %#v", action)
	}
	if action.SurfaceSessionID != "feishu:app-1:user:user-1" || action.ChatID != "oc_1" || action.ActorUserID != "user-1" {
		t.Fatalf("unexpected action routing: %#v", action)
	}
}

func TestParseCardActionTriggerEventBuildsCommandCaptureActions(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-card-6", "feishu:app-1:user:user-1")
	userID := "user-1"
	tests := []struct {
		name string
		kind string
		want control.ActionKind
	}{
		{name: "start", kind: "start_command_capture", want: control.ActionStartCommandCapture},
		{name: "cancel", kind: "cancel_command_capture", want: control.ActionCancelCommandCapture},
	}
	for _, tc := range tests {
		event := &larkcallback.CardActionTriggerEvent{
			Event: &larkcallback.CardActionTriggerRequest{
				Operator: &larkcallback.Operator{UserID: &userID},
				Action: &larkcallback.CallBackAction{
					Value: map[string]interface{}{
						"kind":       tc.kind,
						"command_id": control.FeishuCommandModel,
					},
				},
				Context: &larkcallback.Context{
					OpenChatID:    "oc_1",
					OpenMessageID: "om-card-6",
				},
			},
		}
		action, ok := gateway.parseCardActionTriggerEvent(event)
		if !ok {
			t.Fatalf("%s: expected card callback to be parsed", tc.name)
		}
		if action.Kind != tc.want || action.CommandID != control.FeishuCommandModel {
			t.Fatalf("%s: unexpected action %#v", tc.name, action)
		}
	}
}

func TestParseCardActionTriggerEventBuildsSubmitCommandFormActionFromFormValue(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-card-form-1", "feishu:app-1:user:user-1")
	userID := "user-1"
	event := &larkcallback.CardActionTriggerEvent{
		Event: &larkcallback.CardActionTriggerRequest{
			Operator: &larkcallback.Operator{UserID: &userID},
			Action: &larkcallback.CallBackAction{
				Value: map[string]interface{}{
					"kind":       "submit_command_form",
					"command":    "/model",
					"field_name": "command_args",
				},
				FormValue: map[string]interface{}{
					"command_args": "gpt-5.4 high",
				},
			},
			Context: &larkcallback.Context{
				OpenChatID:    "oc_1",
				OpenMessageID: "om-card-form-1",
			},
		},
	}

	action, ok := gateway.parseCardActionTriggerEvent(event)
	if !ok {
		t.Fatal("expected submit_command_form callback to be parsed")
	}
	if action.Kind != control.ActionModelCommand || action.Text != "/model gpt-5.4 high" {
		t.Fatalf("unexpected form submit action: %#v", action)
	}
}

func TestParseCardActionTriggerEventBuildsSubmitCommandFormActionFromInputValueFallback(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-card-form-2", "feishu:app-1:user:user-1")
	userID := "user-1"
	event := &larkcallback.CardActionTriggerEvent{
		Event: &larkcallback.CardActionTriggerRequest{
			Operator: &larkcallback.Operator{UserID: &userID},
			Action: &larkcallback.CallBackAction{
				Value: map[string]interface{}{
					"kind":    "submit_command_form",
					"command": "/debug",
				},
				InputValue: "track beta",
			},
			Context: &larkcallback.Context{
				OpenChatID:    "oc_1",
				OpenMessageID: "om-card-form-2",
			},
		},
	}

	action, ok := gateway.parseCardActionTriggerEvent(event)
	if !ok {
		t.Fatal("expected submit_command_form callback to be parsed")
	}
	if action.Kind != control.ActionDebugCommand || action.Text != "/debug track beta" {
		t.Fatalf("unexpected input fallback action: %#v", action)
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

func TestParseCardActionTriggerEventBuildsAttachWorkspaceAction(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-card-4", "feishu:app-1:user:user-1")
	userID := "user-1"
	event := &larkcallback.CardActionTriggerEvent{
		Event: &larkcallback.CardActionTriggerRequest{
			Operator: &larkcallback.Operator{UserID: &userID},
			Action: &larkcallback.CallBackAction{
				Value: map[string]interface{}{
					"kind":          "attach_workspace",
					"workspace_key": "/data/dl/droid",
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
	if action.Kind != control.ActionAttachWorkspace || action.WorkspaceKey != "/data/dl/droid" {
		t.Fatalf("unexpected attach-workspace action: %#v", action)
	}
	if action.SurfaceSessionID != "feishu:app-1:user:user-1" || action.ChatID != "oc_1" || action.ActorUserID != "user-1" {
		t.Fatalf("unexpected action routing: %#v", action)
	}
}

func TestParseCardActionTriggerEventBuildsThreadListNavigationActions(t *testing.T) {
	tests := []struct {
		name     string
		value    map[string]interface{}
		wantKind control.ActionKind
	}{
		{
			name: "show recent",
			value: map[string]interface{}{
				"kind": "show_threads",
			},
			wantKind: control.ActionShowThreads,
		},
		{
			name: "show all",
			value: map[string]interface{}{
				"kind": "show_all_threads",
			},
			wantKind: control.ActionShowAllThreads,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
			gateway.recordSurfaceMessage("om-card-nav", "feishu:app-1:user:user-1")
			userID := "user-1"
			event := &larkcallback.CardActionTriggerEvent{
				Event: &larkcallback.CardActionTriggerRequest{
					Operator: &larkcallback.Operator{UserID: &userID},
					Action:   &larkcallback.CallBackAction{Value: tt.value},
					Context: &larkcallback.Context{
						OpenChatID:    "oc_1",
						OpenMessageID: "om-card-nav",
					},
				},
			}

			action, ok := gateway.parseCardActionTriggerEvent(event)
			if !ok {
				t.Fatal("expected card callback to be parsed")
			}
			if action.Kind != tt.wantKind {
				t.Fatalf("unexpected action kind: %#v", action)
			}
			if action.SurfaceSessionID != "feishu:app-1:user:user-1" || action.ChatID != "oc_1" || action.ActorUserID != "user-1" {
				t.Fatalf("unexpected action routing: %#v", action)
			}
		})
	}
}

func TestCallbackCardResponseBuildsReplacementCard(t *testing.T) {
	response := callbackCardResponse(&ActionResult{
		ReplaceCurrentCard: &Operation{
			Kind:         OperationSendCard,
			CardTitle:    "命令菜单",
			CardBody:     "当前在发送设置。",
			CardThemeKey: cardThemeInfo,
			CardElements: []map[string]any{{
				"tag":     "markdown",
				"content": "**发送设置**",
			}},
		},
	})
	if response == nil || response.Card == nil {
		t.Fatalf("expected callback replacement response, got %#v", response)
	}
	if response.Card.Type != "raw" {
		t.Fatalf("unexpected callback card type: %#v", response.Card)
	}
	data, ok := response.Card.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected callback card data map, got %#v", response.Card.Data)
	}
	if data["schema"] != "2.0" {
		t.Fatalf("expected callback card schema 2.0, got %#v", data)
	}
	header, _ := data["header"].(map[string]any)
	title, _ := header["title"].(map[string]any)
	if title["content"] != "命令菜单" {
		t.Fatalf("unexpected callback card header: %#v", data)
	}
	body, _ := data["body"].(map[string]any)
	elements, _ := body["elements"].([]map[string]any)
	if len(elements) != 2 {
		t.Fatalf("expected body markdown plus extra element, got %#v", elements)
	}
}

func TestHandleCardActionTriggerReturnsImmediatelyForAsyncAction(t *testing.T) {
	action := control.Action{
		Kind: control.ActionDebugCommand,
		Inbound: &control.ActionInboundMeta{
			EventType: "card.action.trigger",
		},
	}
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	handler := func(context.Context, control.Action) *ActionResult {
		close(started)
		<-release
		close(done)
		return nil
	}

	begin := time.Now()
	resp, err := handleCardActionTrigger(context.Background(), action, handler)
	if err != nil {
		t.Fatalf("handleCardActionTrigger returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected empty callback response")
	}
	if elapsed := time.Since(begin); elapsed > 100*time.Millisecond {
		t.Fatalf("expected async callback ack, took %s", elapsed)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("expected background handler to start")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected background handler to finish")
	}
}

func TestHandleCardActionTriggerWaitsForInlineReplacementAction(t *testing.T) {
	action := control.Action{
		Kind: control.ActionShowCommandMenu,
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: "life-1",
		},
	}
	started := make(chan struct{})
	release := make(chan struct{})
	resultCh := make(chan *larkcallback.CardActionTriggerResponse, 1)
	errCh := make(chan error, 1)
	handler := func(context.Context, control.Action) *ActionResult {
		close(started)
		<-release
		return &ActionResult{
			ReplaceCurrentCard: &Operation{
				Kind:         OperationSendCard,
				CardTitle:    "命令菜单",
				CardBody:     "已切到发送设置。",
				CardThemeKey: cardThemeInfo,
			},
		}
	}

	go func() {
		resp, err := handleCardActionTrigger(context.Background(), action, handler)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- resp
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("expected handler to start synchronously")
	}
	select {
	case <-resultCh:
		t.Fatal("expected callback to wait for handler result")
	case err := <-errCh:
		t.Fatalf("handleCardActionTrigger returned error: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)

	select {
	case err := <-errCh:
		t.Fatalf("handleCardActionTrigger returned error: %v", err)
	case resp := <-resultCh:
		if resp == nil || resp.Card == nil {
			t.Fatalf("expected replacement callback response, got %#v", resp)
		}
	case <-time.After(time.Second):
		t.Fatal("expected callback to return after handler finished")
	}
}

func TestHandleCardActionTriggerKeepsParameterApplyAsync(t *testing.T) {
	action := control.Action{
		Kind: control.ActionModeCommand,
		Text: "/mode vscode",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: "life-1",
		},
	}
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	handler := func(context.Context, control.Action) *ActionResult {
		close(started)
		<-release
		close(done)
		return nil
	}

	begin := time.Now()
	resp, err := handleCardActionTrigger(context.Background(), action, handler)
	if err != nil {
		t.Fatalf("handleCardActionTrigger returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected empty callback response")
	}
	if elapsed := time.Since(begin); elapsed > 100*time.Millisecond {
		t.Fatalf("expected async callback ack, took %s", elapsed)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("expected background handler to start")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected background handler to finish")
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

func TestParseCardActionTriggerEventBuildsRequestRespondAnswers(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-card-4", "feishu:app-1:user:user-1")
	userID := "user-1"
	event := &larkcallback.CardActionTriggerEvent{
		Event: &larkcallback.CardActionTriggerRequest{
			Operator: &larkcallback.Operator{UserID: &userID},
			Action: &larkcallback.CallBackAction{
				Value: map[string]interface{}{
					"kind":         "request_respond",
					"request_id":   "req-ui-1",
					"request_type": "request_user_input",
					"request_answers": map[string]interface{}{
						"model": []interface{}{"gpt-5.4"},
					},
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
		t.Fatal("expected request_user_input button callback to be parsed")
	}
	if action.Kind != control.ActionRespondRequest || action.RequestID != "req-ui-1" {
		t.Fatalf("unexpected action: %#v", action)
	}
	if got := action.RequestAnswers["model"]; len(got) != 1 || got[0] != "gpt-5.4" {
		t.Fatalf("unexpected request answers payload: %#v", action.RequestAnswers)
	}
}

func TestParseCardActionTriggerEventBuildsSubmitRequestFormAction(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-card-5", "feishu:app-1:user:user-1")
	userID := "user-1"
	event := &larkcallback.CardActionTriggerEvent{
		Event: &larkcallback.CardActionTriggerRequest{
			Operator: &larkcallback.Operator{UserID: &userID},
			Action: &larkcallback.CallBackAction{
				Value: map[string]interface{}{
					"kind":         "submit_request_form",
					"request_id":   "req-ui-2",
					"request_type": "request_user_input",
				},
				FormValue: map[string]interface{}{
					"model": "gpt-5.4",
					"notes": "请用中文回复",
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
		t.Fatal("expected request_user_input form callback to be parsed")
	}
	if action.Kind != control.ActionRespondRequest || action.RequestID != "req-ui-2" {
		t.Fatalf("unexpected action: %#v", action)
	}
	if got := action.RequestAnswers["notes"]; len(got) != 1 || got[0] != "请用中文回复" {
		t.Fatalf("unexpected form request answers: %#v", action.RequestAnswers)
	}
}

func TestIgnoredMissingReactionError(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want bool
	}{
		{name: "english missing message", msg: "message not found", want: true},
		{name: "english missing message sentence", msg: "The message is not found", want: true},
		{name: "english missing target sentence", msg: "The target message is not found", want: true},
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

func int64Ref(value int64) *int64 {
	return &value
}

func TestApplySendImageRepliesToSourceMessage(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	var (
		uploadPath     string
		replyMessageID string
		replyMsgType   string
		replyContent   string
		createCalled   bool
	)
	gateway.uploadImagePathFn = func(_ context.Context, path string) (string, error) {
		uploadPath = path
		return "img-key-1", nil
	}
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
				MessageId: stringRef("om-image-1"),
			},
		}, nil
	}
	gateway.createMessageFn = func(_ context.Context, _, _, _, _ string) (*larkim.CreateMessageResp, error) {
		createCalled = true
		return nil, nil
	}

	err := gateway.Apply(t.Context(), []Operation{{
		Kind:             OperationSendImage,
		SurfaceSessionID: "surface-1",
		ChatID:           "oc_1",
		ReceiveID:        "oc_1",
		ReceiveIDType:    "chat_id",
		ReplyToMessageID: "om-source-1",
		ImagePath:        "/tmp/generated.png",
	}})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if createCalled {
		t.Fatalf("expected reply path without fallback create")
	}
	if uploadPath != "/tmp/generated.png" {
		t.Fatalf("unexpected uploaded image path: %q", uploadPath)
	}
	if replyMessageID != "om-source-1" || replyMsgType != "image" {
		t.Fatalf("unexpected image reply request: message=%q type=%q", replyMessageID, replyMsgType)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(replyContent), &payload); err != nil {
		t.Fatalf("image reply content is not valid json: %v", err)
	}
	if payload["image_key"] != "img-key-1" {
		t.Fatalf("unexpected image reply payload: %#v", payload)
	}
	if gateway.messages["om-image-1"] != "surface-1" {
		t.Fatalf("expected replied image message to be tracked for surface callbacks, got %#v", gateway.messages)
	}
}

func TestApplySendImageFallsBackToBase64WhenSavedPathUploadFails(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	var (
		pathCalls     int
		uploadedBytes []byte
		createMsgType string
		createContent string
	)
	gateway.uploadImagePathFn = func(_ context.Context, _ string) (string, error) {
		pathCalls++
		return "", errors.New("missing file")
	}
	gateway.uploadImageBytesFn = func(_ context.Context, data []byte) (string, error) {
		uploadedBytes = append([]byte(nil), data...)
		return "img-key-2", nil
	}
	gateway.createMessageFn = func(_ context.Context, receiveIDType, receiveID, msgType, content string) (*larkim.CreateMessageResp, error) {
		if receiveIDType != "chat_id" || receiveID != "oc_1" {
			t.Fatalf("unexpected receive target: type=%q id=%q", receiveIDType, receiveID)
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
				MessageId: stringRef("om-image-2"),
			},
		}, nil
	}

	encoded := base64.StdEncoding.EncodeToString([]byte("fake-image"))
	err := gateway.Apply(t.Context(), []Operation{{
		Kind:             OperationSendImage,
		SurfaceSessionID: "surface-1",
		ChatID:           "oc_1",
		ReceiveID:        "oc_1",
		ReceiveIDType:    "chat_id",
		ImagePath:        "/tmp/missing.png",
		ImageBase64:      encoded,
	}})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if pathCalls != 1 {
		t.Fatalf("expected one saved-path upload attempt, got %d", pathCalls)
	}
	if string(uploadedBytes) != "fake-image" {
		t.Fatalf("unexpected uploaded fallback bytes: %q", uploadedBytes)
	}
	if createMsgType != "image" {
		t.Fatalf("unexpected image message type: %q", createMsgType)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(createContent), &payload); err != nil {
		t.Fatalf("image create content is not valid json: %v", err)
	}
	if payload["image_key"] != "img-key-2" {
		t.Fatalf("unexpected image create payload: %#v", payload)
	}
	if gateway.messages["om-image-2"] != "surface-1" {
		t.Fatalf("expected created image message to be tracked for surface callbacks, got %#v", gateway.messages)
	}
}
