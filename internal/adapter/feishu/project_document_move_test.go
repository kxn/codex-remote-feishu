package feishu

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestProjectDocumentMoveAndDestinationPagesUseActualObjectIdentity(t *testing.T) {
	moves, lists := 0, 0
	chat := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/files/document/move"):
			moves++
			body := projectReadJSON(t, r)
			if body["type"] != "docx" || body["folder_token"] != "new-parent" {
				t.Errorf("move body %#v", body)
			}
			fmt.Fprint(w, `{"code":0,"data":{}}`)
		case r.Method == "GET" && r.URL.Path == "/open-apis/drive/v1/files":
			lists++
			if r.URL.Query().Get("folder_token") != "new-parent" {
				t.Error("another directory read")
			}
			if r.URL.Query().Get("page_token") == "" {
				fmt.Fprint(w, `{"code":0,"data":{"files":[],"has_more":true,"next_page_token":"page2"}}`)
			} else {
				if r.URL.Query().Get("page_token") != "page2" {
					t.Error("cursor")
				}
				fmt.Fprint(w, `{"code":0,"data":{"files":[{"token":"document","type":"docx","parent_token":"new-parent"}],"has_more":false}}`)
			}
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
	})
	documents := NewProjectDocumentAPI(chat.broker)
	receipt, err := documents.MoveDocumentToFolder(t.Context(), "document", "new-parent")
	if err != nil || receipt.DocumentID != "document" {
		t.Fatalf("move %+v %v", receipt, err)
	}
	present, err := documents.DocumentInFolder(t.Context(), "document", "new-parent")
	if err != nil || !present || moves != 1 || lists != 2 {
		t.Fatalf("destination %v %v moves%d lists%d", present, err, moves, lists)
	}
}
func TestProjectDocumentWikiMoveDoesNotCopyOrRequestPermissionApproval(t *testing.T) {
	calls := 0
	chat := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/open-apis/wiki/v2/spaces/space/nodes/move_docs_to_wiki" {
			t.Errorf("request %s %s", r.Method, r.URL)
		}
		body := projectReadJSON(t, r)
		if body["obj_token"] != "document" || body["obj_type"] != "docx" || body["parent_wiki_token"] != "parent" || body["apply"] != false {
			t.Errorf("move %#v", body)
		}
		fmt.Fprint(w, `{"code":0,"data":{"task_id":"actual-task","wiki_token":"actual-node","applied":false}}`)
	})
	documents := NewProjectDocumentAPI(chat.broker)
	receipt, err := documents.MoveDocumentToWiki(t.Context(), "document", "", "", "space", "parent")
	if err != nil || receipt.DocumentID != "document" || receipt.NodeToken != "actual-node" || receipt.TaskID != "actual-task" || calls != 1 {
		t.Fatalf("receipt %+v %v", receipt, err)
	}
}
func TestProjectMoveAndNavigationUpdateDoNotRepeatFailedWrites(t *testing.T) {
	calls := 0
	chat := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, `{"code":999,"msg":"uncertain"}`)
	})
	documents := NewProjectDocumentAPI(chat.broker)
	if _, err := documents.MoveDocumentToFolder(t.Context(), "document", "folder"); err == nil || calls != 1 {
		t.Fatal("move failure retried")
	}
	if err := chat.UpdateNavigationText(t.Context(), "nav", "new actual link"); err == nil || calls != 2 {
		t.Fatal("navigation update failure retried")
	}
}
