package feishu

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	larkdocx "github.com/larksuite/oapi-sdk-go/v3/service/docx/v1"
)

func TestProjectResourceWritesNeverRetryAndKeepPartialEvidence(t *testing.T) {
	cases := []struct {
		name string
		call func(context.Context, *ProjectDocumentAPI, *ProjectSheetsAPI) (string, error)
	}{
		{"create-docx", func(ctx context.Context, docs *ProjectDocumentAPI, _ *ProjectSheetsAPI) (string, error) {
			result, err := docs.CreateDocument(ctx, ProjectCreateDocumentRequest{Title: "Home"})
			return string(result.DocumentID), err
		}},
		{"append-block", func(ctx context.Context, docs *ProjectDocumentAPI, _ *ProjectSheetsAPI) (string, error) {
			kind := 2
			result, err := docs.AppendDocumentBlocks(ctx, ProjectAppendDocumentBlocksRequest{DocumentID: "doc-content", ParentBlockID: "parent", RevisionID: -1, Index: -1, Blocks: []*larkdocx.Block{{BlockType: &kind}}})
			return result.ClientToken, err
		}},
		{"update-block", func(ctx context.Context, docs *ProjectDocumentAPI, _ *ProjectSheetsAPI) (string, error) {
			id := "managed"
			result, err := docs.UpdateDocumentBlocks(ctx, ProjectUpdateDocumentBlocksRequest{DocumentID: "doc-content", RevisionID: -1, Requests: []*larkdocx.UpdateBlockRequest{{BlockId: &id, UpdateTextElements: &larkdocx.UpdateTextElementsRequest{}}}})
			return result.ClientToken, err
		}},
		{"create-wiki", func(ctx context.Context, docs *ProjectDocumentAPI, _ *ProjectSheetsAPI) (string, error) {
			result, err := docs.CreateWikiNode(ctx, ProjectCreateWikiNodeRequest{SpaceID: "space-existing", AtSpaceRoot: true, ObjType: "docx", Title: "Home"})
			return string(result.NodeToken), err
		}},
		{"write-range", func(ctx context.Context, _ *ProjectDocumentAPI, sheets *ProjectSheetsAPI) (string, error) {
			result, err := sheets.WriteSheetRange(ctx, ProjectSheetWriteRequest{SpreadsheetToken: "sheet-book", SheetID: "sheet-tab", Range: "A1", Values: [][]any{{"one"}}})
			return result.Range, err
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			fake := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("X-Tt-Logid", "write-evidence")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = fmt.Fprint(w, `{"code":99991400,"msg":"rate limited","data":{"document":{"document_id":"partial-document","revision_id":1},"client_token":"partial-operation","document_revision_id":2,"node":{"node_token":"partial-node","obj_token":"partial-content","obj_type":"docx","space_id":"space-existing"},"updatedRange":"sheet-tab!A1","revision":3}}`)
			})
			evidence, err := test.call(context.Background(), NewProjectDocumentAPI(fake.broker), NewProjectSheetsAPI(fake.broker))
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Code != 99991400 || apiErr.RequestID != "write-evidence" {
				t.Fatalf("write error %#v, %v", apiErr, err)
			}
			if evidence == "" {
				t.Error("discarded partial response evidence")
			}
			if calls != 1 {
				t.Fatalf("write attempted %d times", calls)
			}
		})
	}
}

func TestProjectSheetsMalformedResponseIsNotSuccessfulOrRetried(t *testing.T) {
	for _, test := range []struct{ name, body string }{
		{"invalid-json", `{"code":0,"data":`},
		{"missing-code", `{"data":{"updatedRange":"tab-plan!A1"}}`},
		{"missing-data", `{"code":0}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			fake := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				_, _ = fmt.Fprint(w, test.body)
			})
			_, err := NewProjectSheetsAPI(fake.broker).WriteSheetRange(context.Background(), ProjectSheetWriteRequest{SpreadsheetToken: "sheet-book", SheetID: "tab-plan", Range: "A1", Values: [][]any{{"one"}}})
			if err == nil {
				t.Error("malformed response reported as success")
			}
			if calls != 1 {
				t.Fatalf("uncertain write repeated %d times", calls)
			}
		})
	}
}
