package privatechat

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func TestDetachedUnknownCancelsEarlierInputs(t *testing.T) {
	ctx := context.Background()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	seed := New(nil, orchestrator.Config{})
	if err = seed.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	core, err := seed.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	core.AttachedInstanceID = "old"
	core.Holds = []orchestrator.PrivateRecoveryHold{{Kind: "pending_remote", InstanceID: "old", Frozen: json.RawMessage(`{}`)}}
	if err = j.CommitCheckpoint(ctx, Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: core}, 0, nil); err != nil {
		t.Fatal(err)
	}
	input := privateAction(o, control.ActionTextMessage)
	input.MessageID = "old-input"
	input.Text = "old"
	old := admittedAction(t, j, o, "old-input", "input", input)
	detach := privateAction(o, control.ActionDetach)
	seq := admittedAction(t, j, o, "detach", "control", detach)
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyIngress(ctx, o, 1, seq); err != nil {
		t.Fatal(err)
	}
	var by int64
	if err = j.db.QueryRow(`SELECT canceled_by FROM private_applied WHERE seq=?`, old).Scan(&by); err != nil || by != seq {
		t.Fatalf("detached unknown did not cancel previous input: %d %v", by, err)
	}
}

func TestExplicitDetachCancelsOnlyEarlierSameOwnerInputs(t *testing.T) {
	ctx := context.Background()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	first := Owner{"g", "s1", "c1", "u1", "tenant", "app"}
	second := Owner{"g", "s2", "c2", "u2", "tenant", "app"}
	input := privateAction(first, control.ActionTextMessage)
	input.MessageID = "first-input"
	input.Text = "earlier"
	old := admittedAction(t, j, first, "old", "input", input)
	other := privateAction(second, control.ActionTextMessage)
	other.MessageID = "other"
	other.Text = "other"
	foreign := admittedAction(t, j, second, "other", "input", other)
	detach := privateAction(first, control.ActionDetach)
	controlSeq := admittedAction(t, j, first, "detach", "control", detach)
	later := privateAction(first, control.ActionTextMessage)
	later.MessageID = "later"
	later.Text = "later"
	future := admittedAction(t, j, first, "future", "input", later)
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyIngress(ctx, first, 1, controlSeq); err != nil {
		t.Fatal(err)
	}
	var rev, by int64
	if err = j.db.QueryRow(`SELECT revision,canceled_by FROM private_applied WHERE seq=?`, old).Scan(&rev, &by); err != nil || rev != 0 || by != controlSeq {
		t.Fatalf("cancel provenance revision=%d control=%d err=%v", rev, by, err)
	}
	replay, err := p.ApplyIngress(ctx, first, 1, old)
	if err != nil || len(replay) != 0 {
		t.Fatalf("cancelled replay %v %v", replay, err)
	}
	for _, seq := range []int64{foreign, future} {
		var n int
		if err = j.db.QueryRow(`SELECT count(*) FROM private_applied WHERE seq=?`, seq).Scan(&n); err != nil || n != 0 {
			t.Fatalf("unrelated input %d consumed: %d %v", seq, n, err)
		}
	}
	if _, err = p.ApplyIngress(ctx, second, 1, foreign); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyIngress(ctx, first, 1, future); err != nil {
		t.Fatal(err)
	}
}

func TestRejectedNewDoesNotCancelEarlierInput(t *testing.T) {
	ctx := context.Background()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	input := privateAction(o, control.ActionTextMessage)
	input.MessageID = "input"
	input.Text = "keep"
	seq := admittedAction(t, j, o, "input", "input", input)
	action := privateAction(o, control.ActionNewThread)
	controlSeq := admittedAction(t, j, o, "new", "control", action)
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyIngress(ctx, o, 1, controlSeq); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = j.db.QueryRow(`SELECT count(*) FROM private_applied WHERE seq=?`, seq).Scan(&n); err != nil || n != 0 {
		t.Fatalf("failed /new consumed input: %d %v", n, err)
	}
}

func TestNewOnSamePreparedWorkspaceCancelsOnlyEarlierInputs(t *testing.T) {
	ctx := context.Background()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	n := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	l, err := j.OpenNativeLease(ctx, testNativeAuthority{}, n, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.RegisterNativeHello(ctx, l, nativeHello(n)); err != nil {
		t.Fatal(err)
	}
	attach := privateAction(o, control.ActionAttachInstance)
	attach.InstanceID = n.InstanceID
	if _, err = p.app.Action(attach); err != nil {
		t.Fatal(err)
	}
	newAction := privateAction(o, control.ActionNewThread)
	// First /new establishes the empty-thread route before any pending input.
	first := admittedAction(t, j, o, "new-first", "control", newAction)
	if _, err = p.ApplyIngress(ctx, o, 1, first); err != nil {
		t.Fatal(err)
	}
	input := privateAction(o, control.ActionTextMessage)
	input.MessageID = "input"
	input.Text = "old route"
	old := admittedAction(t, j, o, "input-old", "input", input)
	second := admittedAction(t, j, o, "new-second", "control", newAction)
	if _, err = p.ApplyIngress(ctx, o, 1, second); err != nil {
		t.Fatal(err)
	}
	var by int64
	if err = j.db.QueryRow(`SELECT canceled_by FROM private_applied WHERE seq=?`, old).Scan(&by); err != nil || by != second {
		t.Fatalf("same route /new did not cancel input: %d %v", by, err)
	}
}
