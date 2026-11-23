package codex

import (
	"strings"
	"testing"
)

func TestPredictionDesktopJSONContract(t *testing.T) {
	for _, tc := range []struct{ name, raw, want string }{
		{"null suggestion", `{"suggestion":null}`, ""},
		{"extra fields stripped", `{"suggestion":" next ","other":true}`, "next"},
		{"JSON parse duplicate key", `{"suggestion":"first","suggestion":"last"}`, "last"},
		{"no desktop length cap", `{"suggestion":"` + strings.Repeat("x", 250) + `"}`, strings.Repeat("x", 250)},
		{"ECMAScript trims BOM", `{"suggestion":"\ufeff next \ufeff"}`, "next"},
		{"ECMAScript retains next line", `{"suggestion":"\u0085next\u0085"}`, "\u0085next\u0085"},
		{"BOM only is empty", `{"suggestion":"\ufeff"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parsePredictionSuggestion(tc.raw)
			if err != nil || got != tc.want {
				t.Fatalf("desktop schema parity: got=%q err=%v want=%q", got, err, tc.want)
			}
		})
	}
}

func TestPredictionRequiresOfficialConfiguration(t *testing.T) {
	tr := NewTranslator("instance")
	frames, err := tr.TranslateCommand(predictionCommand("prediction:without-config"))
	if err == nil || len(frames) != 0 {
		t.Fatalf("unconfigured sampling must not substitute a local prompt: frames=%s err=%v", frames, err)
	}
}
