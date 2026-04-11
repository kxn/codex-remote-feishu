package control

import "testing"

func TestSupportsInlineCardReplacement(t *testing.T) {
	tests := []struct {
		name   string
		action Action
		want   bool
	}{
		{
			name:   "menu navigation",
			action: Action{Kind: ActionShowCommandMenu, Text: "/menu send_settings"},
			want:   true,
		},
		{
			name:   "bare mode",
			action: Action{Kind: ActionModeCommand, Text: "/mode"},
			want:   true,
		},
		{
			name:   "parameter apply",
			action: Action{Kind: ActionModeCommand, Text: "/mode vscode"},
			want:   false,
		},
		{
			name:   "scoped thread expansion",
			action: Action{Kind: ActionShowScopedThreads},
			want:   true,
		},
		{
			name:   "workspace thread expansion",
			action: Action{Kind: ActionShowWorkspaceThreads},
			want:   true,
		},
		{
			name:   "workspace list expand",
			action: Action{Kind: ActionShowAllWorkspaces},
			want:   true,
		},
		{
			name:   "workspace list collapse",
			action: Action{Kind: ActionShowRecentWorkspaces},
			want:   true,
		},
		{
			name:   "thread workspace expand",
			action: Action{Kind: ActionShowAllThreadWorkspaces},
			want:   true,
		},
		{
			name:   "thread workspace collapse",
			action: Action{Kind: ActionShowRecentThreadWorkspaces},
			want:   true,
		},
		{
			name:   "thread return action",
			action: Action{Kind: ActionShowAllThreads},
			want:   true,
		},
		{
			name:   "thread attach action",
			action: Action{Kind: ActionUseThread},
			want:   false,
		},
		{
			name:   "workspace attach action",
			action: Action{Kind: ActionAttachWorkspace},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SupportsInlineCardReplacement(tt.action); got != tt.want {
				t.Fatalf("SupportsInlineCardReplacement(%#v) = %v, want %v", tt.action, got, tt.want)
			}
		})
	}
}
