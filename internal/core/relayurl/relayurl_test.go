package relayurl

import "testing"

func TestNormalizeAgentURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "empty path",
			raw:  "ws://127.0.0.1:9100",
			want: "ws://127.0.0.1:9100/ws/agent",
		},
		{
			name: "root path",
			raw:  "ws://127.0.0.1:9100/",
			want: "ws://127.0.0.1:9100/ws/agent",
		},
		{
			name: "existing agent path",
			raw:  "ws://127.0.0.1:9100/ws/agent",
			want: "ws://127.0.0.1:9100/ws/agent",
		},
		{
			name: "non-root path preserved",
			raw:  "ws://127.0.0.1:9100/custom/path",
			want: "ws://127.0.0.1:9100/custom/path",
		},
		{
			name: "query kept while filling agent path",
			raw:  "ws://127.0.0.1:9100?token=abc",
			want: "ws://127.0.0.1:9100/ws/agent?token=abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeAgentURL(tt.raw); got != tt.want {
				t.Fatalf("NormalizeAgentURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
