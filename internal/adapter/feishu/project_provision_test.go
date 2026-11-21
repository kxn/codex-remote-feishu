package feishu

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

func TestProjectProvisionFolderBlockAndCanonicalURLRequests(t *testing.T) {
	step := 0
	fake := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		step++
		switch step {
		case 1:
			if r.Method != "POST" || r.URL.Path != "/open-apis/drive/v1/files/create_folder" {
				t.Errorf("folder request %s %s", r.Method, r.URL)
			}
			body := projectReadJSON(t, r)
			if body["name"] != "Home" || body["folder_token"] != "" {
				t.Errorf("folder body %#v", body)
			}
			fmt.Fprint(w, `{"code":0,"data":{"token":"folder","url":"https://a.feishu.cn/drive/folder/folder"}}`)
		case 2:
			if r.Method != "GET" || r.URL.Path != "/open-apis/drive/v1/files" || r.URL.Query().Get("folder_token") != "folder" || r.URL.Query().Get("page_size") != "1" {
				t.Errorf("folder read %s", r.URL)
			}
			fmt.Fprint(w, `{"code":0,"data":{"files":[],"has_more":false}}`)
		case 3:
			if r.Method != "POST" || r.URL.Path != "/open-apis/drive/v1/metas/batch_query" {
				t.Errorf("meta %s %s", r.Method, r.URL)
			}
			body := projectReadJSON(t, r)
			docs := body["request_docs"].([]any)
			doc := docs[0].(map[string]any)
			if body["with_url"] != true || doc["doc_token"] != "doc" || doc["doc_type"] != "docx" {
				t.Errorf("metadata body %#v", body)
			}
			fmt.Fprint(w, `{"code":0,"data":{"metas":[{"doc_token":"unrelated","url":"https://wrong"},{"doc_token":"doc","url":"https://a.feishu.cn/docx/doc"}]}}`)
		case 4:
			if r.Method != "GET" || r.URL.Path != "/open-apis/docx/v1/documents/doc/blocks/managed" || r.URL.Query().Get("document_revision_id") != "12" {
				t.Errorf("block %s", r.URL)
			}
			fmt.Fprint(w, `{"code":0,"data":{"block":{"block_id":"managed","block_type":2}}}`)
		default:
			t.Errorf("extra SDK request %s", r.URL)
		}
	})
	api := NewProjectDocumentAPI(fake.broker)
	folder, err := api.CreateProjectFolder(context.Background(), "Home", "")
	if err != nil || folder.Token != "folder" {
		t.Fatalf("folder %#v %v", folder, err)
	}
	if err = api.CheckProjectFolder(context.Background(), folder.Token); err != nil {
		t.Fatal(err)
	}
	link, err := api.ProjectFileURL(context.Background(), "doc", "docx")
	if err != nil || link != "https://a.feishu.cn/docx/doc" {
		t.Fatalf("url %s %v", link, err)
	}
	block, err := api.GetProjectDocumentBlock(context.Background(), "doc", "managed", 12)
	if err != nil || block.Block == nil || scopeStringValue(block.Block.BlockId) != "managed" {
		t.Fatalf("block %#v %v", block, err)
	}
	if step != 4 {
		t.Fatalf("requests %d", step)
	}
}

func TestProjectProvisionFolderWriteFailureHasPartialReceiptAndNoRetry(t *testing.T) {
	calls := 0
	fake := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"code":1254001,"msg":"failed after allocation","data":{"token":"known-folder"}}`)
	})
	receipt, err := NewProjectDocumentAPI(fake.broker).CreateProjectFolder(context.Background(), "Home", "")
	if err == nil || receipt.Token != "known-folder" || calls != 1 {
		t.Fatalf("partial %#v calls=%d err=%v", receipt, calls, err)
	}
}

func TestProjectProvisionPinPaginationAndExactNavigationIdentity(t *testing.T) {
	calls := 0
	fake := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			if r.URL.Path != "/open-apis/im/v1/messages/nav" {
				t.Errorf("message path %s", r.URL)
			}
			fmt.Fprint(w, `{"code":0,"data":{"items":[{"message_id":"nav","chat_id":"chat","msg_type":"text","body":{"content":"{\"text\":\"Project home\"}"}}]}}`)
			return
		}
		if r.URL.Path != "/open-apis/im/v1/pins" || r.URL.Query().Get("chat_id") != "chat" || r.URL.Query().Get("page_size") != "100" {
			t.Errorf("pins %s", r.URL)
		}
		if calls == 2 {
			fmt.Fprint(w, `{"code":0,"data":{"items":[{"message_id":"nav","chat_id":"other"}],"has_more":true,"page_token":"next"}}`)
		} else {
			if r.URL.Query().Get("page_token") != "next" {
				t.Errorf("pin cursor %s", r.URL)
			}
			fmt.Fprint(w, `{"code":0,"data":{"items":[{"message_id":"nav","chat_id":"chat"}],"has_more":false}}`)
		}
	})
	nav, err := fake.ReadProjectNavigation(context.Background(), "nav")
	if err != nil || nav.ChatID != "chat" || nav.Text != "Project home" {
		t.Fatalf("navigation %#v %v", nav, err)
	}
	found, err := fake.HasProjectPin(context.Background(), "chat", "nav")
	if err != nil || !found || calls != 3 {
		t.Fatalf("pins %v %d %v", found, calls, err)
	}
}
