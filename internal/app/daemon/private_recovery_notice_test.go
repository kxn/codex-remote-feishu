package daemon

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateRecoveryNoticeSuppressesCompletedOrCanceledInput(t *testing.T) {
	for _, status := range []state.QueueItemStatus{state.QueueItemCompleted, state.QueueItemDiscarded} {
		for _, phase := range []string{"prepared", "sending", "unknown-prepared", "unknown-operation"} {
			t.Run(fmt.Sprintf("%s/%s", status, phase), func(t *testing.T) {
				ctx := t.Context()
				o := privatechat.Owner{GatewayID: "g", SurfaceID: "s", ChatID: "chat", UserID: "user", TenantID: "tenant", AppID: "app"}
				j, err := privatechat.OpenJournal(filepath.Join(t.TempDir(), "journal.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer j.Close()
				seq, _, err := j.Admit(ctx, privatechat.Ingress{Owner: o, Generation: 1, EventID: "msg", Lane: "input", Payload: []byte(`{}`), Sources: []string{"msg"}})
				if err != nil {
					t.Fatal(err)
				}
				seed := privatechat.New(nil, orchestrator.Config{})
				if err = seed.RegisterOwner(o); err != nil {
					t.Fatal(err)
				}
				surface := seed.Service().Surface(o.SurfaceID)
				surface.QueueItems = map[string]*state.QueueItemRecord{"queue-1": {ID: "queue-1", SurfaceSessionID: o.SurfaceID, SourceKind: state.QueueItemSourceUser, SourceMessageID: "msg", Status: state.QueueItemQueued}}
				surface.QueuedQueueItemIDs = []string{"queue-1"}
				cp, err := seed.Service().ExportPrivateCheckpoint(o.SurfaceID)
				if err != nil {
					t.Fatal(err)
				}
				cp.NextQueueID = 1
				hint := eventcontract.Event{Kind: eventcontract.KindTimelineText, SurfaceSessionID: o.SurfaceID, GatewayID: o.GatewayID, SourceMessageID: "msg", TimelineText: &control.TimelineText{Type: control.TimelineTextRecoveryInputWaiting, Text: "waiting", ReplyToMessageID: "msg"}}
				body, err := privatechat.EncodeEffectEvent(hint)
				if err != nil {
					t.Fatal(err)
				}
				if err = j.CommitCheckpoint(ctx, privatechat.Checkpoint{Owner: o, Generation: 1, Revision: 1, Cursor: seq, Core: cp}, 0, []privatechat.Effect{{ID: "hint", Lane: "interactive", Body: body}}); err != nil {
					t.Fatal(err)
				}
				surface.QueueItems["queue-1"].Status = status
				surface.QueuedQueueItemIDs = nil
				cp, err = seed.Service().ExportPrivateCheckpoint(o.SurfaceID)
				if err != nil {
					t.Fatal(err)
				}
				cp.NextQueueID = 1
				var after []privatechat.Effect
				if status == state.QueueItemCompleted {
					final := eventcontract.Event{Kind: eventcontract.KindBlockCommitted, SurfaceSessionID: o.SurfaceID, SourceMessageID: "msg", Block: &render.Block{Text: "done", Final: true}}
					finalBody, e := privatechat.EncodeEffectEvent(final)
					if e != nil {
						t.Fatal(e)
					}
					after = []privatechat.Effect{{ID: "final", Lane: "interactive", Body: finalBody}}
				}
				if err = j.CommitCheckpoint(ctx, privatechat.Checkpoint{Owner: o, Generation: 1, Revision: 2, Cursor: seq, Core: cp}, 1, after); err != nil {
					t.Fatal(err)
				}
				app := privatechat.New(nil, orchestrator.Config{})
				gateway := &privatePublishGateway{}
				host := New(":0", ":0", gateway, agentproto.ServerIdentity{})
				host.service = app.Service()
				p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
				if err != nil {
					t.Fatal(err)
				}
				publisher := &privatePublisher{host: host, processor: p, journal: j}
				if phase != "prepared" {
					if err = publisher.prepare(ctx, o, 1, "hint"); err != nil {
						t.Fatal(err)
					}
					if err = j.MarkEffectSending(ctx, o, 1, "hint"); err != nil {
						t.Fatal(err)
					}
					if phase == "unknown-operation" {
						if err = j.BeginPublicationOperation(ctx, o, 1, "hint", 0); err != nil {
							t.Fatal(err)
						}
						if err = j.CompletePublicationOperation(ctx, o, 1, "hint", 0, "unknown", []byte(`{"reason":"uncertain"}`), nil); err != nil {
							t.Fatal(err)
						}
					}
					if phase == "unknown-prepared" || phase == "unknown-operation" {
						if err = j.ResolveEffect(ctx, o, 1, "hint", "unknown", []byte(`{"reason":"uncertain"}`), nil); err != nil {
							t.Fatal(err)
						}
					}
				}
				if phase == "unknown-operation" {
					if _, err = publisher.Publish(ctx, o, 1, "hint"); !errors.Is(err, errPrivatePublicationUnknown) || len(gateway.operations) != 0 {
						t.Fatalf("uncertain operation was suppressed or sent: %v", err)
					}
					record, e := j.LoadEffect(ctx, o, 1, "hint")
					if e != nil || record.State != "unknown" {
						t.Fatalf("uncertain evidence lost: %s %v", record.State, e)
					}
					return
				}
				if _, err = publisher.Publish(ctx, o, 1, "hint"); err != nil {
					t.Fatal(err)
				}
				if len(gateway.operations) != 0 {
					t.Fatal("obsolete recovery notice reached platform")
				}
				record, err := j.LoadEffect(ctx, o, 1, "hint")
				if err != nil || record.State != "confirmed" {
					t.Fatalf("suppressed effect: %s %v", record.State, err)
				}
				if _, err = publisher.Publish(ctx, o, 1, "hint"); err != nil || len(gateway.operations) != 0 {
					t.Fatalf("suppression replay: %v", err)
				}
			})
		}
	}
}
