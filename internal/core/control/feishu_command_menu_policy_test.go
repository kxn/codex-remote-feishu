package control

import (
	"reflect"
	"testing"
)

func TestFeishuMenuGroupClassificationMatchesTaskModel(t *testing.T) {
	cases := []struct {
		groupID string
		want    []string
	}{
		{
			groupID: FeishuCommandGroupCurrentWork,
			want:    []string{FeishuCommandStop, FeishuCommandCompact, FeishuCommandSteerAll, FeishuCommandNew, FeishuCommandStatus},
		},
		{
			groupID: FeishuCommandGroupSendSettings,
			want:    []string{FeishuCommandReasoning, FeishuCommandModel, FeishuCommandAccess, FeishuCommandPlan, FeishuCommandVerbose, FeishuCommandAutoContinue, FeishuCommandClaudeProfile},
		},
		{
			groupID: FeishuCommandGroupSwitchTarget,
			want: []string{
				FeishuCommandWorkspace,
				FeishuCommandWorkspaceList,
				FeishuCommandWorkspaceNew,
				FeishuCommandWorkspaceNewDir,
				FeishuCommandWorkspaceNewGit,
				FeishuCommandWorkspaceNewWorktree,
				FeishuCommandWorkspaceDetach,
				FeishuCommandList,
				FeishuCommandUse,
				FeishuCommandUseAll,
				FeishuCommandDetach,
				FeishuCommandFollow,
			},
		},
		{
			groupID: FeishuCommandGroupCommonTools,
			want:    []string{FeishuCommandReview, FeishuCommandPatch, FeishuCommandAutoWhip, FeishuCommandHistory, FeishuCommandCron, FeishuCommandSendFile},
		},
		{
			groupID: FeishuCommandGroupMaintenance,
			want:    []string{FeishuCommandMode, FeishuCommandUpgrade, FeishuCommandDebug, FeishuCommandHelp},
		},
	}

	totalVisible := 0
	for _, tc := range cases {
		defs := FeishuCommandDefinitionsForGroup(tc.groupID)
		visible := make([]string, 0, len(defs))
		for _, def := range defs {
			if def.ShowInMenu {
				visible = append(visible, def.ID)
			}
		}
		totalVisible += len(visible)
		if !reflect.DeepEqual(visible, tc.want) {
			t.Fatalf("group %q visible command ids mismatch:\n got: %#v\nwant: %#v", tc.groupID, visible, tc.want)
		}
	}
	if totalVisible != 34 {
		t.Fatalf("unexpected total visible command count: got %d, want 34", totalVisible)
	}
}
