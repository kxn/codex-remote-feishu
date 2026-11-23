package privatechat

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func TestReferenceGatePreventsConcurrentPrivateCommit(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	var gate sync.RWMutex
	j.SetReferenceGate(&gate)
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	a := privateAction(o, control.ActionShowCommandMenu)
	seq := admittedAction(t, j, o, "menu", "control", a)
	gate.Lock()
	done := make(chan error, 1)
	go func() { _, e := p.ApplyIngress(context.Background(), o, 1, seq); done <- e }()
	select {
	case err := <-done:
		gate.Unlock()
		t.Fatalf("commit crossed write gate: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	gate.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("commit did not resume")
	}
}

func TestCodexContractLeaseIsInsideProcessorAndOfflineDoesNotNeedLease(t *testing.T) {
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
	// A restored/offline owner can reconcile local configuration without a carrier.
	if _, err = p.ApplyCodexContracts(ctx, o, 1, "offline", nil, func() bool { return true }, nil); err != nil {
		t.Fatal(err)
	}
	native := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	lease, err := j.OpenNativeLease(ctx, testNativeAuthority{}, native, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.RegisterNativeHello(ctx, lease, nativeHello(native)); err != nil {
		t.Fatal(err)
	}
	attach := privateAction(o, control.ActionAttachInstance)
	attach.InstanceID = native.InstanceID
	if _, err = p.app.Action(attach); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyCodexContracts(ctx, o, 1, "live", nil, func() bool { return true }, nil); !errors.Is(err, ErrDeferred) {
		t.Fatalf("live effect had no lease: %v", err)
	}
	calls := 0
	guard := func(commit func() error) error {
		calls++
		if p.mu.TryLock() {
			p.mu.Unlock()
			t.Fatal("lease acquired before processor")
		}
		return commit()
	}
	if _, err = p.ApplyCodexContracts(ctx, o, 1, "live", nil, func() bool { return true }, guard); err != nil || calls != 1 {
		t.Fatalf("guarded reconcile: %d %v", calls, err)
	}
	if _, err = p.ApplyCodexContracts(ctx, o, 1, "live", nil, func() bool { return true }, nil); err != nil {
		t.Fatalf("current projection required unnecessary lease: %v", err)
	}
}

type lockingNativeAuthority struct{ mu *sync.Mutex }

func (a lockingNativeAuthority) WithNativeLease(_ context.Context, _ agentproto.NativeOwner, _ Owner, _ int64, fn func() error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return fn()
}
func TestSettingsAndNativeShareProcessorThenRegistryOrder(t *testing.T) {
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
	native := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	var registry sync.Mutex
	lease, err := j.OpenNativeLease(ctx, lockingNativeAuthority{&registry}, native, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.RegisterNativeHello(ctx, lease, nativeHello(native)); err != nil {
		t.Fatal(err)
	}
	attach := privateAction(o, control.ActionAttachInstance)
	attach.InstanceID = native.InstanceID
	if _, err = p.app.Action(attach); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	settingsDone := make(chan error, 1)
	nativeDone := make(chan error, 1)
	guard := func(commit func() error) error {
		registry.Lock()
		defer registry.Unlock()
		close(entered)
		<-release
		return commit()
	}
	go func() {
		_, e := p.ApplyCodexContracts(ctx, o, 1, "version", nil, func() bool { return true }, guard)
		settingsDone <- e
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("settings did not acquire registry")
	}
	go func() { _, e := p.ApplyNativeDisconnect(ctx, lease, "connection"); nativeDone <- e }()
	close(release)
	for _, done := range []chan error{settingsDone, nativeDone} {
		select {
		case e := <-done:
			if e != nil {
				t.Fatal(e)
			}
		case <-time.After(time.Second):
			t.Fatal("processor/registry lock inversion")
		}
	}
}
