package workruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/claude"
	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

func TestPrivateNativeReplyJourney(t *testing.T) {
	for _, backend := range []work.Backend{work.BackendCodex, work.BackendClaude} {
		t.Run(string(backend), func(t *testing.T) {
			f := newFixture(t, backend, false, true)
			ctx := context.Background()
			f.coordinator.options.EnqueueReconcile = func(string) {}
			stateDir := t.TempDir()
			journal, err := runtimejournal.Open(ctx, filepath.Join(stateDir, "runtime.db"), stateDir, t.TempDir(), f.carrier.Incarnation)
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			publish := func(events []agentproto.Event) {
				t.Helper()
				stable, e := journal.PersistEvents(events)
				if e != nil {
					t.Fatal(e)
				}
				for _, event := range stable {
					if e := f.coordinator.HandleEvent(ctx, f.carrier.OriginInstanceID, event); e != nil {
						t.Fatalf("%s: %v", event.Kind, e)
					}
				}
			}
			if backend == work.BackendCodex {
				translate := codex.NewTranslator(f.carrier.OriginInstanceID)
				publish([]agentproto.Event{{Kind: agentproto.EventTurnStarted, CommandID: "command", ThreadID: "session", TurnID: "turn-1"}})
				for _, frame := range codexPrivateOutputFrames() {
					f.now = f.now.Add(time.Second)
					result, e := translate.ObserveServer([]byte(frame))
					if e != nil {
						t.Fatal(e)
					}
					publish(result.Events)
				}
			} else {
				translate := claude.NewTranslator(f.carrier.OriginInstanceID)
				translate.PrepareForChildLaunch("session")
				if _, e := translate.TranslateCommand(f.command); e != nil {
					t.Fatal(e)
				}
				for _, frame := range []string{
					`{"type":"assistant","session_id":"session","message":{"id":"msg1","content":[{"type":"text","text":"**Claude reply**\n\n\u0060\u0060\u0060go\nreturn 1\n\u0060\u0060\u0060"}]}}`,
					`{"type":"result","subtype":"success","session_id":"session","is_error":false,"usage":{"input_tokens":19,"output_tokens":7}}`,
				} {
					f.now = f.now.Add(time.Second)
					result, e := translate.ObserveServer([]byte(frame))
					if e != nil {
						t.Fatal(e)
					}
					publish(result.Events)
				}
			}

			// A new daemon opens the same store before publication. The worker
			// replays every event because none of its receipts were delivered.
			if err := f.store.Close(); err != nil {
				t.Fatal(err)
			}
			f.store, err = workstore.Open(ctx, f.path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.store.Close()
			f.coordinator = New(f.store, Options{Driver: f.driver, Clock: func() time.Time { return f.now }, EnqueueReconcile: func(string) {}})
			replayed, _, err := journal.Replay(0, 128)
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range replayed {
				if err := f.coordinator.HandleEvent(ctx, f.carrier.OriginInstanceID, event); err != nil {
					t.Fatalf("replay %s: %v", event.Kind, err)
				}
			}
			sdk := startPrivateSDK(t, false)
			publisher, err := workpublish.New(f.store, privateSDKResolver{sdk.api}, func() time.Time { return f.now })
			if err != nil {
				t.Fatal(err)
			}
			drainPrivatePublisher(t, publisher)
			assertPrivateSDKOutput(t, f, sdk, backend)
			count := len(sdk.requests())
			drainPrivatePublisher(t, publisher)
			if len(sdk.requests()) != count {
				t.Fatal("replay sent duplicate output")
			}
		})
	}
}

func codexPrivateOutputFrames() []string {
	return []string{
		`{"method":"item/completed","params":{"threadId":"session","turnId":"turn-1","item":{"id":"status","type":"agentMessage","phase":"commentary","text":"Preparing the example."}}}`,
		`{"method":"turn/plan/updated","params":{"threadId":"session","turnId":"turn-1","explanation":"User-readable plan","plan":[{"step":"Check the inputs","status":"completed"}]}}`,
		`{"method":"item/completed","params":{"threadId":"session","turnId":"turn-1","item":{"id":"patch","type":"fileChange","status":"completed","changes":[{"path":"example.go","kind":{"type":"update"},"diff":"@@ -1 +1 @@\n-old\n+new"}]}}}`,
		`{"method":"item/completed","params":{"threadId":"session","turnId":"turn-1","item":{"id":"private-summary","type":"reasoning","summary":["**Public summary**"],"content":["hidden reasoning must never appear"]}}}`,
		`{"method":"item/completed","params":{"threadId":"session","turnId":"turn-1","item":{"id":"img","type":"imageGeneration","status":"completed","result":"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADElEQVR42mP8z8BQDwAFhQIAW7+nMgAAAABJRU5ErkJggg=="}}}`,
		`{"method":"item/completed","params":{"threadId":"session","turnId":"turn-1","item":{"id":"final","type":"agentMessage","phase":"final_answer","text":"**Codex reply**\n\n\u0060\u0060\u0060go\nreturn 1\n\u0060\u0060\u0060"}}}`,
		`{"method":"thread/tokenUsage/updated","params":{"threadId":"session","turnId":"turn-1","tokenUsage":{"last":{"inputTokens":17,"outputTokens":9,"totalTokens":26},"total":{"inputTokens":27,"outputTokens":19,"totalTokens":46}}}}`,
		`{"method":"turn/completed","params":{"threadId":"session","turn":{"id":"turn-1","status":"completed"}}}`,
	}
}

type privateSDKResolver struct{ api *feishu.ProjectChatAPI }

func (r privateSDKResolver) Resolve(_ context.Context, binding work.RoleBotBinding) (workpublish.Sender, error) {
	if binding != (work.RoleBotBinding{RoleID: "role", AppID: "app", GatewayRef: "gateway", IdentityGeneration: 1}) {
		return nil, fmt.Errorf("unexpected private bot: %+v", binding)
	}
	return r.api, nil
}

type privateSDKRequest struct{ method, path, kind, body, messageID string }
type privateSDK struct {
	api      *feishu.ProjectChatAPI
	mu       sync.Mutex
	request  []privateSDKRequest
	failEdit bool
}

func startPrivateSDK(t *testing.T, failEdit bool) *privateSDK {
	t.Helper()
	sdk := &privateSDK{failEdit: failEdit}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == larkcore.TenantAccessTokenInternalUrlPath {
			fmt.Fprint(w, `{"code":0,"tenant_access_token":"private-test-token","expire":7200}`)
			return
		}
		sdk.mu.Lock()
		defer sdk.mu.Unlock()
		if r.URL.Path == "/open-apis/im/v1/images" {
			if err := r.ParseMultipartForm(1 << 20); err != nil || r.FormValue("image_type") != "message" {
				t.Errorf("invalid image upload: %v", err)
			}
			sdk.request = append(sdk.request, privateSDKRequest{method: r.Method, path: r.URL.Path})
			fmt.Fprint(w, `{"code":0,"data":{"image_key":"img_private"}}`)
			return
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
		req := privateSDKRequest{method: r.Method, path: r.URL.Path, kind: body.Kind, body: body.Content}
		if r.Method == http.MethodPatch {
			sdk.request = append(sdk.request, req)
			if sdk.failEdit {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			fmt.Fprint(w, `{"code":0}`)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/open-apis/im/v1/messages" || r.URL.Query().Get("receive_id_type") != "chat_id" || body.ReceiveID != "private-chat" || body.ReplyInThread != nil {
			t.Errorf("private execution did not create a top-level message in the direct chat: %+v", req)
		}
		req.messageID = fmt.Sprintf("om_private_%d", len(sdk.request)+1)
		sdk.request = append(sdk.request, req)
		fmt.Fprintf(w, `{"code":0,"data":{"message_id":%q,"chat_id":"private-chat"}}`, req.messageID)
	}))
	t.Cleanup(server.Close)
	client := lark.NewClient("cli_"+t.Name(), "test-secret", lark.WithOpenBaseUrl(server.URL), lark.WithHttpClient(server.Client()))
	sdk.api = feishu.NewProjectChatAPI(feishu.NewFeishuCallBroker(t.Name(), client))
	return sdk
}

func (sdk *privateSDK) requests() []privateSDKRequest {
	sdk.mu.Lock()
	defer sdk.mu.Unlock()
	return append([]privateSDKRequest(nil), sdk.request...)
}

func drainPrivatePublisher(t *testing.T, publisher *workpublish.Publisher) {
	t.Helper()
	for range 12 {
		count, err := publisher.RunOnce(context.Background(), 30)
		if err != nil {
			t.Fatal(err)
		}
		if count == 0 {
			return
		}
	}
	t.Fatal("private outputs did not drain")
}

func assertPrivateSDKOutput(t *testing.T, f *fixture, sdk *privateSDK, backend work.Backend) {
	t.Helper()
	requests := sdk.requests()
	var reply, patch, images int
	progressPatch := ""
	expected := map[work.Backend]string{work.BackendCodex: "Codex reply", work.BackendClaude: "Claude reply"}[backend]
	for i, request := range requests {
		if strings.Contains(request.body, "hidden reasoning") {
			t.Fatal("native private reasoning reached the platform")
		}
		if request.method == http.MethodPatch {
			patch++
			progressPatch += request.body
			found := false
			for _, previous := range requests[:i] {
				if previous.messageID != "" && request.path == "/open-apis/im/v1/messages/"+previous.messageID && strings.Contains(previous.body, "Check the inputs") {
					found = true
				}
			}
			if !found {
				t.Fatalf("progress did not patch the same card: %+v", request)
			}
		}
		if request.kind == "image" {
			images++
		}
		if strings.Contains(request.body, expected) {
			reply++
			if i != len(requests)-1 || !strings.Contains(request.body, "return 1") || !strings.Contains(request.body, "线程累计") {
				t.Fatalf("final reply missing code, exact usage or terminal ordering: %+v", request)
			}
			f.tx(func(tx *workstore.Tx) error {
				mapping, err := tx.GetPlatformMessageMapping("tenant", "private-chat", request.messageID)
				if err != nil {
					return err
				}
				op, err := tx.GetOperation(mapping.OperationID)
				if err != nil {
					return err
				}
				projected, err := workservice.ReadMessageProjection(tx, op)
				if err != nil {
					return err
				}
				if mapped := projected.PrivateDisplay; mapping.ScopeID != "scope" || op.ContextID != "context" || mapped == nil || mapped.ResultHash == "" {
					return fmt.Errorf("final reply lost its durable owner: %+v", mapping)
				}
				return nil
			})
		}
	}
	if reply != 1 || (backend == work.BackendCodex && patch == 0) {
		t.Fatalf("no single automatic final and shared progress updates: reply=%d patch=%d requests=%+v", reply, patch, requests)
	}
	if backend == work.BackendCodex && (images != 1 || !strings.Contains(progressPatch, "example.go") || !strings.Contains(progressPatch, "Check the inputs")) {
		t.Fatalf("Codex did not deliver the image, file change, and plan: image=%d progress=%s", images, progressPatch)
	}
}
