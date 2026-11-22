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

func TestPrivateDeduplicatedExplicitFinalRetainsNativeProcessingEvidence(t *testing.T) {
	for _, mode := range []string{"normal", "quiet"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, work.BackendCodex, false, true)
			setPrivateDisplayMode(f, mode)
			f.start()
			clock := func() time.Time { return f.now }
			signer, err := workauth.New(bytes.Repeat([]byte("n"), 32), clock)
			if err != nil {
				t.Fatal(err)
			}
			service, err := workservice.New(f.store, signer, clock)
			if err != nil {
				t.Fatal(err)
			}
			token, err := signer.Issue(workauth.Claims{Purpose: "worker", RoleID: "role", BotGeneration: 1, ScopeID: "scope", ContextID: "context", WorkerID: "worker", SessionGeneration: 1, ControllerGeneration: 1}, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			const answer = "one actual answer from the same native run"
			if _, err := service.PublishMessage(t.Context(), token, "run", workservice.PublishMessageRequest{Key: "explicit-private-answer", Body: answer, ReplyToInputID: "input"}); err != nil {
				t.Fatal(err)
			}
			privateFinalEvent(f, answer)
			sdk := startPrivateSDK(t, false)
			publisher, err := workpublish.New(f.store, privateSDKResolver{sdk.api}, clock)
			if err != nil {
				t.Fatal(err)
			}
			drainPrivatePublisher(t, publisher)
			if err := completePrivateTurn(f); err != nil {
				t.Fatal(err)
			}
			assertPrivateSucceeded(t, f, 1)
			drainPrivatePublisher(t, publisher)
			replies := 0
			for _, request := range sdk.requests() {
				if strings.Contains(request.body, answer) {
					replies++
				}
			}
			if replies != 1 {
				t.Fatalf("native final deduplication published %d copies: %+v", replies, sdk.requests())
			}
		})
	}
}

func TestPrivateCommentaryOnlyTurnShowsExplicitEndingAndSettles(t *testing.T) {
	f := newFixture(t, work.BackendCodex, false, true)
	f.start()
	if err := f.handle(agentproto.Event{Kind: agentproto.EventItemCompleted, ThreadID: "session", TurnID: "turn-1", ItemKind: "agent_message", ItemID: "only-commentary", Metadata: map[string]any{"text": "visible stage answer", "phase": "commentary"}}); err != nil {
		t.Fatal(err)
	}
	if err := completePrivateTurn(f); err != nil {
		t.Fatal(err)
	}
	assertPrivateSucceeded(t, f, 1)
	sdk := startPrivateSDK(t, false)
	publisher, err := workpublish.New(f.store, privateSDKResolver{sdk.api}, func() time.Time { return f.now })
	if err != nil {
		t.Fatal(err)
	}
	drainPrivatePublisher(t, publisher)
	stage, end := 0, 0
	for _, request := range sdk.requests() {
		if strings.Contains(request.body, "visible stage answer") {
			stage++
		}
		if strings.Contains(request.body, "本轮没有返回最终答复") {
			end++
		}
	}
	if stage != 1 || end != 1 {
		t.Fatalf("commentary-only turn must preserve its stage and show one honest ending: %+v", sdk.requests())
	}
}
