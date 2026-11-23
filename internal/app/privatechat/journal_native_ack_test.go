package privatechat

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type failAfterNativeCommit struct{ fail bool }

func (a *failAfterNativeCommit) WithNativeLease(_ context.Context, _ agentproto.NativeOwner, _ Owner, _ int64, commit func() error) error {
	if err := commit(); err != nil {
		return err
	}
	if a.fail {
		return errors.New("outer ownership transaction failed")
	}
	return nil
}

func TestNativeAcknowledgementRequiresOuterLeaseCommit(t *testing.T) {
	ctx := t.Context()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	authority := &failAfterNativeCommit{}
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "carrier", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "namespace"}
	lease, err := j.OpenNativeLease(ctx, authority, owner, Owner{"gateway", "surface", "chat", "user", "tenant", "app"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	event := agentproto.Event{Kind: agentproto.EventThreadDiscovered, Seq: 1, ThreadID: "thread", NativeBinding: &agentproto.NativeTurnBinding{Version: 1, Owner: owner}}
	hash, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil {
		t.Fatal(err)
	}
	event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: owner.CarrierIncarnation, Sequence: 1, PayloadHash: hash}
	authority.fail = true
	receipt, err := lease.ReceiveNativeEvent(ctx, event)
	if err == nil || receipt != nil {
		t.Fatalf("failed outer fence returned an ACK: %#v %v", receipt, err)
	}
	authority.fail = false
	receipt, err = lease.ReceiveNativeEvent(ctx, event)
	if err != nil || receipt == nil || receipt.Sequence != 1 {
		t.Fatalf("inner durable commit could not replay: %#v %v", receipt, err)
	}
}
