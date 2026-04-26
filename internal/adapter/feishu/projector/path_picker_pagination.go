package projector

import (
	"strings"

	cardtransport "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardtransport"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/selectflow"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func paginatedFileModePathPickerElements(view control.FeishuPathPickerView, daemonLifecycleID string) []map[string]any {
	directoryLane := pathPickerDirectoryLane(view)
	fileLane := pathPickerFileLane(view)

	switch {
	case directoryLane.visible() && fileLane.visible():
		directoryPlan, filePlan := pathPickerPlanFileModeDualLanes(view, daemonLifecycleID, directoryLane, fileLane)
		return pathPickerFileModeElementsWithPages(view, daemonLifecycleID, &directoryPlan.Page, &filePlan.Page)
	case directoryLane.visible():
		directoryPlan := pathPickerPlanSingleLane(view, directoryLane, func(page paginatedSelectPage) []map[string]any {
			return pathPickerFileModeElementsWithPages(view, daemonLifecycleID, &page, nil)
		})
		return pathPickerFileModeElementsWithPages(view, daemonLifecycleID, &directoryPlan.Page, nil)
	case fileLane.visible():
		filePlan := pathPickerPlanSingleLane(view, fileLane, func(page paginatedSelectPage) []map[string]any {
			return pathPickerFileModeElementsWithPages(view, daemonLifecycleID, nil, &page)
		})
		return pathPickerFileModeElementsWithPages(view, daemonLifecycleID, nil, &filePlan.Page)
	default:
		return pathPickerFileModeElementsWithPages(view, daemonLifecycleID, nil, nil)
	}
}

func paginatedDirectoryModePathPickerElements(view control.FeishuPathPickerView, daemonLifecycleID string) []map[string]any {
	directoryLane := pathPickerDirectoryLane(view)
	if !directoryLane.visible() {
		return pathPickerDirectoryModeElementsWithPage(view, daemonLifecycleID, nil)
	}
	plan := pathPickerPlanSingleLane(view, directoryLane, func(page paginatedSelectPage) []map[string]any {
		return pathPickerDirectoryModeElementsWithPage(view, daemonLifecycleID, &page)
	})
	return pathPickerDirectoryModeElementsWithPage(view, daemonLifecycleID, &plan.Page)
}

func paginatedOwnerSubpageDirectoryModePathPickerElements(view control.FeishuPathPickerView, daemonLifecycleID string) []map[string]any {
	directoryLane := pathPickerDirectoryLane(view)
	if !directoryLane.visible() {
		return pathPickerOwnerSubpageDirectoryModeElementsWithPage(view, daemonLifecycleID, nil)
	}
	plan := pathPickerPlanSingleLane(view, directoryLane, func(page paginatedSelectPage) []map[string]any {
		return pathPickerOwnerSubpageDirectoryModeElementsWithPage(view, daemonLifecycleID, &page)
	})
	return pathPickerOwnerSubpageDirectoryModeElementsWithPage(view, daemonLifecycleID, &plan.Page)
}

func pathPickerDirectoryLane(view control.FeishuPathPickerView) paginatedSelectFlowLane {
	childOptions, _ := pathPickerSelectStaticOptions(view, control.PathPickerEntryDirectory)
	fixedOptions := []map[string]any{currentDirectoryPathPickerOption(view.CurrentPath)}
	if view.CanGoUp {
		fixedOptions = append(fixedOptions, map[string]any{
			"text":  cardPlainText(".."),
			"value": "..",
		})
	}
	return paginatedSelectFlowLane{
		Flow:          selectflow.PathPickerDirectoryFlow,
		Label:         "进入目录",
		Placeholder:   ".. 返回上一级，或选择子目录",
		Cursor:        view.DirectoryCursor,
		SelectedValue: ".",
		FixedOptions:  fixedOptions,
		Options:       childOptions,
		SelectPayload: pathPickerFieldActionPayload(cardActionKindPathPickerEnter, view.PickerID, selectflow.PathPickerDirectoryFlow.FieldName),
		PagePayload: func(cursor int) map[string]any {
			return actionPayloadPathPickerCursor(view.PickerID, selectflow.PathPickerDirectoryFlow.FieldName, cursor)
		},
	}
}

func pathPickerFileLane(view control.FeishuPathPickerView) paginatedSelectFlowLane {
	fileOptions, selectedOption := pathPickerSelectStaticOptions(view, control.PathPickerEntryFile)
	return paginatedSelectFlowLane{
		Flow:          selectflow.PathPickerFileFlow,
		Label:         "选择文件",
		Placeholder:   "选择待发送文件",
		Cursor:        view.FileCursor,
		SelectedValue: selectedOption,
		Options:       fileOptions,
		SelectPayload: pathPickerFieldActionPayload(cardActionKindPathPickerSelect, view.PickerID, selectflow.PathPickerFileFlow.FieldName),
		PagePayload: func(cursor int) map[string]any {
			return actionPayloadPathPickerCursor(view.PickerID, selectflow.PathPickerFileFlow.FieldName, cursor)
		},
	}
}

func pathPickerPaginatedLaneElements(lane paginatedSelectFlowLane, daemonLifecycleID string, page paginatedSelectPage) []map[string]any {
	return lane.renderElements(daemonLifecycleID, page)
}

func pathPickerPlanSingleLane(
	view control.FeishuPathPickerView,
	lane paginatedSelectFlowLane,
	build func(page paginatedSelectPage) []map[string]any,
) paginatedSelectPlan {
	return planPaginatedSelectLane(
		cardtransport.InteractiveCardTransportLimitBytes,
		lane,
		func(page paginatedSelectPage) (int, error) {
			return pathPickerCardSize(view, build(page))
		},
	)
}

func pathPickerPlanFileModeDualLanes(
	view control.FeishuPathPickerView,
	daemonLifecycleID string,
	leftLane, rightLane paginatedSelectFlowLane,
) (paginatedSelectPlan, paginatedSelectPlan) {
	baseSize, err := pathPickerCardSize(view, pathPickerFileModeElementsWithPages(view, daemonLifecycleID, nil, nil))
	if err != nil {
		return pathPickerPlanSingleLane(view, leftLane, func(page paginatedSelectPage) []map[string]any {
				return pathPickerFileModeElementsWithPages(view, daemonLifecycleID, &page, nil)
			}),
			pathPickerPlanSingleLane(view, rightLane, func(page paginatedSelectPage) []map[string]any {
				return pathPickerFileModeElementsWithPages(view, daemonLifecycleID, nil, &page)
			})
	}

	available := cardtransport.InteractiveCardTransportLimitBytes - baseSize
	leftFit := func(maxBytes int) paginatedSelectPlan {
		return planPaginatedSelectLane(maxBytes, leftLane, func(page paginatedSelectPage) (int, error) {
			size, err := pathPickerCardSize(view, pathPickerFileModeElementsWithPages(view, daemonLifecycleID, &page, nil))
			if err != nil {
				return 0, err
			}
			return maxInt(size-baseSize, 0), nil
		})
	}
	rightFit := func(maxBytes int) paginatedSelectPlan {
		return planPaginatedSelectLane(maxBytes, rightLane, func(page paginatedSelectPage) (int, error) {
			size, err := pathPickerCardSize(view, pathPickerFileModeElementsWithPages(view, daemonLifecycleID, nil, &page))
			if err != nil {
				return 0, err
			}
			return maxInt(size-baseSize, 0), nil
		})
	}

	leftPlan, rightPlan := planBorrowedDualSelectPages(available, 1, 2, leftFit, rightFit)
	return tightenDualPaginatedSelectPlans(
		cardtransport.InteractiveCardTransportLimitBytes,
		func(leftPage, rightPage paginatedSelectPage) (int, error) {
			return pathPickerCardSize(view, pathPickerFileModeElementsWithPages(view, daemonLifecycleID, &leftPage, &rightPage))
		},
		leftFit,
		rightFit,
		leftPlan,
		rightPlan,
	)
}

func pathPickerFileModeElementsWithPages(
	view control.FeishuPathPickerView,
	daemonLifecycleID string,
	directoryPage, filePage *paginatedSelectPage,
) []map[string]any {
	elements := make([]map[string]any, 0, 12)
	summaryLines := []string{
		"**当前目录**\n" + formatNeutralTextTag(view.CurrentPath),
		"**允许范围**\n" + formatNeutralTextTag(view.RootPath),
	}
	selectedPath := strings.TrimSpace(view.SelectedPath)
	if selectedPath != "" {
		summaryLines = append(summaryLines, "**待发送文件**\n"+formatNeutralTextTag(selectedPath))
	} else {
		summaryLines = append(summaryLines, "**待发送文件**\n"+formatNeutralTextTag("未选择"))
	}
	elements = append(elements, map[string]any{
		"tag":     "markdown",
		"content": strings.Join(summaryLines, "\n"),
	})

	directoryLane := pathPickerDirectoryLane(view)
	if directoryPage != nil {
		elements = append(elements, pathPickerPaginatedLaneElements(directoryLane, daemonLifecycleID, *directoryPage)...)
	}
	fileLane := pathPickerFileLane(view)
	if filePage != nil {
		elements = append(elements, pathPickerPaginatedLaneElements(fileLane, daemonLifecycleID, *filePage)...)
	}

	if len(directoryLane.Options) == 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "当前目录下没有可进入的子目录。",
		})
	}
	if len(fileLane.Options) == 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "当前目录下没有可发送文件。",
		})
	}
	if hint := strings.TrimSpace(view.Hint); hint != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": renderSystemInlineTags(hint),
		})
	}
	if noticeSections := pathPickerNoticeSectionsForView(view); len(noticeSections) != 0 {
		elements = append(elements, cardDividerElement())
		elements = appendCardTextSections(elements, noticeSections)
	}
	return appendCardFooterButtonGroup(elements, pathPickerDefaultFooterButtons(view, daemonLifecycleID))
}

func pathPickerDirectoryModeElementsWithPage(
	view control.FeishuPathPickerView,
	daemonLifecycleID string,
	directoryPage *paginatedSelectPage,
) []map[string]any {
	elements := make([]map[string]any, 0, 10)
	summaryLines := []string{
		"**允许范围**\n" + formatNeutralTextTag(view.RootPath),
		"**当前目录**\n" + formatNeutralTextTag(view.CurrentPath),
	}
	selectedPath := strings.TrimSpace(firstNonEmpty(view.SelectedPath, view.CurrentPath))
	if selectedPath != "" {
		summaryLines = append(summaryLines, "**当前选择**\n"+formatNeutralTextTag(selectedPath))
	}
	elements = append(elements, map[string]any{
		"tag":     "markdown",
		"content": strings.Join(summaryLines, "\n"),
	})

	directoryLane := pathPickerDirectoryLane(view)
	if directoryPage != nil {
		elements = append(elements, pathPickerPaginatedLaneElements(directoryLane, daemonLifecycleID, *directoryPage)...)
	}
	if len(directoryLane.Options) == 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "当前目录下没有可进入的子目录。",
		})
	}
	if hint := strings.TrimSpace(view.Hint); hint != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": renderSystemInlineTags(hint),
		})
	}
	if noticeSections := pathPickerNoticeSectionsForView(view); len(noticeSections) != 0 {
		elements = append(elements, cardDividerElement())
		elements = appendCardTextSections(elements, noticeSections)
	}
	return appendCardFooterButtonGroup(elements, pathPickerDefaultFooterButtons(view, daemonLifecycleID))
}

func pathPickerOwnerSubpageDirectoryModeElementsWithPage(
	view control.FeishuPathPickerView,
	daemonLifecycleID string,
	directoryPage *paginatedSelectPage,
) []map[string]any {
	elements := make([]map[string]any, 0, 10)
	elements = append(elements, targetPickerHeaderElements(view.StageLabel, view.Question)...)
	if strings.TrimSpace(view.RootPath) != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "范围：" + formatNeutralTextTag(view.RootPath),
		})
	}
	if current := strings.TrimSpace(view.CurrentPath); current != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**当前位置**",
		})
		if block := cardPlainTextBlockElement(current); len(block) != 0 {
			elements = append(elements, block)
		}
	}
	directoryLane := pathPickerDirectoryLane(view)
	if directoryPage != nil {
		elements = append(elements, pathPickerPaginatedLaneElements(directoryLane, daemonLifecycleID, *directoryPage)...)
	}
	if len(directoryLane.Options) == 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "当前目录下没有可进入的子目录。",
		})
	}
	if hint := strings.TrimSpace(view.Hint); hint != "" {
		if block := cardPlainTextBlockElement(hint); len(block) != 0 {
			elements = append(elements, block)
		}
	}
	if noticeSections := pathPickerNoticeSectionsForView(view); len(noticeSections) != 0 {
		elements = append(elements, cardDividerElement())
		elements = appendCardTextSections(elements, noticeSections)
	}
	return appendCardFooterButtonGroup(elements, pathPickerOwnerSubpageFooterButtons(view, daemonLifecycleID))
}

func pathPickerDefaultFooterButtons(view control.FeishuPathPickerView, daemonLifecycleID string) []map[string]any {
	return []map[string]any{
		cardCallbackButtonElement(strings.TrimSpace(firstNonEmpty(view.ConfirmLabel, "确认")), "primary", stampActionValue(actionPayloadPathPicker(cardActionKindPathPickerConfirm, view.PickerID, ""), daemonLifecycleID), !view.CanConfirm, ""),
		cardCallbackButtonElement(strings.TrimSpace(firstNonEmpty(view.CancelLabel, "取消")), "default", stampActionValue(actionPayloadPathPicker(cardActionKindPathPickerCancel, view.PickerID, ""), daemonLifecycleID), false, ""),
	}
}

func pathPickerOwnerSubpageFooterButtons(view control.FeishuPathPickerView, daemonLifecycleID string) []map[string]any {
	return []map[string]any{
		cardCallbackButtonElement(strings.TrimSpace(firstNonEmpty(view.CancelLabel, "返回")), "default", stampActionValue(actionPayloadPathPicker(cardActionKindPathPickerCancel, view.PickerID, ""), daemonLifecycleID), false, ""),
		cardCallbackButtonElement(strings.TrimSpace(firstNonEmpty(view.ConfirmLabel, "使用这个目录")), "primary", stampActionValue(actionPayloadPathPicker(cardActionKindPathPickerConfirm, view.PickerID, ""), daemonLifecycleID), !view.CanConfirm, ""),
	}
}

func pathPickerCardSize(view control.FeishuPathPickerView, elements []map[string]any) (int, error) {
	return cardtransport.InteractiveMessageCardSize(
		pathPickerCardTitle(view),
		"",
		cardThemeInfo,
		cloneCardElementSlice(elements),
		true,
	)
}

func pathPickerCardTitle(view control.FeishuPathPickerView) string {
	return firstNonEmpty(strings.TrimSpace(view.Title), "选择路径")
}
