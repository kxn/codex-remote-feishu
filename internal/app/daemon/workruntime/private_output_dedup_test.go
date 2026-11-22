package workruntime

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateNativeAndExplicitSameResultAreOnePublication(t *testing.T) {
	for _, explicitFirst := range []bool{true, false} {
		name := map[bool]string{true: "explicit-first", false: "native-first"}[explicitFirst]
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, work.BackendCodex, false, true)
			f.coordinator.options.EnqueueReconcile = func(string) {}
			f.start()
			clock := func() time.Time { return f.now }
			signer, err := workauth.New(bytes.Repeat([]byte("k"), 32), clock)
			if err != nil {
				t.Fatal(err)
			}
			service, err := workservice.New(f.store, signer, clock)
			if err != nil {
				t.Fatal(err)
			}
			token, err := signer.Issue(workauth.Claims{
				Purpose: "worker", RoleID: "role", BotGeneration: 1, ScopeID: "scope", ContextID: "context",
				WorkerID: "worker", SessionGeneration: 1, ControllerGeneration: 1,
			}, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			explicit := func() {
				t.Helper()
				_, err := service.PublishMessage(t.Context(), token, "run", workservice.PublishMessageRequest{
					Key: "one-private-reply", Body: "single intended reply", ReplyToInputID: "input",
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			if explicitFirst {
				explicit()
			}
			for _, event := range []agentproto.Event{
				{Kind: agentproto.EventItemCompleted, ItemKind: "agent_message", ItemID: "native-final", Metadata: map[string]any{"text": "single intended reply", "phase": "final_answer"}},
				{Kind: agentproto.EventTurnCompleted, Status: "completed"},
			} {
				event.ThreadID, event.TurnID = "session", "turn-1"
				f.now = f.now.Add(time.Second)
				if err := f.handle(event); err != nil {
					t.Fatal(err)
				}
			}
			if !explicitFirst {
				explicit()
			}
			sdk := startPrivateSDK(t, false)
			publisher, err := workpublish.New(f.store, privateSDKResolver{sdk.api}, clock)
			if err != nil {
				t.Fatal(err)
			}
			drainPrivatePublisher(t, publisher)
			count := 0
			for _, request := range sdk.requests() {
				if strings.Contains(request.body, "single intended reply") {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("same run sent %d copies of the intended reply: %+v", count, sdk.requests())
			}
		})
	}
}
