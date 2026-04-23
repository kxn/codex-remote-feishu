package projector

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestWorkspaceSelectionModelRecoverableOptionUsesWorkspaceThreadsAction(t *testing.T) {
	model, ok := selectionRenderModelFromView(control.FeishuSelectionView{
		PromptKind: control.SelectionPromptAttachWorkspace,
		Workspace: &control.FeishuWorkspaceSelectionView{
			Entries: []control.FeishuWorkspaceSelectionEntry{{
				WorkspaceKey:    "ws-1",
				WorkspaceLabel:  "Workspace 1",
				RecoverableOnly: true,
			}},
		},
	}, nil)
	if !ok {
		t.Fatalf("expected workspace selection view to build render model")
	}
	if len(model.Options) != 1 {
		t.Fatalf("expected one option, got %#v", model.Options)
	}
	if model.Options[0].ActionKind != "show_workspace_threads" {
		t.Fatalf("unexpected recoverable action kind: %#v", model.Options[0])
	}
}
