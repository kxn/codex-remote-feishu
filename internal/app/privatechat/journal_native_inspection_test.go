package privatechat

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"path/filepath"
	"testing"
)

func TestCommittedNativeInspectionRequiresExactOwnerCommand(t *testing.T) {
	ctx := t.Context()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	o := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	s := Owner{"gateway", "surface", "chat", "user", "tenant", "app"}
	lease, err := j.OpenNativeLease(ctx, testNativeAuthority{}, o, s, 1)
	if err != nil {
		t.Fatal(err)
	}
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: o}
	command := agentproto.Command{CommandID: "inspect", Kind: agentproto.CommandRuntimeInspect, NativeBinding: &binding, RuntimeInspection: &agentproto.RuntimeInspectionRequest{Version: 1, ExpectedInstanceGeneration: 1, ExpectedProcessGeneration: 1}}
	d := Dispatch{ID: "inspect", Owner: s, Generation: 1, CommandID: "inspect", Lane: "inspect", Mode: "start", Session: NativeSession{Backend: "codex", Namespace: "history", CarrierID: "inc", CarrierGeneration: 1}}
	if err = lease.prepareNativeDispatch(ctx, d, command); err != nil {
		t.Fatal(err)
	}
	if err = lease.MarkSending(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	event := agentproto.Event{Kind: agentproto.EventRuntimeInspectionCompleted, CommandID: "inspect", NativeBinding: &binding, Seq: 1, RuntimeInspection: &agentproto.RuntimeInspectionResult{RequestCommandID: "inspect", CarrierIncarnation: "inc", InstanceGeneration: 1, ProcessGeneration: 1}}
	hash, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil {
		t.Fatal(err)
	}
	event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: "inc", Sequence: 1, PayloadHash: hash}
	if _, err = lease.ReceiveNativeEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	ref := NativeEventRef{Incarnation: "inc", Sequence: 1, PayloadHash: hash}
	if _, err = j.ReadCommittedNativeInspection(ctx, o, s, 1, ref); err != nil {
		t.Fatal(err)
	}
	ref.PayloadHash = "forged"
	if _, err = j.ReadCommittedNativeInspection(ctx, o, s, 1, ref); err == nil {
		t.Fatal("forged event accepted")
	}
	ref.PayloadHash = hash
	o.Epoch++
	if _, err = j.ReadCommittedNativeInspection(ctx, o, s, 1, ref); err == nil {
		t.Fatal("foreign epoch accepted")
	}
}
