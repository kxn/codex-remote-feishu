package projector

import (
	"strings"

	cardtransport "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardtransport"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

const pathPickerPaginationHint = "超出卡片大小，如未找到请翻页。"

type pathPickerPaginatedLane struct {
	PickerID      string
	FieldName     string
	Label         string
	Placeholder   string
	Cursor        int
	SelectedValue string
	FixedOptions  []map[string]any
	Options       []map[string]any
	SelectPayload map[string]any
}

func paginatedFileModePathPickerElements(view control.FeishuPathPickerView, daemonLifecycleID string) []map[string]any {
	directoryLane := pathPickerDirectoryLane(view)
	fileLane := pathPickerFileLane(view)

	switch {
	case pathPickerLaneVisible(directoryLane) && pathPickerLaneVisible(fileLane):
		directoryPlan, filePlan := pathPickerPlanFileModeDualLanes(view, daemonLifecycleID, directoryLane, fileLane)
		return pathPickerFileModeElementsWithPages(view, daemonLifecycleID, &directoryPlan.Page, &filePlan.Page)
	case pathPickerLaneVisible(directoryLane):
		directoryPlan := pathPickerPlanSingleLane(view, directoryLane, func(page paginatedSelectPage) []map[string]any {
			return pathPickerFileModeElementsWithPages(view, daemonLifecycleID, &page, nil)
		})
		return pathPickerFileModeElementsWithPages(view, daemonLifecycleID, &directoryPlan.Page, nil)
	case pathPickerLaneVisible(fileLane):
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
	if !pathPickerLaneVisible(directoryLane) {
		return pathPickerDirectoryModeElementsWithPage(view, daemonLifecycleID, nil)
	}
	plan := pathPickerPlanSingleLane(view, directoryLane, func(page paginatedSelectPage) []map[string]any {
		return pathPickerDirectoryModeElementsWithPage(view, daemonLifecycleID, &page)
	})
	return pathPickerDirectoryModeElementsWithPage(view, daemonLifecycleID, &plan.Page)
}

func paginatedOwnerSubpageDirectoryModePathPickerElements(view control.FeishuPathPickerView, daemonLifecycleID string) []map[string]any {
	directoryLane := pathPickerDirectoryLane(view)
	if !pathPickerLaneVisible(directoryLane) {
		return pathPickerOwnerSubpageDirectoryModeElementsWithPage(view, daemonLifecycleID, nil)
	}
	plan := pathPickerPlanSingleLane(view, directoryLane, func(page paginatedSelectPage) []map[string]any {
		return pathPickerOwnerSubpageDirectoryModeElementsWithPage(view, daemonLifecycleID, &page)
	})
	return pathPickerOwnerSubpageDirectoryModeElementsWithPage(view, daemonLifecycleID, &plan.Page)
}

func pathPickerDirectoryLane(view control.FeishuPathPickerView) pathPickerPaginatedLane {
	childOptions, _ := pathPickerSelectStaticOptions(view, control.PathPickerEntryDirectory)
	fixedOptions := []map[string]any{currentDirectoryPathPickerOption(view.CurrentPath)}
	if view.CanGoUp {
		fixedOptions = append(fixedOptions, map[string]any{
			"text":  cardPlainText(".."),
			"value": "..",
		})
	}
	return pathPickerPaginatedLane{
		PickerID:      view.PickerID,
		FieldName:     cardPathPickerDirectorySelectFieldName,
		Label:         "进入目录",
		Placeholder:   ".. 返回上一级，或选择子目录",
		Cursor:        view.DirectoryCursor,
		SelectedValue: ".",
		FixedOptions:  fixedOptions,
		Options:       childOptions,
		SelectPayload: pathPickerFieldActionPayload(cardActionKindPathPickerEnter, view.PickerID, cardPathPickerDirectorySelectFieldName),
	}
}

func pathPickerFileLane(view control.FeishuPathPickerView) pathPickerPaginatedLane {
	fileOptions, selectedOption := pathPickerSelectStaticOptions(view, control.PathPickerEntryFile)
	return pathPickerPaginatedLane{
		PickerID:      view.PickerID,
		FieldName:     cardPathPickerFileSelectFieldName,
		Label:         "选择文件",
		Placeholder:   "选择待发送文件",
		Cursor:        view.FileCursor,
		SelectedValue: selectedOption,
		Options:       fileOptions,
		SelectPayload: pathPickerFieldActionPayload(cardActionKindPathPickerSelect, view.PickerID, cardPathPickerFileSelectFieldName),
	}
}

func pathPickerLaneVisible(lane pathPickerPaginatedLane) bool {
	return len(lane.FixedOptions) != 0 || len(lane.Options) != 0
}

func pathPickerLaneSpec(lane pathPickerPaginatedLane) paginatedSelectPageSpec {
	return paginatedSelectPageSpec{
		Cursor:           lane.Cursor,
		FixedOptions:     lane.FixedOptions,
		CandidateOptions: lane.Options,
		SelectedValue:    lane.SelectedValue,
	}
}

func pathPickerPaginatedLaneElements(lane pathPickerPaginatedLane, daemonLifecycleID string, page paginatedSelectPage) []map[string]any {
	elements := []map[string]any{{
		"tag":     "markdown",
		"content": "**" + strings.TrimSpace(lane.Label) + "**",
	}}
	elements = append(elements, renderPaginatedSelectElements(paginatedSelectRenderSpec{
		Name:           lane.FieldName,
		Placeholder:    lane.Placeholder,
		SelectPayload:  stampActionValue(cloneCardMap(lane.SelectPayload), daemonLifecycleID),
		PrevPayload:    stampActionValue(actionPayloadPathPickerCursor(lane.PickerID, lane.FieldName, page.PrevCursor), daemonLifecycleID),
		NextPayload:    stampActionValue(actionPayloadPathPickerCursor(lane.PickerID, lane.FieldName, page.NextCursor), daemonLifecycleID),
		Page:           page,
		PaginationHint: pathPickerPaginationHint,
	})...)
	return elements
}

func pathPickerPlanSingleLane(
	view control.FeishuPathPickerView,
	lane pathPickerPaginatedLane,
	build func(page paginatedSelectPage) []map[string]any,
) paginatedSelectPlan {
	return planPaginatedSelectPage(
		pathPickerLaneSpec(lane),
		cardtransport.InteractiveCardTransportLimitBytes,
		func(page paginatedSelectPage) (int, error) {
			return pathPickerCardSize(view, build(page))
		},
	)
}

func pathPickerPlanFileModeDualLanes(
	view control.FeishuPathPickerView,
	daemonLifecycleID string,
	leftLane, rightLane pathPickerPaginatedLane,
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
		return planPaginatedSelectPage(pathPickerLaneSpec(leftLane), maxBytes, func(page paginatedSelectPage) (int, error) {
			size, err := pathPickerCardSize(view, pathPickerFileModeElementsWithPages(view, daemonLifecycleID, &page, nil))
			if err != nil {
				return 0, err
			}
			return maxInt(size-baseSize, 0), nil
		})
	}
	rightFit := func(maxBytes int) paginatedSelectPlan {
		return planPaginatedSelectPage(pathPickerLaneSpec(rightLane), maxBytes, func(page paginatedSelectPage) (int, error) {
			size, err := pathPickerCardSize(view, pathPickerFileModeElementsWithPages(view, daemonLifecycleID, nil, &page))
			if err != nil {
				return 0, err
			}
			return maxInt(size-baseSize, 0), nil
		})
	}

	leftPlan, rightPlan := planBorrowedDualSelectPages(available, 1, 2, leftFit, rightFit)
	return pathPickerTightenFileModeDualPlans(view, daemonLifecycleID, leftLane, rightLane, leftFit, rightFit, leftPlan, rightPlan)
}

func pathPickerTightenFileModeDualPlans(
	view control.FeishuPathPickerView,
	daemonLifecycleID string,
	leftLane, rightLane pathPickerPaginatedLane,
	leftFit, rightFit paginatedSelectFit,
	leftPlan, rightPlan paginatedSelectPlan,
) (paginatedSelectPlan, paginatedSelectPlan) {
	for i := 0; i < 64; i++ {
		size, err := pathPickerCardSize(view, pathPickerFileModeElementsWithPages(view, daemonLifecycleID, &leftPlan.Page, &rightPlan.Page))
		if err != nil || size <= cardtransport.InteractiveCardTransportLimitBytes {
			return leftPlan, rightPlan
		}

		bestLeft, bestRight, bestSize, shrunk := leftPlan, rightPlan, size, false
		if next, ok := pathPickerShrinkSelectPlan(leftPlan, leftFit); ok {
			nextSize, nextErr := pathPickerCardSize(view, pathPickerFileModeElementsWithPages(view, daemonLifecycleID, &next.Page, &rightPlan.Page))
			if nextErr == nil && nextSize < bestSize {
				bestLeft, bestRight, bestSize, shrunk = next, rightPlan, nextSize, true
			}
		}
		if next, ok := pathPickerShrinkSelectPlan(rightPlan, rightFit); ok {
			nextSize, nextErr := pathPickerCardSize(view, pathPickerFileModeElementsWithPages(view, daemonLifecycleID, &leftPlan.Page, &next.Page))
			if nextErr == nil && nextSize < bestSize {
				bestLeft, bestRight, bestSize, shrunk = leftPlan, next, nextSize, true
			}
		}
		if !shrunk {
			return leftPlan, rightPlan
		}
		leftPlan, rightPlan = bestLeft, bestRight
	}
	return leftPlan, rightPlan
}

func pathPickerShrinkSelectPlan(current paginatedSelectPlan, fit paginatedSelectFit) (paginatedSelectPlan, bool) {
	if fit == nil {
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
