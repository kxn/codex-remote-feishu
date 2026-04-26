package projector

import (
	"strings"

	cardtransport "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardtransport"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/selectflow"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func targetPickerEditingElements(view control.FeishuTargetPickerView, daemonLifecycleID string) []map[string]any {
	return targetPickerComposeEditingElements(
		view,
		daemonLifecycleID,
		targetPickerEditingPageElements(view, daemonLifecycleID),
	)
}

func targetPickerEditingPageElements(view control.FeishuTargetPickerView, daemonLifecycleID string) []map[string]any {
	switch view.Page {
	case control.FeishuTargetPickerPageMode:
		return targetPickerModePageElements(view, daemonLifecycleID)
	case control.FeishuTargetPickerPageSource:
		return targetPickerSourcePageElements(view, daemonLifecycleID)
	case control.FeishuTargetPickerPageLocalDirectory:
		return targetPickerLocalDirectoryElements(view, daemonLifecycleID)
	case control.FeishuTargetPickerPageGit:
		return targetPickerGitURLElements(view, daemonLifecycleID)
	case control.FeishuTargetPickerPageWorktree:
		return targetPickerWorktreeElements(view, daemonLifecycleID)
	default:
		return targetPickerTargetPageElements(view, daemonLifecycleID)
	}
}

func targetPickerComposeEditingElements(view control.FeishuTargetPickerView, daemonLifecycleID string, pageElements []map[string]any) []map[string]any {
	elements := make([]map[string]any, 0, len(pageElements)+18)
	elements = append(elements, targetPickerHeaderElements(view.StageLabel, view.Question)...)
	elements = append(elements, cloneCardElementSlice(pageElements)...)
	if messages := TargetPickerMessageElements(view.SourceMessages); len(messages) != 0 {
		elements = append(elements, messages...)
	}
	if messages := TargetPickerMessageElements(view.Messages); len(messages) != 0 {
		elements = append(elements, messages...)
	}
	if hint := strings.TrimSpace(view.Hint); hint != "" {
		if block := cardPlainTextBlockElement(hint); len(block) != 0 {
			elements = append(elements, block)
		}
	}
	if noticeSections := targetPickerNoticeSections(view); len(noticeSections) != 0 {
		elements = append(elements, cardDividerElement())
		elements = appendCardTextSections(elements, noticeSections)
	}
	if targetPickerUsesInlineForm(view) {
		return elements
	}
	return appendCardFooterButtonGroup(elements, targetPickerEditingFooterButtons(view, daemonLifecycleID))
}

func targetPickerTargetPageElements(view control.FeishuTargetPickerView, daemonLifecycleID string) []map[string]any {
	renderWorkspaceSelect := !view.WorkspaceSelectionLocked &&
		(view.ShowWorkspaceSelect || len(view.WorkspaceOptions) != 0 || strings.TrimSpace(view.SelectedWorkspaceKey) != "")
	renderSessionSelect := view.ShowSessionSelect ||
		len(view.SessionOptions) != 0 ||
		strings.TrimSpace(view.SelectedSessionValue) != "" ||
		strings.TrimSpace(view.SessionPlaceholder) != ""

	pagePrefix := make([]map[string]any, 0, 2)
	if view.WorkspaceSelectionLocked {
		pagePrefix = appendCardTextSections(pagePrefix, targetPickerLockedWorkspaceSections(view))
	}

	switch {
	case renderWorkspaceSelect && renderSessionSelect:
		workspacePlan, sessionPlan := targetPickerPlanDualLanes(
			view,
			daemonLifecycleID,
			pagePrefix,
			targetPickerWorkspaceLane(view),
			targetPickerSessionLane(view),
		)
		return targetPickerCombinePageElements(
			pagePrefix,
			targetPickerPaginatedLaneElements(targetPickerWorkspaceLane(view), daemonLifecycleID, workspacePlan.Page),
			targetPickerPaginatedLaneElements(targetPickerSessionLane(view), daemonLifecycleID, sessionPlan.Page),
		)
	case renderWorkspaceSelect:
		lane := targetPickerWorkspaceLane(view)
		plan := targetPickerPlanSingleLane(view, daemonLifecycleID, pagePrefix, lane)
		return targetPickerCombinePageElements(pagePrefix, targetPickerPaginatedLaneElements(lane, daemonLifecycleID, plan.Page))
	case renderSessionSelect:
		lane := targetPickerSessionLane(view)
		plan := targetPickerPlanSingleLane(view, daemonLifecycleID, pagePrefix, lane)
		return targetPickerCombinePageElements(pagePrefix, targetPickerPaginatedLaneElements(lane, daemonLifecycleID, plan.Page))
	default:
		return pagePrefix
	}
}

func targetPickerWorkspaceLane(view control.FeishuTargetPickerView) paginatedSelectFlowLane {
	return targetPickerWorkspaceLaneWithLabel(
		view,
		"工作区",
		firstNonEmpty(strings.TrimSpace(view.WorkspacePlaceholder), "选择工作区"),
	)
}

func targetPickerWorktreeWorkspaceLane(view control.FeishuTargetPickerView) paginatedSelectFlowLane {
	return targetPickerWorkspaceLaneWithLabel(
		view,
		"基准工作区",
		firstNonEmpty(strings.TrimSpace(view.WorkspacePlaceholder), "选择基准工作区"),
	)
}

func targetPickerWorkspaceLaneWithLabel(view control.FeishuTargetPickerView, label, placeholder string) paginatedSelectFlowLane {
	return paginatedSelectFlowLane{
		Flow:          selectflow.TargetPickerWorkspaceFlow,
		Label:         strings.TrimSpace(label),
		Placeholder:   strings.TrimSpace(placeholder),
		Cursor:        view.WorkspaceCursor,
		SelectedValue: strings.TrimSpace(view.SelectedWorkspaceKey),
		Options:       targetPickerWorkspaceOptions(view.WorkspaceOptions),
		SelectPayload: actionPayloadTargetPicker(cardActionKindTargetPickerSelectWorkspace, view.PickerID),
		PagePayload: func(cursor int) map[string]any {
			return actionPayloadTargetPickerCursor(view.PickerID, selectflow.TargetPickerWorkspaceFlow.FieldName, cursor)
		},
	}
}

func targetPickerSessionLane(view control.FeishuTargetPickerView) paginatedSelectFlowLane {
	return paginatedSelectFlowLane{
		Flow:          selectflow.TargetPickerSessionFlow,
		Label:         "会话",
		Placeholder:   firstNonEmpty(strings.TrimSpace(view.SessionPlaceholder), "选择会话"),
		Cursor:        view.SessionCursor,
		SelectedValue: strings.TrimSpace(view.SelectedSessionValue),
		Options:       targetPickerSessionOptions(view.SessionOptions),
		SelectPayload: actionPayloadTargetPicker(cardActionKindTargetPickerSelectSession, view.PickerID),
		PagePayload: func(cursor int) map[string]any {
			return actionPayloadTargetPickerCursor(view.PickerID, selectflow.TargetPickerSessionFlow.FieldName, cursor)
		},
	}
}

func targetPickerPaginatedLaneElements(lane paginatedSelectFlowLane, daemonLifecycleID string, page paginatedSelectPage) []map[string]any {
	return lane.renderElements(daemonLifecycleID, page)
}

func targetPickerPlanSingleLane(
	view control.FeishuTargetPickerView,
	daemonLifecycleID string,
	pagePrefix []map[string]any,
	lane paginatedSelectFlowLane,
) paginatedSelectPlan {
	return planPaginatedSelectLane(
		cardtransport.InteractiveCardTransportLimitBytes,
		lane,
		func(page paginatedSelectPage) (int, error) {
			return targetPickerEditingCardSize(
				view,
				daemonLifecycleID,
				targetPickerCombinePageElements(pagePrefix, targetPickerPaginatedLaneElements(lane, daemonLifecycleID, page)),
			)
		},
	)
}

func targetPickerPlanSingleLaneForm(
	view control.FeishuTargetPickerView,
	daemonLifecycleID string,
	lane paginatedSelectFlowLane,
	render func(page paginatedSelectPage) []map[string]any,
) paginatedSelectPlan {
	return planPaginatedSelectLane(
		cardtransport.InteractiveCardTransportLimitBytes,
		lane,
		func(page paginatedSelectPage) (int, error) {
			return targetPickerEditingCardSize(view, daemonLifecycleID, render(page))
		},
	)
}

func targetPickerPlanDualLanes(
	view control.FeishuTargetPickerView,
	daemonLifecycleID string,
	pagePrefix []map[string]any,
	leftLane, rightLane paginatedSelectFlowLane,
) (paginatedSelectPlan, paginatedSelectPlan) {
	baseSize, err := targetPickerEditingCardSize(view, daemonLifecycleID, pagePrefix)
	if err != nil {
		return targetPickerPlanSingleLane(view, daemonLifecycleID, pagePrefix, leftLane),
			targetPickerPlanSingleLane(view, daemonLifecycleID, pagePrefix, rightLane)
	}

	available := cardtransport.InteractiveCardTransportLimitBytes - baseSize
	leftFit := targetPickerLaneFit(view, daemonLifecycleID, pagePrefix, leftLane, baseSize)
	rightFit := targetPickerLaneFit(view, daemonLifecycleID, pagePrefix, rightLane, baseSize)
	leftPlan, rightPlan := planBorrowedDualSelectPages(available, 1, 2, leftFit, rightFit)
	return tightenDualPaginatedSelectPlans(
		cardtransport.InteractiveCardTransportLimitBytes,
		func(leftPage, rightPage paginatedSelectPage) (int, error) {
			return targetPickerEditingCardSize(
				view,
				daemonLifecycleID,
				targetPickerCombinePageElements(
					pagePrefix,
					targetPickerPaginatedLaneElements(leftLane, daemonLifecycleID, leftPage),
					targetPickerPaginatedLaneElements(rightLane, daemonLifecycleID, rightPage),
				),
			)
		},
		leftFit,
		rightFit,
		leftPlan,
		rightPlan,
	)
}

func targetPickerLaneFit(
	view control.FeishuTargetPickerView,
	daemonLifecycleID string,
	pagePrefix []map[string]any,
	lane paginatedSelectFlowLane,
	baseSize int,
) paginatedSelectFit {
	return func(maxBytes int) paginatedSelectPlan {
		return planPaginatedSelectLane(maxBytes, lane, func(page paginatedSelectPage) (int, error) {
			size, err := targetPickerEditingCardSize(
				view,
				daemonLifecycleID,
				targetPickerCombinePageElements(pagePrefix, targetPickerPaginatedLaneElements(lane, daemonLifecycleID, page)),
			)
			if err != nil {
				return 0, err
			}
			return maxInt(size-baseSize, 0), nil
		})
	}
}

func targetPickerEditingCardSize(view control.FeishuTargetPickerView, daemonLifecycleID string, pageElements []map[string]any) (int, error) {
	return cardtransport.InteractiveMessageCardSize(
		targetPickerCardTitle(view),
		"",
		TargetPickerTheme(view),
		targetPickerComposeEditingElements(view, daemonLifecycleID, pageElements),
		true,
	)
}

func targetPickerCardTitle(view control.FeishuTargetPickerView) string {
	return firstNonEmpty(strings.TrimSpace(view.Title), "选择工作区与会话")
}

func targetPickerCombinePageElements(parts ...[]map[string]any) []map[string]any {
	total := 0
	for _, part := range parts {
		total += len(part)
	}
	out := make([]map[string]any, 0, total)
	for _, part := range parts {
		out = append(out, cloneCardElementSlice(part)...)
	}
	return out
}

func cloneCardElementSlice(elements []map[string]any) []map[string]any {
	if len(elements) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(elements))
	for _, element := range elements {
		if len(element) == 0 {
			continue
		}
		out = append(out, cloneCardMap(element))
	}
	return out
}
