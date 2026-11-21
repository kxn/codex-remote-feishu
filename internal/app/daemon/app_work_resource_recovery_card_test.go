package daemon

import (
	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"strings"
	"testing"
)

func TestResourceRecoveryConfirmationBoundsPreviewWithoutHidingOmissions(t *testing.T) {
	inspection := workservice.ResourceRecoveryInspection{}
	inspection.Observation.Resource.CanonicalURI = "https://a.feishu.cn/docx/actual?from=" + strings.Repeat("x", 50000)
	for range 50 {
		inspection.Data.Documents = append(inspection.Data.Documents, workservice.ResourceDocumentInspection{RequestedRevision: 9, Blocks: []workservice.ResourceDocumentBlock{{Runs: []workservice.ResourceDocumentRun{{Kind: "text", Summary: strings.Repeat("文", 160)}, {Kind: "text", Summary: strings.Repeat("字", 160)}, {Kind: "text", Summary: "more"}}}}})
	}
	sheet := workservice.ResourceSheetInspection{Values: feishu.ProjectSheetValues{Range: "sheet!A1:CV100"}}
	for range 100 {
		row := []any{}
		for range 100 {
			row = append(row, strings.Repeat("格", 1000))
		}
		sheet.Values.Values = append(sheet.Values.Values, row)
	}
	inspection.Data.Sheet = &sheet
	paragraphs := resourceRecoveryPreview(inspection)
	card, err := workui.Card("view", workui.Panel{Title: "核对", Paragraphs: paragraphs, Buttons: []workui.Button{{Label: "采用已核对状态，不重试原请求", Action: "resource.write.adopt"}}})
	if err != nil || len(card) > 30000 {
		t.Fatalf("actual recovery card exceeds Feishu limit: %d %v", len(card), err)
	}
	text := strings.Join(paragraphs, "\n")
	for _, required := range []string{"50个指定段落", "仅展示前8个摘要", "前6行", "更多列", "不撤销原请求", "不能保证原请求不再迟到生效"} {
		if !strings.Contains(text, required) {
			t.Fatalf("missing explicit scope/omission warning %q", required)
		}
	}
	if strings.Contains(text, strings.Repeat("x", 500)) {
		t.Fatal("tracking query leaked into human source link")
	}
	if !json.Valid(card) {
		t.Fatal("invalid card JSON")
	}
}
