package wrapper

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestWrapperBackendFromArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    agentproto.Backend
		wantErr string
	}{
		{
			name: "codex app server",
			args: []string{"app-server", "--analytics-default-enabled"},
			want: agentproto.BackendCodex,
		},
		{
			name: "codex root config before app server",
			args: []string{"-c", "features.code_mode_host=true", "app-server", "--analytics-default-enabled"},
			want: agentproto.BackendCodex,
		},
		{
			name: "codex root cd before app server",
			args: []string{"-C", "/tmp/work", "app-server", "--analytics-default-enabled"},
			want: agentproto.BackendCodex,
		},
		{
			name: "claude app server",
			args: []string{"--cd=/tmp/work", "claude-app-server", "--verbose"},
			want: agentproto.BackendClaude,
		},
		{
			name:    "empty args",
			args:    nil,
			wantErr: "requires app-server",
		},
		{
			name:    "unknown command",
			args:    []string{"resume", "--thread", "abc"},
			wantErr: "only supports app-server",
		},
		{
			name:    "codex root option before daemon",
			args:    []string{"-c", "features.code_mode_host=true", "daemon"},
			wantErr: "only supports app-server",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := wrapperBackendFromArgs(tt.args)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("wrapperBackendFromArgs(%#v) error = %v, want substring %q", tt.args, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("wrapperBackendFromArgs(%#v): %v", tt.args, err)
			}
			if got != tt.want {
				t.Fatalf("wrapperBackendFromArgs(%#v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}
