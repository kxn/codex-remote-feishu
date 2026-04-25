package projector

import (
	"fmt"
	"strings"
	"testing"

	cardtransport "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardtransport"
)

func TestPlanPaginatedSelectPageNormalizesCursorAndComputesPrevNext(t *testing.T) {
	spec := paginatedSelectPageSpec{
		Cursor:           4,
		CandidateOptions: selectPaginationTestOptions(10, "会话"),
	}
	measure := func(page paginatedSelectPage) (int, error) {
		size := page.PageOptionCount * 20
		if page.HasPrev {
			size += 7
		}
		if page.HasNext {
			size += 11
		}
		return size, nil
	}

	plan := planPaginatedSelectPage(spec, 78, measure)
	if plan.Page.Cursor != 3 {
		t.Fatalf("expected normalized cursor 3, got %d", plan.Page.Cursor)
	}
	if !plan.Page.HasPrev || plan.Page.PrevCursor != 0 {
		t.Fatalf("expected previous cursor 0, got %#v", plan.Page)
	}
	if !plan.Page.HasNext || plan.Page.NextCursor != 6 {
		t.Fatalf("expected next cursor 6, got %#v", plan.Page)
	}
	if plan.Page.PageOptionCount != 3 {
		t.Fatalf("expected 3 options on the page, got %#v", plan.Page)
	}
}

func TestPlanPaginatedSelectPageKeepsPinnedOptionsAndOnlyShowsVisibleInitialOption(t *testing.T) {
	spec := paginatedSelectPageSpec{
		FixedOptions: []map[string]any{
			selectPaginationTestOption("当前目录", "."),
			selectPaginationTestOption("返回上级", ".."),
		},
		CandidateOptions: selectPaginationTestOptions(8, "目录"),
		SelectedValue:    "目录-07",
	}
	measure := func(page paginatedSelectPage) (int, error) {
		size := page.FixedOptionCount*5 + page.PageOptionCount*18
		if page.HasPrev {
			size += 7
		}
		if page.HasNext {
			size += 11
		}
		return size, nil
	}

	firstPlan := planPaginatedSelectPage(spec, 70, measure)
	if firstPlan.Page.FixedOptionCount != 2 {
		t.Fatalf("expected fixed options to remain visible, got %#v", firstPlan.Page)
	}
	if got := selectOptionValue(firstPlan.Page.VisibleOptions[0]); got != "." {
		t.Fatalf("expected first fixed option to stay visible, got %q", got)
	}
	if firstPlan.Page.InitialOption != "" {
		t.Fatalf("expected off-page selection to stay hidden, got %#v", firstPlan.Page)
	}

	selectedPage := planPaginatedSelectPage(paginatedSelectPageSpec{
		Cursor:           7,
		FixedOptions:     spec.FixedOptions,
		CandidateOptions: spec.CandidateOptions,
		SelectedValue:    spec.SelectedValue,
	}, 70, measure)
	if selectedPage.Page.InitialOption != "目录-07" {
		t.Fatalf("expected visible selection to become initial option, got %#v", selectedPage.Page)
	}
}

func TestRenderPaginatedSelectElementsUsesSingleRowControlsAndHint(t *testing.T) {
	elements := renderPaginatedSelectElements(paginatedSelectRenderSpec{
		Name:          "session",
		Placeholder:   "选择会话",
		SelectPayload: map[string]any{"kind": "select"},
		PrevPayload:   map[string]any{"cursor": 0},
		NextPayload:   map[string]any{"cursor": 6},
		Page: paginatedSelectPage{
			VisibleOptions: []map[string]any{
				selectPaginationTestOption("会话 A", "a"),
				selectPaginationTestOption("会话 B", "b"),
			},
			HasPrev:            true,
			HasNext:            true,
			PrevCursor:         0,
			NextCursor:         6,
			ShowPaginationHint: true,
		},
		PaginationHint: "超出卡片大小，如未找到请翻页。",
	})
	if len(elements) != 2 {
		t.Fatalf("expected row + hint, got %#v", elements)
	}
	row := elements[0]
	if got := cardStringValue(row["tag"]); got != "column_set" {
		t.Fatalf("expected pagination row column_set, got %#v", row)
	}
	columns, _ := row["columns"].([]map[string]any)
	if len(columns) != 3 {
		t.Fatalf("expected prev/select/next columns, got %#v", row)
	}
	if label := cardButtonLabel(t, columns[0]["elements"].([]map[string]any)[0]); label != "<" {
		t.Fatalf("expected previous button, got %#v", columns[0])
	}
	selectElement := columns[1]["elements"].([]map[string]any)[0]
	if got := cardStringValue(selectElement["tag"]); got != "select_static" {
		t.Fatalf("expected middle column to hold select_static, got %#v", selectElement)
	}
	if label := cardButtonLabel(t, columns[2]["elements"].([]map[string]any)[0]); label != ">" {
		t.Fatalf("expected next button, got %#v", columns[2])
	}
	if block := elements[1]; cardStringValue(block["tag"]) != "div" {
		t.Fatalf("expected pagination hint block, got %#v", block)
	}
}

func TestPlanBorrowedDualSelectPagesReallocatesUnusedBudget(t *testing.T) {
	rightBudgets := make([]int, 0, 2)
	leftFit := func(maxBytes int) paginatedSelectPlan {
		return paginatedSelectPlan{
			UsedBytes: maxBytes / 3,
			Complete:  true,
		}
	}
	rightFit := func(maxBytes int) paginatedSelectPlan {
		rightBudgets = append(rightBudgets, maxBytes)
		used := maxBytes
		if used > 260 {
			used = 260
		}
		return paginatedSelectPlan{
			UsedBytes: used,
			Complete:  maxBytes >= 260,
		}
	}

	leftPlan, rightPlan := planBorrowedDualSelectPages(300, 1, 2, leftFit, rightFit)
	if leftPlan.BudgetBytes != 100 {
		t.Fatalf("expected left target budget 100, got %#v", leftPlan)
	}
	if rightPlan.BudgetBytes != 267 {
		t.Fatalf("expected right borrowed budget 267, got %#v", rightPlan)
	}
	if len(rightBudgets) != 2 || rightBudgets[0] != 200 || rightBudgets[1] != 267 {
		t.Fatalf("expected right fit to rerun with borrowed budget, got %#v", rightBudgets)
	}
	if !rightPlan.Complete {
		t.Fatalf("expected borrowed budget to complete the right page, got %#v", rightPlan)
	}
}

func TestPlanPaginatedSelectPageFitsNearTransportLimit(t *testing.T) {
	spec := paginatedSelectPageSpec{
		FixedOptions: []map[string]any{
			selectPaginationTestOption("当前目录（固定）", "."),
			selectPaginationTestOption("返回上级（固定）", ".."),
		},
		CandidateOptions: selectPaginationLongOptions(220),
		SelectedValue:    "dir-999",
	}
	measure := func(page paginatedSelectPage) (int, error) {
		elements := []map[string]any{
			{
				"tag":     "markdown",
				"content": "**当前目录**",
			},
			cardPlainTextBlockElement("/workspace/alpha/beta/gamma"),
			{
				"tag":     "markdown",
				"content": "**进入目录**",
			},
		}
		elements = append(elements, renderPaginatedSelectElements(paginatedSelectRenderSpec{
			Name:           "directory",
			Placeholder:    "选择目录",
			SelectPayload:  map[string]any{"kind": "enter"},
			PrevPayload:    map[string]any{"cursor": page.PrevCursor},
			NextPayload:    map[string]any{"cursor": page.NextCursor},
			Page:           page,
			PaginationHint: "超出卡片大小，如未找到请翻页。",
		})...)
		elements = appendCardFooterButtonGroup(elements, []map[string]any{
			cardCallbackButtonElement("确认", "primary", map[string]any{"kind": "confirm"}, false, ""),
			cardCallbackButtonElement("取消", "default", map[string]any{"kind": "cancel"}, false, ""),
		})
		return cardtransport.InteractiveMessageCardSize("选择路径", "", cardThemeInfo, elements, true)
	}

	allPage := buildPaginatedSelectPage(spec, 0, 0, len(spec.CandidateOptions))
	allSize, err := measure(allPage)
	if err != nil {
		t.Fatalf("measure full option set: %v", err)
	}
	if allSize <= cardtransport.InteractiveCardTransportLimitBytes {
		t.Fatalf("expected full option set to exceed transport limit, got %d", allSize)
	}

	plan := planPaginatedSelectPage(spec, cardtransport.InteractiveCardTransportLimitBytes, measure)
	if plan.UsedBytes > cardtransport.InteractiveCardTransportLimitBytes {
		t.Fatalf("expected paginated page to fit transport budget, got %#v", plan)
	}
	if !plan.Page.HasNext {
		t.Fatalf("expected near-limit page to paginate, got %#v", plan.Page)
	}
	if plan.Page.FixedOptionCount != 2 {
		t.Fatalf("expected fixed options to stay visible, got %#v", plan.Page)
	}
	if !strings.Contains(fmt.Sprint(plan.Page.VisibleOptions[0]), "当前目录") {
		t.Fatalf("expected fixed option to stay on page, got %#v", plan.Page.VisibleOptions)
	}
}

func selectPaginationTestOptions(count int, prefix string) []map[string]any {
	options := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		value := fmt.Sprintf("%s-%02d", prefix, i)
		options = append(options, selectPaginationTestOption(value, value))
	}
	return options
}

func selectPaginationLongOptions(count int) []map[string]any {
	options := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		value := fmt.Sprintf("dir-%03d", i)
		label := fmt.Sprintf("目录 %03d %s", i, strings.Repeat("x", 120))
		options = append(options, selectPaginationTestOption(label, value))
	}
	return options
}

func selectPaginationTestOption(label, value string) map[string]any {
	return map[string]any{
		"text":  cardPlainText(label),
		"value": value,
	}
}

func cardButtonLabel(t *testing.T, button map[string]any) string {
	t.Helper()
	text, _ := button["text"].(map[string]any)
	return cardStringValue(text["content"])
}
