package cardtheme

import "testing"

func TestTemplateUsesSemanticColors(t *testing.T) {
	tests := map[string]string{
		"info":        "grey",
		"progress":    "wathet",
		"plan":        "blue",
		"final":       "blue",
		"success":     "green",
		"approval":    "green",
		"error":       "red",
		"relay-error": "red",
		"thread-1":    "grey",
	}
	for input, want := range tests {
		if got := Template(input, ""); got != want {
			t.Fatalf("Template(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestTemplateFallsBackWhenThemeKeyEmpty(t *testing.T) {
	if got := Template("", "progress"); got != "wathet" {
		t.Fatalf("Template should use fallback when themeKey empty, got %q", got)
	}
}
