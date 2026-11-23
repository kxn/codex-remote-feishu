package workruntime

import (
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workdisplay"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateLivePublicSummaryUsesSameSDKCardAndFinalUsage(t *testing.T) {
	for _, mode := range []string{"quiet", "normal", "verbose", "chatty"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, work.BackendCodex, false, true)
			f.coordinator.options.EnqueueReconcile = func(string) {}
			setPrivateDisplayMode(f, mode)
			sdk := startPrivateSDK(t, false)
			publisher, e := workpublish.New(f.store, privateSDKResolver{sdk.api}, func() time.Time { return f.now })
			if e != nil {
				t.Fatal(e)
			}
			stateDir := t.TempDir()
			journal, e := runtimejournal.Open(t.Context(), filepath.Join(stateDir, "runtime.db"), stateDir, t.TempDir(), f.carrier.Incarnation)
			if e != nil {
				t.Fatal(e)
			}
			defer journal.Close()
			emit := func(events []agentproto.Event) {
				t.Helper()
				folded, e := journal.PersistEvents(events)
				if e != nil {
					t.Fatal(e)
				}
				for _, event := range folded {
					if e = f.coordinator.HandleEvent(t.Context(), f.carrier.OriginInstanceID, event); e != nil {
						t.Fatal(e)
					}
				}
			}
			emit([]agentproto.Event{{Kind: agentproto.EventTurnStarted, CommandID: "command", ThreadID: "session", TurnID: "turn-1"}})
			drainPrivatePublisher(t, publisher)
			translator := codex.NewTranslator(f.carrier.OriginInstanceID)
			live := func(raw string) {
				t.Helper()
				result, e := translator.ObserveServer([]byte(raw))
				if e != nil {
					t.Fatal(e)
				}
				emit(result.Events)
			}
			f.now = f.now.Add(time.Second)
			live(`{"method":"item/reasoning/summaryTextDelta","params":{"threadId":"session","turnId":"turn-1","itemId":"reason-a","summaryIndex":0,"delta":"**Inspecting inputs**"}}`)
			drainPrivatePublisher(t, publisher)
			before := len(sdk.requests())
			f.now = f.now.Add(100 * time.Millisecond)
			live(`{"method":"item/reasoning/summaryTextDelta","params":{"threadId":"session","turnId":"turn-1","itemId":"reason-b","summaryIndex":0,"delta":"**Checking context**"}}`)
			drainPrivatePublisher(t, publisher)
			if len(sdk.requests()) != before {
				t.Fatal("public summary exceeded old one-second flush cadence")
			}
			f.now = f.now.Add(time.Second)
			if e = workdisplay.FlushPublicReasoning(t.Context(), f.store, f.now, 32); e != nil {
				t.Fatal(e)
			}
			drainPrivatePublisher(t, publisher)
			detailed := mode == "verbose" || mode == "chatty"
			summarySeen := false
			var anchor string
			for _, request := range sdk.requests() {
				if anchor == "" && request.messageID != "" {
					anchor = request.messageID
				}
				if strings.Contains(request.body, "Checking context") {
					summarySeen = true
					if request.method != "PATCH" || request.path != "/open-apis/im/v1/messages/"+anchor {
						t.Fatal("live summary created another card instead of updating original")
					}
				}
			}
			if summarySeen != detailed {
				t.Fatalf("summary visibility mode=%s seen=%v", mode, summarySeen)
			}
			live(`{"method":"item/reasoning/textDelta","params":{"threadId":"session","turnId":"turn-1","itemId":"reason-b","delta":"RAW_PRIVATE_THOUGHT_SECRET"}}`)
			live(`{"method":"thread/tokenUsage/updated","params":{"threadId":"session","turnId":"turn-1","tokenUsage":{"last":{"inputTokens":17,"outputTokens":9,"totalTokens":26},"total":{"inputTokens":27,"outputTokens":19,"totalTokens":46}}}}`)
			// A token observation without an exact turn may inform thread status, but
			// cannot become the current attempt's final usage by temporal proximity.
			live(`{"method":"thread/tokenUsage/updated","params":{"threadId":"session","tokenUsage":{"last":{"inputTokens":999,"totalTokens":999},"total":{"totalTokens":999}}}}`)
			final, e := translator.ObserveServer([]byte(`{"method":"item/completed","params":{"threadId":"session","turnId":"turn-1","item":{"id":"final","type":"agentMessage","phase":"final_answer","text":"Safe final"}}}`))
			if e != nil {
				t.Fatal(e)
			}
			emit(final.Events)
			emit([]agentproto.Event{{Kind: agentproto.EventTurnCompleted, CommandID: "command", ThreadID: "session", TurnID: "turn-1", Status: "completed"}})
			drainPrivatePublisher(t, publisher)
			finalSeen := false
			for _, request := range sdk.requests() {
				if strings.Contains(request.body, "RAW_PRIVATE_THOUGHT_SECRET") {
					t.Fatal("private reasoning reached Feishu")
				}
				if strings.Contains(request.body, "Safe final") {
					finalSeen = true
					if !strings.Contains(request.body, "17") || !strings.Contains(request.body, "27") || strings.Contains(request.body, "999") {
						t.Fatalf("final card did not use exact turn's latest usage: %s", request.body)
					}
				}
			}
			if !finalSeen {
				t.Fatal("final reply missing")
			}
			f.tx(func(tx *workstore.Tx) error {
				observation, e := tx.GetThreadTokenUsage(f.carrier.Incarnation, "session")
				if e != nil {
					return e
				}
				if observation.Usage.Last.TotalTokens != 999 {
					t.Fatal("thread-only usage observation not retained for status")
				}
				return nil
			})
		})
	}
}
