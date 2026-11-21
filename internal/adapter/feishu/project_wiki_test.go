package feishu

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

func TestProjectWikiResolveKeepsNavigationAndContentTokensDistinct(t *testing.T) {
	fake := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/open-apis/wiki/v2/spaces/get_node" || r.URL.Query().Get("token") != "wiki-navigation" || r.URL.Query().Get("obj_type") != "wiki" {
			t.Errorf("resolve %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		_, _ = fmt.Fprint(w, `{"code":0,"data":{"node":{"space_id":"space-existing","node_token":"wiki-navigation","obj_token":"doc-content","obj_type":"docx","parent_node_token":"wiki-parent","node_type":"origin","title":"Project home"}}}`)
	})
	api := NewProjectDocumentAPI(fake.broker)
	node, err := api.ResolveWikiNode(context.Background(), "wiki-navigation")
	if err != nil || node.NodeToken != "wiki-navigation" || node.ObjToken != "doc-content" {
		t.Fatalf("node %#v, %v", node, err)
	}
	documentID, err := node.DocxDocumentID()
	if err != nil || documentID != "doc-content" {
		t.Fatalf("content id %q, %v", documentID, err)
	}
	node.ObjType = "sheet"
	if _, err := node.DocxDocumentID(); err == nil {
		t.Error("sheet object token converted to Docx id")
	}
}

func TestProjectWikiCreateUsesExistingParentOrExplicitSpaceRoot(t *testing.T) {
	step := 0
	fake := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		step++
		if r.Method != "POST" || r.URL.Path != "/open-apis/wiki/v2/spaces/space-existing/nodes" {
			t.Errorf("create %s %s", r.Method, r.URL.Path)
		}
		body := projectReadJSON(t, r)
		if body["node_type"] != "origin" || body["obj_type"] != "docx" || body["title"] != "Project home" {
			t.Errorf("node %#v", body)
		}
		if step == 1 && body["parent_node_token"] != "wiki-parent" {
			t.Errorf("parent %#v", body)
		}
		if step == 2 && body["parent_node_token"] != nil {
			t.Errorf("root must omit parent %#v", body)
		}
		if body["obj_token"] != nil {
			t.Error("new origin node must not smuggle another document token")
		}
		_, _ = fmt.Fprint(w, `{"code":0,"data":{"node":{"space_id":"space-existing","node_token":"wiki-new","obj_token":"doc-new","obj_type":"docx","node_type":"origin"}}}`)
	})
	api := NewProjectDocumentAPI(fake.broker)
	req := ProjectCreateWikiNodeRequest{SpaceID: "space-existing", ParentNodeToken: "wiki-parent", ObjType: "docx", Title: "Project home"}
	if _, err := api.CreateWikiNode(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	req.ParentNodeToken = ""
	req.AtSpaceRoot = true
	if _, err := api.CreateWikiNode(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	req.AtSpaceRoot = false
	if _, err := api.CreateWikiNode(context.Background(), req); err == nil {
		t.Error("implicit root accepted")
	}
	req.AtSpaceRoot = true
	req.ParentNodeToken = "wiki-parent"
	if _, err := api.CreateWikiNode(context.Background(), req); err == nil {
		t.Error("root+parent conflict accepted")
	}
	if step != 2 {
		t.Fatalf("unrequested platform write %d", step)
	}
}
