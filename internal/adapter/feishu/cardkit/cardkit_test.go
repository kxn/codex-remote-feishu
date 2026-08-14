package cardkit

import (
	"reflect"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestPlainText(t *testing.T) {
	got := PlainText("  hello  ")
	want := map[string]any{"tag": "plain_text", "content": "hello"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PlainText = %v, want %v", got, want)
	}
}

func TestPlainTextBlockElement(t *testing.T) {
	if got := PlainTextBlockElement("  "); got != nil {
		t.Fatalf("empty content: got %v, want nil", got)
	}
	got := PlainTextBlockElement(" hi ")
	want := map[string]any{
		"tag": "div",
		"text": map[string]any{
			"tag":     "plain_text",
			"content": "hi",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PlainTextBlockElement = %v, want %v", got, want)
	}
}

func TestButtonGroupElement(t *testing.T) {
	if got := ButtonGroupElement(nil); got != nil {
		t.Fatalf("empty buttons: got %v, want nil", got)
	}
	single := ButtonGroupElement([]map[string]any{{"tag": "button"}})
	if !reflect.DeepEqual(single, map[string]any{"tag": "button"}) {
		t.Fatalf("single button = %v, want the button itself", single)
	}
	// Empty entries are filtered out.
	single = ButtonGroupElement([]map[string]any{{}, {"tag": "button"}})
	if !reflect.DeepEqual(single, map[string]any{"tag": "button"}) {
		t.Fatalf("single button with empty entry = %v, want the button itself", single)
	}
	group := ButtonGroupElement([]map[string]any{{"tag": "button", "x": 1}, {"tag": "button", "y": 2}})
	if group["tag"] != "column_set" {
		t.Fatalf("many buttons tag = %v, want column_set", group["tag"])
	}
	columns, ok := group["columns"].([]map[string]any)
	if !ok || len(columns) != 2 {
		t.Fatalf("many buttons columns = %#v, want 2 columns", group["columns"])
	}
}

func TestAppendTextSections(t *testing.T) {
	sections := []control.FeishuCardTextSection{
		{Label: "Name", Lines: []string{"Alice", "Bob"}},
		{}, // skipped
	}
	got := AppendTextSections(nil, sections)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (label markdown + text block)", len(got))
	}
	if got[0]["tag"] != "markdown" || got[0]["content"] != "**Name**" {
		t.Fatalf("label element = %v", got[0])
	}
	if got[1]["tag"] != "div" {
		t.Fatalf("lines element = %v", got[1])
	}
}

func TestCloneMapDeepCopies(t *testing.T) {
	orig := map[string]any{
		"a": map[string]any{"b": "c"},
		"d": []map[string]any{{"e": "f"}},
		"g": []any{"h", map[string]any{"i": "j"}},
		"k": "l",
	}
	clone := CloneMap(orig)
	if !reflect.DeepEqual(clone, orig) {
		t.Fatalf("clone = %v, want %v", clone, orig)
	}
	// Mutating the clone must not affect the original.
	clone["a"].(map[string]any)["b"] = "changed"
	clone["d"].([]map[string]any)[0]["e"] = "changed"
	clone["g"].([]any)[1].(map[string]any)["i"] = "changed"
	if orig["a"].(map[string]any)["b"] != "c" {
		t.Fatal("nested map mutation leaked into original")
	}
	if orig["d"].([]map[string]any)[0]["e"] != "f" {
		t.Fatal("[]map mutation leaked into original")
	}
	if orig["g"].([]any)[1].(map[string]any)["i"] != "j" {
		t.Fatal("[]any mutation leaked into original")
	}
	if CloneMap(nil) != nil {
		t.Fatal("empty map: want nil")
	}
}

func TestStringValue(t *testing.T) {
	if got := StringValue("x"); got != "x" {
		t.Fatalf("StringValue(string) = %q", got)
	}
	if got := StringValue(42); got != "" {
		t.Fatalf("StringValue(int) = %q, want empty", got)
	}
}
