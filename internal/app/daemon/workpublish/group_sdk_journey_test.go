package workpublish

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

type sdkGroupResolver struct{ api *feishu.ProjectChatAPI }

func (r sdkGroupResolver) Resolve(context.Context, work.RoleBotBinding) (Sender, error) {
	return r.api, nil
}

// Exercises persisted role evidence -> queue merging/claim -> actual SDK HTTP
// create/PATCH -> receipt mapping. No client or tenant traffic leaves localhost.
func TestGroupSDKJourneyEditsSnapshotAndPrioritizesFinal(t *testing.T) {
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("ALL_PROXY", "")
	var mu sync.Mutex
	var requests []string
	creates := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == larkcore.TenantAccessTokenInternalUrlPath {
			fmt.Fprint(w, `{"code":0,"tenant_access_token":"group-sdk","expire":7200}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer group-sdk" {
			t.Errorf("wrong token")
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		content, _ := payload["content"].(string)
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, r.Method+" "+r.URL.Path+" "+content)
		if r.Method == http.MethodPatch {
			if r.URL.Path != "/open-apis/im/v1/messages/om_sdk_1" {
				t.Errorf("wrong patch target %s", r.URL.Path)
			}
			fmt.Fprint(w, `{"code":0,"data":{}}`)
			return
		}
		creates++
		fmt.Fprintf(w, `{"code":0,"data":{"message_id":"om_sdk_%d","chat_id":"oc_group"}}`, creates)
	}))
	defer server.Close()
	client := lark.NewClient("cli_"+t.Name(), "local-secret", lark.WithOpenBaseUrl(server.URL), lark.WithHttpClient(server.Client()))
	api := feishu.NewProjectChatAPI(feishu.NewFeishuCallBroker("sdk-group", client))
	store, _, _, now := publisherFixture(t)
	publisher, err := New(store, sdkGroupResolver{api}, func() time.Time { return *now })
	if err != nil {
		t.Fatal(err)
	}
	queueSnapshot(t, store, "a", "role", "first", workservice.PublicationProgress, *now)
	if n, e := publisher.RunOnce(t.Context(), 10); e != nil || n != 1 {
		t.Fatalf("start n%d %v", n, e)
	}
	for i := range 12 {
		queueSnapshot(t, store, fmt.Sprintf("burst%02d", i), "role", fmt.Sprintf("progress-%02d", i), workservice.PublicationProgress, now.Add(time.Duration(i+1)*time.Millisecond))
	}
	if n, e := publisher.RunOnce(t.Context(), 10); e != nil || n != 0 {
		t.Fatalf("burst leaked n%d %v", n, e)
	}
	*now = now.Add(31 * time.Second)
	if n, e := publisher.RunOnce(t.Context(), 10); e != nil || n != 1 {
		t.Fatalf("snapshot n%d %v", n, e)
	}
	queueSnapshot(t, store, "old", "role", "obsolete-after-work", workservice.PublicationProgress, *now)
	queueSnapshot(t, store, "done", "role", "delivered", workservice.PublicationFinal, now.Add(time.Millisecond))
	if n, e := publisher.RunOnce(t.Context(), 10); e != nil || n != 1 {
		t.Fatalf("final n%d %v", n, e)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 3 || !strings.Contains(requests[0], "first") || !strings.HasPrefix(requests[1], "PATCH ") || !strings.Contains(requests[1], "progress-11") || !strings.Contains(requests[2], "delivered") {
		t.Fatalf("wrong user-visible journey: %#v", requests)
	}
}
