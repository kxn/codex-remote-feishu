package daemon

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestPrivatePhysicalExitedCarrierRequiresRealHostAndJournalProof(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("process group proof fixture uses Linux")
	}
	ctx := context.Background()
	root, cwd := t.TempDir(), t.TempDir()
	h := &privateNativeHost{hostID: "host", stateDir: root}
	var err error
	h.store, err = openPrivateNativeHostStore(filepath.Join(root, "host.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer h.store.Close()
	h.journal, err = privatechat.OpenJournal(filepath.Join(root, "chat.db"))
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
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "o", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	surface := privatechat.Owner{"gateway", "surface", "chat", "user", "tenant", "app"}
	rec := privateCarrierRecord{Snapshot: agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: filepath.Join(root, "runtime.db")}, Surface: surface, SurfaceGeneration: 1}
	if _, err = h.store.prepare(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if err = h.registry.Register(ctx, privateRegistration(owner)); err != nil {
		t.Fatal(err)
	}
	child := exec.Command("sleep", "30")
	hostinventory.Prepare(child)
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	scope, err := hostinventory.Bind(child)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	process, err := hostinventory.CaptureProcess(child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.store.transition(ctx, "instance", "prepared", "sending", hostinventory.ProcessIdentity{}); err != nil {
		t.Fatal(err)
	}
	if err = h.store.transition(ctx, "instance", "sending", "running", process); err != nil {
		t.Fatal(err)
	}
	if err = h.beginDrain(ctx, "instance"); err != nil {
		t.Fatal(err)
	}
	if _, err = h.store.stopIntent(ctx, "instance", "shutdown"); err != nil {
		t.Fatal(err)
	}
	w, err := runtimejournal.Open(ctx, rec.Snapshot.JournalPath, root, cwd, "inc")
	if err != nil {
		t.Fatal(err)
	}
	if err = w.BindPrivateLaunchSnapshot(rec.Snapshot); err != nil {
		t.Fatal(err)
	}
	// A physical stop proof does not require an empty historical journal and
	// does not make any claim about this command's business outcome.
	if _, _, err = w.PreparePrivateCommand(agentproto.Command{CommandID: "history", Kind: agentproto.CommandPromptSend, NativeBinding: &agentproto.NativeTurnBinding{Version: 1, Owner: owner}}); err != nil {
		t.Fatal(err)
	}
	if err = h.recoverPhysicalExitedCarrier(ctx, rec); err == nil {
		t.Fatal("live process incorrectly retired")
	}
	savedWhileRunning := scope.Inspect()
	if !savedWhileRunning.Complete || !savedWhileRunning.RootAlive {
		t.Fatal("running scope snapshot unavailable")
	}
	scope.MarkControlledStop()
	_ = child.Process.Kill()
	_ = child.Wait()
	scope.MarkWaitObserved()
	saved := scope.Inspect()
	if !saved.Complete || saved.RootAlive {
		t.Fatalf("invalid scope fixture: %+v", saved)
	}
	inventory, _ := json.Marshal(map[string]any{"version": 1, "native_owner": owner, "state": map[string]any{"scopes": []hostinventory.Snapshot{savedWhileRunning}, "active_native_commands": map[string]bool{}, "pending_inputs": map[string]string{}, "active_tools": map[string]bool{}, "goal_active": map[string]bool{}, "tasks": map[string]bool{}, "uncertainties": map[string]bool{}, "turns": map[string]any{}, "task_inventory_known": true}})
	if err = w.SaveInventory(inventory); err != nil {
		t.Fatal(err)
	}
	if err = h.recoverPhysicalExitedCarrier(ctx, rec); err == nil {
		t.Fatal("live journal writer incorrectly retired")
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if err = h.recoverPhysicalExitedCarrier(ctx, rec); err != nil {
		t.Fatal(err)
	}
	savedRec, err := h.store.lookup(ctx, "instance")
	if err != nil || savedRec.State != "retired" || !savedRec.PhysicalExitProof {
		t.Fatalf("retirement=%+v err=%v", savedRec, err)
	}
	if err = h.WithRetiredRuntime(ctx, surface, 1, owner, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err = h.advanceCarrierStop(ctx, savedRec, "later-user-detach"); err != nil {
		t.Fatalf("later detach could not reuse proven shutdown retirement: %v", err)
	}
	// Even a later prepared frame invalidates the frozen write watermark.
	w, err = runtimejournal.Open(ctx, rec.Snapshot.JournalPath, root, cwd, "inc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.PrepareFrame("history", []byte(`{"id":"late","method":"turn/start"}`)); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if err = h.WithRetiredRuntime(ctx, surface, 1, owner, func() error { return nil }); err == nil {
		t.Fatal("later frame did not invalidate physical proof")
	}
}
