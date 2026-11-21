package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

// This boundary exercises the actual SDK request serialization and broker path,
// while all HTTP (including tenant token issuance) stays in a local fake server.
func projectChatTestAPI(t *testing.T, handler http.HandlerFunc) *ProjectChatAPI {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == larkcore.TenantAccessTokenInternalUrlPath {
			_, _ = fmt.Fprint(w, `{"code":0,"tenant_access_token":"project-test-token","expire":7200}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer project-test-token" {
			t.Errorf("wrong bound identity: %q", r.Header.Get("Authorization"))
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	client := lark.NewClient("cli_"+t.Name(), "test-secret", lark.WithOpenBaseUrl(server.URL), lark.WithHttpClient(server.Client()))
	return NewProjectChatAPI(NewFeishuCallBroker("project-test", client))
}

func projectReadJSON(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Errorf("decode request: %v", err)
	}
	return body
}

func TestProjectChatTabsSDKRequests(t *testing.T) {
	var step int
	api := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		step++
		base := "/open-apis/im/v1/chats/oc_project/chat_tabs"
		switch step {
		case 1:
			if r.Method != "GET" || r.URL.Path != base+"/list_tabs" {
				t.Errorf("list request %s %s", r.Method, r.URL.Path)
			}
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"chat_tabs":[{"tab_id":"system","tab_type":"file","tab_name":"Files"},{"tab_id":"tab1","tab_type":"doc","tab_name":"项目","tab_content":{"doc":"https://example.feishu.cn/wiki/node"}}]}}`)
			return
		case 2, 3:
			path := base
			if step == 3 {
				path += "/update_tabs"
			}
			if r.Method != "POST" || r.URL.Path != path {
				t.Errorf("write request %s %s", r.Method, r.URL.Path)
			}
			body := projectReadJSON(t, r)
			tabs, _ := body["chat_tabs"].([]any)
			if len(tabs) != 1 {
				t.Errorf("chat_tabs: %#v", body)
				break
			}
			tab := tabs[0].(map[string]any)
			content := tab["tab_content"].(map[string]any)
			if step == 2 {
				if tab["tab_type"] != "doc" || tab["tab_id"] != nil || content["doc"] != "https://example.feishu.cn/wiki/node" || content["url"] != nil {
					t.Errorf("create: %#v", tab)
				}
			} else {
				if tab["tab_type"] != "url" || tab["tab_id"] != "tab1" || content["url"] != "https://example.com/project" || content["doc"] != nil {
					t.Errorf("update: %#v", tab)
				}
				if tab["tab_config"].(map[string]any)["is_built_in"] != false {
					t.Errorf("embedded false lost: %#v", tab)
				}
			}
		case 4:
			if r.Method != "DELETE" || r.URL.Path != base+"/delete_tabs" {
				t.Errorf("delete request %s %s", r.Method, r.URL.Path)
			}
			if got := projectReadJSON(t, r)["tab_ids"]; !reflect.DeepEqual(got, []any{"tab1"}) {
				t.Errorf("delete ids %#v", got)
			}
		default:
			t.Errorf("unexpected call %d", step)
		}
		_, _ = fmt.Fprint(w, `{"code":0,"data":{"chat_tabs":[{"tab_id":"tab1","tab_type":"doc"}]}}`)
	})
	ctx := context.Background()
	tabs, err := api.ListChatTabs(ctx, "oc_project")
	if err != nil || len(tabs) != 2 || tabs[0].Type != "file" || tabs[1].URL != "https://example.feishu.cn/wiki/node" {
		t.Fatalf("list = %#v, %v", tabs, err)
	}
	if _, err := api.CreateChatTab(ctx, "oc_project", ProjectChatTab{Name: "项目", Type: "doc", URL: "https://example.feishu.cn/wiki/node"}); err != nil {
		t.Fatal(err)
	}
	embedded := false
	if _, err := api.UpdateChatTab(ctx, "oc_project", ProjectChatTab{ID: "tab1", Name: "项目", Type: "url", URL: "https://example.com/project", Embedded: &embedded}); err != nil {
		t.Fatal(err)
	}
	if _, err := api.DeleteChatTab(ctx, "oc_project", "tab1"); err != nil {
		t.Fatal(err)
	}
	if step != 4 {
		t.Fatalf("requests = %d", step)
	}
}

func TestProjectChatRejectsSystemTabAndImplicitTopNotice(t *testing.T) {
	var calls atomic.Int32
	api := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); _, _ = fmt.Fprint(w, `{"code":0}`) })
	ctx := context.Background()
	for _, kind := range []string{"file", "doc_list", "message"} {
		if _, err := api.CreateChatTab(ctx, "oc_project", ProjectChatTab{Name: "Files", Type: kind, URL: "https://example.com"}); err == nil {
			t.Errorf("accepted %s", kind)
		}
	}
	if err := api.PutTopNotice(ctx, ProjectTopNoticeRequest{ChatID: "oc_project", Type: "message", MessageID: "om_nav"}); err == nil {
		t.Error("implicit top notice accepted")
	}
	if calls.Load() != 0 {
		t.Errorf("invalid request reached HTTP: %d", calls.Load())
	}
}

func TestProjectChatPinAndExplicitNoticeSDKRequests(t *testing.T) {
	var step int
	api := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		step++
		body := projectReadJSON(t, r)
		if r.Method != "POST" {
			t.Errorf("method %s", r.Method)
		}
		if step == 1 {
			if r.URL.Path != "/open-apis/im/v1/pins" || body["message_id"] != "om_nav" {
				t.Errorf("pin request %s %#v", r.URL.Path, body)
			}
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"pin":{"message_id":"om_nav","chat_id":"oc_project","operator_id":"cli_role","operator_id_type":"app_id","create_time":"123"}}}`)
			return
		}
		if r.URL.Path != "/open-apis/im/v1/chats/oc_project/top_notice/put_top_notice" {
			t.Errorf("notice path %s", r.URL.Path)
		}
		notices := body["chat_top_notice"].([]any)
		notice := notices[0].(map[string]any)
		if step == 2 && (notice["action_type"] != "1" || notice["message_id"] != "om_nav") {
			t.Errorf("message notice %#v", notice)
		}
		if step == 3 && (notice["action_type"] != "2" || notice["message_id"] != nil) {
			t.Errorf("announcement notice %#v", notice)
		}
		_, _ = fmt.Fprint(w, `{"code":0}`)
	})
	pin, err := api.PinMessage(context.Background(), "om_nav")
	if err != nil || pin.ChatID != "oc_project" || pin.OperatorIDType != "app_id" {
		t.Fatalf("pin %#v, %v", pin, err)
	}
	for _, kind := range []string{"message", "announcement"} {
		req := ProjectTopNoticeRequest{ChatID: "oc_project", Type: kind, ExplicitlyRequested: true}
		if kind == "message" {
			req.MessageID = "om_nav"
		}
		if err := api.PutTopNotice(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	if step != 3 {
		t.Fatalf("requests %d", step)
	}
}

func TestProjectChatWriteErrorPreservesEvidenceAndDoesNotRetry(t *testing.T) {
	var calls atomic.Int32
	api := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("X-Tt-Logid", "request-123")
		_, _ = fmt.Fprint(w, `{"code":230001,"msg":"invalid request"}`)
	})
	_, err := api.CreateChatTab(context.Background(), "oc_project", ProjectChatTab{Name: "项目", Type: "doc", URL: "https://example.feishu.cn/docx/doc"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 230001 || apiErr.API != "im.v1.chat.tab.create" || apiErr.RequestID != "request-123" {
		t.Fatalf("error %#v (%v)", apiErr, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("unsafe write retried: %d", calls.Load())
	}
}

func TestProjectChatGatewaySharesBrokerAndRateLimitedWriteDoesNotRetry(t *testing.T) {
	var calls atomic.Int32
	api := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(w, `{"code":99991400,"msg":"rate limited"}`)
	})
	gateway := &LiveGateway{broker: api.broker}
	if gateway.ProjectChatAPI().broker != api.broker {
		t.Fatal("gateway adapter replaced credential broker")
	}
	_, err := gateway.ProjectChatAPI().CreateChatTab(context.Background(), "oc_project", ProjectChatTab{Name: "项目", Type: "doc", URL: "https://example.feishu.cn/docx/doc"})
	if _, ok := ExtractRateLimit(err); !ok {
		t.Fatalf("missing rate evidence: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("write retried %d times", calls.Load())
	}
}
