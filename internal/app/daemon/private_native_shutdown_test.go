package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestPrivateShutdownAbortsPreparedAndPreservesUncertainSending(t *testing.T) {
	root := t.TempDir()
	h := &privateNativeHost{hostID: "host", stateDir: root}
	var err error
	h.store, err = openPrivateNativeHostStore(filepath.Join(root, "host.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer h.store.Close()
	h.journal, err = privatechat.OpenJournal(filepath.Join(root, "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer h.journal.Close()
	h.registry, err = executionowner.Open(filepath.Join(root, "owners.db"), h)
	if err != nil {
		t.Fatal(err)
	}
	defer h.registry.Close()
	h.outbound, err = executionowner.NewOutbound(h.registry)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	rec := privateCarrierRecord{Snapshot: agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: t.TempDir(), JournalPath: filepath.Join(root, "runtime.db")}, Surface: privatechat.Owner{"gateway", "surface", "chat", "user", "tenant", "app"}, SurfaceGeneration: 1}
	if _, err = h.store.prepare(t.Context(), rec); err != nil {
		t.Fatal(err)
	}
	if err = h.registry.Register(t.Context(), privateRegistration(owner)); err != nil {
		t.Fatal(err)
	}
	if err = h.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	saved, err := h.store.lookup(t.Context(), "instance")
	if err != nil || saved.State != "retired" {
		t.Fatalf("prepared launch not retired: %+v %v", saved, err)
	}
	if _, err = h.registry.Resolve(t.Context(), privateRegistration(owner).Carrier, owner.Epoch); err == nil {
		t.Fatal("unspawned carrier still owns lease")
	}
	owner.InstanceID, owner.CarrierIncarnation = "interrupted-abort", "inc-abort"
	rec.Snapshot.Owner = owner
	if _, err = h.store.prepare(t.Context(), rec); err != nil {
		t.Fatal(err)
	}
	if err = h.registry.Register(t.Context(), privateRegistration(owner)); err != nil {
		t.Fatal(err)
	}
	if err = h.store.transition(t.Context(), owner.InstanceID, "prepared", "aborted", rec.Process); err != nil {
		t.Fatal(err)
	}
	if err = h.restoreAborted(t.Context()); err != nil {
		t.Fatal(err)
	}
	recovered, err := h.store.lookup(t.Context(), owner.InstanceID)
	if err != nil || recovered.State != "retired" {
		t.Fatalf("aborted launch did not recover: %+v %v", recovered, err)
	}
	owner.InstanceID, owner.CarrierIncarnation = "uncertain", "inc2"
	rec.Snapshot.Owner = owner
	if _, err = h.store.prepare(t.Context(), rec); err != nil {
		t.Fatal(err)
	}
	if err = h.store.transition(t.Context(), "uncertain", "prepared", "sending", rec.Process); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if err = h.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("uncertain launch was considered stopped: %v", err)
	}
	saved, err = h.store.lookup(t.Context(), "uncertain")
	if err != nil || saved.State != "sending" {
		t.Fatalf("uncertain evidence lost: %+v %v", saved, err)
	}
}
