package orchestrator

import (
	"reflect"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestHelpActionBuildsCommandCatalogEvent(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowCommandHelp,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		GatewayID:        "app-1",
	})

	if len(events) != 1 || events[0].FeishuDirectCommandCatalog == nil {
		t.Fatalf("expected command catalog event, got %#v", events)
	}
	if events[0].Kind != control.UIEventFeishuDirectCommandCatalog {
		t.Fatalf("unexpected event kind: %#v", events[0])
	}
	if events[0].FeishuDirectCommandCatalog.Interactive {
		t.Fatalf("help catalog should be non-interactive: %#v", events[0].FeishuDirectCommandCatalog)
	}
	if events[0].FeishuDirectCommandCatalog.Title != "Slash 命令帮助" {
		t.Fatalf("unexpected help catalog title: %#v", events[0].FeishuDirectCommandCatalog)
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
	catalog := events[0].FeishuDirectCommandCatalog
	if catalog == nil {
		t.Fatalf("expected help catalog event, got %#v", events)
	}
	var switchEntries []control.CommandCatalogEntry
	for _, section := range catalog.Sections {
		if section.Title == "切换目标" {
			switchEntries = section.Entries
			break
		}
	}
	got := firstCommands(switchEntries)
	want := []string{"/list", "/detach", "/follow"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normal help switch_target commands = %#v, want %#v", got, want)
	}
	if len(switchEntries) == 0 || switchEntries[0].Title != "选择工作区/会话" {
		t.Fatalf("expected unified normal switch target entry title, got %#v", switchEntries)
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
	catalog := events[0].FeishuDirectCommandCatalog
	if catalog == nil {
		t.Fatalf("expected help catalog event, got %#v", events)
	}
	var switchEntries []control.CommandCatalogEntry
	for _, section := range catalog.Sections {
		if section.Title == "切换目标" {
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
	if events[0].FeishuCommandContext == nil {
		t.Fatalf("expected feishu command context, got %#v", events[0])
	}
	if events[0].FeishuCommandView == nil || events[0].FeishuCommandView.Menu == nil {
		t.Fatalf("expected feishu command view menu payload, got %#v", events[0].FeishuCommandView)
	}
	if events[0].FeishuCommandContext.DTOOwner != control.FeishuUIDTOwnerCommand {
		t.Fatalf("unexpected dto owner: %#v", events[0].FeishuCommandContext)
	}
	if events[0].FeishuCommandContext.ViewKind != "menu" || events[0].FeishuCommandContext.MenuStage != "detached" {
		t.Fatalf("unexpected command context: %#v", events[0].FeishuCommandContext)
	}
	if events[0].FeishuCommandContext.Surface.CallbackPayloadOwner != control.FeishuUICallbackPayloadOwnerAdapter {
		t.Fatalf("unexpected callback payload owner: %#v", events[0].FeishuCommandContext)
	}
	if events[0].FeishuCommandContext.Surface.InlineReplaceFreshness != "daemon_lifecycle" || !events[0].FeishuCommandContext.Surface.InlineReplaceRequiresFreshness {
		t.Fatalf("unexpected inline replace context: %#v", events[0].FeishuCommandContext.Surface)
	}
	if events[0].FeishuCommandContext.Surface.InlineReplaceViewSession != "surface_state_rederived" || events[0].FeishuCommandContext.Surface.InlineReplaceRequiresViewState {
		t.Fatalf("unexpected inline replace view/session context: %#v", events[0].FeishuCommandContext.Surface)
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
	if len(catalog.Sections) != 1 || catalog.Sections[0].Title != "全部分组" {
		t.Fatalf("unexpected detached home catalog: %#v", catalog)
	}
	if len(firstCommands(catalog.Sections[0].Entries)) != 0 {
		t.Fatalf("expected home catalog to be pure group navigation, got %#v", catalog.Sections[0].Entries)
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
	got := firstCommands(catalog.Sections[0].Entries)
	want := []string{"/list", "/detach"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normal switch_target commands = %#v, want %#v", got, want)
	}
	if len(catalog.Sections[0].Entries) == 0 || catalog.Sections[0].Entries[0].Title != "选择工作区/会话" {
		t.Fatalf("expected unified normal switch target title, got %#v", catalog.Sections[0].Entries)
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
	want := []string{"/stop", "/compact", "/steerall", "/new", "/history", "/sendfile"}
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
	want := []string{"/stop", "/compact", "/steerall", "/history", "/sendfile"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("vscode current_work commands = %#v, want %#v", got, want)
	}
}

func TestMenuActionMaintenanceGroupIncludesCron(t *testing.T) {
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
	want := []string{"/status", "/mode", "/autowhip", "/help", "/cron", "/upgrade", "/debug"}
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
	if events[0].FeishuCommandView == nil || events[0].FeishuCommandView.Config == nil || events[0].FeishuCommandView.Config.CommandID != control.FeishuCommandReasoning {
		t.Fatalf("expected reasoning command view, got %#v", events[0].FeishuCommandView)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if catalog.Title != "推理强度" {
		t.Fatalf("unexpected reasoning catalog title: %#v", catalog)
	}
	if len(catalog.Breadcrumbs) != 3 || catalog.Breadcrumbs[1].Label != "发送设置" {
		t.Fatalf("unexpected breadcrumbs: %#v", catalog.Breadcrumbs)
	}
	buttons := catalog.Sections[0].Entries[0].Buttons
	if len(buttons) != 5 || buttons[0].CommandText != "/reasoning low" || buttons[4].CommandText != "/reasoning clear" {
		t.Fatalf("unexpected reasoning buttons: %#v", buttons)
	}
	if len(catalog.Sections) < 2 || catalog.Sections[1].Entries[0].Form == nil {
		t.Fatalf("expected reasoning card to expose manual form input, got %#v", catalog.Sections)
	}
}

func TestBareModelCommandBuildsPresetAndFormCard(t *testing.T) {
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
	if events[0].FeishuCommandView == nil || events[0].FeishuCommandView.Config == nil || events[0].FeishuCommandView.Config.CommandID != control.FeishuCommandModel {
		t.Fatalf("expected model command view, got %#v", events[0].FeishuCommandView)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if len(catalog.Sections) != 2 {
		t.Fatalf("expected preset + manual sections, got %#v", catalog.Sections)
	}
	buttons := catalog.Sections[0].Entries[0].Buttons
	if len(buttons) != 2 || buttons[0].CommandText != "/model gpt-5.4" || buttons[1].CommandText != "/model gpt-5.4-mini" {
		t.Fatalf("unexpected model preset buttons: %#v", buttons)
	}
	manual := catalog.Sections[1].Entries[0]
	if manual.Form == nil || manual.Form.CommandText != "/model" {
		t.Fatalf("expected manual model form, got %#v", manual)
	}
	if svc.root.Surfaces["surface-1"].ActiveCommandCapture != nil {
		t.Fatalf("expected model catalog not to create command capture state")
	}
}
