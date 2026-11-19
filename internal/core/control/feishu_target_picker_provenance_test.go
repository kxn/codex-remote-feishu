package control

import "testing"

func TestTargetPickerIntentKeepsTypedActionWithLauncherProvenance(t *testing.T) {
	for _, family := range []string{FeishuCommandList, FeishuCommandUse, FeishuCommandUseAll, FeishuCommandMode, FeishuCommandGoal} {
		for actionKind, intentKind := range map[ActionKind]FeishuUIIntentKind{
			ActionTargetPickerSelectWorkspace: FeishuUIIntentTargetPickerSelectWorkspace,
			ActionTargetPickerSelectSession:   FeishuUIIntentTargetPickerSelectSession,
			ActionTargetPickerPage:            FeishuUIIntentTargetPickerPage,
		} {
			action := Action{Kind: actionKind, CatalogFamilyID: family, CatalogVariantID: family + ".codex.normal", PickerID: "picker", WorkspaceKey: "/workspace", TargetPickerValue: "thread:thread", FieldName: "target_picker_session", Cursor: 10}
			intent, ok := FeishuUIIntentFromAction(action)
			if !ok || intent.Kind != intentKind || intent.PickerID != action.PickerID {
				t.Fatalf("%s provenance replayed launcher for %s: %#v", family, actionKind, intent)
			}
		}
	}
}
