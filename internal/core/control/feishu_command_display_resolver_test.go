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

func resolvedDisplayCommands(values []FeishuCommandDisplayResolution) []string {
	commands := make([]string, 0, len(values))
	for _, value := range values {
		if command := strings.TrimSpace(value.Definition.CanonicalSlash); command != "" {
			commands = append(commands, command)
		}
	}
	return commands
}
