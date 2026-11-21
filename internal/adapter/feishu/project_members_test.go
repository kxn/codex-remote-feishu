package feishu

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"
)

func TestProjectChatMembersAddBotPreservesPartialResults(t *testing.T) {
	api := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/open-apis/im/v1/chats/oc_project/members" || r.Method != "POST" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("member_id_type") != "app_id" || r.URL.Query().Get("succeed_type") != "1" {
			t.Errorf("query %s", r.URL.RawQuery)
		}
		if got := projectReadJSON(t, r)["id_list"]; !reflect.DeepEqual(got, []any{"cli_pending", "cli_invalid", "cli_missing"}) {
			t.Errorf("bot ids %#v", got)
		}
		_, _ = fmt.Fprint(w, `{"code":0,"data":{"pending_approval_id_list":["cli_pending"],"invalid_id_list":["cli_invalid"],"not_existed_id_list":["cli_missing"]}}`)
	})
	result, err := api.AddChatMembers(context.Background(), ProjectChatMembersRequest{ChatID: "oc_project", MemberIDType: "app_id", IDs: []string{"cli_pending", "cli_invalid", "cli_missing"}, SucceedType: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.PendingApprovalIDs, []string{"cli_pending"}) || !reflect.DeepEqual(result.InvalidIDs, []string{"cli_invalid"}) || !reflect.DeepEqual(result.NotExistedIDs, []string{"cli_missing"}) {
		t.Fatalf("partial results %#v", result)
	}
}

func TestProjectChatUsersPaginationDoesNotInferBotAbsence(t *testing.T) {
	var pages, callerQueries int
	api := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/open-apis/im/v1/chats/oc_project/members/is_in_chat" {
			callerQueries++
			if r.URL.Query().Get("member_id_type") != "" {
				t.Error("caller query must use bound token identity")
			}
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"is_in_chat":true}}`)
			return
		}
		pages++
		if r.URL.Path != "/open-apis/im/v1/chats/oc_project/members" || r.Method != "GET" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("member_id_type") != "open_id" || r.URL.Query().Get("page_size") != "10" {
			t.Errorf("query %s", r.URL.RawQuery)
		}
		if pages == 1 {
			if r.URL.Query().Get("page_token") != "" {
				t.Errorf("first page token %s", r.URL.RawQuery)
			}
			// Bots can consume platform pages but are filtered from returned users.
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"items":[],"has_more":true,"page_token":"next","member_total":1}}`)
		} else {
			if r.URL.Query().Get("page_token") != "next" {
				t.Errorf("next page token %s", r.URL.RawQuery)
			}
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"items":[{"member_id":"ou_person","member_id_type":"open_id","name":"Person","tenant_key":"tenant"}],"has_more":false,"trigger_security_conf_limit":true,"security_conf_limit":1}}`)
		}
	})
	ctx := context.Background()
	page, err := api.ListChatUsersPage(ctx, "oc_project", "open_id", "", 10)
	if err != nil || len(page.Users) != 0 || !page.HasMore || page.PageToken != "next" {
		t.Fatalf("first page %#v, %v", page, err)
	}
	inChat, err := api.IsCallerInChat(ctx, "oc_project")
	if err != nil || !inChat {
		t.Fatalf("bot membership after empty user page: %v, %v", inChat, err)
	}
	page, err = api.ListChatUsersPage(ctx, "oc_project", "open_id", page.PageToken, 10)
	if err != nil || len(page.Users) != 1 || page.Users[0].ID != "ou_person" || page.HasMore || !page.TriggerSecurityConfLimit || page.SecurityConfLimit == nil || *page.SecurityConfLimit != 1 {
		t.Fatalf("last page %#v, %v", page, err)
	}
	if callerQueries != 1 || pages != 2 {
		t.Fatalf("requests: pages=%d caller=%d", pages, callerQueries)
	}
}

func TestProjectChatUsersRejectsStuckPaginationAndMissingCallerFact(t *testing.T) {
	api := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/open-apis/im/v1/chats/oc_project/members/is_in_chat" {
			_, _ = fmt.Fprint(w, `{"code":0,"data":{}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"code":0,"data":{"items":[],"has_more":true,"page_token":"same"}}`)
	})
	if _, err := api.ListChatUsersPage(context.Background(), "oc_project", "open_id", "same", 10); err == nil {
		t.Error("accepted non-advancing token")
	}
	if _, err := api.IsCallerInChat(context.Background(), "oc_project"); err == nil {
		t.Error("missing caller fact became false")
	}
	if _, err := api.ListChatUsersPage(context.Background(), "oc_project", "app_id", "", 10); err == nil {
		t.Error("user list accepted app_id")
	}
}

func TestProjectChatMembersUserIDRequest(t *testing.T) {
	api := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("member_id_type") != "open_id" {
			t.Errorf("query %s", r.URL.RawQuery)
		}
		if got := projectReadJSON(t, r)["id_list"]; !reflect.DeepEqual(got, []any{"ou_person"}) {
			t.Errorf("ids %#v", got)
		}
		_, _ = fmt.Fprint(w, `{"code":0,"data":{}}`)
	})
	if _, err := api.AddChatMembers(context.Background(), ProjectChatMembersRequest{ChatID: "oc_project", MemberIDType: "open_id", IDs: []string{"ou_person"}}); err != nil {
		t.Fatal(err)
	}
}
