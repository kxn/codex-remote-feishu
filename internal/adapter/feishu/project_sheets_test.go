package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"
)

func TestProjectSheetsSDKMetadataAndSheetIDs(t *testing.T) {
	step := 0
	fake := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		step++
		if r.Method != "GET" {
			t.Errorf("method %s", r.Method)
		}
		if step == 1 {
			if r.URL.Path != "/open-apis/sheets/v3/spreadsheets/sheet-book" || r.URL.Query().Get("user_id_type") != "open_id" {
				t.Errorf("metadata %s?%s", r.URL.Path, r.URL.RawQuery)
			}
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"spreadsheet":{"token":"sheet-book","title":"Project plan","owner_id":"ou_owner","url":"https://example.feishu.cn/sheets/sheet-book"}}}`)
		} else {
			if r.URL.Path != "/open-apis/sheets/v3/spreadsheets/sheet-book/sheets/query" {
				t.Errorf("sheet metadata %s", r.URL.Path)
			}
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"sheets":[{"sheet_id":"tab-plan","title":"Plan","index":0,"grid_properties":{"row_count":100,"column_count":10}}]}}`)
		}
	})
	api := NewProjectSheetsAPI(fake.broker)
	ctx := context.Background()
	metadata, err := api.GetSpreadsheet(ctx, "sheet-book")
	if err != nil || metadata.SpreadsheetToken != "sheet-book" || metadata.OwnerID != "ou_owner" {
		t.Fatalf("metadata %#v, %v", metadata, err)
	}
	sheets, err := api.ListSpreadsheetSheets(ctx, "sheet-book")
	if err != nil || len(sheets) != 1 || scopeStringValue(sheets[0].SheetId) != "tab-plan" {
		t.Fatalf("sheets %#v, %v", sheets, err)
	}
}

func TestProjectSheetsReadPreservesFormulaNumericAndRevisionEvidence(t *testing.T) {
	fake := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/open-apis/sheets/v2/spreadsheets/sheet-book/values/tab-plan!A1:B2" {
			t.Errorf("range URL %s %s", r.Method, r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("valueRenderOption") != "Formula" || query.Get("dateTimeRenderOption") != "FormattedString" || query.Get("user_id_type") != "open_id" {
			t.Errorf("render query %s", r.URL.RawQuery)
		}
		w.Header().Set("X-Tt-Logid", "range-evidence")
		_, _ = fmt.Fprint(w, `{"code":0,"data":{"spreadsheetToken":"sheet-book","revision":9007199254740993,"valueRange":{"range":"tab-plan!A1:B2","revision":"range-version","majorDimension":"ROWS","values":[["=SUM(C1:C3)",9007199254740993],[{"type":"text","text":"rich"},null]]},"custom_evidence":"retained"}}`)
	})
	api := NewProjectSheetsAPI(fake.broker)
	values, err := api.ReadSheetRange(context.Background(), ProjectSheetRangeRequest{SpreadsheetToken: "sheet-book", SheetID: "tab-plan", Range: "A1:B2", ValueRenderOption: "Formula", DateTimeRenderOption: "FormattedString"})
	if err != nil {
		t.Fatal(err)
	}
	if values.Values[0][0] != "=SUM(C1:C3)" || values.Values[0][1] != json.Number("9007199254740993") || string(values.Revision) != "9007199254740993" || string(values.RangeRevision) != `"range-version"` || values.RequestID != "range-evidence" || values.ValueRenderOption != "Formula" {
		t.Fatalf("evidence %#v", values)
	}
	var raw map[string]any
	if err := json.Unmarshal(values.RawData, &raw); err != nil || raw["custom_evidence"] != "retained" {
		t.Fatalf("raw evidence %s", values.RawData)
	}
}

func TestProjectSheetsWriteExactRangeAndPreservesCellRepresentations(t *testing.T) {
	fake := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.URL.Path != "/open-apis/sheets/v2/spreadsheets/sheet-book/values" {
			t.Errorf("write %s %s", r.Method, r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("invented idempotency/CAS query %s", r.URL.RawQuery)
		}
		body := projectReadJSON(t, r)
		valueRange := body["valueRange"].(map[string]any)
		expected := []any{[]any{"Human note", map[string]any{"type": "formula", "text": "=SUM(C1:C3)"}}}
		if len(body) != 1 || valueRange["range"] != "tab-plan!A1:B1" || !reflect.DeepEqual(valueRange["values"], expected) {
			t.Errorf("exact write %#v", body)
		}
		_, _ = fmt.Fprint(w, `{"code":0,"data":{"spreadsheetToken":"sheet-book","revision":21,"updatedRange":"tab-plan!A1:B1","updatedRows":1,"updatedColumns":2,"updatedCells":2}}`)
	})
	api := NewProjectSheetsAPI(fake.broker)
	values, err := api.WriteSheetRange(context.Background(), ProjectSheetWriteRequest{SpreadsheetToken: "sheet-book", SheetID: "tab-plan", Range: "tab-plan!A1:B1", Values: [][]any{{"Human note", map[string]any{"type": "formula", "text": "=SUM(C1:C3)"}}}})
	if err != nil || values.Range != "tab-plan!A1:B1" || string(values.Revision) != "21" || values.UpdatedCells == nil || *values.UpdatedCells != 2 || values.Values != nil {
		t.Fatalf("write receipt %#v, %v", values, err)
	}
}

func TestProjectSheetsRejectsUnboundedMismatchedAndInjectedRanges(t *testing.T) {
	calls := 0
	fake := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) { calls++; _, _ = fmt.Fprint(w, `{"code":0,"data":{}}`) })
	api := NewProjectSheetsAPI(fake.broker)
	for _, cellRange := range []string{"", "A:A", "1:10", "A0:B1", "B2:A1", "other-sheet!A1:B1", "A1:B1/escape", "A1:B1?x=y"} {
		if _, err := api.ReadSheetRange(context.Background(), ProjectSheetRangeRequest{SpreadsheetToken: "sheet-book", SheetID: "tab-plan", Range: cellRange}); err == nil {
			t.Errorf("accepted %q", cellRange)
		}
	}
	if _, err := api.WriteSheetRange(context.Background(), ProjectSheetWriteRequest{SpreadsheetToken: "sheet-book", SheetID: "tab-plan", Range: "A1:B2", Values: [][]any{{"one", "row"}}}); err == nil {
		t.Error("accepted partial matrix")
	}
	if _, err := api.ReadSheetRange(context.Background(), ProjectSheetRangeRequest{SpreadsheetToken: "https://example.feishu.cn/wiki/node", SheetID: "tab-plan", Range: "A1"}); err == nil {
		t.Error("accepted wiki URL as spreadsheet token")
	}
	if calls != 0 {
		t.Fatalf("invalid inputs sent %d requests", calls)
	}
}
