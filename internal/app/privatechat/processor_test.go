package privatechat

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type actionDecoder struct{}

func (actionDecoder) Decode(e JournalEntry) (control.Action, error) {
	var a control.Action
	err := json.Unmarshal(e.Ingress.Payload, &a)
	return a, err
}

type failingReducer struct{}

func (failingReducer) Reduce(a *App, action control.Action) ([]eventcontract.Event, error) {
	_, _ = a.Action(action)
	return nil, errors.New("failure after core mutation")
}
func admittedAction(t *testing.T, j *Journal, o Owner, id, lane string, action control.Action) int64 {
	t.Helper()
	raw, err := json.Marshal(action)
	if err != nil {
		t.Fatal(err)
	}
	seq, _, err := j.Admit(context.Background(), Ingress{Owner: o, Generation: 1, EventID: id, Lane: lane, Payload: raw, Sources: []string{id}})
	if err != nil {
		t.Fatal(err)
	}
	return seq
}
func privateAction(o Owner, kind control.ActionKind) control.Action {
	return control.Action{Kind: kind, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID}
}

func TestProcessorCommitsOnceAndKeepsMenuIndependent(t *testing.T) {
	ctx := context.Background()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	input := privateAction(o, control.ActionTextMessage)
	input.MessageID = "text"
	input.Text = "hello"
	inputSeq := admittedAction(t, j, o, "text", "input", input)
	menu := privateAction(o, control.ActionShowCommandMenu)
	menu.Text = "/menu"
	menuSeq := admittedAction(t, j, o, "menu", "control", menu)
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	results := make(chan []EffectRecord, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); out, e := p.ApplyIngress(ctx, o, 1, menuSeq); errs <- e; results <- out }()
	}
	wg.Wait()
	close(errs)
	close(results)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var effectID string
	for out := range results {
		if len(out) != 1 || out[0].Effect.Lane != "interactive" {
			t.Fatalf("menu effects: %#v", out)
		}
		if effectID == "" {
			effectID = out[0].Effect.ID
		} else if effectID != out[0].Effect.ID {
			t.Fatal("duplicate apply changed effect identity")
		}
	}
	var count int
	if err = j.db.QueryRow(`SELECT count(*) FROM private_applied WHERE seq=?`, menuSeq).Scan(&count); err != nil || count != 1 {
		t.Fatalf("applied marker: %d %v", count, err)
	}
	// Menu did not advance or consume the earlier text.
	if err = j.db.QueryRow(`SELECT count(*) FROM private_applied WHERE seq=?`, inputSeq).Scan(&count); err != nil || count != 0 {
		t.Fatalf("input consumed: %d %v", count, err)
	}
	if _, err = p.ApplyIngress(ctx, o, 1, inputSeq); err != nil {
		t.Fatal(err)
	}
	replayed, err := p.ApplyIngress(ctx, o, 1, menuSeq)
	if err != nil || len(replayed) != 1 || replayed[0].Effect.ID != effectID {
		t.Fatalf("older committed event replay: %v %#v", err, replayed)
	}
}

func TestProcessorPoisonAfterPartialReductionAndRecover(t *testing.T) {
	ctx := context.Background()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	menu := privateAction(o, control.ActionShowCommandMenu)
	menu.Text = "/menu"
	first := admittedAction(t, j, o, "first", "control", menu)
	second := admittedAction(t, j, o, "second", "control", menu)
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyIngress(ctx, o, 1, first); err != nil {
		t.Fatal(err)
	}
	p.reducer = failingReducer{}
	if _, err = p.ApplyIngress(ctx, o, 1, second); !errors.Is(err, ErrProcessorPoisoned) {
		t.Fatalf("failure not poisoned: %v", err)
	}
	if _, err = p.ApplyIngress(ctx, o, 1, first); !errors.Is(err, ErrProcessorPoisoned) {
		t.Fatalf("poison reused: %v", err)
	}
	var count int
	if err = j.db.QueryRow(`SELECT count(*) FROM private_applied WHERE seq=?`, second).Scan(&count); err != nil || count != 0 {
		t.Fatalf("uncommitted event marked applied: %d %v", count, err)
	}
	restarted, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.ApplyIngress(ctx, o, 1, second); err != nil {
		t.Fatal(err)
	}
	cp, err := j.LoadCheckpoint(ctx, o, 1)
	if err != nil || cp.Revision != 2 {
		t.Fatalf("restart checkpoint: %v %#v", err, cp)
	}
}

func TestProcessorBindsCommandBeforeAtomicCheckpoint(t *testing.T) {
	ctx := context.Background()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	app := New(nil, orchestrator.Config{})
	p, err := NewProcessor(app, j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	// Controlled native setup stands in for the separately-owned lease/hello path.
	if err = app.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	if err = app.RegisterInstance("s", &state.InstanceRecord{InstanceID: "i", Online: true, WorkspaceRoot: "/tmp/workspace", WorkspaceKey: "/tmp/workspace", ObservedFocusedThreadID: "thread", Threads: map[string]*state.ThreadRecord{"thread": {ThreadID: "thread", CWD: "/tmp/workspace"}}}); err != nil {
		t.Fatal(err)
	}
	attach := privateAction(o, control.ActionAttachInstance)
	attach.InstanceID = "i"
	if _, err = app.Action(attach); err != nil {
		t.Fatal(err)
	}
	p.owners[o] = processorOwner{generation: 1}
	action := privateAction(o, control.ActionTextMessage)
	action.MessageID = "msg"
	action.Text = "hello"
	seq := admittedAction(t, j, o, "msg", "input", action)
	out, err := p.ApplyIngress(ctx, o, 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	var commandID string
	for _, e := range out {
		value, inputs, e2 := DecodeStoredEffect(e.Effect.Body)
		if e2 != nil {
			t.Fatal(e2)
		}

		if value.Command != nil && value.Command.Kind == agentproto.CommandPromptSend {
			commandID = value.Command.CommandID
			if inputs == nil || inputs.CommandID != commandID || len(inputs.Items) != 1 || len(inputs.Items[0].SourceMessageIDs) != 1 || inputs.Items[0].SourceMessageIDs[0] != "msg" {
				t.Fatalf("missing frozen command sources: %#v", inputs)
			}
			payload, ok := value.Payload.(eventcontract.AgentCommandPayload)
			if !ok || payload.Command.CommandID != commandID {
				t.Fatalf("canonical command mismatch: %#v", value.Payload)
			}
			if e.Effect.Lane != "background" {
				t.Fatal("native command not background")
			}
		}
	}
	if commandID == "" {
		t.Fatalf("command id missing: %#v", out)
	}
	saved, err := j.LoadCheckpoint(ctx, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	matched := false
	for _, hold := range saved.Core.Holds {
		if hold.Kind == "pending_remote" && hold.CommandID == commandID {
			matched = true
		}
	}
	if !matched {
		t.Fatalf("checkpoint lacks bound command: %#v", saved.Core.Holds)
	}
}

func TestProcessorCommitFailurePoisonsAndDoesNotPublish(t *testing.T) {
	ctx := context.Background()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	menu := privateAction(o, control.ActionShowCommandMenu)
	menu.Text = "/menu"
	first := admittedAction(t, j, o, "first", "control", menu)
	second := admittedAction(t, j, o, "second", "control", menu)
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyIngress(ctx, o, 1, first); err != nil {
		t.Fatal(err)
	}
	if _, err = j.db.Exec(`CREATE TRIGGER fail_effect BEFORE INSERT ON private_effect BEGIN SELECT RAISE(FAIL,'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyIngress(ctx, o, 1, second); !errors.Is(err, ErrProcessorPoisoned) {
		t.Fatalf("failed commit did not poison: %v", err)
	}
	var count int
	if err = j.db.QueryRow(`SELECT count(*) FROM private_applied WHERE seq=?`, second).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rolled back applied marker: %d %v", count, err)
	}
	cp, err := j.LoadCheckpoint(ctx, o, 1)
	if err != nil || cp.Revision != 1 {
		t.Fatalf("checkpoint advanced: %v %#v", err, cp)
	}
	if _, err = j.db.Exec(`DROP TRIGGER fail_effect`); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.ApplyIngress(ctx, o, 1, second); err != nil {
		t.Fatal(err)
	}
}

func TestProcessorLeavesHeldInputPendingWhileMenuCommits(t *testing.T) {
	ctx := context.Background()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	app := New(nil, orchestrator.Config{})
	if err = app.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	core, err := app.Service().ExportPrivateCheckpoint("s")
	if err != nil {
		t.Fatal(err)
	}
	core.Holds = []orchestrator.PrivateRecoveryHold{{Kind: "active_remote", InstanceID: "carrier", CommandID: "pending-command", Frozen: json.RawMessage(`{}`)}}
	if err = j.CommitCheckpoint(ctx, Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: core}, 0, nil); err != nil {
		t.Fatal(err)
	}
	text := privateAction(o, control.ActionTextMessage)
	text.MessageID = "text"
	text.Text = "hello"
	textSeq := admittedAction(t, j, o, "text", "input", text)
	menu := privateAction(o, control.ActionShowCommandMenu)
	menu.Text = "/menu"
	menu.LocalPageAction = true
	menu.MessageID = "card"
	menu.Inbound = &control.ActionInboundMeta{CardCallback: true}
	menuSeq := admittedAction(t, j, o, "menu", "control", menu)
	verbose := privateAction(o, control.ActionVerboseCommand)
	verbose.Text = "/verbose quiet"
	verboseSeq := admittedAction(t, j, o, "verbose", "control", verbose)
	access := privateAction(o, control.ActionAccessCommand)
	access.Text = "/access full"
	accessSeq := admittedAction(t, j, o, "access", "control", access)
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyIngress(ctx, o, 1, textSeq); !errors.Is(err, ErrRecoveryHeld) {
		t.Fatalf("unsafe input: %v", err)
	}
	effects, err := p.ApplyIngress(ctx, o, 1, menuSeq)
	if err != nil || len(effects) == 0 {
		t.Fatalf("menu blocked: %v %#v", err, effects)
	}
	page, e := DecodeEffectEvent(effects[0].Effect.Body)
	if e != nil || !page.InlineReplaceCurrentCard || page.PageView == nil {
		t.Fatalf("not a current-card menu: %v %#v", e, page)
	}
	if _, err = p.ApplyIngress(ctx, o, 1, verboseSeq); err != nil {
		t.Fatalf("safe setting blocked: %v", err)
	}
	checkpoint, err := j.LoadCheckpoint(ctx, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.Core.Verbosity != state.SurfaceVerbosityQuiet {
		t.Fatalf("setting not committed: %q", checkpoint.Core.Verbosity)
	}
	if _, err = p.ApplyIngress(ctx, o, 1, accessSeq); err != nil {
		t.Fatalf("safe access blocked: %v", err)
	}
	checkpoint, err = j.LoadCheckpoint(ctx, o, 1)
	if err != nil || checkpoint.Core.PromptOverride.AccessMode != agentproto.AccessModeFullAccess {
		t.Fatalf("access not durable: %v", err)
	}

	var n int
	if err = j.db.QueryRow(`SELECT count(*) FROM private_applied WHERE seq=?`, textSeq).Scan(&n); err != nil || n != 0 {
		t.Fatalf("held input consumed: %d %v", n, err)
	}
	if _, err = p.ApplyIngress(ctx, o, 1, textSeq); !errors.Is(err, ErrRecoveryHeld) {
		t.Fatalf("held input disappeared after menu: %v", err)
	}
}
