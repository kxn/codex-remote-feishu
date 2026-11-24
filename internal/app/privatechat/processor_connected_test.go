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

func TestAuthenticatedHelloPersistsDeliveryCapabilityPerConnection(t *testing.T) {
	ctx := context.Background()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	n := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	j, err := OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	l, err := j.OpenNativeLease(ctx, testNativeAuthority{}, n, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	old := nativeHello(n)
	if _, err = p.ApplyAuthenticatedNativeHello(ctx, l, old, "old-connection"); err != nil {
		t.Fatal(err)
	}
	newer := old
	newer.PrivateDeliveryVersion = agentproto.PrivateDeliveryVersion
	if _, err = p.ApplyAuthenticatedNativeHello(ctx, l, newer, "new-connection"); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		id   string
		want int
	}{{"old-connection", 0}, {"new-connection", agentproto.PrivateDeliveryVersion}} {
		var got int
		if err = j.db.QueryRow(`SELECT delivery_version FROM private_native_connection_delivery WHERE incarnation=? AND connection_id=?`, n.CarrierIncarnation, test.id).Scan(&got); err != nil || got != test.want {
			t.Fatalf("%s capability=%d err=%v", test.id, got, err)
		}
	}
}

func TestDisconnectedUnattachedCarrierDoesNotChangeCurrentSurface(t *testing.T) {
	ctx := context.Background()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	n := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	j, err := OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	l, err := j.OpenNativeLease(ctx, testNativeAuthority{}, n, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	h := nativeHello(n)
	h.Instance.CodexProfileID = "cp_oauth"
	if _, err = p.ApplyAuthenticatedNativeHello(ctx, l, h, "connection"); err != nil {
		t.Fatal(err)
	}
	if got := p.app.Service().Instance(n.InstanceID); got == nil || !got.Online || got.CodexProfileID != "cp_oauth" {
		t.Fatalf("hello projection: %+v", got)
	}
	if err = p.app.RegisterInstance(o.SurfaceID, &state.InstanceRecord{InstanceID: "replacement", Backend: agentproto.BackendCodex, Online: true, WorkspaceRoot: "/tmp/other", WorkspaceKey: "/tmp/other", Threads: map[string]*state.ThreadRecord{}}); err != nil {
		t.Fatal(err)
	}
	attach := privateAction(o, control.ActionAttachInstance)
	attach.InstanceID = "replacement"
	if _, err = p.app.Action(attach); err != nil {
		t.Fatal(err)
	}
	effects, err := p.ApplyNativeDisconnect(ctx, l, "connection")
	if err != nil {
		t.Fatal(err)
	}
	if p.app.Service().Instance(n.InstanceID).Online {
		t.Fatal("carrier remained online")
	}
	if len(effects) != 0 {
		t.Fatalf("unattached carrier produced surface effects: %+v", effects)
	}
	if got := p.app.Service().Surface(o.SurfaceID); got.AttachedInstanceID != "replacement" {
		t.Fatalf("replacement navigation changed: %+v", got)
	}
}
