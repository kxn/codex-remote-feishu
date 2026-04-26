package jsonrpcutil

import "testing"

func TestExtractErrorMessage(t *testing.T) {
	tests := []struct {
		name    string
		message map[string]any
		want    string
	}{
		{
			name: "prefer nested error message",
			message: map[string]any{
				"error": map[string]any{
					"message": " nested failure ",
				},
			},
			want: "nested failure",
		},
		{
			name: "fallback to top-level string error",
			message: map[string]any{
				"error": " top-level failure ",
			},
			want: "top-level failure",
		},
		{
			name: "ignore non-string top-level error",
			message: map[string]any{
				"error": map[string]any{
					"code": 123,
				},
			},
			want: "",
		},
		{
			name: "nil message",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractErrorMessage(tt.message); got != tt.want {
				t.Fatalf("ExtractErrorMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}
