package feishu

import (
	projectorpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/projector"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

const targetPickerPaginationHint = "超出卡片大小，如未找到请翻页。"

func projectNoticeContent(notice control.Notice) (string, []map[string]any) {
	return projectorpkg.ProjectNoticeContent(notice)
}

func pageBody(view control.FeishuPageView) string {
	return projectorpkg.PageBody(view)
}

func pageElements(view control.FeishuPageView, daemonLifecycleID string) []map[string]any {
	return projectorpkg.PageElements(view, daemonLifecycleID)
}

func selectionViewStructuredProjection(
	view control.FeishuSelectionView,
	ctx *control.FeishuUISelectionContext,
	daemonLifecycleID string,
) (string, []map[string]any, bool) {
	return projectorpkg.SelectionViewStructuredProjection(view, ctx, daemonLifecycleID)
}

func pathPickerElements(view control.FeishuPathPickerView, daemonLifecycleID string) []map[string]any {
	return projectorpkg.PathPickerElements(view, daemonLifecycleID)
}

func targetPickerElements(view control.FeishuTargetPickerView, daemonLifecycleID string) []map[string]any {
	return projectorpkg.TargetPickerElements(view, daemonLifecycleID)
}

func targetPickerTheme(view control.FeishuTargetPickerView) string {
	return projectorpkg.TargetPickerTheme(view)
}

func targetPickerMessageElements(messages []control.FeishuTargetPickerMessage) []map[string]any {
	return projectorpkg.TargetPickerMessageElements(messages)
}

func planUpdateElements(update control.PlanUpdate) []map[string]any {
	return projectorpkg.PlanUpdateElements(update)
}

func requestPromptElements(prompt control.FeishuRequestView, daemonLifecycleID string) []map[string]any {
	return projectorpkg.RequestPromptElements(prompt, daemonLifecycleID)
}
