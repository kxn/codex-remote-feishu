package projector

import (
	"strings"

	cardtransport "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardtransport"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

const targetPickerPaginationHint = "超出卡片大小，如未找到请翻页。"

type targetPickerPaginatedLane struct {
	PickerID      string
	FieldName     string
	Label         string
	Placeholder   string
	Cursor        int
	SelectedValue string
	Options       []map[string]any
	SelectPayload map[string]any
}

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
	if targetPickerUsesInlineGitForm(view) {
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

func targetPickerWorkspaceLane(view control.FeishuTargetPickerView) targetPickerPaginatedLane {
	return targetPickerPaginatedLane{
		PickerID:      view.PickerID,
		FieldName:     cardTargetPickerWorkspaceFieldName,
		Label:         "工作区",
		Placeholder:   firstNonEmpty(strings.TrimSpace(view.WorkspacePlaceholder), "选择工作区"),
		Cursor:        view.WorkspaceCursor,
		SelectedValue: strings.TrimSpace(view.SelectedWorkspaceKey),
		Options:       targetPickerWorkspaceOptions(view.WorkspaceOptions),
		SelectPayload: actionPayloadTargetPicker(cardActionKindTargetPickerSelectWorkspace, view.PickerID),
	}
}

func targetPickerSessionLane(view control.FeishuTargetPickerView) targetPickerPaginatedLane {
	return targetPickerPaginatedLane{
		PickerID:      view.PickerID,
		FieldName:     cardTargetPickerSessionFieldName,
		Label:         "会话",
		Placeholder:   firstNonEmpty(strings.TrimSpace(view.SessionPlaceholder), "选择会话"),
		Cursor:        view.SessionCursor,
		SelectedValue: strings.TrimSpace(view.SelectedSessionValue),
		Options:       targetPickerSessionOptions(view.SessionOptions),
		SelectPayload: actionPayloadTargetPicker(cardActionKindTargetPickerSelectSession, view.PickerID),
	}
}

func targetPickerLaneSpec(lane targetPickerPaginatedLane) paginatedSelectPageSpec {
	return paginatedSelectPageSpec{
		Cursor:           lane.Cursor,
		CandidateOptions: lane.Options,
		SelectedValue:    lane.SelectedValue,
	}
}

func targetPickerPaginatedLaneElements(lane targetPickerPaginatedLane, daemonLifecycleID string, page paginatedSelectPage) []map[string]any {
	elements := []map[string]any{{
		"tag":     "markdown",
		"content": "**" + strings.TrimSpace(lane.Label) + "**",
	}}
	elements = append(elements, renderPaginatedSelectElements(paginatedSelectRenderSpec{
		Name:           lane.FieldName,
		Placeholder:    lane.Placeholder,
		SelectPayload:  stampActionValue(cloneCardMap(lane.SelectPayload), daemonLifecycleID),
		PrevPayload:    stampActionValue(actionPayloadTargetPickerCursor(lane.PickerID, lane.FieldName, page.PrevCursor), daemonLifecycleID),
		NextPayload:    stampActionValue(actionPayloadTargetPickerCursor(lane.PickerID, lane.FieldName, page.NextCursor), daemonLifecycleID),
		Page:           page,
		PaginationHint: targetPickerPaginationHint,
	})...)
	return elements
}

func targetPickerPlanSingleLane(
	view control.FeishuTargetPickerView,
	daemonLifecycleID string,
	pagePrefix []map[string]any,
	lane targetPickerPaginatedLane,
) paginatedSelectPlan {
	return planPaginatedSelectPage(
		targetPickerLaneSpec(lane),
		cardtransport.InteractiveCardTransportLimitBytes,
		func(page paginatedSelectPage) (int, error) {
			return targetPickerEditingCardSize(
				view,
				daemonLifecycleID,
				targetPickerCombinePageElements(pagePrefix, targetPickerPaginatedLaneElements(lane, daemonLifecycleID, page)),
			)
		},
	)
}

func targetPickerPlanDualLanes(
	view control.FeishuTargetPickerView,
	daemonLifecycleID string,
	pagePrefix []map[string]any,
	leftLane, rightLane targetPickerPaginatedLane,
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
	return targetPickerTightenDualLanePlans(
		view,
		daemonLifecycleID,
		pagePrefix,
		leftLane,
		rightLane,
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
	lane targetPickerPaginatedLane,
	baseSize int,
) paginatedSelectFit {
	return func(maxBytes int) paginatedSelectPlan {
		return planPaginatedSelectPage(targetPickerLaneSpec(lane), maxBytes, func(page paginatedSelectPage) (int, error) {
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

func targetPickerTightenDualLanePlans(
	view control.FeishuTargetPickerView,
	daemonLifecycleID string,
	pagePrefix []map[string]any,
	leftLane, rightLane targetPickerPaginatedLane,
	leftFit, rightFit paginatedSelectFit,
	leftPlan, rightPlan paginatedSelectPlan,
) (paginatedSelectPlan, paginatedSelectPlan) {
	for i := 0; i < 64; i++ {
		size, err := targetPickerEditingCardSize(
			view,
			daemonLifecycleID,
			targetPickerCombinePageElements(
				pagePrefix,
				targetPickerPaginatedLaneElements(leftLane, daemonLifecycleID, leftPlan.Page),
				targetPickerPaginatedLaneElements(rightLane, daemonLifecycleID, rightPlan.Page),
			),
		)
		if err != nil || size <= cardtransport.InteractiveCardTransportLimitBytes {
			return leftPlan, rightPlan
		}

		bestLeft, bestRight, bestSize, shrunk := leftPlan, rightPlan, size, false
		if next, ok := targetPickerShrinkSelectPlan(leftPlan, leftFit); ok {
			nextSize, nextErr := targetPickerEditingCardSize(
				view,
				daemonLifecycleID,
				targetPickerCombinePageElements(
					pagePrefix,
					targetPickerPaginatedLaneElements(leftLane, daemonLifecycleID, next.Page),
					targetPickerPaginatedLaneElements(rightLane, daemonLifecycleID, rightPlan.Page),
				),
			)
			if nextErr == nil && nextSize < bestSize {
				bestLeft = next
				bestRight = rightPlan
				bestSize = nextSize
				shrunk = true
			}
		}
		if next, ok := targetPickerShrinkSelectPlan(rightPlan, rightFit); ok {
			nextSize, nextErr := targetPickerEditingCardSize(
				view,
				daemonLifecycleID,
				targetPickerCombinePageElements(
					pagePrefix,
					targetPickerPaginatedLaneElements(leftLane, daemonLifecycleID, leftPlan.Page),
					targetPickerPaginatedLaneElements(rightLane, daemonLifecycleID, next.Page),
				),
			)
			if nextErr == nil && nextSize < bestSize {
				bestLeft = leftPlan
				bestRight = next
				bestSize = nextSize
				shrunk = true
			}
		}
		if !shrunk {
			return leftPlan, rightPlan
		}
		leftPlan, rightPlan = bestLeft, bestRight
	}
	return leftPlan, rightPlan
}

func targetPickerShrinkSelectPlan(current paginatedSelectPlan, fit paginatedSelectFit) (paginatedSelectPlan, bool) {
	if fit == nil || current.Page.PageOptionCount <= 1 {
		return paginatedSelectPlan{}, false
	}
	for budget := current.UsedBytes - 1; budget >= 0; budget-- {
		next := fit(budget)
		if next.Page.PageOptionCount < current.Page.PageOptionCount || next.UsedBytes < current.UsedBytes {
			return next, true
		}
	}
	return paginatedSelectPlan{}, false
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
