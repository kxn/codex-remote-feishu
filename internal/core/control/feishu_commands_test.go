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
