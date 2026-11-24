package privatechat

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestShutdownPreparedPromptUsesDurableUnsentEvidence(t *testing.T) {
	for _, mode := range []string{"unregistered", "prepared", "sending", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			j, p, l, o, b := effectFixture(t)
			action := privateAction(o, control.ActionTextMessage)
			action.MessageID = "message"
			action.Text = "hello"
			_, effects := applyEffectAction(t, j, p, o, "message", "input", []string{"message"}, action)
			effect := commandEffect(t, effects, agentproto.CommandPromptSend)
			if mode != "unregistered" {
				d, _, err := l.PrepareEffectDispatch(t.Context(), effect.Effect.ID, b)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "sending" || mode == "unknown" {
					if err = l.MarkSending(t.Context(), d.ID); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "unknown" {
					if err = l.MarkDispatchUnknown(t.Context(), d.ID, "uncertain"); err != nil {
						t.Fatal(err)
					}
				}
			}
			saved, err := l.RecordStoppedPreparedUnsent(t.Context(), effect.Effect.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := mode == "unregistered" || mode == "prepared"
			if saved != want {
				t.Fatalf("mode %s recorded=%v", mode, saved)
			}
			if want {
				if _, err = p.ApplyLocalHostReceipt(t.Context(), o, 1, effect.Effect.ID, ShutdownKnownUnsentReducer{}); err != nil {
					t.Fatal(err)
				}
				surface := p.app.Service().Surface(o.SurfaceID)
				if surface.ActiveQueueItemID != "" || len(surface.QueuedQueueItemIDs) != 1 || surface.QueueItems[surface.QueuedQueueItemIDs[0]].Status != state.QueueItemQueued {
					t.Fatal("unsent prompt not restored")
				}
			} else {
				record, err := j.LoadEffect(t.Context(), o, 1, effect.Effect.ID)
				if err != nil || record.State != "prepared" {
					t.Fatalf("uncertain effect changed: %+v %v", record, err)
				}
			}
		})
	}
}

func TestShutdownUnsentReceiptRestoresAfterJournalRestart(t *testing.T) {
	j, p, l, o, _ := effectFixture(t)
	action := privateAction(o, control.ActionTextMessage)
	action.MessageID = "message"
	action.Text = "hello"
	_, effects := applyEffectAction(t, j, p, o, "message", "input", []string{"message"}, action)
	effect := commandEffect(t, effects, agentproto.CommandPromptSend)
	saved, err := l.RecordStoppedPreparedUnsent(t.Context(), effect.Effect.ID)
	if err != nil || !saved {
		t.Fatalf("unsent receipt %v %v", saved, err)
	}
	var seq int
	var name, path string
	if err = j.db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	p, err = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyLocalHostReceipt(t.Context(), o, 1, effect.Effect.ID, ShutdownKnownUnsentReducer{}); err != nil {
		t.Fatal(err)
	}
	cp, err := j.LoadCheckpoint(t.Context(), o, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cp.Core.ActiveQueueItemID != "" || len(cp.Core.QueuedQueueItemIDs) != 1 {
		t.Fatalf("not recovered: %+v", cp.Core)
	}
	if _, err = p.ApplyLocalHostReceipt(t.Context(), o, 1, effect.Effect.ID, ShutdownKnownUnsentReducer{}); err != nil {
		t.Fatal(err)
	}
}

func TestShutdownUnsentRejectsTamperedPreparedCommand(t *testing.T) {
	j, p, l, o, b := effectFixture(t)
	action := privateAction(o, control.ActionTextMessage)
	action.MessageID = "message"
	action.Text = "hello"
	_, effects := applyEffectAction(t, j, p, o, "message", "input", []string{"message"}, action)
	effect := commandEffect(t, effects, agentproto.CommandPromptSend)
	dispatch, _, err := l.PrepareEffectDispatch(t.Context(), effect.Effect.ID, b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = j.db.Exec(`UPDATE native_commands SET payload_hash='changed' WHERE dispatch_id=?`, dispatch.ID); err != nil {
		t.Fatal(err)
	}
	if recorded, err := l.RecordStoppedPreparedUnsent(t.Context(), effect.Effect.ID); err == nil || recorded {
		t.Fatal("tampered command accepted")
	}
}
