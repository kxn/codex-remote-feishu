package workruntime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workreaction"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	lark "github.com/larksuite/oapi-sdk-go/v3"
)

type reactionSDK struct {
	mu                      sync.Mutex
	api                     *feishu.ProjectChatAPI
	items                   map[string]feishu.ProjectReaction
	creates, deletes, lists int
	failNextCreate          bool
	messages                map[string]string
}

func newReactionSDK(t *testing.T) *reactionSDK {
	t.Helper()
	sdk := &reactionSDK{items: map[string]feishu.ProjectReaction{}, messages: map[string]string{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "tenant_access_token") {
			fmt.Fprint(w, `{"code":0,"tenant_access_token":"reaction-fixture","expire":7200}`)
			return
		}
		sdk.mu.Lock()
		defer sdk.mu.Unlock()
		if !strings.Contains(r.URL.Path, "/reactions") {
			t.Errorf("unexpected reaction route %s", r.URL.Path)
			http.Error(w, "unexpected", 500)
			return
		}
		switch r.Method {
		case http.MethodPost:
			var body struct {
				ReactionType struct {
					Emoji string `json:"emoji_type"`
				} `json:"reaction_type"`
			}
			if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
				t.Error(e)
			}
			sdk.creates++
			id := fmt.Sprintf("own-%d", sdk.creates)
			sdk.messages[id] = strings.Split(strings.TrimPrefix(r.URL.Path, "/open-apis/im/v1/messages/"), "/")[0]
			sdk.items[id] = feishu.ProjectReaction{ID: id, Emoji: body.ReactionType.Emoji, OperatorID: "app", OperatorType: "app"}
			if sdk.failNextCreate {
				sdk.failNextCreate = false
				w.WriteHeader(503)
				fmt.Fprint(w, `{"code":999,"msg":"unknown fixture write"}`)
				return
			}
			fmt.Fprintf(w, `{"code":0,"data":{"reaction_id":%q}}`, id)
		case http.MethodDelete:
			id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			if strings.HasPrefix(id, "human-") || strings.HasPrefix(id, "other-") {
				t.Errorf("deleted another actor's reaction %s", id)
			}
			delete(sdk.items, id)
			sdk.deletes++
			fmt.Fprint(w, `{"code":0}`)
		case http.MethodGet:
			sdk.lists++
			var items []map[string]any
			for _, item := range sdk.items {
				if item.Emoji != r.URL.Query().Get("reaction_type") {
					continue
				}
				items = append(items, map[string]any{"reaction_id": item.ID, "reaction_type": map[string]string{"emoji_type": item.Emoji}, "operator": map[string]string{"operator_id": item.OperatorID, "operator_type": item.OperatorType}})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"items": items, "has_more": false}})
		default:
			t.Errorf("unexpected reaction method %s", r.Method)
		}
	}))
	t.Cleanup(server.Close)
	client := lark.NewClient("app", "fixture-secret", lark.WithOpenBaseUrl(server.URL), lark.WithHttpClient(server.Client()))
	sdk.api = feishu.NewProjectChatAPI(feishu.NewFeishuCallBroker(t.Name(), client))
	return sdk
}
func (s *reactionSDK) own(emoji string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.items {
		if r.Emoji == emoji && r.OperatorID == "app" && r.OperatorType == "app" {
			return true
		}
	}
	return false
}
func reactionRun(t *testing.T, f *fixture, sdk *reactionSDK, store *workstore.Store) {
	t.Helper()
	service := workreaction.Service{Store: store, Resolver: privateSDKResolver{sdk.api}, Now: func() time.Time { return f.now }}
	if e := service.RunOnce(t.Context(), 16); e != nil {
		t.Fatal(e)
	}
}
func queueReactionInput(t *testing.T, f *fixture, id string, awaiting bool) {
	t.Helper()
	f.tx(func(tx *workstore.Tx) error {
		old, e := tx.GetMessage("private-human-input")
		if e != nil {
			return e
		}
		old.ID = id + "-message"
		old.PlatformKey = &work.MessageKey{Tenant: "tenant", ChatID: "private-chat", MessageID: "om_" + id}
		old.Revision = 1
		input := work.Inbox{ID: id, ScopeID: old.ScopeID, RecipientRoleID: "role", RecipientContextID: "context", EventID: id + "-event", MessageID: old.ID, State: work.InboxPending, AwaitingText: awaiting, Revision: 1}
		_, _, e = tx.AcceptMessage(old, []work.Inbox{input})
		return e
	})
}

func TestPrivateReactionSDKQueuedCancelledAndTerminalLifecycle(t *testing.T) {
	for _, status := range []string{"completed", "failed", "interrupted"} {
		t.Run(status, func(t *testing.T) {
			f := newFixture(t, work.BackendCodex, false, true)
			sdk := newReactionSDK(t)
			reactionRun(t, f, sdk, f.store)
			if !sdk.own("THINKING") {
				t.Fatal("claimed prompt lacks THINKING")
			}
			f.start()
			queueReactionInput(t, f, "queued", false)
			reactionRun(t, f, sdk, f.store)
			if !sdk.own("OneSecond") {
				t.Fatal("new input during execution lacks queue reaction")
			}
			f.tx(func(tx *workstore.Tx) error {
				input, e := tx.GetInbox("queued")
				if e != nil {
					return e
				}
				input.State = work.InboxCancelled
				previous := input.Revision
				input.Revision++
				return tx.PutInbox(input, previous)
			})
			reactionRun(t, f, sdk, f.store)
			if sdk.own("OneSecond") || !sdk.own("ThumbsDown") {
				t.Fatal("discard did not replace queue reaction")
			}
			f.report()
			if e := f.handle(agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "session", TurnID: "turn-1", Status: status}); e != nil {
				t.Fatal(e)
			}
			f.now = f.now.Add(2 * time.Second)
			reactionRun(t, f, sdk, f.store)
			if sdk.own("THINKING") {
				t.Fatal("terminal input still shows THINKING")
			}
			sdk.mu.Lock()
			before := sdk.creates + sdk.deletes
			sdk.mu.Unlock()
			reopened, e := workstore.Open(t.Context(), f.path)
			if e != nil {
				t.Fatal(e)
			}
			defer reopened.Close()
			reactionRun(t, f, sdk, reopened)
			sdk.mu.Lock()
			defer sdk.mu.Unlock()
			if sdk.creates+sdk.deletes != before {
				t.Fatal("restart duplicated confirmed reactions")
			}
		})
	}
}

func TestPrivateReactionSDKUnknownCreateReconcilesExactBotAfterRestart(t *testing.T) {
	f := newFixture(t, work.BackendCodex, false, true)
	sdk := newReactionSDK(t)
	sdk.failNextCreate = true
	service := workreaction.Service{Store: f.store, Resolver: privateSDKResolver{sdk.api}, Now: func() time.Time { return f.now }}
	if e := service.RunOnce(t.Context(), 16); e == nil {
		t.Fatal("uncertain HTTP write falsely confirmed")
	}
	sdk.mu.Lock()
	sdk.items["human-thinking"] = feishu.ProjectReaction{ID: "human-thinking", Emoji: "THINKING", OperatorType: "user", OperatorID: "person"}
	sdk.items["other-thinking"] = feishu.ProjectReaction{ID: "other-thinking", Emoji: "THINKING", OperatorType: "app", OperatorID: "another-app"}
	sdk.mu.Unlock()
	reopened, e := workstore.Open(t.Context(), f.path)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	f.now = f.now.Add(11 * time.Second)
	reactionRun(t, f, sdk, reopened)
	sdk.mu.Lock()
	if sdk.creates != 1 || sdk.lists != 1 {
		t.Errorf("unknown create replayed: creates=%d lists=%d", sdk.creates, sdk.lists)
	}
	sdk.mu.Unlock()
	f.start()
	f.report()
	if e = f.handle(agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "session", TurnID: "turn-1", Status: "completed"}); e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(2 * time.Second)
	reactionRun(t, f, sdk, reopened)
	sdk.mu.Lock()
	defer sdk.mu.Unlock()
	if sdk.deletes != 1 || sdk.items["human-thinking"].ID == "" || sdk.items["other-thinking"].ID == "" {
		t.Fatal("cleanup crossed reaction operator ownership")
	}
}

func TestPrivateReactionSDKRecallStopsWithoutReaddingOrDeletingGoneMessage(t *testing.T) {
	f := newFixture(t, work.BackendCodex, false, true)
	sdk := newReactionSDK(t)
	reactionRun(t, f, sdk, f.store)
	f.tx(func(tx *workstore.Tx) error {
		_, _, e := tx.AcceptPlatformObservation(workstore.PlatformObservation{Gateway: work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "role", BotGeneration: 1}, EventID: "human-recalled", Tenant: "tenant", ChatID: "private-chat", MessageID: "om_user", Kind: "message.recalled", Facts: json.RawMessage(`{"recalled":true}`), ReceivedAt: f.now})
		return e
	})
	f.now = f.now.Add(2 * time.Second)
	reactionRun(t, f, sdk, f.store)
	sdk.mu.Lock()
	defer sdk.mu.Unlock()
	if sdk.creates != 1 || sdk.deletes != 0 {
		t.Fatal("recalled message received fresh reaction traffic")
	}
}
