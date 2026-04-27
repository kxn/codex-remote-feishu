package control

import (
	"reflect"
	"strings"
	"testing"
)

func TestResolveFeishuCommandDisplayFamilyCarriesDefaultVariantIdentity(t *testing.T) {
	resolved, ok := ResolveFeishuCommandDisplayFamily(FeishuCommandMode, false, CatalogContext{})
	if !ok {
		t.Fatal("expected mode family to resolve")
	}
	if resolved.FamilyID != FeishuCommandMode {
		t.Fatalf("FamilyID = %q, want %q", resolved.FamilyID, FeishuCommandMode)
	}
	if resolved.VariantID != defaultFeishuCommandDisplayVariantID(FeishuCommandMode) {
		t.Fatalf("VariantID = %q, want %q", resolved.VariantID, defaultFeishuCommandDisplayVariantID(FeishuCommandMode))
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

func TestResolveFeishuCommandDisplayProfileTracksModeSpecificFamilies(t *testing.T) {
	normal := ResolveFeishuCommandDisplayProfile("normal")
	if got, want := normal.VisibleFamiliesForGroup(FeishuCommandGroupSwitchTarget), []string{
		FeishuCommandWorkspace,
		FeishuCommandWorkspaceList,
		FeishuCommandWorkspaceNew,
		FeishuCommandWorkspaceNewDir,
		FeishuCommandWorkspaceNewGit,
		FeishuCommandWorkspaceNewWorktree,
		FeishuCommandWorkspaceDetach,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("normal visible switch_target families = %#v, want %#v", got, want)
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
	if normal.IncludesFamily(FeishuCommandVSCodeMigrate) {
		t.Fatal("expected normal profile to hide vscode migrate")
	}
}

func TestBuildFeishuCommandMenuHomePageUsesProfileAwareRootEntry(t *testing.T) {
	normal := BuildFeishuCommandMenuHomePageViewForContext(CatalogContext{ProductMode: "normal"})
	if got := commandTextForMenuHomeEntry(normal, "工作会话"); got != "/workspace" {
		t.Fatalf("normal switch_target home command = %q, want /workspace", got)
	}

	vscode := BuildFeishuCommandMenuHomePageViewForContext(CatalogContext{ProductMode: "vscode"})
	if got := commandTextForMenuHomeEntry(vscode, "工作会话"); got != "/menu switch_target" {
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
