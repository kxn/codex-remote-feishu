package workservice

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

func platformConnector(t *testing.T, handler http.HandlerFunc) *FeishuResourceConnector {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Tt-Logid", "platform-evidence")
		if r.URL.Path == larkcore.TenantAccessTokenInternalUrlPath {
			_, _ = fmt.Fprint(w, `{"code":0,"tenant_access_token":"bound-tenant-token","expire":7200}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer bound-tenant-token" {
			t.Error("connector selected another application identity")
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	client := lark.NewClient("cli_"+t.Name(), "secret", lark.WithOpenBaseUrl(server.URL), lark.WithHttpClient(server.Client()))
	broker := feishu.NewFeishuCallBroker("bound-role", client)
	return &FeishuResourceConnector{identity: "bound-role", documents: feishu.NewProjectDocumentAPI(broker), sheets: feishu.NewProjectSheetsAPI(broker)}
}

func TestFeishuResourceConnectorResolvesWikiAndFreezesDocxHistoricalPages(t *testing.T) {
	pages := 0
	connector := platformConnector(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/wiki/v2/spaces/get_node":
			if r.URL.Query().Get("token") != "wiki-navigation" {
				t.Error("wrong wiki navigation token")
			}
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"node":{"space_id":"space","node_token":"wiki-navigation","obj_type":"docx","obj_token":"content-document"}}}`)
		case "/open-apis/docx/v1/documents/content-document":
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"document":{"document_id":"content-document","revision_id":8,"title":"Current title"}}}`)
		case "/open-apis/docx/v1/documents/content-document/blocks":
			pages++
			if r.URL.Query().Get("document_revision_id") != "7" {
				t.Errorf("historical capture used changing latest revision: %s", r.URL.RawQuery)
			}
			if r.URL.Query().Get("page_token") == "" {
				_, _ = fmt.Fprint(w, `{"code":0,"data":{"items":[{"block_id":"first","block_type":2}],"has_more":true,"page_token":"next-page"}}`)
			} else {
				_, _ = fmt.Fprint(w, `{"code":0,"data":{"items":[{"block_id":"second","block_type":2}],"has_more":false}}`)
			}
		default:
			t.Errorf("unexpected SDK path %s", r.URL.Path)
			_, _ = fmt.Fprint(w, `{"code":1}`)
		}
	})
	resolved, err := connector.Resolve(context.Background(), "https://a.feishu.cn/wiki/wiki-navigation")
	if err != nil || resolved.SourceIdentity != "docx:content-document" {
		t.Fatalf("resolved %#v, %v", resolved, err)
	}
	revision := 7
	captured, err := connector.Capture(context.Background(), work.Resource{SourceIdentity: resolved.SourceIdentity}, CaptureResourceRequest{RevisionID: &revision})
	if err != nil || pages != 2 || !captured.Complete || captured.Consistency != work.ConsistencyAtomic || captured.Source.Version != "7" || !strings.Contains(string(captured.Content), "second") {
		t.Fatalf("captured %#v pages=%d err=%v", captured, pages, err)
	}
}

func TestFeishuResourceConnectorPreservesSeparateSheetValueAndFormulaRevisions(t *testing.T) {
	calls := 0
	connector := platformConnector(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/open-apis/sheets/v2/spreadsheets/book/values/sheet-tab!A1:B1" {
			t.Errorf("wrong explicit range %s", r.URL.Path)
		}
		calls++
		if r.URL.Query().Get("valueRenderOption") == "Formula" {
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"revision":12,"valueRange":{"range":"sheet-tab!A1:B1","values":[["=A2",2]]}}}`)
		} else {
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"revision":11,"valueRange":{"range":"sheet-tab!A1:B1","values":[[1,2]]}}}`)
		}
	})
	captured, err := connector.Capture(context.Background(), work.Resource{SourceIdentity: "sheets:book"}, CaptureResourceRequest{SheetID: "sheet-tab", Range: "A1:B1"})
	if err != nil || calls != 2 || captured.Consistency != work.ConsistencyObserved || captured.Kind != work.VersionObservedDataset {
		t.Fatalf("sheet capture %#v, %v", captured, err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(captured.Content, &raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw["values"]), `"revision":11`) || !strings.Contains(string(raw["formulas"]), `"revision":12`) || !strings.Contains(string(raw["formulas"]), "=A2") {
		t.Fatalf("formula/revision evidence lost %s", captured.Content)
	}
}

func TestFeishuResourceLinkDoesNotClaimForeignHosts(t *testing.T) {
	if _, _, err := work.ParseFeishuDocumentLink("https://evil.example/docx/real-document"); err == nil {
		t.Error("foreign URL could be advertised as canonical Feishu resource")
	}
	if _, _, err := work.ParseFeishuDocumentLink("https://a.feishu.cn/wiki/node/extra"); err == nil {
		t.Error("ambiguous path accepted")
	}
}
