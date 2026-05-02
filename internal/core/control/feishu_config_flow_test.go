package control

import (
	"reflect"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestFeishuConfigFlowRegistryRoundTrip(t *testing.T) {
	tests := []struct {
		commandID   string
		actionKind  ActionKind
		bareCommand string
		intentKind  FeishuUIIntentKind
	}{
		{commandID: FeishuCommandMode, actionKind: ActionModeCommand, bareCommand: "/mode", intentKind: FeishuUIIntentShowModeCatalog},
		{commandID: FeishuCommandCodexProvider, actionKind: ActionCodexProviderCommand, bareCommand: "/codexprovider", intentKind: FeishuUIIntentShowCodexProviderCatalog},
		{commandID: FeishuCommandClaudeProfile, actionKind: ActionClaudeProfileCommand, bareCommand: "/claudeprofile", intentKind: FeishuUIIntentShowClaudeProfileCatalog},
		{commandID: FeishuCommandAutoWhip, actionKind: ActionAutoWhipCommand, bareCommand: "/autowhip", intentKind: FeishuUIIntentShowAutoWhipCatalog},
		{commandID: FeishuCommandAutoContinue, actionKind: ActionAutoContinueCommand, bareCommand: "/autocontinue", intentKind: FeishuUIIntentShowAutoContinueCatalog},
		{commandID: FeishuCommandReasoning, actionKind: ActionReasoningCommand, bareCommand: "/reasoning", intentKind: FeishuUIIntentShowReasoningCatalog},
		{commandID: FeishuCommandAccess, actionKind: ActionAccessCommand, bareCommand: "/access", intentKind: FeishuUIIntentShowAccessCatalog},
		{commandID: FeishuCommandPlan, actionKind: ActionPlanCommand, bareCommand: "/plan", intentKind: FeishuUIIntentShowPlanCatalog},
		{commandID: FeishuCommandModel, actionKind: ActionModelCommand, bareCommand: "/model", intentKind: FeishuUIIntentShowModelCatalog},
		{commandID: FeishuCommandVerbose, actionKind: ActionVerboseCommand, bareCommand: "/verbose", intentKind: FeishuUIIntentShowVerboseCatalog},
	}

	for _, tt := range tests {
		t.Run(tt.commandID, func(t *testing.T) {
			defByCommand, ok := FeishuConfigFlowDefinitionByCommandID(tt.commandID)
			if !ok {
				t.Fatalf("expected config flow for command %q", tt.commandID)
			}
			if defByCommand.ActionKind != tt.actionKind || defByCommand.BareCommand != tt.bareCommand || defByCommand.IntentKind != tt.intentKind {
				t.Fatalf("unexpected command registry entry: %#v", defByCommand)
			}

			defByAction, ok := FeishuConfigFlowDefinitionByActionKind(tt.actionKind)
			if !ok || defByAction.CommandID != tt.commandID {
				t.Fatalf("expected action lookup for %q, got %#v", tt.actionKind, defByAction)
			}

			defByCatalog, ok := FeishuConfigFlowDefinitionForCatalog(tt.commandID, tt.commandID+".codex.normal")
			if !ok || defByCatalog.CommandID != tt.commandID {
				t.Fatalf("expected catalog lookup for %q, got %#v", tt.commandID, defByCatalog)
			}

			defByIntent, ok := FeishuConfigFlowDefinitionByIntentKind(tt.intentKind)
			if !ok || defByIntent.CommandID != tt.commandID {
				t.Fatalf("expected intent lookup for %q, got %#v", tt.intentKind, defByIntent)
			}

			actionKind, ok := ActionKindForFeishuCommandID(tt.commandID)
			if !ok || actionKind != tt.actionKind {
				t.Fatalf("ActionKindForFeishuCommandID(%q) = (%q, %v), want (%q, true)", tt.commandID, actionKind, ok, tt.actionKind)
			}

			if got := BuildFeishuActionText(tt.actionKind, ""); got != tt.bareCommand {
				t.Fatalf("BuildFeishuActionText(%q) = %q, want %q", tt.actionKind, got, tt.bareCommand)
			}

			if got := ResolveFeishuLauncherDisposition(Action{Kind: tt.actionKind, Text: tt.bareCommand}); got != FeishuFrontstageLauncherKeep {
				t.Fatalf("ResolveFeishuLauncherDisposition(%q) = %q, want keep", tt.actionKind, got)
			}

			page := BuildFeishuCommandConfigPageView(FeishuCatalogConfigView{CommandID: tt.commandID})
			if page.CommandID != tt.commandID || page.Title == "" {
				t.Fatalf("BuildFeishuCommandConfigPageView(%q) returned %#v", tt.commandID, page)
			}
		})
	}
}

func TestBuildFeishuCommandConfigPageViewResolvesFromCatalogFamily(t *testing.T) {
	page := BuildFeishuCommandConfigPageView(FeishuCatalogConfigView{
		CatalogFamilyID:  FeishuCommandModel,
		CatalogVariantID: "model.codex.normal",
		CatalogBackend:   agentproto.BackendCodex,
	})
	if page.CommandID != FeishuCommandModel || page.Title == "" {
		t.Fatalf("expected model config page, got %#v", page)
	}
	if page.CatalogBackend != agentproto.BackendCodex {
		t.Fatalf("expected codex backend, got %#v", page)
	}
	if len(page.Sections) == 0 || len(page.Sections[0].Entries) == 0 || page.Sections[0].Entries[0].Form == nil {
		t.Fatalf("expected model config page form, got %#v", page)
	}
	form := page.Sections[0].Entries[0].Form
	if form.CatalogFamilyID != FeishuCommandModel || form.CatalogVariantID != "model.codex.normal" || form.CatalogBackend != agentproto.BackendCodex {
		t.Fatalf("expected catalog provenance to stay on config form, got %#v", form)
	}
}

func TestBuildFeishuReasoningConfigPageUsesBackendSpecificOptions(t *testing.T) {
	codexPage := BuildFeishuCommandConfigPageView(FeishuCatalogConfigView{
		CommandID:      FeishuCommandReasoning,
		CatalogBackend: agentproto.BackendCodex,
	})
	if got := commandTextsForFirstButtonRow(codexPage); !reflect.DeepEqual(got, []string{
		"/reasoning low",
		"/reasoning medium",
		"/reasoning high",
		"/reasoning xhigh",
		"/reasoning clear",
	}) {
		t.Fatalf("unexpected codex reasoning options: %#v", got)
	}

	claudePage := BuildFeishuCommandConfigPageView(FeishuCatalogConfigView{
		CommandID:      FeishuCommandReasoning,
		CatalogBackend: agentproto.BackendClaude,
	})
	if got := commandTextsForFirstButtonRow(claudePage); !reflect.DeepEqual(got, []string{
		"/reasoning low",
		"/reasoning medium",
		"/reasoning high",
		"/reasoning max",
		"/reasoning clear",
	}) {
		t.Fatalf("unexpected claude reasoning options: %#v", got)
	}
}

func commandTextsForFirstButtonRow(page FeishuPageView) []string {
	if len(page.Sections) == 0 || len(page.Sections[0].Entries) == 0 {
		return nil
	}
	buttons := page.Sections[0].Entries[0].Buttons
	commands := make([]string, 0, len(buttons))
	for _, button := range buttons {
		commands = append(commands, button.CommandText)
	}
	return commands
}

func TestFeishuConfigFlowIntentOnlyMatchesBareCommands(t *testing.T) {
	tests := []struct {
		name   string
		action Action
		want   FeishuUIIntentKind
		ok     bool
	}{
		{
			name:   "bare command opens config catalog",
			action: Action{Kind: ActionReasoningCommand, Text: "/reasoning"},
			want:   FeishuUIIntentShowReasoningCatalog,
			ok:     true,
		},
		{
			name:   "parameter command stays product owned",
			action: Action{Kind: ActionReasoningCommand, Text: "/reasoning high"},
			ok:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent, ok := FeishuConfigFlowIntentFromAction(tt.action)
			if ok != tt.ok {
				t.Fatalf("FeishuConfigFlowIntentFromAction(%#v) ok = %v, want %v", tt.action, ok, tt.ok)
			}
			if !tt.ok {
				if intent != nil {
					t.Fatalf("expected nil intent, got %#v", intent)
				}
				return
			}
			if intent == nil || intent.Kind != tt.want {
				t.Fatalf("unexpected intent: %#v", intent)
			}
		})
	}
}
