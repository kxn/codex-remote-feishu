package projector

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func SelectionViewStructuredProjection(
	view control.FeishuSelectionView,
	ctx *control.FeishuUISelectionContext,
	daemonLifecycleID string,
) (string, []map[string]any, bool) {
	semantics := control.DeriveFeishuSelectionSemantics(view)
	switch {
	case view.Thread != nil && view.PromptKind == control.SelectionPromptUseThread && selectionViewUsesDirectThreadPicker(view.Thread.Mode):
		return firstNonEmpty(strings.TrimSpace(semantics.Title), "选择会话"),
			threadSelectionDropdownElements(view, semantics, daemonLifecycleID),
			true
	default:
		model, ok := selectionRenderModelFromView(view, ctx)
		if !ok {
			return "", nil, false
		}
		title := strings.TrimSpace(model.Title)
		if title == "" {
			title = selectionPromptDefaultTitle(model.Kind)
		}
		return title, selectionPromptElements(model, daemonLifecycleID), true
	}
}

func selectionPromptDefaultTitle(kind control.SelectionPromptKind) string {
	switch kind {
	case control.SelectionPromptAttachInstance:
		return "在线 VS Code 实例"
	case control.SelectionPromptAttachWorkspace:
		return "工作区列表"
	case control.SelectionPromptUseThread:
		return "会话列表"
	case control.SelectionPromptKickThread:
		return "强踢当前会话？"
	default:
		return "请选择"
	}
}

func selectionViewStructuredContextElements(semantics control.FeishuSelectionSemantics) []map[string]any {
	if strings.TrimSpace(semantics.ContextTitle) == "" && strings.TrimSpace(semantics.ContextText) == "" {
		return nil
	}
	elements := make([]map[string]any, 0, 2)
	if title := strings.TrimSpace(semantics.ContextTitle); title != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**" + title + "**",
		})
	}
	if text := strings.TrimSpace(semantics.ContextText); text != "" {
		if block := cardPlainTextBlockElement(text); len(block) != 0 {
			elements = append(elements, block)
		}
	}
	return elements
}

func selectionViewUsesDirectThreadPicker(mode control.FeishuThreadSelectionViewMode) bool {
	switch mode {
	case control.FeishuThreadSelectionVSCodeRecent,
		control.FeishuThreadSelectionVSCodeAll,
		control.FeishuThreadSelectionVSCodeScopedAll:
		return true
	default:
		return false
	}
}

func threadSelectionDropdownElements(view control.FeishuSelectionView, semantics control.FeishuSelectionSemantics, daemonLifecycleID string) []map[string]any {
	return paginatedThreadSelectionDropdownElements(view, semantics, daemonLifecycleID)
}
