package control

import "testing"

func TestParseFeishuTextActionRecognizesCompactCommand(t *testing.T) {
	action, ok := ParseFeishuTextAction("/compact")
	if !ok {
		t.Fatal("expected /compact to be parsed")
	}
	if action.Kind != ActionCompact {
		t.Fatalf("action kind = %q, want %q", action.Kind, ActionCompact)
	}
}

func TestParseFeishuMenuActionRecognizesCompactCommand(t *testing.T) {
	action, ok := ParseFeishuMenuAction("compact")
	if !ok {
		t.Fatal("expected compact menu key to be parsed")
	}
	if action.Kind != ActionCompact {
		t.Fatalf("action kind = %q, want %q", action.Kind, ActionCompact)
	}
}

func TestFeishuCommandCatalogsIncludeCompact(t *testing.T) {
	for _, catalog := range []FeishuPageView{FeishuCommandHelpPageView(), FeishuCommandMenuPageView()} {
		found := false
		for _, section := range catalog.Sections {
			for _, entry := range section.Entries {
				for _, command := range entry.Commands {
					if command == "/compact" {
						found = true
						break
					}
				}
			}
		}
		if !found {
			t.Fatalf("catalog %#v does not include /compact", catalog.Title)
		}
	}
}
