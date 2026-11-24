package privatechat

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func TestMarkNativeStoppingUsesProcessorBeforeRegistry(t *testing.T) {
	ctx := context.Background()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	n := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	var registry sync.Mutex
	l, err := j.OpenNativeLease(ctx, lockingNativeAuthority{&registry}, n, o, 1)
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
	p.mu.Lock()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() { close(started); done <- p.MarkNativeStopping(ctx, l) }()
	<-started
	registryAvailable := make(chan struct{})
	go func() { registry.Lock(); registry.Unlock(); close(registryAvailable) }()
	select {
	case <-registryAvailable:
	case <-time.After(time.Second):
		p.mu.Unlock()
		t.Fatal("stop held registry while waiting for processor")
	}
	p.mu.Unlock()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("stop did not finish")
	}
}
