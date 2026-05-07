package daemon

import (
	"strings"
	"testing"
)

func TestParseAdminCommandTextRecognizesSupportedModes(t *testing.T) {
	tests := []struct {
		text string
		want adminCommandMode
	}{
		{text: "/admin web", want: adminCommandWeb},
		{text: "/admin localweb", want: adminCommandLocalWeb},
		{text: "/admin autostart", want: adminCommandAutostart},
		{text: "/admin autostart on", want: adminCommandAutostartOn},
		{text: "/admin autostart off", want: adminCommandAutostartOff},
	}
	for _, tt := range tests {
		parsed, err := parseAdminCommandText(tt.text)
		if err != nil {
			t.Fatalf("parseAdminCommandText(%q): %v", tt.text, err)
		}
		if parsed.Mode != tt.want {
			t.Fatalf("parseAdminCommandText(%q) mode = %q, want %q", tt.text, parsed.Mode, tt.want)
		}
	}
}

func TestParseAdminCommandTextRejectsUnsupportedForms(t *testing.T) {
	tests := []string{
		"",
		"/admin",
		"/admin nope",
		"/admin autostart maybe",
		"/debug admin",
	}
	for _, input := range tests {
		_, err := parseAdminCommandText(input)
		if err == nil {
			t.Fatalf("expected %q to be rejected", input)
		}
		if !strings.Contains(err.Error(), "/admin") {
			t.Fatalf("expected %q rejection to include usage guidance, got %v", input, err)
		}
	}
}
