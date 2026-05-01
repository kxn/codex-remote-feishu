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

func TestResolveFeishuCommandDisplayGroupAppliesClaudeStrategyProjection(t *testing.T) {
	currentWork := ResolveFeishuCommandDisplayGroup(FeishuCommandGroupCurrentWork, true, CatalogContext{
		Backend:     agentproto.BackendClaude,
		ProductMode: "normal",
		MenuStage:   string(FeishuCommandMenuStageNormalWorking),
	})
	if got, want := resolvedDisplayCommands(currentWork), []string{"/stop", "/new", "/status", "/detach"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("claude current_work menu commands = %#v, want %#v", got, want)
	}

	switchTarget := ResolveFeishuCommandDisplayGroup(FeishuCommandGroupSwitchTarget, false, CatalogContext{
		Backend:     agentproto.BackendClaude,
		ProductMode: "normal",
	})
	if got, want := resolvedDisplayCommands(switchTarget), []string{"/list", "/use"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("claude switch_target help commands = %#v, want %#v", got, want)
	}

	sendSettings := ResolveFeishuCommandDisplayGroup(FeishuCommandGroupSendSettings, false, CatalogContext{
		Backend:     agentproto.BackendClaude,
		ProductMode: "normal",
	})
	if got, want := resolvedDisplayCommands(sendSettings), []string{"/reasoning", "/model", "/access", "/verbose", "/claudeprofile"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("claude send_settings help commands = %#v, want %#v", got, want)
	}

	commonTools := ResolveFeishuCommandDisplayGroup(FeishuCommandGroupCommonTools, false, CatalogContext{
		Backend:     agentproto.BackendClaude,
		ProductMode: "normal",
	})
	if got, want := resolvedDisplayCommands(commonTools), []string{"/history"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("claude common_tools help commands = %#v, want %#v", got, want)
	}

	maintenance := ResolveFeishuCommandDisplayGroup(FeishuCommandGroupMaintenance, false, CatalogContext{
		Backend:     agentproto.BackendClaude,
		ProductMode: "normal",
	})
	if got, want := resolvedDisplayCommands(maintenance), []string{"/mode", "/upgrade", "/debug", "/help", "/menu"}; !reflect.DeepEqual(got, want) {
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
	codex := ResolveFeishuCommandDisplayProfile("codex")
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
		FeishuCommandHistory,
		FeishuCommandCron,
		FeishuCommandSendFile,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("codex visible common_tools families = %#v, want %#v", got, want)
	}

	vscode := ResolveFeishuCommandDisplayProfile("vscode")
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
	if !codex.IncludesFamily(FeishuCommandCodexProvider) {
		t.Fatal("expected codex profile to include codex provider")
	}
	if vscode.IncludesFamily(FeishuCommandCodexProvider) {
		t.Fatal("expected vscode profile to hide codex provider")
	}
}

func TestResolveFeishuCommandDisplayProfileForContextUsesClaudeVisibleProfile(t *testing.T) {
	profile := ResolveFeishuCommandDisplayProfileForContext(CatalogContext{
		Backend:     agentproto.BackendClaude,
		ProductMode: "normal",
	})
	if !profile.IncludesFamily(FeishuCommandList) || !profile.IncludesFamily(FeishuCommandUse) || !profile.IncludesFamily(FeishuCommandDetach) {
		t.Fatalf("expected claude visible profile to include list/use/detach, got %#v", profile.VisibleFamiliesForGroup(FeishuCommandGroupSwitchTarget))
	}
}

func TestBuildFeishuCommandMenuHomePageUsesProfileAwareRootEntry(t *testing.T) {
	normal := BuildFeishuCommandMenuHomePageViewForContext(CatalogContext{ProductMode: "normal"})
	if got := commandTextForMenuHomeEntry(normal, "工作区与会话"); got != "/workspace" {
		t.Fatalf("normal switch_target home command = %q, want /workspace", got)
	}

	vscode := BuildFeishuCommandMenuHomePageViewForContext(CatalogContext{ProductMode: "vscode"})
	if got := commandTextForMenuHomeEntry(vscode, "工作区与会话"); got != "/menu switch_target" {
		t.Fatalf("vscode switch_target home command = %q, want /menu switch_target", got)
	}
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
