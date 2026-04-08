package control

import "testing"

func TestParseFeishuTextActionRecognizesDebugCommand(t *testing.T) {
	action, ok := ParseFeishuTextAction("/debug upgrade")
	if !ok {
		t.Fatal("expected /debug upgrade to be parsed")
	}
	if action.Kind != ActionDebugCommand {
		t.Fatalf("action kind = %q, want %q", action.Kind, ActionDebugCommand)
	}
	if action.Text != "/debug upgrade" {
		t.Fatalf("action text = %q, want %q", action.Text, "/debug upgrade")
	}
}

func TestFeishuCommandCatalogsHideKillInstanceFromVisibleEntries(t *testing.T) {
	cases := []struct {
		name    string
		catalog CommandCatalog
	}{
		{name: "help", catalog: FeishuCommandHelpCatalog()},
		{name: "menu", catalog: FeishuCommandMenuCatalog()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, section := range tc.catalog.Sections {
				for _, entry := range section.Entries {
					for _, command := range entry.Commands {
						if command == "/killinstance" {
							t.Fatalf("catalog still exposes /killinstance in commands: %#v", entry)
						}
					}
					for _, button := range entry.Buttons {
						if button.CommandText == "/killinstance" {
							t.Fatalf("catalog still exposes /killinstance in buttons: %#v", entry)
						}
					}
				}
			}
		})
	}
}
