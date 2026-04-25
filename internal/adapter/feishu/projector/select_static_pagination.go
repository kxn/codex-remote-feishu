package projector

import "strings"

type paginatedSelectPageSpec struct {
	Cursor           int
	FixedOptions     []map[string]any
	CandidateOptions []map[string]any
	SelectedValue    string
}

type paginatedSelectPage struct {
	Cursor             int
	VisibleOptions     []map[string]any
	InitialOption      string
	FixedOptionCount   int
	PageOptionCount    int
	HasPrev            bool
	PrevCursor         int
	HasNext            bool
	NextCursor         int
	ShowPaginationHint bool
}

type paginatedSelectPlan struct {
	Page        paginatedSelectPage
	BudgetBytes int
	UsedBytes   int
	Complete    bool
}

type paginatedSelectMeasure func(page paginatedSelectPage) (int, error)

type paginatedSelectFit func(maxBytes int) paginatedSelectPlan

type paginatedSelectRenderSpec struct {
	Name           string
	Placeholder    string
	SelectPayload  map[string]any
	PrevPayload    map[string]any
	NextPayload    map[string]any
	Page           paginatedSelectPage
	PaginationHint string
}

func planPaginatedSelectPage(spec paginatedSelectPageSpec, maxBytes int, measure paginatedSelectMeasure) paginatedSelectPlan {
	spec = normalizePaginatedSelectPageSpec(spec)
	if len(spec.CandidateOptions) == 0 {
		page := buildPaginatedSelectPage(spec, 0, 0, 0)
		used, _ := measurePaginatedSelectPage(measure, page)
		return paginatedSelectPlan{
			Page:        page,
			BudgetBytes: maxBytes,
			UsedBytes:   used,
			Complete:    true,
		}
	}

	cursor, prevCursor := normalizePaginatedSelectCursor(spec, maxBytes, measure)
	plan := fitPaginatedSelectPageAtCursor(spec, cursor, prevCursor, maxBytes, measure)
	plan.BudgetBytes = maxBytes
	return plan
}

func planBorrowedDualSelectPages(totalBudget, leftWeight, rightWeight int, leftFit, rightFit paginatedSelectFit) (paginatedSelectPlan, paginatedSelectPlan) {
	totalBudget = maxInt(totalBudget, 0)
	leftWeight = maxInt(leftWeight, 1)
	rightWeight = maxInt(rightWeight, 1)
	sum := leftWeight + rightWeight
	leftBudget := totalBudget * leftWeight / sum
	rightBudget := totalBudget - leftBudget

	leftPlan := leftFit(leftBudget)
	leftPlan.BudgetBytes = leftBudget
	rightPlan := rightFit(rightBudget)
	rightPlan.BudgetBytes = rightBudget

	switch {
	case leftPlan.Complete && !rightPlan.Complete && leftPlan.UsedBytes < leftBudget:
		rightBudget += leftBudget - leftPlan.UsedBytes
		rightPlan = rightFit(rightBudget)
		rightPlan.BudgetBytes = rightBudget
	case rightPlan.Complete && !leftPlan.Complete && rightPlan.UsedBytes < rightBudget:
		leftBudget += rightBudget - rightPlan.UsedBytes
		leftPlan = leftFit(leftBudget)
		leftPlan.BudgetBytes = leftBudget
	}

	return leftPlan, rightPlan
}

func renderPaginatedSelectElements(spec paginatedSelectRenderSpec) []map[string]any {
	selectElement := selectStaticElement(
		spec.Name,
		spec.Placeholder,
		spec.SelectPayload,
		spec.Page.VisibleOptions,
		spec.Page.InitialOption,
	)
	if len(selectElement) == 0 {
		return nil
	}
	if !spec.Page.HasPrev && !spec.Page.HasNext {
		return []map[string]any{selectElement}
	}

	row := map[string]any{
		"tag":                "column_set",
		"horizontal_spacing": "small",
		"columns":            paginatedSelectRowColumns(selectElement, spec),
	}
	elements := []map[string]any{row}
	if spec.Page.ShowPaginationHint {
		hint := strings.TrimSpace(spec.PaginationHint)
		if hint == "" {
			hint = "选项过多，如未找到请翻页。"
		}
		if block := cardPlainTextBlockElement(hint); len(block) != 0 {
			elements = append(elements, block)
		}
	}
	return elements
}

func paginatedSelectRowColumns(selectElement map[string]any, spec paginatedSelectRenderSpec) []map[string]any {
	columns := make([]map[string]any, 0, 3)
	if spec.Page.HasPrev && len(spec.PrevPayload) != 0 {
		columns = append(columns, paginatedSelectButtonColumn(
			cardCallbackButtonElement("<", "default", spec.PrevPayload, false, "fill"),
		))
	}
	columns = append(columns, map[string]any{
		"tag":            "column",
		"width":          "weighted",
		"weight":         5,
		"vertical_align": "top",
		"elements":       []map[string]any{cloneCardMap(selectElement)},
	})
	if spec.Page.HasNext && len(spec.NextPayload) != 0 {
		columns = append(columns, paginatedSelectButtonColumn(
			cardCallbackButtonElement(">", "default", spec.NextPayload, false, "fill"),
		))
	}
	return columns
}

func paginatedSelectButtonColumn(button map[string]any) map[string]any {
	return map[string]any{
		"tag":            "column",
		"width":          "auto",
		"vertical_align": "top",
		"elements":       []map[string]any{button},
	}
}

func normalizePaginatedSelectPageSpec(spec paginatedSelectPageSpec) paginatedSelectPageSpec {
	spec.SelectedValue = strings.TrimSpace(spec.SelectedValue)
	return spec
}

func normalizePaginatedSelectCursor(spec paginatedSelectPageSpec, maxBytes int, measure paginatedSelectMeasure) (int, int) {
	if len(spec.CandidateOptions) == 0 {
		return 0, 0
	}
	requested := spec.Cursor
	if requested < 0 {
		requested = 0
	}
	if requested >= len(spec.CandidateOptions) {
		requested = len(spec.CandidateOptions) - 1
	}

	start := 0
	prev := 0
	for {
		plan := fitPaginatedSelectPageAtCursor(spec, start, prev, maxBytes, measure)
		next := plan.Page.NextCursor
		if !plan.Page.HasNext || next <= start {
			return start, prev
		}
		if next > requested {
			return start, prev
		}
		prev = start
		start = next
	}
}

func fitPaginatedSelectPageAtCursor(spec paginatedSelectPageSpec, cursor, prevCursor, maxBytes int, measure paginatedSelectMeasure) paginatedSelectPlan {
	remaining := len(spec.CandidateOptions) - cursor
	if remaining <= 0 {
		page := buildPaginatedSelectPage(spec, cursor, prevCursor, 0)
		used, _ := measurePaginatedSelectPage(measure, page)
		return paginatedSelectPlan{
			Page:        page,
			BudgetBytes: maxBytes,
			UsedBytes:   used,
			Complete:    true,
		}
	}

	bestCount := 0
	bestUsed := 0
	found := false
	for count := 1; count <= remaining; count++ {
		page := buildPaginatedSelectPage(spec, cursor, prevCursor, count)
		used, err := measurePaginatedSelectPage(measure, page)
		if err != nil {
			continue
		}
		if used <= maxBytes {
			bestCount = count
			bestUsed = used
			found = true
		}
	}
	if !found {
		bestCount = 1
		page := buildPaginatedSelectPage(spec, cursor, prevCursor, bestCount)
		bestUsed, _ = measurePaginatedSelectPage(measure, page)
		return paginatedSelectPlan{
			Page:        page,
			BudgetBytes: maxBytes,
			UsedBytes:   bestUsed,
			Complete:    cursor+bestCount >= len(spec.CandidateOptions),
		}
	}

	page := buildPaginatedSelectPage(spec, cursor, prevCursor, bestCount)
	return paginatedSelectPlan{
		Page:        page,
		BudgetBytes: maxBytes,
		UsedBytes:   bestUsed,
		Complete:    cursor+bestCount >= len(spec.CandidateOptions),
	}
}

func buildPaginatedSelectPage(spec paginatedSelectPageSpec, cursor, prevCursor, pageCount int) paginatedSelectPage {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(spec.CandidateOptions) {
		cursor = len(spec.CandidateOptions)
	}
	if pageCount < 0 {
		pageCount = 0
	}
	if cursor+pageCount > len(spec.CandidateOptions) {
		pageCount = len(spec.CandidateOptions) - cursor
	}

	visibleOptions := make([]map[string]any, 0, len(spec.FixedOptions)+pageCount)
	for _, option := range spec.FixedOptions {
		visibleOptions = append(visibleOptions, cloneCardMap(option))
	}
	for _, option := range spec.CandidateOptions[cursor : cursor+pageCount] {
		visibleOptions = append(visibleOptions, cloneCardMap(option))
	}

	nextCursor := cursor + pageCount
	hasPrev := cursor > 0
	hasNext := nextCursor < len(spec.CandidateOptions)
	return paginatedSelectPage{
		Cursor:             cursor,
		VisibleOptions:     visibleOptions,
		InitialOption:      visibleSelectOptionValue(visibleOptions, spec.SelectedValue),
		FixedOptionCount:   len(spec.FixedOptions),
		PageOptionCount:    pageCount,
		HasPrev:            hasPrev,
		PrevCursor:         prevCursor,
		HasNext:            hasNext,
		NextCursor:         nextCursor,
		ShowPaginationHint: hasPrev || hasNext,
	}
}

func visibleSelectOptionValue(options []map[string]any, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, option := range options {
		if selectOptionValue(option) == value {
			return value
		}
	}
	return ""
}

func measurePaginatedSelectPage(measure paginatedSelectMeasure, page paginatedSelectPage) (int, error) {
	if measure == nil {
		return 0, nil
	}
	return measure(page)
}

func selectStaticElement(name, placeholder string, payload map[string]any, options []map[string]any, initialOption string) map[string]any {
	element := map[string]any{
		"tag":         "select_static",
		"name":        strings.TrimSpace(name),
		"placeholder": cardPlainText(placeholder),
		"options":     cloneCardAny(options),
		"behaviors": []map[string]any{{
			"type":  "callback",
			"value": cloneCardMap(payload),
		}},
	}
	if initial := visibleSelectOptionValue(options, initialOption); initial != "" {
		element["initial_option"] = initial
	}
	return element
}

func selectOptionValue(option map[string]any) string {
	return strings.TrimSpace(cardStringValue(option["value"]))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
