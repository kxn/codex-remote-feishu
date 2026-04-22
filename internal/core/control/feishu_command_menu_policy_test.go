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
			want:    []string{FeishuCommandStop, FeishuCommandCompact, FeishuCommandSteerAll, FeishuCommandNew, FeishuCommandHistory, FeishuCommandSendFile},
		},
		{
			groupID: FeishuCommandGroupSendSettings,
			want:    []string{FeishuCommandReasoning, FeishuCommandModel, FeishuCommandAccess, FeishuCommandPlan, FeishuCommandVerbose},
		},
		{
			groupID: FeishuCommandGroupSwitchTarget,
			want:    []string{FeishuCommandList, FeishuCommandUse, FeishuCommandUseAll, FeishuCommandDetach, FeishuCommandFollow},
		},
		{
			groupID: FeishuCommandGroupMaintenance,
			want:    []string{FeishuCommandStatus, FeishuCommandMode, FeishuCommandAutoContinue, FeishuCommandHelp, FeishuCommandCron, FeishuCommandUpgrade, FeishuCommandDebug},
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
	if totalVisible != 23 {
		t.Fatalf("unexpected total visible command count: got %d, want 23", totalVisible)
	}
}

func TestFeishuCommandVisibleInMenuStagePolicy(t *testing.T) {
	if FeishuCommandVisibleInMenuStage(FeishuCommandFollow, string(FeishuCommandMenuStageDetached)) {
		t.Fatal("follow should be hidden in detached stage")
	}
	if FeishuCommandVisibleInMenuStage(FeishuCommandFollow, string(FeishuCommandMenuStageNormalWorking)) {
		t.Fatal("follow should be hidden in normal stage")
	}
	if !FeishuCommandVisibleInMenuStage(FeishuCommandFollow, string(FeishuCommandMenuStageVSCodeWorking)) {
		t.Fatal("follow should be visible in vscode stage")
	}

	if FeishuCommandVisibleInMenuStage(FeishuCommandNew, string(FeishuCommandMenuStageDetached)) {
		t.Fatal("new should be hidden in detached stage")
	}
	if !FeishuCommandVisibleInMenuStage(FeishuCommandNew, string(FeishuCommandMenuStageNormalWorking)) {
		t.Fatal("new should be visible in normal stage")
	}
	if FeishuCommandVisibleInMenuStage(FeishuCommandNew, string(FeishuCommandMenuStageVSCodeWorking)) {
		t.Fatal("new should be hidden in vscode stage")
	}

	if !FeishuCommandVisibleInMenuStage(FeishuCommandStatus, "unknown-stage") {
		t.Fatal("status should remain visible for unknown stage")
	}
	if FeishuCommandVisibleInMenuStage(FeishuCommandFollow, "unknown-stage") {
		t.Fatal("follow should be hidden for unknown stage")
	}
}
