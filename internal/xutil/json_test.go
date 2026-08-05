package xutil

import (
	"reflect"
	"testing"
)

func TestLookupStringFromAny(t *testing.T) {
	if got := LookupStringFromAny("abc"); got != "abc" {
		t.Errorf("string case = %q, want abc", got)
	}
	if got := LookupStringFromAny(42); got != "" {
		t.Errorf("int case = %q, want empty", got)
	}
	if got := LookupStringFromAny(nil); got != "" {
		t.Errorf("nil case = %q, want empty", got)
	}
}

func TestStringify(t *testing.T) {
	if got := Stringify("  x  "); got != "x" {
		t.Errorf("string = %q, want trimmed x", got)
	}
	if got := Stringify(123); got != "123" {
		t.Errorf("int = %q, want 123", got)
	}
	if got := Stringify(1.5); got != "1.5" {
		t.Errorf("float = %q, want 1.5", got)
	}
	if got := Stringify(true); got != "true" {
		t.Errorf("bool = %q, want true", got)
	}
	if got := Stringify(nil); got != "" {
		t.Errorf("nil = %q, want empty", got)
	}
	if got := Stringify(map[string]any{"a": 1}); got != "map[a:1]" {
		t.Errorf("map = %q, want map[a:1]", got)
	}
}

func TestLookupBoolFromAny(t *testing.T) {
	if got := LookupBoolFromAny(true); !got {
		t.Errorf("true case = %v, want true", got)
	}
	if got := LookupBoolFromAny(false); got {
		t.Errorf("false case = %v, want false", got)
	}
	if got := LookupBoolFromAny("x"); got {
		t.Errorf("string case = %v, want false", got)
	}
}

func TestLookupIntFromAny(t *testing.T) {
	cases := map[string]struct {
		in   any
		want int
	}{
		"int":     {int(7), 7},
		"int32":   {int32(8), 8},
		"int64":   {int64(9), 9},
		"float32": {float32(3.9), 3},
		"float64": {float64(4.9), 4},
		"string":  {"12", 0},
		"nil":     {nil, 0},
	}
	for name, tc := range cases {
		if got := LookupIntFromAny(tc.in); got != tc.want {
			t.Errorf("%s = %d, want %d", name, got, tc.want)
		}
	}
}

func TestCloneJSONValue(t *testing.T) {
	original := map[string]any{
		"name": "x",
		"nested": map[string]any{
			"list": []any{1, "two", []map[string]any{{"k": "v"}}},
		},
	}
	cloned := CloneJSONValue(original).(map[string]any)
	if !reflect.DeepEqual(cloned, original) {
		t.Fatalf("clone = %#v, want equal to original", cloned)
	}
	// Mutating the clone must not affect the original.
	cloned["name"] = "y"
	cloned["nested"].(map[string]any)["list"].([]any)[0] = 999
	if original["name"] != "x" {
		t.Errorf("original name mutated: %v", original["name"])
	}
	if original["nested"].(map[string]any)["list"].([]any)[0] != 1 {
		t.Errorf("original list mutated: %v", original["nested"].(map[string]any)["list"].([]any)[0])
	}

	if CloneJSONValue(nil) != nil {
		t.Errorf("nil case should stay nil")
	}
	if got := CloneJSONValue(42); got != 42 {
		t.Errorf("scalar case = %v, want 42", got)
	}

	// []map[string]any branch.
	slice := []map[string]any{{"a": 1}}
	clonedSlice := CloneJSONValue(slice).([]map[string]any)
	if !reflect.DeepEqual(clonedSlice, slice) {
		t.Fatalf("slice clone = %#v, want equal", clonedSlice)
	}
	clonedSlice[0]["a"] = 2
	if slice[0]["a"] != 1 {
		t.Errorf("slice original mutated: %v", slice[0]["a"])
	}
}

func TestCloneMap(t *testing.T) {
	original := map[string]any{"a": map[string]any{"b": []any{1, 2}}}
	cloned := CloneMap(original)
	if !reflect.DeepEqual(cloned, original) {
		t.Fatalf("clone = %#v, want equal", cloned)
	}
	cloned["a"].(map[string]any)["b"].([]any)[0] = 99
	if original["a"].(map[string]any)["b"].([]any)[0] != 1 {
		t.Errorf("original mutated via clone")
	}
	if got := CloneMap(nil); got == nil || len(got) != 0 {
		t.Errorf("nil input = %#v, want empty non-nil map", got)
	}
	if got := CloneMap(map[string]any{}); got == nil || len(got) != 0 {
		t.Errorf("empty input = %#v, want empty non-nil map", got)
	}
}

func TestMapsFromAny(t *testing.T) {
	// []map[string]any input: deep clone.
	typed := []map[string]any{{"a": []any{1}}}
	got := MapsFromAny(typed)
	if !reflect.DeepEqual(got, typed) {
		t.Fatalf("typed = %#v, want equal", got)
	}
	got[0]["a"].([]any)[0] = 2
	if typed[0]["a"].([]any)[0] != 1 {
		t.Errorf("typed original mutated")
	}

	// []any input: nil items skipped, others deep cloned.
	anySlice := []any{
		map[string]any{"x": 1},
		nil,
		map[string]any{"y": []any{2}},
	}
	got2 := MapsFromAny(anySlice)
	if len(got2) != 2 {
		t.Fatalf("any slice len = %d, want 2", len(got2))
	}
	if got2[0]["x"] != 1 || got2[1]["y"].([]any)[0] != 2 {
		t.Errorf("any slice content = %#v", got2)
	}
	got2[0]["x"] = 99
	if anySlice[0].(map[string]any)["x"] != 1 {
		t.Errorf("any slice original mutated")
	}

	if MapsFromAny("nope") != nil {
		t.Errorf("string input should yield nil")
	}
}

func TestCompactJSON(t *testing.T) {
	if got := CompactJSON(nil); got != "" {
		t.Errorf("nil = %q, want empty", got)
	}
	if got := CompactJSON(map[string]any{"a": 1}); got != `{"a":1}` {
		t.Errorf("map = %q, want {\"a\":1}", got)
	}
	// Marshal failure falls back to fmt.Sprintf.
	if got := CompactJSON(map[string]any{"f": func() {}}); got == "" {
		t.Errorf("marshal-fail case should fall back to %v formatting", got)
	}
}
