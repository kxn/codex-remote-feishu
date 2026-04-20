package feishu

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestProjectWorkspaceSelectionViewDoesNotAddLegacyCreateWorkspaceEntry(t *testing.T) {
	projector := NewProjector()
	view := control.FeishuSelectionView{
		PromptKind: control.SelectionPromptAttachWorkspace,
		Workspace: &control.FeishuWorkspaceSelectionView{
			Page:       1,
			PageSize:   8,
			TotalPages: 1,
			Entries: []control.FeishuWorkspaceSelectionEntry{
				{
					WorkspaceKey:      "/data/dl/web",
					WorkspaceLabel:    "web",
					AgeText:           "2分前",
					HasVSCodeActivity: true,
					Attachable:        true,
				},
			},
		},
	}
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:                control.UIEventFeishuDirectSelectionPrompt,
		FeishuSelectionView: &view,
		FeishuSelectionContext: &control.FeishuUISelectionContext{
			DTOOwner:   control.FeishuUIDTOwnerSelection,
			PromptKind: control.SelectionPromptAttachWorkspace,
			Layout:     "grouped_attach_workspace",
			Title:      "工作区列表",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	for _, element := range ops[0].CardElements {
		tag, _ := element["tag"].(string)
		if tag != "button" && tag != "column_set" && tag != "action" {
			continue
		}
		for _, button := range cardElementButtons(t, element) {
			if cardButtonLabel(t, button) != "新建 · 添加工作区" {
				continue
			}
			t.Fatalf("expected projected workspace selection view to omit legacy create-workspace button, got %#v", button)
		}
	}
}
