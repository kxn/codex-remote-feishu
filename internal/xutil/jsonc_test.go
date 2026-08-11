package xutil

import (
	"encoding/json"
	"testing"
)

func TestStripJSONComments(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "line comment",
			in:   `{"a": 1 // comment` + "\n" + `}`,
			want: `{"a": 1 ` + "\n" + `}`,
		},
		{
			name: "block comment",
			in:   `{"a": 1 /* c */}`,
			want: `{"a": 1 }`,
		},
		{
			name: "multi-line block comment keeps newlines",
			in:   "{\n/* line1\nline2 */\n\"a\": 1\n}\n",
			want: "{\n\n\n\"a\": 1\n}\n",
		},
		{
			name: "comment markers inside string preserved",
			in:   `{"url": "https://example.com/a/*b*/c", "x": 1}`,
			want: `{"url": "https://example.com/a/*b*/c", "x": 1}`,
		},
		{
			name: "escaped quote inside string",
			in:   `{"a": "x\" // not a comment", "b": 1}`,
			want: `{"a": "x\" // not a comment", "b": 1}`,
		},
		{
			name: "escaped backslash then quote ends string",
			in:   `{"a": "\\" // comment` + "\n" + `}`,
			want: `{"a": "\\" ` + "\n" + `}`,
		},
		{
			name: "comment in array",
			in:   `[1, /* c */ 2]`,
			want: `[1,  2]`,
		},
		{
			name: "empty input",
			in:   ``,
			want: ``,
		},
		{
			name: "plain json unchanged",
			in:   `{"a": 1}`,
			want: `{"a": 1}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(StripJSONComments([]byte(tt.in)))
			if got != tt.want {
				t.Fatalf("StripJSONComments(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestStripJSONTrailingCommas(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "object",
			in:   `{"a": 1,}`,
			want: `{"a": 1}`,
		},
		{
			name: "array",
			in:   `[1, 2,]`,
			want: `[1, 2]`,
		},
		{
			name: "nested",
			in:   `{"a": {"b": 2,}, "c": [1,],}`,
			want: `{"a": {"b": 2}, "c": [1]}`,
		},
		{
			name: "comma inside string preserved",
			in:   `{"a": "x,}", "b": 1}`,
			want: `{"a": "x,}", "b": 1}`,
		},
		{
			name: "whitespace before closing",
			in:   "{\n  \"a\": 1,\n}\n",
			want: "{\n  \"a\": 1\n}\n",
		},
		{
			name: "empty input",
			in:   ``,
			want: ``,
		},
		{
			name: "plain json unchanged",
			in:   `{"a": 1}`,
			want: `{"a": 1}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(StripJSONTrailingCommas([]byte(tt.in)))
			if got != tt.want {
				t.Fatalf("StripJSONTrailingCommas(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeJSONCProducesValidJSON(t *testing.T) {
	tests := []string{
		"{\n  // comment\n  \"a\": 1,\n}\n",
		"{\n/* block\ncomment */\n\"a\": [1, 2,],\n}\n",
		`{"url": "https://example.com", "note": "a/*b*/c",}`,
	}
	for _, input := range tests {
		normalized := NormalizeJSONC([]byte(input))
		var value any
		if err := json.Unmarshal(normalized, &value); err != nil {
			t.Fatalf("NormalizeJSONC(%q) produced invalid JSON %q: %v", input, string(normalized), err)
		}
	}
}
