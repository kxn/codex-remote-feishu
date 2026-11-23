package privatechat

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func effectFixture(t *testing.T) (*Journal, *Processor, *NativeLease, Owner, agentproto.NativeTurnBinding) {
	t.Helper()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	native := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	l, err := j.OpenNativeLease(t.Context(), testNativeAuthority{}, native, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.RegisterNativeHello(t.Context(), l, nativeHello(native)); err != nil {
		t.Fatal(err)
	}
	i := p.app.Service().Instance("instance")
	i.CapabilitiesDeclared = false
	i.ObservedFocusedThreadID = "thread"
	i.Threads["thread"] = &state.ThreadRecord{ThreadID: "thread", CWD: "/tmp/workspace"}
	a := privateAction(o, control.ActionAttachInstance)
	a.InstanceID = "instance"
	applyEffectAction(t, j, p, o, "attach", "control", nil, a)
	u := privateAction(o, control.ActionUseThread)
	u.ThreadID = "thread"
	applyEffectAction(t, j, p, o, "use", "control", nil, u)
	return j, p, l, o, agentproto.NativeTurnBinding{Version: 1, Owner: native, ExecutionID: "execution"}
}

func applyEffectAction(t *testing.T, j *Journal, p *Processor, o Owner, id, lane string, sources []string, a control.Action) (int64, []EffectRecord) {
	t.Helper()
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	seq, _, err := j.Admit(t.Context(), Ingress{Owner: o, Generation: 1, EventID: id, Lane: lane, Payload: raw, Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := p.ApplyIngress(t.Context(), o, 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	return seq, effects
}

func commandEffect(t *testing.T, effects []EffectRecord, kind agentproto.CommandKind) EffectRecord {
	t.Helper()
	for _, e := range effects {
		event, err := DecodeEffectEvent(e.Effect.Body)
		if err != nil {
			t.Fatal(err)
		}
		if event.Command != nil && event.Command.Kind == kind {
			return e
		}
	}
	t.Fatalf("no %s command in committed effects", kind)
	return EffectRecord{}
}

func TestEffectDispatchKeepsSelectedMessageAndImage(t *testing.T) {
	j, p, l, o, b := effectFixture(t)
	message := func(id string) (int64, []EffectRecord) {
		a := privateAction(o, control.ActionTextMessage)
		a.MessageID = id
		a.Text = id
		return applyEffectAction(t, j, p, o, id, "input", []string{id}, a)
	}
	_, start := message("running")
	startEffect := commandEffect(t, start, agentproto.CommandPromptSend)
	d, _, err := l.PrepareEffectDispatch(t.Context(), startEffect.Effect.ID, b)
	if err != nil {
		t.Fatal(err)
	}
	// This fixture isolates core queue selection after the first native turn
	// accepted. The complete translator/receipt/crash journey has its own test.
	p.app.Service().ApplyAgentEvent("instance", agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "thread", TurnID: "turn", Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown}})
	if _, err = j.db.Exec(`UPDATE dispatch SET state='accepted' WHERE id=?`, d.ID); err != nil {
		t.Fatal(err)
	}
	first, _ := message("first")
	a := privateAction(o, control.ActionImageMessage)
	a.MessageID = "image"
	a.LocalPath = "/tmp/reference.png"
	a.MIMEType = "image/png"
	imageSeq, _ := applyEffectAction(t, j, p, o, "image", "input", []string{"image"}, a)
	selected, _ := message("selected")
	a = privateAction(o, control.ActionReactionCreated)
	a.TargetMessageID = "selected"
	a.ReactionType = "THUMBSUP"
	_, effects := applyEffectAction(t, j, p, o, "reaction", "control", nil, a)
	e := commandEffect(t, effects, agentproto.CommandTurnSteer)
	frozen, _, err := DecodeStoredEffect(e.Effect.Body)
	if err != nil {
		t.Fatal(err)
	}
	d, c, err := l.PrepareEffectDispatch(t.Context(), e.Effect.ID, b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.InputSequences, []int64{imageSeq, selected}) {
		t.Fatalf("selected source cluster lost: %#v; earlier=%d", d.InputSequences, first)
	}
	if !reflect.DeepEqual(c.Prompt, frozen.Command.Prompt) {
		t.Fatal("dispatch reordered or changed prompt")
	}
	if c.NativeInputAdmission == nil || len(c.NativeInputAdmission.InputIDs) != 1 {
		t.Fatal("steer lost exact queue selection")
	}
	if err = l.MarkSending(t.Context(), d.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = l.PrepareEffectDispatch(t.Context(), e.Effect.ID, b); err != nil {
		t.Fatalf("exact replay: %v", err)
	}
	if err = l.MarkSending(t.Context(), d.ID); err == nil {
		t.Fatal("replay allowed a second socket send")
	}
}

func TestEffectDispatchCannotBorrowAnotherInputOrCarrier(t *testing.T) {
	j, p, l, o, b := effectFixture(t)
	a := privateAction(o, control.ActionTextMessage)
	a.MessageID = "text"
	a.Text = "hello"
	seq, effects := applyEffectAction(t, j, p, o, "text", "input", []string{"text"}, a)
	e := commandEffect(t, effects, agentproto.CommandPromptSend)
	wrong := b
	wrong.Owner.CarrierIncarnation = "other"
	if _, _, err := l.PrepareEffectDispatch(t.Context(), e.Effect.ID, wrong); err == nil {
		t.Fatal("foreign carrier acquired effect")
	}
	if _, err := j.db.Exec(`DELETE FROM private_applied WHERE seq=?`, seq); err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.PrepareEffectDispatch(t.Context(), e.Effect.ID, b); err == nil {
		t.Fatal("unapplied source used as native input")
	}
	var count int
	if err := j.db.QueryRow(`SELECT count(*) FROM native_commands`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed prepare left command: %d %v", count, err)
	}
}

func TestEffectControlAndAutomaticInputsNeedNoInventedSDKMessage(t *testing.T) {
	for _, kind := range []string{"control", "auto_whip", "auto_continue"} {
		t.Run(kind, func(t *testing.T) {
			j, _, l, o, b := effectFixture(t)
			c := agentproto.Command{CommandID: "frozen-control", Kind: agentproto.CommandTurnInterrupt, Target: agentproto.Target{ThreadID: "thread", TurnID: "turn"}}
			var inputs *orchestrator.FrozenCommandInputs
			cp, err := j.LoadCheckpoint(t.Context(), o, 1)
			if err != nil {
				t.Fatal(err)
			}
			if kind != "control" {
				c.Kind = agentproto.CommandPromptSend
				c.Target.ExecutionMode = agentproto.PromptExecutionModeResumeExisting
				c.Prompt.Inputs = []agentproto.Input{{Type: agentproto.InputText, Text: "continue the existing work"}}
				inputs = &orchestrator.FrozenCommandInputs{CommandID: c.CommandID, SurfaceID: o.SurfaceID, InstanceID: "instance", Items: []orchestrator.FrozenCommandInput{{QueueItemID: "queue-auto", SourceKind: state.QueueItemSourceKind(kind), AutoContinueEpisodeID: "episode"}}}
				cp.Core.AutoWhip.Enabled = true
				cp.Core.AutoContinue.Enabled = true
				cp.Core.AutoContinue.Episode = &state.PendingAutoContinueEpisodeRecord{EpisodeID: "episode"}
			}
			target := "instance"
			body, err := encodeEffectTarget(eventcontract.Event{SurfaceSessionID: o.SurfaceID, Command: &c}, inputs, &target)
			if err != nil {
				t.Fatal(err)
			}
			previous := cp.Revision
			cp.Revision++
			if err = j.CommitCheckpoint(context.Background(), cp, previous, []Effect{{ID: "cause", Lane: "background", Body: body}}); err != nil {
				t.Fatal(err)
			}
			d, _, err := l.PrepareEffectDispatch(t.Context(), "cause", b)
			if err != nil {
				t.Fatal(err)
			}
			if len(d.InputSequences) != 0 {
				t.Fatal("invented a human source for a control/automatic effect")
			}
		})
	}
}

func TestEffectAutomaticSettingDisabledBeforePreparation(t *testing.T) {
	j, _, l, o, b := effectFixture(t)
	c := agentproto.Command{CommandID: "automatic", Kind: agentproto.CommandPromptSend, Target: agentproto.Target{ThreadID: "thread", ExecutionMode: agentproto.PromptExecutionModeResumeExisting}}
	inputs := &orchestrator.FrozenCommandInputs{CommandID: c.CommandID, SurfaceID: o.SurfaceID, InstanceID: "instance", Items: []orchestrator.FrozenCommandInput{{QueueItemID: "auto", SourceKind: state.QueueItemSourceAutoWhip}}}
	target := "instance"
	body, err := encodeEffectTarget(eventcontract.Event{SurfaceSessionID: o.SurfaceID, Command: &c}, inputs, &target)
	if err != nil {
		t.Fatal(err)
	}
	cp, err := j.LoadCheckpoint(t.Context(), o, 1)
	if err != nil {
		t.Fatal(err)
	}
	prev := cp.Revision
	cp.Revision++
	cp.Core.AutoWhip.Enabled = false
	if err = j.CommitCheckpoint(t.Context(), cp, prev, []Effect{{ID: "cause", Lane: "background", Body: body}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = l.PrepareEffectDispatch(t.Context(), "cause", b); !errors.Is(err, ErrEffectSuperseded) {
		t.Fatalf("disabled automatic effect: %v", err)
	}
}
