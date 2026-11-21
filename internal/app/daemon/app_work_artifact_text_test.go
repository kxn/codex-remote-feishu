package daemon

import (
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
	"testing"
	"time"
)

func TestSavedArtifactReadsDocumentHeadingsAndTableWithoutMovingSource(t *testing.T) {
	content := work.NewContent("doc", "scope", "application/json", []byte(`{"blocks":[{"block_id":"root","page":{},"children":["title","table"]},{"block_id":"title","heading1":{"elements":[{"text_run":{"content":"旧版方案"}}]}},{"block_id":"table","table":{"property":{"column_size":2},"cells":["a","b"]}},{"block_id":"a","table_cell":{},"children":["ta"]},{"block_id":"b","table_cell":{},"children":["tb"]},{"block_id":"ta","text":{"elements":[{"text_run":{"content":"名称"}}]}},{"block_id":"tb","text":{"elements":[{"text_run":{"content":"第一版"}}]}}]}`), time.Now())
	text, err := renderSavedArtifact(content)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "旧版方案") || !strings.Contains(text, "名称 | 第一版") || strings.Contains(text, "block_id") {
		t.Fatalf("unreadable artifact %s", text)
	}
}
func TestSavedArtifactPreservesSheetRegionValuesAndFormulaAsHumanText(t *testing.T) {
	content := work.NewContent("sheet", "scope", "application/json", []byte(`{"values":{"Range":"sheet!A5:B6","Values":[["人数","结论"],[9007199254740993,"通过"]]},"formulas":{"Range":"sheet!A5:B6","Values":[["人数","结论"],["=SUM(A1:A4)","通过"]]}}`), time.Now())
	text, err := renderSavedArtifact(content)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"sheet!A5:B6", "人数 | 结论", "9007199254740993", "=SUM(A1:A4)"} {
		if !strings.Contains(text, want) {
			t.Fatalf("lost %s in%s", want, text)
		}
	}
	if strings.Contains(text, `"Values"`) {
		t.Fatal("raw JSON shown as document")
	}
}
