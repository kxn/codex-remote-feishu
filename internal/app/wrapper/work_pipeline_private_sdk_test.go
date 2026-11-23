package wrapper

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

type pipelinePrivatePlatform struct {
	api      *feishu.ProjectChatAPI
	mu       sync.Mutex
	commands []string
}

func newPipelinePrivatePlatform(t *testing.T, chat, source string) *pipelinePrivatePlatform {
	t.Helper()
	platform := &pipelinePrivatePlatform{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == larkcore.TenantAccessTokenInternalUrlPath {
			fmt.Fprint(w, `{"code":0,"tenant_access_token":"private-pipeline-token","expire":7200}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer private-pipeline-token" {
			t.Error("private pipeline lost its bound Bot")
		}
		var body struct {
			Kind          string `json:"msg_type"`
			Content       string `json:"content"`
			ReplyInThread *bool  `json:"reply_in_thread"`
			ReceiveID     string `json:"receive_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		platform.mu.Lock()
		defer platform.mu.Unlock()
		switch {
		case r.Method == http.MethodPatch && r.URL.Path == "/open-apis/im/v1/messages/pipeline-private-1":
			platform.commands = append(platform.commands, "PATCH "+body.Content)
			fmt.Fprint(w, `{"code":0}`)
		case r.Method == http.MethodPost && r.URL.Path == "/open-apis/im/v1/messages":
			if body.ReplyInThread != nil || body.Kind != "interactive" || body.ReceiveID != chat || r.URL.Query().Get("receive_id_type") != "chat_id" {
				t.Errorf("native card did not create a top-level private message: %+v", body)
			}
			platform.commands = append(platform.commands, "CREATE "+body.Content)
			fmt.Fprintf(w, `{"code":0,"data":{"message_id":"pipeline-private-%d","chat_id":%q}}`, len(platform.commands), chat)
		default:
			t.Errorf("unexpected private SDK request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := lark.NewClient("cli_"+t.Name(), "fixture-only", lark.WithOpenBaseUrl(server.URL), lark.WithHttpClient(server.Client()))
	platform.api = feishu.NewProjectChatAPI(feishu.NewFeishuCallBroker(t.Name(), client))
	return platform
}

func (p *pipelinePrivatePlatform) Resolve(_ context.Context, binding work.RoleBotBinding) (workpublish.Sender, error) {
	if binding.RoleID != "pipeline-role" || binding.AppID != "pipeline-app" || binding.GatewayRef != "pipeline-gateway" || binding.IdentityGeneration != 1 {
		return nil, fmt.Errorf("unexpected bound private identity: %+v", binding)
	}
	return p.api, nil
}

func (p *pipelinePrivatePlatform) verify(t *testing.T, ctx context.Context, publisher *workpublish.Publisher, backend work.Backend) {
	t.Helper()
	want := 1
	if backend == work.BackendClaude {
		want++ // The Claude fixture emits a distinct stage paragraph.
	}
	if count, err := publisher.RunOnce(ctx, 16); err != nil || count != want {
		t.Fatalf("expected automatic reply: %d %v", count, err)
	}
	if count, err := publisher.RunOnce(ctx, 16); err != nil || count != 0 {
		t.Fatalf("expected no repeated private publications: %d %v", count, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	writes := p.commands
	if len(writes) != want || !strings.HasPrefix(writes[want-1], "CREATE ") || !strings.Contains(writes[want-1], "pipeline native final") {
		t.Fatalf("private native subprocess did not reach the same-card progress and final sequence: %s", writes)
	}
	if backend == work.BackendClaude && (!strings.HasPrefix(writes[0], "CREATE ") || !strings.Contains(writes[0], "Starting the explicitly admitted work.")) {
		t.Fatalf("intermediate Claude text was lost or reordered: %s", writes[0])
	}
}
