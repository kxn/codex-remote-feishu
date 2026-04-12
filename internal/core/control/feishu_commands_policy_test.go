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

	catalog := FeishuCommandHelpCatalog()
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
