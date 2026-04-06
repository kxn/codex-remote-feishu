package feishuapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
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
		Bot struct {
			Menus []struct {
				Key string `json:"key"`
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
}
