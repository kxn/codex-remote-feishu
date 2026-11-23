package daemon

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateReactionUnsupportedBackendPublishesOneIndependentSDKNotice(t *testing.T) {
	f := newReactionActionFixture(t, work.BackendOpenCode)
	var mu sync.Mutex
	notices := 0
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "tenant_access_token"):
			fmt.Fprint(w, `{"code":0,"tenant_access_token":"notice-fixture","expire":7200}`)
		case r.URL.Path == "/callback/ws/endpoint":
			fmt.Fprint(w, `{"code":1}`)
		case r.Method == http.MethodPost && r.URL.Path == "/open-apis/im/v1/messages":
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			posts++
			number := posts
			mu.Unlock()
			if strings.Contains(string(raw), errTeamReactionUnsupported.Error()) {
				mu.Lock()
				notices++
				mu.Unlock()
				if strings.Contains(string(raw), "菜单首页") || strings.Contains(string(raw), "系统管理") {
					t.Error("unsupported reaction opened a navigation card")
				}
			}
			fmt.Fprintf(w, `{"code":0,"data":{"message_id":"notice-%d","chat_id":"private-chat"}}`, number)
		default:
			t.Errorf("unexpected notice request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", 500)
		}
	}))
	defer server.Close()
	useDeliveryJourneyGateway(t, f.a, server.URL, f.source.Gateway)
	var revision int64
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		nav, e := tx.GetPrivateNavigation(f.source.ScopeID)
		revision = nav.Revision
		return e
	})
	f.reaction(t, "unsupported-human-reaction", "ThumbsUp", f.source.SenderIDs, time.Now())
	f.relay.send = func(_ string, _ agentproto.Command) error {
		t.Fatal("unsupported backend received native input")
		return nil
	}
	for range 2 {
		if err := f.a.runTeamPrivateReactionActions(t.Context()); err != nil {
			t.Fatal(err)
		}
		for range 8 {
			n, err := f.a.teamRuntime.publisher.RunOnce(t.Context(), 16)
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				break
			}
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if notices != 1 {
		t.Fatalf("unsupported notices=%d", notices)
	}
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		input, e := tx.GetInbox(f.late.ID)
		if e != nil {
			return e
		}
		if input.State != work.InboxPending {
			t.Fatal("unsupported reaction consumed input")
		}
		nav, e := tx.GetPrivateNavigation(f.source.ScopeID)
		if e != nil {
			return e
		}
		if nav.Revision != revision {
			t.Fatal("reaction feedback stole current navigation")
		}
		return nil
	})
}
