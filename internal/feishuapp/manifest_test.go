package feishuapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestDeployTemplateStaysInSyncWithManifest(t *testing.T) {
	manifest := DefaultManifest()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	templatePath := filepath.Join(filepath.Dir(file), "..", "..", "deploy", "feishu", "app-template.json")
	raw, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}

	var parsed struct {
		ScopesImport       ScopesImport `json:"scopes_import"`
		EventSubscriptions []struct {
			Event string `json:"event"`
		} `json:"event_subscriptions"`
		CallbackSubscriptions []struct {
			Callback string `json:"callback"`
		} `json:"callback_subscriptions"`
		Bot struct {
			Menus []struct {
				Key         string `json:"key"`
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"menus"`
		} `json:"bot"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("decode template: %v", err)
	}

	if !reflect.DeepEqual(parsed.ScopesImport, manifest.Scopes) {
		t.Fatalf("scopes import mismatch: %#v vs %#v", parsed.ScopesImport, manifest.Scopes)
	}

	gotEvents := make([]string, 0, len(parsed.EventSubscriptions))
	for _, item := range parsed.EventSubscriptions {
		gotEvents = append(gotEvents, item.Event)
	}
	wantEvents := make([]string, 0, len(manifest.Events))
	for _, item := range manifest.Events {
		wantEvents = append(wantEvents, item.Event)
	}
	if !reflect.DeepEqual(gotEvents, wantEvents) {
		t.Fatalf("events mismatch: %#v vs %#v", gotEvents, wantEvents)
	}

	gotCallbacks := make([]string, 0, len(parsed.CallbackSubscriptions))
	for _, item := range parsed.CallbackSubscriptions {
		gotCallbacks = append(gotCallbacks, item.Callback)
	}
	wantCallbacks := make([]string, 0, len(manifest.Callbacks))
	for _, item := range manifest.Callbacks {
		wantCallbacks = append(wantCallbacks, item.Callback)
	}
	if !reflect.DeepEqual(gotCallbacks, wantCallbacks) {
		t.Fatalf("callbacks mismatch: %#v vs %#v", gotCallbacks, wantCallbacks)
	}

	gotMenus := make([]string, 0, len(parsed.Bot.Menus))
	for _, item := range parsed.Bot.Menus {
		gotMenus = append(gotMenus, item.Key)
	}
	wantMenus := make([]string, 0, len(manifest.Menus))
	for _, item := range manifest.Menus {
		wantMenus = append(wantMenus, item.Key)
	}
	if !reflect.DeepEqual(gotMenus, wantMenus) {
		t.Fatalf("menu keys mismatch: %#v vs %#v", gotMenus, wantMenus)
	}

	if len(manifest.Menus) != len(control.FeishuRecommendedMenus()) {
		t.Fatalf("manifest menus count = %d, want %d", len(manifest.Menus), len(control.FeishuRecommendedMenus()))
	}
	for index, item := range manifest.Menus {
		want := control.FeishuRecommendedMenus()[index]
		if item.Key != want.Key || item.Name != want.Name || item.Description != want.Description {
			t.Fatalf("manifest menu[%d] = %#v, want %#v", index, item, want)
		}
		if parsed.Bot.Menus[index].Key != want.Key {
			t.Fatalf("template menu[%d] key = %q, want %q", index, parsed.Bot.Menus[index].Key, want.Key)
		}
	}

	for index, item := range parsed.Bot.Menus {
		want := manifest.Menus[index]
		if item.Key != want.Key || item.Name != want.Name || item.Description != want.Description {
			t.Fatalf("template menu[%d] = %#v, want %#v", index, item, want)
		}
	}
}

func TestDefaultManifestRequirementsMetadata(t *testing.T) {
	manifest := DefaultManifest()
	if len(manifest.ScopeRequirements) != len(manifest.Scopes.Scopes.Tenant)+len(manifest.Scopes.Scopes.User) {
		t.Fatalf("scope requirements count = %d, want %d", len(manifest.ScopeRequirements), len(manifest.Scopes.Scopes.Tenant)+len(manifest.Scopes.Scopes.User))
	}

	requiredEvents := 0
	for _, item := range manifest.Events {
		if strings.TrimSpace(item.Feature) == "" {
			t.Fatalf("event %q missing feature metadata", item.Event)
		}
		if !item.Required && strings.TrimSpace(item.DegradeMessage) == "" {
			t.Fatalf("optional event %q missing degradeMessage", item.Event)
		}
		if item.Required {
			requiredEvents++
		}
	}
	if requiredEvents == 0 {
		t.Fatal("expected at least one required event")
	}

	requiredCallbacks := 0
	for _, item := range manifest.Callbacks {
		if strings.TrimSpace(item.Feature) == "" {
			t.Fatalf("callback %q missing feature metadata", item.Callback)
		}
		if !item.Required && strings.TrimSpace(item.DegradeMessage) == "" {
			t.Fatalf("optional callback %q missing degradeMessage", item.Callback)
		}
		if item.Required {
			requiredCallbacks++
		}
	}
	if requiredCallbacks == 0 {
		t.Fatal("expected at least one required callback")
	}
}

func TestDefaultFixedPolicy(t *testing.T) {
	policy := DefaultFixedPolicy()
	if policy.EventSubscriptionType != FeishuEventSubscriptionTypeWebsocket {
		t.Fatalf("event subscription type = %q, want %q", policy.EventSubscriptionType, FeishuEventSubscriptionTypeWebsocket)
	}
	if policy.CallbackType != FeishuCallbackTypeWebsocket {
		t.Fatalf("callback type = %q, want %q", policy.CallbackType, FeishuCallbackTypeWebsocket)
	}
	if !policy.BotEnabled {
		t.Fatal("expected bot to be enabled in fixed policy")
	}
	if policy.MobileDefaultAbility != FeishuDefaultAbilityBot || policy.PcDefaultAbility != FeishuDefaultAbilityBot {
		t.Fatalf("unexpected default abilities: %#v", policy)
	}
	if !policy.PreserveExistingEncryptKV {
		t.Fatal("expected preserve-existing encryption strategy in stage 1")
	}
}
