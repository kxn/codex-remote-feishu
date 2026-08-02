package feishu

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestV7AppAutoConfigUsesTypedSDKEndpoints(t *testing.T) {
	appID := "cli_v7_typed_sdk"
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tenant-token"}`))
		case "/open-apis/application/v7/applications/" + appID + "/config":
			calls = append(calls, "config")
			body := requireV7Request(t, r, http.MethodPatch)
			requireBodyContains(t, body, `"scope_name":"im:message"`, `"subscription_type":"websocket"`, `"add_events":["im.message.receive_v1"]`, `"callback_type":"websocket"`)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
		case "/open-apis/application/v7/applications/" + appID + "/ability":
			calls = append(calls, "ability")
			body := requireV7Request(t, r, http.MethodPatch)
			requireBodyContains(t, body, `"bot":{"enable":true}`)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
		case "/open-apis/application/v7/applications/" + appID + "/publish":
			calls = append(calls, "publish")
			body := requireV7Request(t, r, http.MethodPost)
			requireBodyContains(t, body, `"remark":""`, `"changelog":"setup config changed"`)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"version_id":"vid-1","version":"1.0.1"}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	setup := NewSetupClient(SetupClientConfig{
		GatewayID: "main",
		AppID:     appID,
		AppSecret: "secret",
		Domain:    server.URL,
	})
	client, broker := setup.sdk()

	eventURL := "https://example.com/event"
	callbackURL := "https://example.com/callback"
	if err := patchV7AppConfig(context.Background(), broker, client, appID, v7PatchConfigRequest{
		Scope: &v7PatchConfigScope{AddScopes: []v7PatchConfigScopeItem{{ScopeName: "im:message", TokenType: "tenant"}}},
		Event: &v7PatchConfigEvent{
			SubscriptionType: "websocket",
			RequestURL:       &eventURL,
			AddEvents:        []string{"im.message.receive_v1"},
		},
		Callback: &v7PatchConfigCallback{
			CallbackType: "websocket",
			RequestURL:   &callbackURL,
		},
	}); err != nil {
		t.Fatalf("patchV7AppConfig: %v", err)
	}
	if err := patchV7AppAbility(context.Background(), broker, client, appID, v7PatchAbilityRequest{
		Bot: &v7PatchAbilityBot{Enable: true},
	}); err != nil {
		t.Fatalf("patchV7AppAbility: %v", err)
	}
	versionID, version, err := publishV7App(context.Background(), broker, client, appID, v7PublishRequest{
		Remark:    "",
		Changelog: "setup config changed",
	})
	if err != nil {
		t.Fatalf("publishV7App: %v", err)
	}
	if versionID != "vid-1" || version != "1.0.1" {
		t.Fatalf("publish return = %q/%q, want vid-1/1.0.1", versionID, version)
	}
	if want := []string{"config", "ability", "publish"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestToSDKPatchApplicationConfigReqPreservesDiffPayload(t *testing.T) {
	eventURL := "https://example.com/event"
	callbackURL := "https://example.com/callback"

	body := toSDKPatchApplicationConfigReqBody(v7PatchConfigRequest{
		Scope: &v7PatchConfigScope{
			AddScopes: []v7PatchConfigScopeItem{
				{ScopeName: " im:message ", TokenType: " tenant "},
			},
			RemoveScopes: []v7PatchConfigScopeItem{
				{ScopeName: " drive:drive ", TokenType: " tenant "},
			},
		},
		Event: &v7PatchConfigEvent{
			SubscriptionType: " websocket ",
			RequestURL:       &eventURL,
			AddEvents:        []string{"im.message.receive_v1"},
			RemoveEvents:     []string{"im.message.recalled_v1"},
		},
		Callback: &v7PatchConfigCallback{
			CallbackType:    " webhook ",
			RequestURL:      &callbackURL,
			AddCallbacks:    []string{"card.action.trigger"},
			RemoveCallbacks: []string{"card.action.trigger_v1"},
		},
	})

	if body == nil {
		t.Fatalf("request body is nil")
	}
	if body.Scope == nil || len(body.Scope.AddScopes) != 1 || len(body.Scope.RemoveScopes) != 1 {
		t.Fatalf("unexpected scope payload: %#v", body.Scope)
	}
	if got := stringPtr(body.Scope.AddScopes[0].ScopeName); got != "im:message" {
		t.Fatalf("add scope name = %q, want trimmed im:message", got)
	}
	if got := stringPtr(body.Scope.AddScopes[0].TokenType); got != "tenant" {
		t.Fatalf("add scope token type = %q, want tenant", got)
	}
	if got := stringPtr(body.Scope.RemoveScopes[0].ScopeName); got != "drive:drive" {
		t.Fatalf("remove scope name = %q, want trimmed drive:drive", got)
	}
	if body.Event == nil || stringPtr(body.Event.SubscriptionType) != "websocket" || stringPtr(body.Event.RequestUrl) != eventURL {
		t.Fatalf("unexpected event payload: %#v", body.Event)
	}
	if len(body.Event.AddEvents) != 1 || body.Event.AddEvents[0] != "im.message.receive_v1" {
		t.Fatalf("unexpected add events: %#v", body.Event.AddEvents)
	}
	if body.Callback == nil || stringPtr(body.Callback.CallbackType) != "webhook" || stringPtr(body.Callback.RequestUrl) != callbackURL {
		t.Fatalf("unexpected callback payload: %#v", body.Callback)
	}
	if len(body.Callback.RemoveCallbacks) != 1 || body.Callback.RemoveCallbacks[0] != "card.action.trigger_v1" {
		t.Fatalf("unexpected remove callbacks: %#v", body.Callback.RemoveCallbacks)
	}
}

func TestToSDKPatchApplicationAbilityReqPreservesBotEnable(t *testing.T) {
	body := toSDKPatchApplicationAbilityReqBody(v7PatchAbilityRequest{
		Bot: &v7PatchAbilityBot{Enable: true},
	})
	if body == nil || body.Bot == nil || body.Bot.Enable == nil {
		t.Fatalf("bot enable payload is nil: %#v", body)
	}
	if !boolPtr(body.Bot.Enable) {
		t.Fatalf("bot enable = false, want true")
	}
}

func TestToSDKCreateApplicationPublishReqPreservesRequiredTextAndOmitsEmptyDefaults(t *testing.T) {
	body := toSDKCreateApplicationPublishReqBody(v7PublishRequest{
		Remark:    "",
		Changelog: "setup config changed",
	})
	if body == nil {
		t.Fatalf("publish request body is nil")
	}
	if body.Remark == nil || stringPtr(body.Remark) != "" {
		t.Fatalf("remark pointer should preserve explicit empty string, got %#v", body.Remark)
	}
	if got := stringPtr(body.Changelog); got != "setup config changed" {
		t.Fatalf("changelog = %q, want setup config changed", got)
	}
	if body.MobileDefaultAbility != nil || body.PcDefaultAbility != nil || body.Version != nil {
		t.Fatalf("empty optional defaults should be omitted: %#v", body)
	}
}

func requireV7Request(t *testing.T, r *http.Request, method string) string {
	t.Helper()
	if r.Method != method {
		t.Fatalf("method = %s, want %s", r.Method, method)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer tenant-token" {
		t.Fatalf("Authorization = %q, want tenant token", got)
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	return string(body)
}

func requireBodyContains(t *testing.T, body string, needles ...string) {
	t.Helper()
	for _, needle := range needles {
		if !strings.Contains(body, needle) {
			t.Fatalf("request body %s does not contain %s", body, needle)
		}
	}
}
