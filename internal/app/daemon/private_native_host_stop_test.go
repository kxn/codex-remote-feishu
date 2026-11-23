package daemon

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestPrivateHostRetirementNeedsInspectionExitAndFence(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	cwd := t.TempDir()
	host := &privateNativeHost{hostID: "host", stateDir: root}
	var err error
	host.store, err = openPrivateNativeHostStore(filepath.Join(root, "host.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer host.store.Close()
	host.journal, err = privatechat.OpenJournal(filepath.Join(root, "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer host.journal.Close()
	host.registry, err = executionowner.Open(filepath.Join(root, "owners.db"), host)
	if err != nil {
		t.Fatal(err)
	}
	defer host.registry.Close()
	host.outbound, err = executionowner.NewOutbound(host.registry)
	if err != nil {
		t.Fatal(err)
	}
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "o", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	surface := privatechat.Owner{"gateway", "surface", "chat", "user", "tenant", "app"}
	rec := privateCarrierRecord{Snapshot: agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: filepath.Join(root, "runtime.db")}, Surface: surface, SurfaceGeneration: 1}
	if _, err = host.store.prepare(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if err = host.registry.Register(ctx, privateRegistration(owner)); err != nil {
		t.Fatal(err)
	}
	child := exec.Command("sleep", "30")
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	process, err := hostinventory.CaptureProcess(child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if err = host.store.transition(ctx, "instance", "prepared", "sending", hostinventory.ProcessIdentity{}); err != nil {
		t.Fatal(err)
	}
	if err = host.store.transition(ctx, "instance", "sending", "running", process); err != nil {
		t.Fatal(err)
	}
	wrapper, err := runtimejournal.Open(ctx, rec.Snapshot.JournalPath, root, cwd, "inc")
	if err != nil {
		t.Fatal(err)
	}
	if err = wrapper.BindPrivateLaunchSnapshot(rec.Snapshot); err != nil {
		t.Fatal(err)
	}
	if err = wrapper.Close(); err != nil {
		t.Fatal(err)
	}
	lease, err := host.lease(ctx, rec, nil)
	if err != nil {
		t.Fatal(err)
	}
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner}
	command := agentproto.Command{CommandID: "inspection", Kind: agentproto.CommandRuntimeInspect, NativeBinding: &binding, RuntimeInspection: &agentproto.RuntimeInspectionRequest{Version: 1, ExpectedInstanceGeneration: 1, ExpectedProcessGeneration: 1, StopBackground: true}}
	d := privatechat.Dispatch{ID: "inspection", Owner: surface, Generation: 1, CommandID: "inspection", Lane: "inspect", Mode: "start", Session: privatechat.NativeSession{Backend: "codex", Namespace: "history", CarrierID: "inc", CarrierGeneration: 1}}
	if err = lease.PrepareHostInspection(ctx, d, command); err != nil {
		t.Fatal(err)
	}
	if err = lease.MarkSending(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	bootstrap := agentproto.RPCProbe{Request: json.RawMessage(`{"id":"init","method":"initialize"}`), Response: json.RawMessage(`{"id":"init","result":{}}`)}
	inspection := agentproto.RuntimeInspectionResult{Version: 1, RequestCommandID: "inspection", CarrierIncarnation: "inc", InstanceGeneration: 1, ProcessGeneration: 1, ObservedAt: time.Now(), OwnerScope: &agentproto.RuntimeOwnerInspection{Bootstrap: bootstrap, KnownSessionsComplete: true}, Background: agentproto.BackgroundInspection{CarrierIncarnation: "inc", ProcessGeneration: 1, Complete: true, NativeTaskSetComplete: true, NativeInputsComplete: true, ContinuationInventoryKnown: true, Evidence: json.RawMessage(`{"source":"inventory"}`)}}
	event := agentproto.Event{Seq: 1, Kind: agentproto.EventRuntimeInspectionCompleted, CommandID: "inspection", NativeBinding: &binding, RuntimeInspection: &inspection}
	hash, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil {
		t.Fatal(err)
	}
	event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: "inc", Sequence: 1, PayloadHash: hash}
	if _, err = lease.ReceiveNativeEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	ref := privatechat.NativeEventRef{Incarnation: "inc", Sequence: 1, PayloadHash: hash}
	if err = host.beginDrain(ctx, "instance"); err != nil {
		t.Fatal(err)
	}
	if _, err = host.store.stopIntent(ctx, "instance", "stop-effect"); err != nil {
		t.Fatal(err)
	}
	first, err := host.store.inspectionIntent(ctx, "instance", "stop-effect", false)
	if err != nil {
		t.Fatal(err)
	}
	again, err := host.store.inspectionIntent(ctx, "instance", "stop-effect", false)
	if err != nil || first.StopInspectionID == "" || again.StopInspectionID != first.StopInspectionID {
		t.Fatalf("inspection retry did not reuse durable identity: %+v %v", again, err)
	}
	if _, err = host.store.stopIntent(ctx, "instance", "different-effect"); err == nil {
		t.Fatal("different stop intent replaced existing intent")
	}
	if err = host.sealDrain(ctx, "instance", ref); err != nil {
		t.Fatal(err)
	}
	timeout, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if err = host.retire(timeout, "instance"); err == nil {
		t.Fatal("live process retired")
	}
	if err = child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err = child.Wait(); err != nil { /* killed process is expected */
	}
	if err = host.retire(ctx, "instance"); err != nil {
		t.Fatal(err)
	}
	if _, err = host.registry.Resolve(ctx, privateRegistration(owner).Carrier, 1); err == nil {
		t.Fatal("retired carrier still active")
	}
}
