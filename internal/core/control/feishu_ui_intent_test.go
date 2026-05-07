package control

import (
	"reflect"
	"testing"
)

func TestFeishuUIIntentFromAction(t *testing.T) {
	tests := []struct {
		name   string
		action Action
		want   *FeishuUIIntent
	}{
		{
			name:   "bare admin",
			action: Action{Kind: ActionAdminRoot, Text: "/admin"},
			want:   &FeishuUIIntent{Kind: FeishuUIIntentShowAdminRoot, RawText: "/admin"},
		},
		{
			name:   "menu navigation",
			action: Action{Kind: ActionShowCommandMenu, Text: "/menu send_settings"},
			want:   &FeishuUIIntent{Kind: FeishuUIIntentShowCommandMenu, RawText: "/menu send_settings"},
		},
		{
			name:   "bare mode",
			action: Action{Kind: ActionModeCommand, Text: "/mode"},
			want:   &FeishuUIIntent{Kind: FeishuUIIntentShowModeCatalog, RawText: "/mode"},
		},
		{
			name:   "bare claude profile",
			action: Action{Kind: ActionClaudeProfileCommand, Text: "/claudeprofile"},
			want:   &FeishuUIIntent{Kind: FeishuUIIntentShowClaudeProfileCatalog, RawText: "/claudeprofile"},
		},
		{
			name:   "bare codex provider",
			action: Action{Kind: ActionCodexProviderCommand, Text: "/codexprovider"},
			want:   &FeishuUIIntent{Kind: FeishuUIIntentShowCodexProviderCatalog, RawText: "/codexprovider"},
		},
		{
			name:   "workspace new page",
			action: Action{Kind: ActionWorkspaceNew, Text: "/workspace new"},
			want:   &FeishuUIIntent{Kind: FeishuUIIntentShowWorkspaceNew, RawText: "/workspace new"},
		},
		{
			name:   "workspace new worktree page",
			action: Action{Kind: ActionWorkspaceNewWorktree, Text: "/workspace new worktree"},
			want:   &FeishuUIIntent{Kind: FeishuUIIntentShowWorkspaceNewWorktree, RawText: "/workspace new worktree"},
		},
		{
			name:   "mode apply stays product owned",
			action: Action{Kind: ActionModeCommand, Text: "/mode vscode"},
			want:   nil,
		},
		{
			name:   "claude profile apply stays product owned",
			action: Action{Kind: ActionClaudeProfileCommand, Text: "/claudeprofile devseek"},
			want:   nil,
		},
		{
			name:   "codex provider apply stays product owned",
			action: Action{Kind: ActionCodexProviderCommand, Text: "/codexprovider team-proxy"},
			want:   nil,
		},
		{
			name:   "bare autowhip",
			action: Action{Kind: ActionAutoWhipCommand, Text: "/autowhip"},
			want:   &FeishuUIIntent{Kind: FeishuUIIntentShowAutoWhipCatalog, RawText: "/autowhip"},
		},
		{
			name:   "autowhip apply stays product owned",
			action: Action{Kind: ActionAutoWhipCommand, Text: "/autowhip on"},
			want:   nil,
		},
		{
			name:   "bare autocontinue",
			action: Action{Kind: ActionAutoContinueCommand, Text: "/autocontinue"},
			want:   &FeishuUIIntent{Kind: FeishuUIIntentShowAutoContinueCatalog, RawText: "/autocontinue"},
		},
		{
			name:   "autocontinue apply stays product owned",
			action: Action{Kind: ActionAutoContinueCommand, Text: "/autocontinue on"},
			want:   nil,
		},
		{
			name:   "workspace thread expansion",
			action: Action{Kind: ActionShowWorkspaceThreads, WorkspaceKey: "/data/dl/web"},
			want:   &FeishuUIIntent{Kind: FeishuUIIntentShowWorkspaceThreads, WorkspaceKey: "/data/dl/web"},
		},
		{
			name:   "bare verbose",
			action: Action{Kind: ActionVerboseCommand, Text: "/verbose"},
			want:   &FeishuUIIntent{Kind: FeishuUIIntentShowVerboseCatalog, RawText: "/verbose"},
		},
		{
			name: "list handoff",
			action: Action{
				Kind:      ActionListInstances,
				MessageID: "om-card-1",
				Inbound:   &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: &FeishuUIIntent{Kind: FeishuUIIntentShowList, SourceMessageID: "om-card-1", Inline: true},
		},
		{
			name: "workspace session provenance wins over legacy action kind",
			action: Action{
				Kind:             ActionShowThreads,
				CatalogFamilyID:  FeishuCommandUseAll,
				CatalogVariantID: "useall.codex.normal",
				MessageID:        "om-card-3",
				Inbound:          &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: &FeishuUIIntent{Kind: FeishuUIIntentShowAllThreads, SourceMessageID: "om-card-3", Inline: true},
		},
		{
			name: "send file handoff",
			action: Action{
				Kind:      ActionSendFile,
				MessageID: "om-card-2",
				Inbound:   &ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
			},
			want: &FeishuUIIntent{Kind: FeishuUIIntentOpenSendFilePicker, SourceMessageID: "om-card-2", Inline: true},
		},
		{
			name:   "verbose apply stays product owned",
			action: Action{Kind: ActionVerboseCommand, Text: "/verbose quiet"},
			want:   nil,
		},
		{
			name:   "path picker enter",
			action: Action{Kind: ActionPathPickerEnter, PickerID: "picker-1", PickerEntry: "subdir"},
			want:   &FeishuUIIntent{Kind: FeishuUIIntentPathPickerEnter, PickerID: "picker-1", PickerEntry: "subdir"},
		},
		{
			name:   "path picker page",
			action: Action{Kind: ActionPathPickerPage, PickerID: "picker-1", FieldName: "path_picker_file", Cursor: 9},
			want:   &FeishuUIIntent{Kind: FeishuUIIntentPathPickerPage, PickerID: "picker-1", FieldName: "path_picker_file", Cursor: 9},
		},
		{
			name:   "thread selection page",
			action: Action{Kind: ActionThreadSelectionPage, ViewMode: string(FeishuThreadSelectionVSCodeAll), Cursor: 12},
			want:   &FeishuUIIntent{Kind: FeishuUIIntentThreadSelectionPage, ViewMode: string(FeishuThreadSelectionVSCodeAll), Cursor: 12},
		},
		{
			name:   "path picker confirm",
			action: Action{Kind: ActionPathPickerConfirm, PickerID: "picker-1"},
			want:   &FeishuUIIntent{Kind: FeishuUIIntentPathPickerConfirm, PickerID: "picker-1"},
		},
		{
			name:   "attach stays product owned",
			action: Action{Kind: ActionAttachWorkspace, WorkspaceKey: "/data/dl/web"},
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := FeishuUIIntentFromAction(tt.action)
			if tt.want == nil {
				if ok || got != nil {
					t.Fatalf("expected no intent, got %#v", got)
				}
				return
			}
			if !ok || got == nil {
				t.Fatalf("expected intent %#v, got nil", tt.want)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("intent = %#v, want %#v", got, tt.want)
			}
		})
	}
}
