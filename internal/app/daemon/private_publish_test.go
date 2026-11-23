package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

type privatePublishGateway struct {
	calls      int
	fail       bool
	operations []feishu.Operation
}

func TestPrivatePublisherQueuedProgressUpdatesConfirmedCardAndUnknownDoesNotBlockMenu(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(fmt.Sprint("unknown=", unknown), func(t *testing.T) {
			gateway := &privatePublishGateway{fail: unknown}
			host := New(":0", ":0", gateway, agentproto.ServerIdentity{})
			o := privatechat.Owner{GatewayID: "g", SurfaceID: "s", ChatID: "chat", UserID: "user", TenantID: "tenant", AppID: "bot"}
			seed := privatechat.New(nil, orchestrator.Config{})
			if err := seed.RegisterOwner(o); err != nil {
				t.Fatal(err)
			}
			core, err := seed.Service().ExportPrivateCheckpoint(o.SurfaceID)
			if err != nil {
				t.Fatal(err)
			}
			j, err := privatechat.OpenJournal(filepath.Join(t.TempDir(), "private.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer j.Close()
			var effects []privatechat.Effect
			for i := 1; i <= 2; i++ {
				progress := control.ExecCommandProgress{ThreadID: "thread", TurnID: "turn", ItemID: "command"}
				for row := 1; row <= i; row++ {
					progress.Timeline = append(progress.Timeline, control.ExecCommandProgressTimelineItem{ID: fmt.Sprintf("command-%d", row), Kind: "command_execution", Label: "执行", Summary: fmt.Sprintf("echo step-%d", row), LastSeq: row})
				}
				raw, e := privatechat.EncodeEffectEvent(eventcontract.Event{Kind: eventcontract.KindExecCommandProgress, SurfaceSessionID: o.SurfaceID, GatewayID: o.GatewayID, ExecCommandProgress: &progress})
				if e != nil {
					t.Fatal(e)
				}
				// Both business snapshots precede the first platform receipt; their
				// message fields are empty. The native chain binder is covered in
				// privatechat; this fixture exercises its persisted publisher input.
				effects = append(effects, privatechat.Effect{ID: fmt.Sprintf("progress-%d", i), Lane: "interactive", DisplayKey: "exact-carrier-thread-turn", Body: raw})
			}
			if err = j.CommitCheckpoint(t.Context(), privatechat.Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: core}, 0, effects); err != nil {
				t.Fatal(err)
			}
			app := privatechat.New(nil, orchestrator.Config{})
			host.service = app.Service()
			processor, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
			if err != nil {
				t.Fatal(err)
			}
			publisher := &privatePublisher{host: host, processor: processor, journal: j}
			if _, err = publisher.Publish(t.Context(), o, 1, "progress-2"); !errors.Is(err, privatechat.ErrDisplayHeld) {
				t.Fatalf("unconfirmed predecessor bypassed: %v", err)
			}
			_, err = publisher.Publish(t.Context(), o, 1, "progress-1")
			if unknown {
				if !errors.Is(err, errPrivatePublicationUnknown) {
					t.Fatal(err)
				}
				gateway.fail = false
				a := control.Action{Kind: control.ActionShowCommandMenu, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID, Text: "/menu"}
				raw, _ := json.Marshal(a)
				seq, _, e := j.Admit(t.Context(), privatechat.Ingress{Owner: o, Generation: 1, EventID: "menu", Lane: "control", Payload: raw})
				if e != nil {
					t.Fatal(e)
				}
				menu, e := processor.ApplyIngress(t.Context(), o, 1, seq)
				if e != nil || len(menu) != 1 {
					t.Fatalf("menu: %v", e)
				}
				if _, e = publisher.Publish(t.Context(), o, 1, menu[0].Effect.ID); e != nil {
					t.Fatalf("unknown progress froze menu: %v", e)
				}
				if _, e = publisher.Publish(t.Context(), o, 1, "progress-2"); !errors.Is(e, privatechat.ErrDisplayHeld) {
					t.Fatalf("unknown display chain reopened: %v", e)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = publisher.Publish(t.Context(), o, 1, "progress-2"); err != nil {
				t.Fatal(err)
			}
			if len(gateway.operations) != 2 || gateway.operations[0].Kind != feishu.OperationSendCard || gateway.operations[1].Kind != feishu.OperationUpdateCard || gateway.operations[1].MessageID != gateway.operations[0].MessageID {
				t.Fatalf("progress made duplicate cards: %#v", gateway.operations)
			}
		})
	}
}

func (g *privatePublishGateway) Start(context.Context, feishu.ActionHandler) error { return nil }
func (g *privatePublishGateway) Apply(_ context.Context, ops []feishu.Operation) error {
	g.calls++
	if g.fail {
		return errors.New("reply accepted but connection closed before response")
	}
	for i := range ops {
		if ops[i].Kind == feishu.OperationAddReaction {
			ops[i].ReactionID = "platform-reaction-" + ops[i].EmojiType
		}
		if ops[i].MessageID == "" {
			ops[i].MessageID = fmt.Sprintf("platform-%d", g.calls)
		}
		g.operations = append(g.operations, ops[i])
	}
	return nil
}

func TestPrivatePublisherRestoresReactionIdentityAcrossUnrelatedStatusAndRestart(t *testing.T) {
	gateway := &privatePublishGateway{}
	o := privatechat.Owner{GatewayID: "g", SurfaceID: "s", ChatID: "chat", UserID: "user", TenantID: "tenant", AppID: "bot"}
	path := filepath.Join(t.TempDir(), "private.db")
	j, err := privatechat.OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	seed := privatechat.New(nil, orchestrator.Config{})
	if err = seed.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	core, err := seed.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	statuses := []control.PendingInputState{{SourceMessageID: "source", TypingOn: true}, {SourceMessageID: "source", ThumbsUp: true}, {SourceMessageID: "source", TypingOff: true}}
	var effects []privatechat.Effect
	for i, status := range statuses {
		raw, e := privatechat.EncodeEffectEvent(eventcontract.Event{Kind: eventcontract.KindPendingInput, SurfaceSessionID: o.SurfaceID, PendingInput: &status})
		if e != nil {
			t.Fatal(e)
		}
		effects = append(effects, privatechat.Effect{ID: fmt.Sprintf("status-%d", i), Lane: "interactive", DisplayKey: "same-source-message", Body: raw})
	}
	if err = j.CommitCheckpoint(t.Context(), privatechat.Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: core}, 0, effects); err != nil {
		t.Fatal(err)
	}
	makePublisher := func(j *privatechat.Journal) *privatePublisher {
		h := New(":0", ":0", gateway, agentproto.ServerIdentity{})
		a := privatechat.New(nil, orchestrator.Config{})
		h.service = a.Service()
		p, e := privatechat.NewProcessor(a, j, privateHostTestDecoder{}, privateHostReducer{h})
		if e != nil {
			t.Fatal(e)
		}
		return &privatePublisher{host: h, processor: p, journal: j}
	}
	p := makePublisher(j)
	for _, id := range []string{"status-0", "status-1"} {
		if _, err = p.Publish(t.Context(), o, 1, id); err != nil {
			t.Fatal(err)
		}
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = privatechat.OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	p = makePublisher(j)
	if _, err = p.Publish(t.Context(), o, 1, "status-2"); err != nil {
		t.Fatal(err)
	}
	last := gateway.operations[len(gateway.operations)-1]
	if last.Kind != feishu.OperationRemoveReaction || last.MessageID != "source" || last.EmojiType != "THINKING" || last.ReactionID != "platform-reaction-THINKING" || last.ReactionAbsent {
		t.Fatalf("restart forgot actual older reaction: %#v", last)
	}
}

func TestPrivatePublisherMenuUsesRealProjectionAndDoesNotReplayAfterRestart(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(fmt.Sprint("uncertain=", uncertain), func(t *testing.T) {
			gateway := &privatePublishGateway{fail: uncertain}
			path := filepath.Join(t.TempDir(), "private.db")
			o := privatechat.Owner{GatewayID: "g", SurfaceID: "s", ChatID: "chat", UserID: "user", TenantID: "tenant", AppID: "bot"}
			open := func() (*privatechat.Journal, *privatePublisher) {
				j, err := privatechat.OpenJournal(path)
				if err != nil {
					t.Fatal(err)
				}
				host := New(":0", ":0", gateway, agentproto.ServerIdentity{})
				app := privatechat.New(nil, orchestrator.Config{})
				host.service = app.Service()
				p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
				if err != nil {
					t.Fatal(err)
				}
				return j, &privatePublisher{host: host, processor: p, journal: j}
			}
			j, p := open()
			a := control.Action{Kind: control.ActionShowCommandMenu, Text: "/menu", GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID}
			raw, _ := json.Marshal(a)
			seq, _, err := j.Admit(t.Context(), privatechat.Ingress{Owner: o, Generation: 1, EventID: "menu", Lane: "control", Payload: raw})
			if err != nil {
				t.Fatal(err)
			}
			effects, err := p.processor.ApplyIngress(t.Context(), o, 1, seq)
			if err != nil || len(effects) != 1 {
				t.Fatalf("menu: %v %#v", err, effects)
			}
			id := effects[0].Effect.ID
			_, err = p.Publish(t.Context(), o, 1, id)
			if uncertain && !errors.Is(err, errPrivatePublicationUnknown) || !uncertain && err != nil {
				t.Fatalf("publish: %v", err)
			}
			if gateway.calls != 1 {
				t.Fatalf("initial sends: %d", gateway.calls)
			}
			if !uncertain {
				if len(gateway.operations) != 1 || gateway.operations[0].ReplyToMessageID != "" || gateway.operations[0].CardTitle == "" {
					t.Fatalf("menu was not a direct card: %#v", gateway.operations)
				}
				cp, e := j.LoadCheckpoint(t.Context(), o, 1)
				if e != nil || cp.Core.SurfaceMessages["platform-1"] == nil {
					t.Fatalf("receipt not projected: %v", e)
				}
			}
			if err = j.Close(); err != nil {
				t.Fatal(err)
			}
			j, p = open()
			defer j.Close()
			gateway.fail = false
			_, err = p.Publish(t.Context(), o, 1, id)
			if uncertain && !errors.Is(err, errPrivatePublicationUnknown) || !uncertain && err != nil {
				t.Fatalf("restart: %v", err)
			}
			if gateway.calls != 1 {
				t.Fatal("restart duplicated the menu")
			}
		})
	}
}
