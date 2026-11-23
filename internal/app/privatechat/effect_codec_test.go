package privatechat

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestCommittedEffectsDecodeToProjectorAndNativeAfterRestart(t *testing.T) {
	ctx := context.Background()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	app := New(nil, orchestrator.Config{})
	if err := app.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	if err := app.RegisterInstance("s", &state.InstanceRecord{InstanceID: "i", Backend: agentproto.BackendCodex, Online: true, WorkspaceRoot: "/tmp/workspace", WorkspaceKey: "/tmp/workspace", ObservedFocusedThreadID: "thread", Threads: map[string]*state.ThreadRecord{"thread": {ThreadID: "thread", CWD: "/tmp/workspace"}}}); err != nil {
		t.Fatal(err)
	}
	a := privateAction(o, control.ActionAttachInstance)
	a.InstanceID = "i"
	if _, err := app.Action(a); err != nil {
		t.Fatal(err)
	}
	menu := privateAction(o, control.ActionShowCommandMenu)
	menu.Text = "/menu"
	pages, err := app.Action(menu)
	if err != nil || len(pages) == 0 {
		t.Fatalf("menu: %v", err)
	}
	text := privateAction(o, control.ActionTextMessage)
	text.MessageID = "msg"
	text.Text = "hello"
	outputs, err := app.Action(text)
	if err != nil {
		t.Fatal(err)
	}
	var command *agentproto.Command
	for _, v := range outputs {
		if v.Command != nil && v.Command.Kind == agentproto.CommandPromptSend {
			command = v.Command
		}
	}
	if command == nil {
		t.Fatalf("no native command: %#v", outputs)
	}
	command.CommandID = "command"
	app.Service().BindPendingRemoteCommand("s", "command")
	var native eventcontract.Event
	for _, v := range outputs {
		if v.Command == command {
			native = v
		}
	}
	menuBytes, err := EncodeEffectEvent(pages[0])
	if err != nil {
		t.Fatal(err)
	}
	nativeBytes, err := EncodeEffectEvent(native)
	if err != nil {
		t.Fatal(err)
	}
	core, err := app.Service().ExportPrivateCheckpoint("s")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "db")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	seq, _, err := j.Admit(ctx, Ingress{Owner: o, Generation: 1, EventID: "event", Lane: "input", Payload: []byte(`{}`), Sources: []string{"msg"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = j.CommitCheckpoint(ctx, Checkpoint{Owner: o, Generation: 1, Revision: 1, Cursor: seq, Core: core}, 0, []Effect{{ID: "menu", Lane: "interactive", Body: menuBytes}, {ID: "native", Lane: "background", Body: nativeBytes}}); err != nil {
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
	effects, err := j.PreparedEffects(ctx, o, 1, 0, -1, 10)
	if err != nil || len(effects) != 2 {
		t.Fatalf("prepared: %v %#v", err, effects)
	}
	page, err := DecodeEffectEvent(effects[0].Effect.Body)
	if err != nil {
		t.Fatal(err)
	}
	if operations := feishu.NewProjector().ProjectEvent("c", page); len(operations) == 0 {
		t.Fatal("restored menu did not project")
	}
	restored, err := DecodeEffectEvent(effects[1].Effect.Body)
	if err != nil || restored.Command == nil || restored.Command.CommandID != "command" {
		t.Fatalf("restored command: %v %#v", err, restored.Command)
	}
	frames, err := codex.NewTranslator("i").TranslateCommand(*restored.Command)
	if err != nil || len(frames) == 0 {
		t.Fatalf("native translator: %v", err)
	}
}
