package feishu

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	larkdocx "github.com/larksuite/oapi-sdk-go/v3/service/docx/v1"
)

func TestProjectDocumentCreateMetadataAndHistoricalPages(t *testing.T) {
	step := 0
	fake := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		step++
		w.Header().Set("X-Tt-Logid", "doc-evidence")
		switch step {
		case 1:
			if r.Method != "POST" || r.URL.Path != "/open-apis/docx/v1/documents" {
				t.Errorf("create %s %s", r.Method, r.URL.Path)
			}
			body := projectReadJSON(t, r)
			if body["title"] != "Project home" || body["folder_token"] != "folder-app" {
				t.Errorf("create body %#v", body)
			}
			if r.URL.Query().Get("client_token") != "" {
				t.Error("document create invented client_token")
			}
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"document":{"document_id":"doc-content","revision_id":1,"title":"Project home"}}}`)
		case 2:
			if r.Method != "GET" || r.URL.Path != "/open-apis/docx/v1/documents/doc-content" {
				t.Errorf("get %s %s", r.Method, r.URL.Path)
			}
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"document":{"document_id":"doc-content","revision_id":7,"title":"Home updated"}}}`)
		case 3, 4:
			if r.URL.Path != "/open-apis/docx/v1/documents/doc-content/blocks" || r.URL.Query().Get("document_revision_id") != "7" || r.URL.Query().Get("page_size") != "2" {
				t.Errorf("page request %s?%s", r.URL.Path, r.URL.RawQuery)
			}
			if step == 3 {
				if r.URL.Query().Get("page_token") != "" {
					t.Error("unexpected first-page token")
				}
				_, _ = fmt.Fprint(w, `{"code":0,"data":{"items":[{"block_id":"root","block_type":1,"children":["block-a"]}],"has_more":true,"page_token":"cursor-next"}}`)
			} else {
				if r.URL.Query().Get("page_token") != "cursor-next" {
					t.Error("next page token missing")
				}
				_, _ = fmt.Fprint(w, `{"code":0,"data":{"items":[{"block_id":"block-a","block_type":2,"text":{"elements":[{"text_run":{"content":"Human text"}}]}}],"has_more":false}}`)
			}
		default:
			t.Errorf("extra request %d", step)
		}
	})
	api := NewProjectDocumentAPI(fake.broker)
	ctx := context.Background()
	metadata, err := api.CreateDocument(ctx, ProjectCreateDocumentRequest{Title: "Project home", FolderToken: "folder-app"})
	if err != nil || metadata.DocumentID != "doc-content" || metadata.RevisionID == nil || *metadata.RevisionID != 1 || metadata.RequestID != "doc-evidence" {
		t.Fatalf("created %#v, %v", metadata, err)
	}
	metadata, err = api.GetDocument(ctx, metadata.DocumentID)
	if err != nil || *metadata.RevisionID != 7 {
		t.Fatalf("metadata %#v, %v", metadata, err)
	}
	req := ProjectDocumentBlocksRequest{DocumentID: metadata.DocumentID, RevisionID: *metadata.RevisionID, PageSize: 2}
	first, err := api.ListDocumentBlocksPage(ctx, req)
	if err != nil || !first.HasMore || first.NextPageToken != "cursor-next" || first.RequestedRevisionID != 7 || len(first.Blocks) != 1 {
		t.Fatalf("first page %#v, %v", first, err)
	}
	req.PageToken = first.NextPageToken
	last, err := api.ListDocumentBlocksPage(ctx, req)
	if err != nil || last.HasMore || len(last.Blocks) != 1 || scopeStringValue(last.Blocks[0].BlockId) != "block-a" {
		t.Fatalf("last page %#v, %v", last, err)
	}
	if step != 4 {
		t.Fatalf("requests %d", step)
	}
}

func TestProjectDocumentMutationsPreservePositionTokenAndBlockTargets(t *testing.T) {
	step := 0
	fake := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		step++
		if r.URL.Query().Get("document_revision_id") != "-1" || r.URL.Query().Get("client_token") != "operation-id" {
			t.Errorf("mutation query %s", r.URL.RawQuery)
		}
		body := projectReadJSON(t, r)
		if step == 1 {
			if r.Method != "POST" || r.URL.Path != "/open-apis/docx/v1/documents/doc-content/blocks/managed-parent/children" {
				t.Errorf("append %s %s", r.Method, r.URL.Path)
			}
			if body["index"] != float64(-1) || len(body["children"].([]any)) != 1 {
				t.Errorf("append body %#v", body)
			}
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"children":[{"block_id":"managed-block","block_type":2}],"document_revision_id":8,"client_token":"operation-id"}}`)
		} else {
			if r.Method != "PATCH" || r.URL.Path != "/open-apis/docx/v1/documents/doc-content/blocks/batch_update" {
				t.Errorf("patch %s %s", r.Method, r.URL.Path)
			}
			updates := body["requests"].([]any)
			update := updates[0].(map[string]any)
			if len(updates) != 1 || update["block_id"] != "managed-block" || update["update_text_elements"] == nil {
				t.Errorf("local update %#v", body)
			}
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"blocks":[{"block_id":"managed-block","block_type":2}],"document_revision_id":9,"client_token":"operation-id"}}`)
		}
	})
	api := NewProjectDocumentAPI(fake.broker)
	ctx := context.Background()
	kind := 2
	mutation, err := api.AppendDocumentBlocks(ctx, ProjectAppendDocumentBlocksRequest{DocumentID: "doc-content", ParentBlockID: "managed-parent", RevisionID: -1, Index: -1, ClientToken: "operation-id", Blocks: []*larkdocx.Block{{BlockType: &kind}}})
	if err != nil || mutation.RevisionID == nil || *mutation.RevisionID != 8 || mutation.ClientToken != "operation-id" {
		t.Fatalf("append %#v, %v", mutation, err)
	}
	id := "managed-block"
	mutation, err = api.UpdateDocumentBlocks(ctx, ProjectUpdateDocumentBlocksRequest{DocumentID: "doc-content", RevisionID: -1, ClientToken: "operation-id", Requests: []*larkdocx.UpdateBlockRequest{{BlockId: &id, UpdateTextElements: &larkdocx.UpdateTextElementsRequest{}}}})
	if err != nil || mutation.RevisionID == nil || *mutation.RevisionID != 9 || len(mutation.Blocks) != 1 {
		t.Fatalf("update %#v, %v", mutation, err)
	}
}

func TestProjectDocumentPageMissingAndStuckCursorAreErrors(t *testing.T) {
	fake := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"code":0,"data":{"items":[{"block_id":"partial"}],"has_more":true,"page_token":"same"}}`)
	})
	api := NewProjectDocumentAPI(fake.broker)
	page, err := api.ListDocumentBlocksPage(context.Background(), ProjectDocumentBlocksRequest{DocumentID: "doc-content", RevisionID: 2, PageToken: "same", PageSize: 10})
	if err == nil || len(page.Blocks) != 1 || !page.HasMore {
		t.Fatalf("partial page %#v, %v", page, err)
	}
	if _, err := api.ListDocumentBlocksPage(context.Background(), ProjectDocumentBlocksRequest{DocumentID: "doc-content", PageSize: 10}); err == nil {
		t.Error("implicit revision accepted")
	}
	if _, err := api.GetDocument(context.Background(), ProjectDocumentID("https://example.feishu.cn/wiki/node")); err == nil {
		t.Error("wiki URL accepted as document id")
	}
	id := "same"
	if _, err := api.UpdateDocumentBlocks(context.Background(), ProjectUpdateDocumentBlocksRequest{DocumentID: "doc-content", RevisionID: -1, Requests: []*larkdocx.UpdateBlockRequest{{BlockId: &id}, {BlockId: &id}}}); err == nil {
		t.Error("duplicate block targets accepted")
	}
}
