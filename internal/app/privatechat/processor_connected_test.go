package privatechat

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestNativeConnectedReplayAndReconnectDoNotRedispatch(t *testing.T) {
	ctx := context.Background()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	n := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	j, err := OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	lease, err := j.OpenNativeLease(ctx, testNativeAuthority{}, n, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.RegisterNativeHello(ctx, lease, nativeHello(n)); err != nil {
		t.Fatal(err)
	}
	inst := p.app.Service().Instance(n.InstanceID)
	inst.ObservedFocusedThreadID = "thread"
	inst.Threads["thread"] = &state.ThreadRecord{ThreadID: "thread", CWD: "/tmp/workspace"}
	attach := privateAction(o, control.ActionAttachInstance)
	attach.InstanceID = n.InstanceID
	if _, err = p.app.Action(attach); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyAuthenticatedNativeHello(ctx, lease, nativeHello(n), "connection-1"); err != nil {
		t.Fatal(err)
	}
	revision := p.owners[o].revision
	if _, err = p.ApplyAuthenticatedNativeHello(ctx, lease, nativeHello(n), "connection-1"); err != nil {
		t.Fatal(err)
	}
	if p.owners[o].revision != revision {
		t.Fatal("duplicate hello committed another revision")
	}
	message := privateAction(o, control.ActionTextMessage)
	message.MessageID = "message"
	message.Text = "hello"
	seq := admittedAction(t, j, o, "message", "input", message)
	first, err := p.ApplyIngress(ctx, o, 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	commands := 0
	for _, effect := range first {
		event, _, e := DecodeStoredEffect(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			commands++
		}
	}
	if commands != 1 {
		t.Fatalf("expected one initial prompt, got %d", commands)
	}
	if _, err = p.ApplyNativeDisconnect(ctx, lease, "disconnect-1"); err != nil {
		t.Fatal(err)
	}
	reconnected, err := p.ApplyAuthenticatedNativeHello(ctx, lease, nativeHello(n), "connection-2")
	if err != nil {
		t.Fatal(err)
	}
	for _, effect := range reconnected {
		event, _, e := DecodeStoredEffect(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			t.Fatal("reconnect dispatched the already prepared prompt again")
		}
	}
	changed := nativeHello(n)
	changed.Instance.DisplayName = "different identity"
	if _, err = p.ApplyAuthenticatedNativeHello(ctx, lease, changed, "connection-2"); err == nil {
		t.Fatal("same socket accepted a different hello")
	}
	if !p.poisoned {
		t.Fatal("changed hello left uncommitted in-memory state usable")
	}
}
