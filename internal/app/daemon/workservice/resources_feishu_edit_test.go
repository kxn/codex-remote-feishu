package workservice

import (
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"net/http"
	"strings"
	"testing"
)

const editableDocBlock = `{"block_id":"block","block_type":2,"text":{"elements":[{"text_run":{"content":"first","text_element_style":{"bold":true}}},{"mention_doc":{"token":"linked","obj_type":22}},{"text_run":{"content":"untouched","text_element_style":{"italic":true}}}]}}`

func TestResourceDocumentInspectAndRunUpdatePreserveOtherElements(t *testing.T) {
	writes := 0
	connector := platformConnector(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/open-apis/docx/v1/documents/document":
			fmt.Fprint(w, `{"code":0,"data":{"document":{"document_id":"document","revision_id":7}}}`)
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/blocks/block"):
			if r.URL.Query().Get("document_revision_id") != "7" {
				t.Error("block read lost frozen revision")
			}
			fmt.Fprintf(w, `{"code":0,"data":{"block":%s}}`, editableDocBlock)
		case r.Method == "PATCH" && strings.HasSuffix(r.URL.Path, "/blocks/batch_update"):
			writes++
			if r.URL.Query().Get("client_token") != "same-write-token" || r.URL.Query().Get("document_revision_id") != "7" {
				t.Error("write lost revision/token")
			}
			var body struct {
				Requests []struct {
					BlockID string `json:"block_id"`
					Update  struct {
						Elements []map[string]any `json:"elements"`
					} `json:"update_text_elements"`
				} `json:"requests"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.Requests) != 1 || body.Requests[0].BlockID != "block" {
				t.Fatalf("targets %+v", body)
			}
			elements := body.Requests[0].Update.Elements
			if len(elements) != 3 || elements[0]["text_run"].(map[string]any)["content"] != "edited" || elements[1]["mention_doc"] == nil || elements[2]["text_run"].(map[string]any)["content"] != "untouched" || elements[0]["text_run"].(map[string]any)["text_element_style"].(map[string]any)["bold"] != true {
				t.Fatalf("destructive run edit %#v", elements)
			}
			fmt.Fprint(w, `{"code":0,"data":{"document_revision_id":8,"blocks":[]}}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", 500)
		}
	})
	resource := work.Resource{SourceIdentity: "docx:document"}
	inspected, err := connector.InspectResourceDocument(t.Context(), resource, InspectResourceDocumentRequest{BlockID: "block"})
	if err != nil || len(inspected.Blocks) != 1 || inspected.Blocks[0].RunCount != 3 {
		t.Fatalf("inspect %+v %v", inspected, err)
	}
	request := UpdateResourceDocumentRequest{ObservedDocumentRevision: 7, Edits: []ResourceTextRunEdit{{BlockID: "block", TextRunIndex: 0, ExpectedBlockSHA256: inspected.Blocks[0].SHA256, Text: "edited"}}}
	receipt, err := connector.UpdateResourceDocument(t.Context(), resource, request, "same-write-token")
	if err != nil || !receipt.Attempted || receipt.DocumentRevision == nil || *receipt.DocumentRevision != 8 || writes != 1 {
		t.Fatalf("write %+v %v calls%d", receipt, err, writes)
	}
	request.Edits[0].ExpectedBlockSHA256 = strings.Repeat("0", 64)
	receipt, err = connector.UpdateResourceDocument(t.Context(), resource, request, "same-write-token")
	if err == nil || receipt.Attempted || writes != 1 {
		t.Fatal("changed block triggered a write")
	}
	request.ObservedDocumentRevision = 6
	receipt, err = connector.UpdateResourceDocument(t.Context(), resource, request, "same-write-token")
	if err == nil || receipt.Attempted || writes != 1 {
		t.Fatal("stale revision triggered a write")
	}
}
func TestResourceDocumentInspectKeepsPaginationAndBoundedRunText(t *testing.T) {
	connector := platformConnector(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/documents/document"):
			fmt.Fprint(w, `{"code":0,"data":{"document":{"document_id":"document","revision_id":9}}}`)
		case strings.HasSuffix(r.URL.Path, "/blocks/block"):
			fmt.Fprintf(w, `{"code":0,"data":{"block":%s}}`, editableDocBlock)
		case strings.HasSuffix(r.URL.Path, "/blocks"):
			if r.URL.Query().Get("page_token") != "page2" || r.URL.Query().Get("document_revision_id") != "7" {
				t.Error("lost explicit continuation revision")
			}
			fmt.Fprintf(w, `{"code":0,"data":{"items":[%s],"has_more":true,"page_token":"page3"}}`, editableDocBlock)
		default:
			t.Error(r.URL.Path)
		}
	})
	resource := work.Resource{SourceIdentity: "docx:document"}
	result, err := connector.InspectResourceDocument(t.Context(), resource, InspectResourceDocumentRequest{DocumentRevision: 7, PageToken: "page2"})
	if err != nil || result.ObservedRevision != 9 || result.RequestedRevision != 7 || !result.HasMore || result.NextPageToken != "page3" {
		t.Fatalf("page %+v %v", result, err)
	}
	index := 0
	result, err = connector.InspectResourceDocument(t.Context(), resource, InspectResourceDocumentRequest{DocumentRevision: 7, BlockID: "block", TextRunIndex: &index, Offset: 1, Limit: 2})
	if err != nil || result.Blocks[0].Text.Text != "ir" || result.Blocks[0].Text.NextOffset == nil {
		t.Fatalf("text page %+v %v", result, err)
	}
}
func TestResourceSheetWriteRequiresExactObservationAndRetainsFormulaValues(t *testing.T) {
	writes := 0
	connector := platformConnector(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/sheets/v2/spreadsheets/spreadsheet/values") {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Method == "GET" {
			if r.URL.Query().Get("valueRenderOption") != "Formula" || !strings.HasSuffix(r.URL.Path, "/sheet!A1:B1") {
				t.Errorf("range read %s", r.URL)
			}
			fmt.Fprint(w, `{"code":0,"data":{"revision":9007199254740993,"valueRange":{"range":"sheet!A1:B1","revision":3,"values":[["heading","=SUM(A2:A3)"]]}}}`)
			return
		}
		writes++
		if r.Method != "PUT" {
			t.Error(r.Method)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		vr := body["valueRange"].(map[string]any)
		values := vr["values"].([]any)
		cell := values[0].([]any)[1].(map[string]any)
		if vr["range"] != "sheet!A1:B1" || cell["type"] != "formula" || cell["text"] != "=SUM(A2:A4)" {
			t.Fatalf("coerced values %#v", body)
		}
		fmt.Fprint(w, `{"code":0,"data":{"revision":9007199254740994,"updatedRange":"sheet!A1:B1","updatedRows":1,"updatedColumns":2,"updatedCells":2}}`)
	})
	resource := work.Resource{SourceIdentity: "sheets:spreadsheet"}
	read, err := connector.ReadResourceSheet(t.Context(), resource, ReadResourceSheetRequest{SheetID: "sheet", Range: "A1:B1"})
	if err != nil {
		t.Fatal(err)
	}
	if string(read.Values.Revision) != "9007199254740993" {
		t.Fatal("revision precision lost")
	}
	req := WriteResourceSheetRequest{SheetID: "sheet", Range: "A1:B1", ExpectedDataSHA256: read.DataSHA256, Values: [][]any{{"heading", map[string]any{"type": "formula", "text": "=SUM(A2:A4)"}}}}
	receipt, err := connector.WriteResourceSheet(t.Context(), resource, req)
	if err != nil || !receipt.Attempted || writes != 1 {
		t.Fatalf("write %+v %v", receipt, err)
	}
	req.ExpectedDataSHA256 = strings.Repeat("0", 64)
	receipt, err = connector.WriteResourceSheet(t.Context(), resource, req)
	if err == nil || receipt.Attempted || writes != 1 {
		t.Fatal("changed range wrote data")
	}
	req.ExpectedDataSHA256 = read.DataSHA256
	req.Values = [][]any{{"only one column"}}
	receipt, err = connector.WriteResourceSheet(t.Context(), resource, req)
	if err == nil || receipt.Attempted || writes != 1 {
		t.Fatal("dimension error wrote data")
	}
	req.Range = ""
	receipt, err = connector.WriteResourceSheet(t.Context(), resource, req)
	if err == nil || receipt.Attempted || writes != 1 {
		t.Fatal("unbounded range wrote data")
	}
}
