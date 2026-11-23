package privatechat

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestCheckpointCrashKeepsInputsAndMenuWhileNativeHeld(t *testing.T) {
	ctx := context.Background()
	owner := Owner{"g", "s", "c", "u", "tenant", "app"}
	app := New(nil, orchestrator.Config{})
	if err := app.RegisterOwner(owner); err != nil {
		t.Fatal(err)
	}
	if err := app.RegisterInstance("s", &state.InstanceRecord{InstanceID: "i", Online: true, WorkspaceRoot: "/tmp/workspace", WorkspaceKey: "/tmp/workspace", ObservedFocusedThreadID: "thread", Threads: map[string]*state.ThreadRecord{"thread": {ThreadID: "thread", CWD: "/tmp/workspace"}}}); err != nil {
		t.Fatal(err)
	}
	action := func(kind control.ActionKind) control.Action {
		return control.Action{Kind: kind, GatewayID: "g", SurfaceSessionID: "s", ChatID: "c", ActorUserID: "u"}
	}
	attach := action(control.ActionAttachInstance)
	attach.InstanceID = "i"
	if _, err := app.Action(attach); err != nil {
		t.Fatal(err)
	}
	first := action(control.ActionTextMessage)
	first.MessageID = "m1"
	first.Text = "start"
	if _, err := app.Action(first); err != nil {
		t.Fatal(err)
	}
	img := action(control.ActionImageMessage)
	img.MessageID = "image"
	img.LocalPath = "/tmp/image.png"
	img.MIMEType = "image/png"
	if _, err := app.Action(img); err != nil {
		t.Fatal(err)
	}
	next := action(control.ActionTextMessage)
	next.MessageID = "m2"
	next.Text = "queued"
	if _, err := app.Action(next); err != nil {
		t.Fatal(err)
	}
	core, err := app.Service().ExportPrivateCheckpoint("s")
	if err != nil {
		t.Fatal(err)
	}
	if len(core.QueueItems) < 2 || len(core.Holds) == 0 {
		t.Fatalf("missing pending inputs/command: %#v", core)
	}
	path := filepath.Join(t.TempDir(), "private.db")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	seq, _, err := j.Admit(ctx, Ingress{Owner: owner, Generation: 1, EventID: "e1", Lane: "input", Payload: []byte(`{"message_id":"m1"}`), Sources: []string{"m1"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"image", "m2"} {
		seq, _, err = j.Admit(ctx, Ingress{Owner: owner, Generation: 1, EventID: "event-" + id, Lane: "input", Payload: []byte(`{}`), Sources: []string{id}})
		if err != nil {
			t.Fatal(err)
		}
	}
	cp := Checkpoint{Owner: owner, Generation: 1, Revision: 1, Cursor: seq, Core: core}
	effects := []Effect{{ID: "effect-1", Lane: "background", Body: []byte(`{"kind":"notice"}`)}}
	if err = j.CommitCheckpoint(ctx, cp, 0, effects); err != nil {
		t.Fatal(err)
	}
	missing := cp
	missing.Revision = 2
	missing.Core.StagedImages = map[string]*state.StagedImageRecord{"img-999": {ImageID: "img-999", SurfaceSessionID: "s", SourceMessageID: "never-admitted"}}
	missing.Core.NextImageID = 999
	if err = j.CommitCheckpoint(ctx, missing, 1, nil); err == nil {
		t.Fatal("checkpoint accepted an image without durable ingress")
	}
	collision := cp
	collision.Revision = 2
	if err = j.CommitCheckpoint(ctx, collision, 1, effects); err == nil {
		t.Fatal("duplicate outbox effect accepted")
	}
	unchanged, err := j.LoadCheckpoint(ctx, owner, 1)
	if err != nil || unchanged.Revision != 1 || unchanged.Cursor != seq {
		t.Fatalf("failed atomic commit changed checkpoint: %v %#v", err, unchanged)
	}
	if err = j.CommitCheckpoint(ctx, cp, 0, effects); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if err = j.MarkEffectSending(ctx, owner, 1, "effect-1"); err != nil {
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
	saved, err := j.LoadCheckpoint(ctx, owner, 1)
	if err != nil {
		t.Fatal(err)
	}
	got, err := j.RecoveryEffects(ctx, owner, 1, 0, -1, 10)
	if err != nil || len(got) != 1 || got[0].State != "unknown" {
		t.Fatalf("outbox recovery: %v %#v", err, got)
	}
	restored := New(nil, orchestrator.Config{})
	if err = restored.RegisterOwner(owner); err != nil {
		t.Fatal(err)
	}
	if err = restored.Service().RestorePrivateCheckpoint(saved.Core); err != nil {
		t.Fatal(err)
	}
	if _, ok := restored.Service().PrivateRecoveryCheckpoint("s"); !ok {
		t.Fatal("pending native work did not hold")
	}
	v := restored.Service().Surface("s")
	if len(v.QueueItems) != len(core.QueueItems) || len(v.QueuedQueueItemIDs) != len(core.QueuedQueueItemIDs) || len(v.StagedImages) != len(core.StagedImages) || restored.Service().Instance("i") != nil {
		t.Fatalf("recovery changed input or fabricated instance: %#v", v)
	}
	menu := action(control.ActionShowCommandMenu)
	menu.Text = "/menu"
	menu.LocalPageAction = true
	menu.MessageID = "menu-card"
	menu.Inbound = &control.ActionInboundMeta{CardCallback: true}
	events, err := restored.Action(menu)
	if err != nil || len(events) == 0 || events[0].PageView == nil {
		t.Fatalf("menu blocked: %v %#v", err, events)
	}
	if !events[0].InlineReplaceCurrentCard {
		t.Fatalf("menu did not target current card: %#v", events)
	}
	events, err = restored.Action(first)
	if err != nil || len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "private_recovery_hold" {
		t.Fatalf("unsafe execution not held: %v %#v", err, events)
	}
	if events = restored.Service().Tick(time.Now().Add(time.Hour)); len(events) != 0 {
		t.Fatalf("held tick emitted effects: %#v", events)
	}
}

func TestCheckpointIdleRestartAndV1Upgrade(t *testing.T) {
	ctx := context.Background()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	path := filepath.Join(t.TempDir(), "db")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	seq, _, err := j.Admit(ctx, Ingress{Owner: o, Generation: 1, EventID: "old-event", Lane: "input", Payload: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`DROP TABLE private_checkpoint; DROP TABLE private_effect; PRAGMA user_version=1`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	j, err = OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	pending, err := j.Pending(ctx, o, 1, "input", 0, 10)
	if err != nil || len(pending) != 1 || pending[0].Sequence != seq {
		t.Fatalf("v1 event lost: %v %#v", err, pending)
	}
	app := New(nil, orchestrator.Config{})
	if err = app.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	core, err := app.Service().ExportPrivateCheckpoint("s")
	if err != nil {
		t.Fatal(err)
	}
	cp := Checkpoint{Owner: o, Generation: 1, Revision: 1, Cursor: seq, Core: core}
	if err = j.CommitCheckpoint(ctx, cp, 0, nil); err != nil {
		t.Fatal(err)
	}
	restarted := New(nil, orchestrator.Config{})
	if err = restarted.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	if err = restarted.Service().RestorePrivateCheckpoint(core); err != nil {
		t.Fatal(err)
	}
	if _, held := restarted.Service().PrivateRecoveryCheckpoint("s"); held {
		t.Fatal("idle snapshot unnecessarily held")
	}
}

// The legacy reaction selects its target even when another input is older.
// The journal must retain that older input and bind only the selected source.
func TestReactionSelectsNonHeadAndJournalPreservesEarlierInput(t *testing.T) {
	ctx := context.Background()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	app := New(nil, orchestrator.Config{})
	if err := app.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	if err := app.RegisterInstance("s", &state.InstanceRecord{InstanceID: "i", Online: true, WorkspaceRoot: "/tmp/workspace", WorkspaceKey: "/tmp/workspace", ObservedFocusedThreadID: "thread", Threads: map[string]*state.ThreadRecord{"thread": {ThreadID: "thread", CWD: "/tmp/workspace"}}}); err != nil {
		t.Fatal(err)
	}
	action := func(k control.ActionKind) control.Action {
		return control.Action{Kind: k, GatewayID: "g", SurfaceSessionID: "s", ChatID: "c", ActorUserID: "u"}
	}
	attach := action(control.ActionAttachInstance)
	attach.InstanceID = "i"
	if _, err := app.Action(attach); err != nil {
		t.Fatal(err)
	}
	first := action(control.ActionTextMessage)
	first.MessageID = "active"
	first.Text = "active"
	if _, err := app.Action(first); err != nil {
		t.Fatal(err)
	}
	if _, err := app.AgentEvent("i", agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "thread", TurnID: "turn", Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"earlier", "selected"} {
		a := action(control.ActionTextMessage)
		a.MessageID = id
		a.Text = id
		if _, err := app.Action(a); err != nil {
			t.Fatal(err)
		}
	}
	reaction := action(control.ActionReactionCreated)
	reaction.TargetMessageID = "selected"
	reaction.ReactionType = "ThumbsUp"
	events, err := app.Action(reaction)
	if err != nil || len(events) != 1 || events[0].Command == nil || events[0].Command.Kind != agentproto.CommandTurnSteer || events[0].Command.Prompt.Inputs[0].Text != "selected" {
		t.Fatalf("legacy reaction: %v %#v", err, events)
	}
	j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	var seqs []int64
	for _, id := range []string{"earlier", "selected"} {
		seq, _, e := j.Admit(ctx, Ingress{Owner: o, Generation: 1, EventID: id, Lane: "input", Payload: []byte(`{}`), Sources: []string{id}})
		if e != nil {
			t.Fatal(e)
		}
		seqs = append(seqs, seq)
	}
	d := Dispatch{ID: "select", Lane: "input", Mode: "resume", Owner: o, Generation: 1, Session: NativeSession{"codex", "account", "thread", "carrier", 1}, CommandID: "cmd", InputSequences: []int64{seqs[1]}}
	native := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "carrier", InstanceID: "i", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "account"}
	lease, err := j.OpenNativeLease(ctx, testNativeAuthority{}, native, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	command := *events[0].Command
	command.CommandID = "cmd"
	command.NativeBinding = &agentproto.NativeTurnBinding{Version: 1, Owner: native, ExecutionID: "execution"}
	command.NativeInputAdmission = &agentproto.NativeInputAdmissionCommand{Version: agentproto.NativeInputAdmissionProtocolVersion, ID: d.ID, InputIDs: []string{"selected"}}
	if err = lease.prepareNativeDispatch(ctx, d, command); err != nil {
		t.Fatal(err)
	}
	pending, err := j.Pending(ctx, o, 1, "input", 0, 10)
	if err != nil || len(pending) != 1 || pending[0].Sequence != seqs[0] {
		t.Fatalf("earlier input not retained: %v %#v", err, pending)
	}
}
