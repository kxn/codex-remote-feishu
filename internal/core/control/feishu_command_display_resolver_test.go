package control

import (
	"reflect"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestResolveFeishuCommandDisplayFamilyCarriesContextualVariantIdentity(t *testing.T) {
	resolved, ok := ResolveFeishuCommandDisplayFamily(FeishuCommandMode, false, CatalogContext{})
	if !ok {
		t.Fatal("expected mode family to resolve")
	}
	if resolved.FamilyID != FeishuCommandMode {
		t.Fatalf("FamilyID = %q, want %q", resolved.FamilyID, FeishuCommandMode)
	}
	if resolved.VariantID != "mode.codex.normal" {
		t.Fatalf("VariantID = %q, want %q", resolved.VariantID, "mode.codex.normal")
	}
	if resolved.Definition.ID != FeishuCommandMode {
		t.Fatalf("Definition.ID = %q, want %q", resolved.Definition.ID, FeishuCommandMode)
	}
}

func TestResolveFeishuCommandDisplayGroupDefaultsToCodexNormalHelpProjection(t *testing.T) {
	resolved := ResolveFeishuCommandDisplayGroup(FeishuCommandGroupSwitchTarget, false, CatalogContext{})
	got := resolvedDisplayCommands(resolved)
	want := []string{"/workspace", "/workspace list", "/workspace new", "/workspace new dir", "/workspace new git", "/workspace new worktree", "/workspace detach"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default help switch_target commands = %#v, want %#v", got, want)
	}
}

func TestResolveFeishuCommandDisplayGroupSupportsVSCodeHelpProjection(t *testing.T) {
	resolved := ResolveFeishuCommandDisplayGroup(FeishuCommandGroupSwitchTarget, false, CatalogContext{
		ProductMode: "vscode",
	})
	got := resolvedDisplayCommands(resolved)
	want := []string{"/list", "/use", "/useall", "/detach", "/follow"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("vscode help switch_target commands = %#v, want %#v", got, want)
	}
}

func TestResolveFeishuCommandDisplayGroupSupportsMenuStageProjection(t *testing.T) {
	normalWorking := ResolveFeishuCommandDisplayGroup(FeishuCommandGroupCurrentWork, true, CatalogContext{
		ProductMode: "normal",
		MenuStage:   string(FeishuCommandMenuStageNormalWorking),
	})
	if got, want := resolvedDisplayCommands(normalWorking), []string{"/stop", "/compact", "/steerall", "/new", "/status"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("normal working menu commands = %#v, want %#v", got, want)
	}

	vscodeWorking := ResolveFeishuCommandDisplayGroup(FeishuCommandGroupCurrentWork, true, CatalogContext{
		ProductMode: "vscode",
		MenuStage:   string(FeishuCommandMenuStageVSCodeWorking),
	})
	if got, want := resolvedDisplayCommands(vscodeWorking), []string{"/stop", "/compact", "/steerall", "/status"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("vscode working menu commands = %#v, want %#v", got, want)
	}
}

func TestResolveFeishuCommandDisplayGroupAppliesClaudeSupportProfile(t *testing.T) {
	currentWork := ResolveFeishuCommandDisplayGroup(FeishuCommandGroupCurrentWork, true, CatalogContext{
		Backend:     agentproto.BackendClaude,
		ProductMode: "normal",
		MenuStage:   string(FeishuCommandMenuStageNormalWorking),
	})
	if got, want := resolvedDisplayCommands(currentWork), []string{"/stop", "/steerall", "/new", "/status"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("claude current_work menu commands = %#v, want %#v", got, want)
	}

	switchTarget := ResolveFeishuCommandDisplayGroup(FeishuCommandGroupSwitchTarget, false, CatalogContext{
		Backend:     agentproto.BackendClaude,
		ProductMode: "normal",
	})
	if got, want := resolvedDisplayCommands(switchTarget), []string{"/workspace", "/workspace list", "/workspace new", "/workspace new dir", "/workspace new git", "/workspace new worktree", "/workspace detach"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("claude switch_target help commands = %#v, want %#v", got, want)
	}

	sendSettings := ResolveFeishuCommandDisplayGroup(FeishuCommandGroupSendSettings, false, CatalogContext{
		Backend:     agentproto.BackendClaude,
		ProductMode: "normal",
	})
	if got, want := resolvedDisplayCommands(sendSettings), []string{"/mode", "/reasoning", "/access", "/plan", "/verbose", "/claudeprofile"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("claude send_settings help commands = %#v, want %#v", got, want)
	}

	commonTools := ResolveFeishuCommandDisplayGroup(FeishuCommandGroupCommonTools, false, CatalogContext{
		Backend:     agentproto.BackendClaude,
		ProductMode: "normal",
	})
	if got, want := resolvedDisplayCommands(commonTools), []string{"/coworkers", "/history", "/sendfile"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("claude common_tools help commands = %#v, want %#v", got, want)
	}

	maintenance := ResolveFeishuCommandDisplayGroup(FeishuCommandGroupMaintenance, false, CatalogContext{
		Backend:     agentproto.BackendClaude,
		ProductMode: "normal",
	})
	if got, want := resolvedDisplayCommands(maintenance), []string{"/admin", "/upgrade", "/debug", "/help", "/menu"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("claude maintenance help commands = %#v, want %#v", got, want)
	}
}

func TestResolveFeishuCommandDisplayFamilySupportsMenuStageProjection(t *testing.T) {
	tests := []struct {
		name        string
		familyID    string
		productMode string
		menuStage   string
		wantVisible bool
	}{
		{name: "follow hidden when detached", familyID: FeishuCommandFollow, productMode: "vscode", menuStage: string(FeishuCommandMenuStageDetached), wantVisible: false},
		{name: "follow hidden in normal mode", familyID: FeishuCommandFollow, productMode: "normal", menuStage: string(FeishuCommandMenuStageNormalWorking), wantVisible: false},
		{name: "follow visible in vscode working", familyID: FeishuCommandFollow, productMode: "vscode", menuStage: string(FeishuCommandMenuStageVSCodeWorking), wantVisible: true},
		{name: "new hidden when detached", familyID: FeishuCommandNew, productMode: "normal", menuStage: string(FeishuCommandMenuStageDetached), wantVisible: false},
		{name: "new visible in normal working", familyID: FeishuCommandNew, productMode: "normal", menuStage: string(FeishuCommandMenuStageNormalWorking), wantVisible: true},
		{name: "new hidden in vscode working", familyID: FeishuCommandNew, productMode: "vscode", menuStage: string(FeishuCommandMenuStageVSCodeWorking), wantVisible: false},
		{name: "patch hidden when detached", familyID: FeishuCommandPatch, productMode: "normal", menuStage: string(FeishuCommandMenuStageDetached), wantVisible: false},
		{name: "patch visible in normal working", familyID: FeishuCommandPatch, productMode: "normal", menuStage: string(FeishuCommandMenuStageNormalWorking), wantVisible: true},
		{name: "patch hidden in vscode working", familyID: FeishuCommandPatch, productMode: "vscode", menuStage: string(FeishuCommandMenuStageVSCodeWorking), wantVisible: false},
		{name: "status stays visible for unknown stage", familyID: FeishuCommandStatus, productMode: "normal", menuStage: "unknown-stage", wantVisible: true},
		{name: "follow stays hidden for unknown stage", familyID: FeishuCommandFollow, productMode: "vscode", menuStage: "unknown-stage", wantVisible: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := ResolveFeishuCommandDisplayFamily(tt.familyID, true, CatalogContext{
				ProductMode: tt.productMode,
				MenuStage:   tt.menuStage,
			})
			if ok != tt.wantVisible {
				t.Fatalf("ResolveFeishuCommandDisplayFamily(%q, true, %+v) visible = %v, want %v", tt.familyID, CatalogContext{
					ProductMode: tt.productMode,
					MenuStage:   tt.menuStage,
				}, ok, tt.wantVisible)
			}
		})
	}
}

func TestResolveFeishuCommandDisplayProfileTracksModeSpecificFamilies(t *testing.T) {
	codex := ResolveFeishuCommandDisplayProfileForContext(CatalogContext{ProductMode: "normal"})
	if got, want := codex.VisibleFamiliesForGroup(FeishuCommandGroupSwitchTarget), []string{
		FeishuCommandWorkspace,
		FeishuCommandWorkspaceList,
		FeishuCommandWorkspaceNew,
		FeishuCommandWorkspaceNewDir,
		FeishuCommandWorkspaceNewGit,
		FeishuCommandWorkspaceNewWorktree,
		FeishuCommandWorkspaceDetach,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("codex visible switch_target families = %#v, want %#v", got, want)
	}
	if got, want := codex.VisibleFamiliesForGroup(FeishuCommandGroupCommonTools), []string{
		FeishuCommandReview,
		FeishuCommandPatch,
		FeishuCommandAutoWhip,
		FeishuCommandPrimary,
		FeishuCommandCoworkers,
		FeishuCommandHistory,
		FeishuCommandCron,
		FeishuCommandSendFile,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("codex visible common_tools families = %#v, want %#v", got, want)
	}

	vscode := ResolveFeishuCommandDisplayProfileForContext(CatalogContext{ProductMode: "vscode"})
	if got, want := vscode.VisibleFamiliesForGroup(FeishuCommandGroupSwitchTarget), []string{
		FeishuCommandList,
		FeishuCommandUse,
		FeishuCommandUseAll,
		FeishuCommandDetach,
		FeishuCommandFollow,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("vscode visible switch_target families = %#v, want %#v", got, want)
	}
	if !vscode.IncludesFamily(FeishuCommandVSCodeMigrate) {
		t.Fatal("expected vscode profile to include vscode migrate")
	}
	if codex.IncludesFamily(FeishuCommandVSCodeMigrate) {
		t.Fatal("expected codex profile to hide vscode migrate")
	}
	if !codex.IncludesFamily(FeishuCommandCodexProfile) {
		t.Fatal("expected codex profile to include codex profile")
	}
	if vscode.IncludesFamily(FeishuCommandCodexProfile) {
		t.Fatal("expected vscode profile to hide codex profile")
	}
}

func TestGroupCatalogContextHidesBotCapabilitySettings(t *testing.T) {
	group := ResolveFeishuCommandDisplayProfileForContext(CatalogContext{
		ProductMode:                   "normal",
		BotCapabilitySettingsReadOnly: true,
	})
	for _, familyID := range []string{
		FeishuCommandMode,
		FeishuCommandCodexProfile,
		FeishuCommandClaudeProfile,
		FeishuCommandOpenCodeProfile,
		FeishuCommandModel,
		FeishuCommandReasoning,
		FeishuCommandAccess,
		FeishuCommandPlan,
	} {
		if group.IncludesFamily(familyID) {
			t.Fatalf("group catalog should hide bot capability family %q", familyID)
		}
	}
	for _, familyID := range []string{FeishuCommandAutoWhip, FeishuCommandAutoContinue, FeishuCommandVerbose} {
		if !group.IncludesFamily(familyID) {
			t.Fatalf("group catalog should keep context family %q", familyID)
		}
	}
	page := BuildFeishuCommandMenuGroupPageViewForContext(FeishuCommandGroupSendSettings, CatalogContext{
		ProductMode:                   "normal",
		BotCapabilitySettingsReadOnly: true,
	})
	for _, command := range []string{"/mode", "/codexprofile", "/opencodeprofile", "/model", "/reasoning", "/access", "/plan"} {
		if catalogContainsCommand(page, command) {
			t.Fatalf("group send settings menu should hide %q: %#v", command, page.Sections)
		}
	}
	for _, command := range []string{"/verbose"} {
		if !catalogContainsCommand(page, command) {
			t.Fatalf("group send settings menu should keep %q: %#v", command, page.Sections)
		}
	}
}

func TestPrimaryCommandHiddenFromSingleChatMenu(t *testing.T) {
	page := BuildFeishuCommandMenuGroupPageViewForContext(FeishuCommandGroupCommonTools, CatalogContext{
		ProductMode:      "normal",
		SurfaceScopeKind: string(CatalogSurfaceScopeKindUser),
	})
	if catalogContainsCommand(page, "/primary") {
		t.Fatalf("single-chat tools menu should hide /primary: %#v", page.Sections)
	}
}

func TestPrimaryCommandProjectsGroupMenuForNoPrimaryBot(t *testing.T) {
	page := BuildFeishuCommandMenuGroupPageViewForContext(FeishuCommandGroupCommonTools, CatalogContext{
		ProductMode:      "normal",
		SurfaceScopeKind: string(CatalogSurfaceScopeKindChat),
		PrimaryBotState:  string(CatalogPrimaryBotStateNone),
	})
	entry := commandEntryForCommand(t, page, "/primary")
	if got, want := entry.Title, "群主机器人"; got != want {
		t.Fatalf("primary entry title = %q, want %q", got, want)
	}
	if got, want := buttonCommandTexts(entry), []string{"/primary on", "/primary status"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("no-primary buttons = %#v, want %#v; entry=%#v", got, want, entry)
	}
	if got, want := buttonLabels(entry), []string{"设为本群主机器人", "查看状态"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("no-primary button labels = %#v, want %#v; entry=%#v", got, want, entry)
	}
}

func TestPrimaryCommandProjectsGroupMenuForCurrentPrimaryBot(t *testing.T) {
	page := BuildFeishuCommandMenuGroupPageViewForContext(FeishuCommandGroupCommonTools, CatalogContext{
		ProductMode:      "normal",
		SurfaceScopeKind: string(CatalogSurfaceScopeKindChat),
		PrimaryBotState:  string(CatalogPrimaryBotStateCurrent),
	})
	entry := commandEntryForCommand(t, page, "/primary")
	if got, want := buttonCommandTexts(entry), []string{"/primary off", "/primary status", "/primary refresh"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("current-primary buttons = %#v, want %#v; entry=%#v", got, want, entry)
	}
	if got, want := buttonLabels(entry), []string{"取消主机器人", "查看状态", "刷新权限"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("current-primary button labels = %#v, want %#v; entry=%#v", got, want, entry)
	}
}

func TestPrimaryCommandProjectsGroupMenuForOtherPrimaryBot(t *testing.T) {
	page := BuildFeishuCommandMenuGroupPageViewForContext(FeishuCommandGroupCommonTools, CatalogContext{
		ProductMode:      "normal",
		SurfaceScopeKind: string(CatalogSurfaceScopeKindChat),
		PrimaryBotState:  string(CatalogPrimaryBotStateOther),
	})
	entry := commandEntryForCommand(t, page, "/primary")
	if got, want := buttonCommandTexts(entry), []string{"/primary on", "/primary status", "/primary refresh"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("other-primary buttons = %#v, want %#v; entry=%#v", got, want, entry)
	}
	if got, want := buttonLabels(entry), []string{"切换为当前机器人", "查看状态", "刷新权限"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("other-primary button labels = %#v, want %#v; entry=%#v", got, want, entry)
	}
	if !strings.Contains(entry.Description, "替换") {
		t.Fatalf("other-primary description should warn replacement, got %q", entry.Description)
	}
}

func TestResolveFeishuCommandDisplayProfileForContextUsesClaudeVisibleProfile(t *testing.T) {
	profile := ResolveFeishuCommandDisplayProfileForContext(CatalogContext{
		Backend:     agentproto.BackendClaude,
		ProductMode: "normal",
	})
	if got, want := profile.VisibleFamiliesForGroup(FeishuCommandGroupSwitchTarget), []string{
		FeishuCommandWorkspace,
		FeishuCommandWorkspaceList,
		FeishuCommandWorkspaceNew,
		FeishuCommandWorkspaceNewDir,
		FeishuCommandWorkspaceNewGit,
		FeishuCommandWorkspaceNewWorktree,
		FeishuCommandWorkspaceDetach,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected claude visible profile to align with workspace family, got %#v want %#v", got, want)
	}
	if profile.IncludesFamily(FeishuCommandList) {
		t.Fatalf("expected claude visible profile to hide list, got %#v", profile.VisibleFamiliesForGroup(FeishuCommandGroupSwitchTarget))
	}
	if profile.IncludesFamily(FeishuCommandUse) {
		t.Fatalf("expected claude visible profile to hide use, got %#v", profile.VisibleFamiliesForGroup(FeishuCommandGroupSwitchTarget))
	}
	if profile.IncludesFamily(FeishuCommandDetach) {
		t.Fatalf("expected claude visible profile to hide detach, got %#v", profile.VisibleFamiliesForGroup(FeishuCommandGroupSwitchTarget))
	}
	if profile.IncludesFamily(FeishuCommandModel) {
		t.Fatalf("expected claude visible profile to hide model, got %#v", profile.VisibleFamiliesForGroup(FeishuCommandGroupSendSettings))
	}
	if profile.IncludesFamily(FeishuCommandReview) {
		t.Fatalf("expected claude visible profile to hide review, got %#v", profile.VisibleFamiliesForGroup(FeishuCommandGroupCommonTools))
	}
}

func TestResolveFeishuCommandDisplayProfileForContextUsesOpenCodeProfile(t *testing.T) {
	ctx := CatalogContext{
		Backend:     agentproto.BackendOpenCode,
		ProductMode: "normal",
		MenuStage:   string(FeishuCommandMenuStageNormalWorking),
	}
	profile := ResolveFeishuCommandDisplayProfileForContext(ctx)
	if profile.VisibleMode != "opencode" {
		t.Fatalf("VisibleMode = %q, want opencode", profile.VisibleMode)
	}

	currentWork := ResolveFeishuCommandDisplayGroup(FeishuCommandGroupCurrentWork, true, ctx)
	if got, want := resolvedDisplayCommands(currentWork), []string{"/stop", "/new", "/status"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("opencode current_work menu commands = %#v, want %#v", got, want)
	}

	sendSettings := ResolveFeishuCommandDisplayGroup(FeishuCommandGroupSendSettings, false, ctx)
	for _, command := range []string{"/access", "/plan", "/verbose", "/opencodeprofile", "/mode"} {
		if !containsCommandSlash(sendSettings, command) {
			t.Fatalf("opencode send_settings should include %q, got %#v", command, resolvedDisplayCommands(sendSettings))
		}
	}
	for _, command := range []string{"/model", "/reasoning", "/codexprofile", "/claudeprofile"} {
		if containsCommandSlash(sendSettings, command) {
			t.Fatalf("opencode send_settings should hide %q, got %#v", command, resolvedDisplayCommands(sendSettings))
		}
	}

	for _, tt := range []struct {
		familyID string
		kind     FeishuCommandSupportKind
		notePart string
	}{
		{familyID: FeishuCommandNew, kind: FeishuCommandSupportApproximation, notePart: "OpenCode"},
		{familyID: FeishuCommandSteerAll, kind: FeishuCommandSupportReject, notePart: "OpenCode"},
		{familyID: FeishuCommandReasoning, kind: FeishuCommandSupportReject, notePart: "OpenCode"},
		{familyID: FeishuCommandAccess, kind: FeishuCommandSupportNative, notePart: ""},
		{familyID: FeishuCommandPlan, kind: FeishuCommandSupportNative, notePart: ""},
		{familyID: FeishuCommandModel, kind: FeishuCommandSupportReject, notePart: "OpenCode"},
		{familyID: FeishuCommandPatch, kind: FeishuCommandSupportReject, notePart: "OpenCode"},
		{familyID: FeishuCommandAutoWhip, kind: FeishuCommandSupportReject, notePart: "OpenCode"},
		{familyID: FeishuCommandAutoContinue, kind: FeishuCommandSupportReject, notePart: "OpenCode"},
		{familyID: FeishuCommandCodexProfile, kind: FeishuCommandSupportReject, notePart: "/mode codex"},
		{familyID: FeishuCommandClaudeProfile, kind: FeishuCommandSupportReject, notePart: "/mode claude"},
		{familyID: FeishuCommandOpenCodeProfile, kind: FeishuCommandSupportNative, notePart: ""},
	} {
		t.Run(tt.familyID, func(t *testing.T) {
			support, ok := ResolveFeishuCommandSupport(ctx, tt.familyID)
			if !ok {
				t.Fatalf("ResolveFeishuCommandSupport(%q) missing", tt.familyID)
			}
			if support.Kind != tt.kind || support.DispatchAllowed != (tt.kind != FeishuCommandSupportReject) {
				t.Fatalf("support for %q = %#v, want kind %q dispatch %v", tt.familyID, support, tt.kind, tt.kind != FeishuCommandSupportReject)
			}
			if !strings.Contains(support.Note, tt.notePart) {
				t.Fatalf("support note for %q = %q, want to contain %q", tt.familyID, support.Note, tt.notePart)
			}
		})
	}
}

func TestBuildFeishuCommandMenuHomePageUsesProfileAwareRootEntry(t *testing.T) {
	normal := BuildFeishuCommandMenuHomePageViewForContext(CatalogContext{ProductMode: "normal"})
	if got := commandTextForMenuHomeEntry(normal, "工作区与会话"); got != "/workspace" {
		t.Fatalf("normal switch_target home command = %q, want /workspace", got)
	}
	if got := commandTextForMenuHomeEntry(normal, "系统管理"); got != "/admin" {
		t.Fatalf("normal maintenance home command = %q, want /admin", got)
	}

	vscode := BuildFeishuCommandMenuHomePageViewForContext(CatalogContext{ProductMode: "vscode"})
	if got := commandTextForMenuHomeEntry(vscode, "工作区与会话"); got != "/menu switch_target" {
		t.Fatalf("vscode switch_target home command = %q, want /menu switch_target", got)
	}

	claude := BuildFeishuCommandMenuHomePageViewForContext(CatalogContext{Backend: agentproto.BackendClaude, ProductMode: "normal"})
	if got := commandTextForMenuHomeEntry(claude, "工作区与会话"); got != "/workspace" {
		t.Fatalf("claude switch_target home command = %q, want /workspace", got)
	}
}

func containsCommandSlash(values []FeishuCommandDisplayResolution, command string) bool {
	for _, value := range values {
		if strings.TrimSpace(value.Definition.CanonicalSlash) == command {
			return true
		}
	}
	return false
}

func resolvedDisplayCommands(values []FeishuCommandDisplayResolution) []string {
	commands := make([]string, 0, len(values))
	for _, value := range values {
		if command := strings.TrimSpace(value.Definition.CanonicalSlash); command != "" {
			commands = append(commands, command)
		}
	}
	return commands
}

func commandTextForMenuHomeEntry(page FeishuPageView, title string) string {
	for _, section := range page.Sections {
		for _, entry := range section.Entries {
			if strings.TrimSpace(entry.Title) != title {
				continue
			}
			if len(entry.Buttons) == 0 {
				return ""
			}
			return strings.TrimSpace(entry.Buttons[0].CommandText)
		}
	}
	return ""
}

func commandEntryForCommand(t *testing.T, page FeishuPageView, command string) CommandCatalogEntry {
	t.Helper()
	for _, section := range page.Sections {
		for _, entry := range section.Entries {
			for _, current := range entry.Commands {
				if strings.TrimSpace(current) == command {
					return entry
				}
			}
		}
	}
	t.Fatalf("command %q not found in page: %#v", command, page.Sections)
	return CommandCatalogEntry{}
}

func buttonCommandTexts(entry CommandCatalogEntry) []string {
	out := make([]string, 0, len(entry.Buttons))
	for _, button := range entry.Buttons {
		out = append(out, strings.TrimSpace(button.CommandText))
	}
	return out
}

func buttonLabels(entry CommandCatalogEntry) []string {
	out := make([]string, 0, len(entry.Buttons))
	for _, button := range entry.Buttons {
		out = append(out, strings.TrimSpace(button.Label))
	}
	return out
}
