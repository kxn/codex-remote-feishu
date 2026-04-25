package orchestrator

import (
	"reflect"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestHelpActionBuildsPageCatalogEvent(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowCommandHelp,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		GatewayID:        "app-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected page catalog event, got %#v", events)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if events[0].Kind != eventcontract.KindPage {
		t.Fatalf("unexpected event kind: %#v", events[0])
	}
	if catalog.Interactive {
		t.Fatalf("help catalog should be non-interactive: %#v", catalog)
	}
	if catalog.Title != "命令帮助" {
		t.Fatalf("unexpected help catalog title: %#v", catalog)
	}
}

func TestHelpActionNormalModeCollapsesSwitchTargetCommands(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowCommandHelp,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		GatewayID:        "app-1",
	})
	catalog := commandCatalogFromEvent(t, events[0])
	if catalog == nil {
		t.Fatalf("expected help catalog event, got %#v", events)
	}
	var switchEntries []control.CommandCatalogEntry
	for _, section := range catalog.Sections {
		if section.Title == "工作会话" {
			switchEntries = section.Entries
			break
		}
	}
	got := firstCommands(switchEntries)
	want := []string{"/workspace", "/workspace list", "/workspace new", "/workspace new dir", "/workspace new git", "/workspace detach"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normal help switch_target commands = %#v, want %#v", got, want)
	}
	if len(switchEntries) == 0 || switchEntries[0].Title != "工作会话" {
		t.Fatalf("expected workspace family help entries, got %#v", switchEntries)
	}
}

func TestHelpActionVSCodeModeKeepsSeparateSwitchTargetCommands(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowCommandHelp,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		GatewayID:        "app-1",
	})
	catalog := commandCatalogFromEvent(t, events[0])
	if catalog == nil {
		t.Fatalf("expected help catalog event, got %#v", events)
	}
	var switchEntries []control.CommandCatalogEntry
	for _, section := range catalog.Sections {
		if section.Title == "工作会话" {
			switchEntries = section.Entries
			break
		}
	}
	got := firstCommands(switchEntries)
	want := []string{"/list", "/use", "/useall", "/detach", "/follow"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("vscode help switch_target commands = %#v, want %#v", got, want)
	}
}

func TestMenuActionBuildsInteractiveCommandCatalogEvent(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowCommandMenu,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		GatewayID:        "app-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected interactive command catalog event, got %#v", events)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if !catalog.Interactive {
		t.Fatalf("menu catalog should be interactive: %#v", catalog)
	}
	if catalog.DisplayStyle != control.CommandCatalogDisplayCompactButtons {
		t.Fatalf("menu catalog should use compact button display: %#v", catalog)
	}
	if catalog.Title != "命令菜单" {
		t.Fatalf("unexpected menu catalog title: %#v", catalog)
	}
	if events[0].PageContext == nil {
		t.Fatalf("expected feishu page context, got %#v", events[0])
	}
	if events[0].PageView == nil {
		t.Fatalf("expected feishu page view menu payload, got %#v", events[0].PageView)
	}
	if events[0].PageContext.DTOOwner != control.FeishuUIDTOwnerPage {
		t.Fatalf("unexpected dto owner: %#v", events[0].PageContext)
	}
	if events[0].PageContext.Surface.CallbackPayloadOwner != control.FeishuUICallbackPayloadOwnerAdapter {
		t.Fatalf("unexpected callback payload owner: %#v", events[0].PageContext)
	}
	if events[0].PageContext.Surface.InlineReplaceFreshness != "daemon_lifecycle" || !events[0].PageContext.Surface.InlineReplaceRequiresFreshness {
		t.Fatalf("unexpected inline replace context: %#v", events[0].PageContext.Surface)
	}
	if events[0].PageContext.Surface.InlineReplaceViewSession != "surface_state_rederived" || events[0].PageContext.Surface.InlineReplaceRequiresViewState {
		t.Fatalf("unexpected inline replace view/session context: %#v", events[0].PageContext.Surface)
	}
}

func TestMenuActionDetachedHomepageShowsGroupNavigationOnly(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowCommandMenu,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		GatewayID:        "app-1",
	})
	if len(events) != 1 {
		t.Fatalf("expected command catalog, got %#v", events)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if len(catalog.Sections) != 1 || catalog.Sections[0].Title != "" {
		t.Fatalf("unexpected detached home catalog: %#v", catalog)
	}
	if len(firstCommands(catalog.Sections[0].Entries)) != 0 {
		t.Fatalf("expected home catalog to be pure group navigation, got %#v", catalog.Sections[0].Entries)
	}
	if len(catalog.Breadcrumbs) != 1 || catalog.Breadcrumbs[0].Label != "菜单首页" {
		t.Fatalf("expected detached home to stay at root breadcrumb, got %#v", catalog.Breadcrumbs)
	}
	if len(catalog.RelatedButtons) != 0 {
		t.Fatalf("expected detached home to avoid back buttons, got %#v", catalog.RelatedButtons)
	}
}

func TestMenuActionBareMenuResetsSubmenuChromeBackToRoot(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	submenu := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowCommandMenu,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		GatewayID:        "app-1",
		Text:             "/menu maintenance",
	})
	submenuCatalog := commandCatalogFromEvent(t, submenu[0])
	if len(submenuCatalog.Breadcrumbs) != 2 || submenuCatalog.Breadcrumbs[1].Label != "系统管理" {
		t.Fatalf("expected submenu breadcrumbs before reset, got %#v", submenuCatalog.Breadcrumbs)
	}
	if len(submenuCatalog.RelatedButtons) != 1 || submenuCatalog.RelatedButtons[0].CommandText != "/menu" {
		t.Fatalf("expected submenu back button before reset, got %#v", submenuCatalog.RelatedButtons)
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowCommandMenu,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		GatewayID:        "app-1",
		Text:             "/menu",
	})
	if len(events) != 1 {
		t.Fatalf("expected root command catalog, got %#v", events)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if len(catalog.Breadcrumbs) != 1 || catalog.Breadcrumbs[0].Label != "菜单首页" {
		t.Fatalf("expected bare /menu to reset breadcrumb to root, got %#v", catalog.Breadcrumbs)
	}
	if len(catalog.RelatedButtons) != 0 {
		t.Fatalf("expected bare /menu to clear submenu back buttons, got %#v", catalog.RelatedButtons)
	}
}

func TestMenuActionNormalSwitchTargetGroupUsesUnifiedPickerEntry(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	svc.root.Surfaces["surface-1"].AttachedInstanceID = "inst-1"

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowCommandMenu,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		GatewayID:        "app-1",
		Text:             "/menu switch_target",
	})
	catalog := commandCatalogFromEvent(t, events[0])
	got := firstButtonLabels(catalog.Sections[0].Entries)
	want := []string{"切换", "从目录新建", "从 GIT URL 新建", "解除接管"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normal switch_target button labels = %#v, want %#v", got, want)
	}
	if catalog.CommandID != control.FeishuCommandWorkspace || len(catalog.RelatedButtons) != 1 || catalog.RelatedButtons[0].CommandText != "/menu" {
		t.Fatalf("expected normal switch_target menu group to open workspace root page, got %#v", catalog)
	}
}

func TestMenuActionVSCodeSwitchTargetGroupShowsFollow(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	svc.root.Surfaces["surface-1"].AttachedInstanceID = "inst-1"

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowCommandMenu,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		GatewayID:        "app-1",
		Text:             "/menu switch_target",
	})
	catalog := commandCatalogFromEvent(t, events[0])
	got := firstCommands(catalog.Sections[0].Entries)
	want := []string{"/list", "/use", "/useall", "/detach", "/follow"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("vscode switch_target commands = %#v, want %#v", got, want)
	}
}

func TestMenuActionNormalCurrentWorkGroupShowsNew(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	svc.root.Surfaces["surface-1"].AttachedInstanceID = "inst-1"

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowCommandMenu,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		GatewayID:        "app-1",
		Text:             "/menu current_work",
	})
	catalog := commandCatalogFromEvent(t, events[0])
	got := firstCommands(catalog.Sections[0].Entries)
	want := []string{"/stop", "/compact", "/steerall", "/new", "/status"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normal current_work commands = %#v, want %#v", got, want)
	}
}

func TestMenuActionVSCodeCurrentWorkGroupHidesNew(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	svc.root.Surfaces["surface-1"].AttachedInstanceID = "inst-1"

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowCommandMenu,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		GatewayID:        "app-1",
		Text:             "/menu current_work",
	})
	catalog := commandCatalogFromEvent(t, events[0])
	got := firstCommands(catalog.Sections[0].Entries)
	want := []string{"/stop", "/compact", "/steerall", "/status"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("vscode current_work commands = %#v, want %#v", got, want)
	}
}

func TestMenuActionMaintenanceGroupShowsSystemManagementCommands(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowCommandMenu,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		GatewayID:        "app-1",
		Text:             "/menu maintenance",
	})
	catalog := commandCatalogFromEvent(t, events[0])
	got := firstCommands(catalog.Sections[0].Entries)
	want := []string{"/mode", "/upgrade", "/debug", "/help"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("maintenance commands = %#v, want %#v", got, want)
	}
}

func TestMenuSubmenuShowsReturnToPreviousLevelButton(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowCommandMenu,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		GatewayID:        "app-1",
		Text:             "/menu send_settings",
	})
	if len(events) != 1 {
		t.Fatalf("expected command catalog, got %#v", events)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if len(catalog.RelatedButtons) != 1 || catalog.RelatedButtons[0].CommandText != "/menu" {
		t.Fatalf("submenu should expose a back button to /menu, got %#v", catalog.RelatedButtons)
	}
}

func TestBareReasoningCommandBuildsParameterCard(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	svc.root.Surfaces["surface-1"].AttachedInstanceID = "inst-1"
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReasoningCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/reasoning",
	})
	if len(events) != 1 {
		t.Fatalf("expected reasoning command catalog, got %#v", events)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if catalog.CommandID != control.FeishuCommandReasoning {
		t.Fatalf("expected reasoning command page, got %#v", catalog)
	}
	if catalog.Title != "推理强度" {
		t.Fatalf("unexpected reasoning catalog title: %#v", catalog)
	}
	if len(catalog.Breadcrumbs) != 3 || catalog.Breadcrumbs[1].Label != "参数设置" {
		t.Fatalf("unexpected breadcrumbs: %#v", catalog.Breadcrumbs)
	}
	buttons := catalog.Sections[0].Entries[0].Buttons
	if len(buttons) != 5 || buttons[0].CommandText != "/reasoning low" || buttons[4].CommandText != "/reasoning clear" {
		t.Fatalf("unexpected reasoning buttons: %#v", buttons)
	}
	if len(catalog.Sections) != 1 || catalog.Sections[0].Entries[0].Form != nil {
		t.Fatalf("expected reasoning card to keep fixed choices only, got %#v", catalog.Sections)
	}
}

func TestBareModelCommandBuildsDropdownAndManualFormCard(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	svc.root.Surfaces["surface-1"].AttachedInstanceID = "inst-1"
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModelCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/model",
	})
	if len(events) != 1 {
		t.Fatalf("expected model catalog, got %#v", events)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if catalog.CommandID != control.FeishuCommandModel {
		t.Fatalf("expected model command page, got %#v", catalog)
	}
	if len(catalog.Sections) != 2 {
		t.Fatalf("expected dropdown + manual sections, got %#v", catalog.Sections)
	}
	preset := catalog.Sections[0].Entries[0]
	if preset.Form == nil || preset.Form.CommandText != "/model" {
		t.Fatalf("expected preset model dropdown form, got %#v", preset)
	}
	if preset.Form.Field.Kind != control.CommandCatalogFormFieldSelectStatic {
		t.Fatalf("expected preset form to use select_static, got %#v", preset.Form.Field)
	}
	options := preset.Form.Field.Options
	if len(options) != 6 || options[0].Value != "gpt-5.5" || options[1].Value != "gpt-5.4" || options[2].Value != "gpt-5.4-mini" || options[3].Value != "gpt-5.3-codex" || options[4].Value != "gpt-5.2" || options[5].Value != "gpt-5.2-codex" {
		t.Fatalf("unexpected model preset options: %#v", options)
	}
	manual := catalog.Sections[1].Entries[0]
	if manual.Form == nil || manual.Form.CommandText != "/model" {
		t.Fatalf("expected manual model form, got %#v", manual)
	}
}
