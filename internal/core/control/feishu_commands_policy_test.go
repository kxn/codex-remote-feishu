package control

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/buildinfo"
)

func withBuildFlavorForControlTest(t *testing.T, flavor buildinfo.Flavor) {
	t.Helper()
	previous := buildinfo.FlavorValue
	buildinfo.FlavorValue = string(flavor)
	t.Cleanup(func() {
		buildinfo.FlavorValue = previous
	})
}

func TestUpgradeDefinitionRespectsShippingPolicy(t *testing.T) {
	withBuildFlavorForControlTest(t, buildinfo.FlavorShipping)

	def, ok := FeishuCommandDefinitionByID(FeishuCommandUpgrade)
	if !ok {
		t.Fatal("expected upgrade definition")
	}
	if strings.Contains(strings.Join(def.Examples, " "), "/upgrade local") {
		t.Fatalf("shipping upgrade examples should hide local upgrade: %#v", def.Examples)
	}
	if strings.Contains(strings.Join(def.Examples, " "), "/upgrade track alpha") {
		t.Fatalf("shipping upgrade examples should hide alpha track: %#v", def.Examples)
	}
	if strings.Contains(def.ArgumentFormNote, "local") {
		t.Fatalf("shipping upgrade form note should hide local upgrade: %q", def.ArgumentFormNote)
	}
	for _, option := range def.Options {
		if option.CommandText == "/upgrade local" || option.CommandText == "/upgrade track alpha" {
			t.Fatalf("shipping upgrade options should hide restricted commands: %#v", def.Options)
		}
	}
}

func TestHelpCatalogReflectsShippingUpgradePolicy(t *testing.T) {
	withBuildFlavorForControlTest(t, buildinfo.FlavorShipping)

	catalog := FeishuCommandHelpPageView()
	for _, section := range catalog.Sections {
		for _, entry := range section.Entries {
			if entry.Title != "升级" {
				continue
			}
			if strings.Contains(strings.Join(entry.Examples, " "), "/upgrade local") {
				t.Fatalf("shipping help catalog should hide local upgrade example: %#v", entry)
			}
			if strings.Contains(strings.Join(entry.Examples, " "), "/upgrade track alpha") {
				t.Fatalf("shipping help catalog should hide alpha track example: %#v", entry)
			}
			return
		}
	}
	t.Fatal("expected upgrade entry in help catalog")
}

func TestVSCodeMigrateDisplayRespectsProductMode(t *testing.T) {
	def, ok := FeishuCommandDefinitionByID(FeishuCommandVSCodeMigrate)
	if !ok {
		t.Fatal("expected vscode migrate definition")
	}

	if _, ok := FeishuCommandDefinitionForDisplay(def, "normal", false, ""); ok {
		t.Fatalf("expected /vscode-migrate to stay hidden from normal help")
	}
	if _, ok := FeishuCommandDefinitionForDisplay(def, "normal", true, string(FeishuCommandMenuStageNormalWorking)); ok {
		t.Fatalf("expected /vscode-migrate to stay hidden from normal menu")
	}
	if projected, ok := FeishuCommandDefinitionForDisplay(def, "vscode", false, ""); !ok {
		t.Fatalf("expected /vscode-migrate to stay visible in vscode help")
	} else if projected.CanonicalSlash != "/vscode-migrate" {
		t.Fatalf("unexpected vscode migrate display projection: %#v", projected)
	}

	normalHelp := BuildFeishuCommandDisplayPageView("Slash 命令帮助", "", false, "normal", "")
	if catalogContainsCommand(normalHelp, "/vscode-migrate") {
		t.Fatalf("expected normal help catalog to hide /vscode-migrate: %#v", normalHelp)
	}

	vscodeHelp := BuildFeishuCommandDisplayPageView("Slash 命令帮助", "", false, "vscode", "")
	if !catalogContainsCommand(vscodeHelp, "/vscode-migrate") {
		t.Fatalf("expected vscode help catalog to include /vscode-migrate: %#v", vscodeHelp)
	}

	normalMenu := BuildFeishuCommandMenuGroupPageView(FeishuCommandGroupMaintenance, "normal", string(FeishuCommandMenuStageNormalWorking))
	if catalogContainsCommand(normalMenu, "/vscode-migrate") {
		t.Fatalf("expected normal maintenance menu to hide /vscode-migrate: %#v", normalMenu)
	}
}

func catalogContainsCommand(catalog FeishuCommandPageView, command string) bool {
	for _, section := range catalog.Sections {
		for _, entry := range section.Entries {
			for _, current := range entry.Commands {
				if current == command {
					return true
				}
			}
		}
	}
	return false
}
