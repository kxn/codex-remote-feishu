package projector

import (
	"strings"

	cardtransport "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardtransport"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/selectflow"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func paginatedThreadSelectionDropdownElements(
	selectionView control.FeishuSelectionView,
	semantics control.FeishuSelectionSemantics,
	daemonLifecycleID string,
) []map[string]any {
	view := *selectionView.Thread
	elements := selectionViewStructuredContextElements(semantics)

	options := make([]map[string]any, 0, len(view.Entries))
	selectedValue := ""
	hiddenCount := 0
	allowCrossWorkspace := false
	for _, entry := range view.Entries {
		if entry.Disabled && !entry.Current {
			hiddenCount++
			continue
		}
		value := strings.TrimSpace(entry.ThreadID)
		label := strings.TrimSpace(firstNonEmpty(entry.Summary, entry.ThreadID))
		if value == "" || label == "" {
			continue
		}
		options = append(options, map[string]any{
			"text":  cardPlainText(label),
			"value": value,
		})
		if entry.Current {
			selectedValue = value
		}
		if entry.AllowCrossWorkspace {
			allowCrossWorkspace = true
		}
	}

	if len(options) == 0 {
		if hiddenCount != 0 {
			if block := cardPlainTextBlockElement("当前没有可切换的会话。"); len(block) != 0 {
				elements = append(elements, block)
			}
		}
		return elements
	}

	hiddenHint := ""
	if hiddenCount != 0 {
		hiddenHint = firstNonEmpty(strings.TrimSpace(semantics.HiddenEntriesNotice), "已省略当前不可切换的会话。")
	}
	lane := threadSelectionLane(selectionView, view.Mode, view.Cursor, selectedValue, options, allowCrossWorkspace)
	plan := planPaginatedSelectLane(
		cardtransport.InteractiveCardTransportLimitBytes,
		lane,
		func(page paginatedSelectPage) (int, error) {
			return threadSelectionCardSize(
				semantics,
				threadSelectionDropdownElementsWithPage(lane, daemonLifecycleID, page, hiddenHint),
			)
		},
	)
	return append(
		elements,
		threadSelectionDropdownElementsWithPage(lane, daemonLifecycleID, plan.Page, hiddenHint)...,
	)
}

func threadSelectionLane(
	selectionView control.FeishuSelectionView,
	mode control.FeishuThreadSelectionViewMode,
	cursor int,
	selectedValue string,
	options []map[string]any,
	allowCrossWorkspace bool,
) paginatedSelectFlowLane {
	return paginatedSelectFlowLane{
		Flow:          selectflow.ThreadSelectionFlow,
		Label:         "会话",
		Placeholder:   "选择会话",
		Cursor:        cursor,
		SelectedValue: selectedValue,
		Options:       options,
		SelectPayload: actionPayloadWithCatalog(
			actionPayloadUseThreadField(selectflow.ThreadSelectionFlow.FieldName, allowCrossWorkspace),
			selectionView.CatalogFamilyID,
			selectionView.CatalogVariantID,
			string(selectionView.CatalogBackend),
		),
		PagePayload: func(cursor int) map[string]any {
			return actionPayloadWithCatalog(
				actionPayloadThreadSelectionCursor(string(mode), cursor),
				selectionView.CatalogFamilyID,
				selectionView.CatalogVariantID,
				string(selectionView.CatalogBackend),
			)
		},
	}
}

func threadSelectionDropdownElementsWithPage(
	lane paginatedSelectFlowLane,
	daemonLifecycleID string,
	page paginatedSelectPage,
	hiddenHint string,
) []map[string]any {
	elements := lane.renderElements(daemonLifecycleID, page)
	if strings.TrimSpace(hiddenHint) != "" {
		if block := cardPlainTextBlockElement(hiddenHint); len(block) != 0 {
			elements = append(elements, block)
		}
	}
	return elements
}

func threadSelectionCardSize(semantics control.FeishuSelectionSemantics, elements []map[string]any) (int, error) {
	return cardtransport.InteractiveMessageCardSize(
		firstNonEmpty(strings.TrimSpace(semantics.Title), "选择会话"),
		"",
		cardThemeInfo,
		cloneCardElementSlice(elements),
		true,
	)
}
