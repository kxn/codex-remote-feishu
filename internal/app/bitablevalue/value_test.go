package bitablevalue

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestStringExtractsBitableScalarAndListShapes(t *testing.T) {
	value := []any{
		map[string]any{"text": " Alpha "},
		map[string]any{"name": "Beta"},
		"",
	}
	if got := String(value); got != "Alpha\nBeta" {
		t.Fatalf("String(...) = %q, want joined non-empty text", got)
	}

	nested := map[string]any{
		"value": "",
		"values": []any{
			map[string]any{"record_id": "rec-1"},
			map[string]any{"label": "Label"},
		},
	}
	if got := String(nested); got != "rec-1\nLabel" {
		t.Fatalf("String(nested) = %q, want nested values fallback", got)
	}
}

func TestBoolHandlesBitableScalarAndNestedShapes(t *testing.T) {
	tests := []struct {
		name      string
		value     any
		want      bool
		wantValid bool
	}{
		{name: "nil is false valid", value: nil, want: false, wantValid: true},
		{name: "checked text", value: "checked", want: true, wantValid: true},
		{name: "localized disabled", value: "停用", want: false, wantValid: true},
		{name: "json number one", value: json.Number("1"), want: true, wantValid: true},
		{name: "nested value", value: map[string]any{"checked": "启用"}, want: true, wantValid: true},
		{name: "multi list invalid", value: []any{"true", "false"}, want: false, wantValid: false},
		{name: "unknown text invalid", value: "maybe", want: false, wantValid: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, valid := Bool(tt.value)
			if got != tt.want || valid != tt.wantValid {
				t.Fatalf("Bool(%#v) = (%v, %v), want (%v, %v)", tt.value, got, valid, tt.want, tt.wantValid)
			}
		})
	}
}

func TestStringSliceFlattensBitableLinks(t *testing.T) {
	value := []any{
		map[string]any{"record_ids": []any{"rec-1", "rec-2"}},
		map[string]any{"id": "rec-3"},
		" ",
	}
	want := []string{"rec-1", "rec-2", "rec-3"}
	if got := StringSlice(value); !reflect.DeepEqual(got, want) {
		t.Fatalf("StringSlice(...) = %#v, want %#v", got, want)
	}
}

func TestIntHandlesBitableNumericShapes(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  int
	}{
		{name: "int", value: 3, want: 3},
		{name: "float", value: 4.8, want: 4},
		{name: "json number", value: json.Number("5"), want: 5},
		{name: "nested number", value: map[string]any{"number": "6"}, want: 6},
		{name: "invalid string", value: "bad", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Int(tt.value); got != tt.want {
				t.Fatalf("Int(%#v) = %d, want %d", tt.value, got, tt.want)
			}
		})
	}
}
