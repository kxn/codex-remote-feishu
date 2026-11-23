package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worktransport"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type activityTestTransport struct{ entered, release chan struct{} }

func (v activityTestTransport) SendCommand(string, agentproto.Command) error {
	close(v.entered)
	<-v.release
	return nil
}

type activityTestRegistrar struct{}

func (activityTestRegistrar) RegisterCommand(context.Context, string, agentproto.Command) error {
	return nil
}

func TestPersonalMigrationWaitsForInFlightNativeSocketWrite(t *testing.T) {
	ctx := t.Context()
	store, err := workstore.Open(ctx, filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	err = store.Within(ctx, func(tx *workstore.Tx) error {
		if e := tx.PutRole(work.Role{ID: "r", Name: "R", Description: "work", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutPrincipal(work.Principal{ID: "p", DisplayName: "P", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutScope(work.WorkScope{ID: "s", Kind: work.ScopePersonal, PrincipalID: "p", RoleID: "r", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutContext(work.WorkContext{ID: "c", ScopeID: "s", RoleID: "r", Workstream: "main", Revision: 1}, 0); e != nil {
			return e
		}
		return tx.PutWorker(work.WorkerSession{ID: "w", ScopeID: "s", ContextID: "c", RoleID: "r", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "instance", CarrierIncarnation: "inc", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &teamRuntime{store: store}
	transport := activityTestTransport{make(chan struct{}), make(chan struct{})}
	driver, err := worktransport.New(store, transport, runtime.beginPersonalSourceActivity)
	if err != nil {
		t.Fatal(err)
	}
	driver.BindRegistrar(activityTestRegistrar{})
	cmd := agentproto.Command{Kind: agentproto.CommandThreadGoalClear, Execution: &agentproto.ExecutionBinding{ScopeID: "s", WorkerSessionID: "w"}}
	done := make(chan error, 1)
	go func() { done <- driver.SendCommand(context.Background(), "instance", cmd) }()
	<-transport.entered
	_, active, err := runtime.beginPersonalSourceFence(ctx, "s", "migration", time.Now())
	if err != nil || active != 1 {
		close(transport.release)
		t.Fatalf("active=%d err=%v", active, err)
	}
	if err = driver.SendCommand(ctx, "instance", cmd); !errors.Is(err, workstore.ErrPersonalMigrationFenced) {
		close(transport.release)
		t.Fatalf("fenced send: %v", err)
	}
	wait, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if !errors.Is(runtime.waitPersonalSourceActivities(wait, "s"), context.DeadlineExceeded) {
		close(transport.release)
		t.Fatal("migration did not wait for socket")
	}
	close(transport.release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if err = runtime.waitPersonalSourceActivities(ctx, "s"); err != nil {
		t.Fatal(err)
	}
}

func TestPersonalMigrationBlocksLateViewButAllowsExistingReplay(t *testing.T) {
	ctx := t.Context()
	store, err := workstore.Open(ctx, filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	err = store.Within(ctx, func(tx *workstore.Tx) error {
		if e := tx.PutRole(work.Role{ID: "r", Name: "R", Description: "work", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutPrincipal(work.Principal{ID: "p", DisplayName: "P", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutScope(work.WorkScope{ID: "s", Kind: work.ScopePersonal, PrincipalID: "p", RoleID: "r", Revision: 1}, 0); e != nil {
			return e
		}
		return tx.PutContext(work.WorkContext{ID: "c", ScopeID: "s", RoleID: "r", Workstream: "main", Revision: 1}, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
	view := workstore.UIView{ID: "existing", ScopeID: "s", ContextID: "c", SurfaceKey: "surface", Kind: "test", TargetID: "target", TargetRevision: 1, Binding: work.RoleBotBinding{RoleID: "r", GatewayRef: "g", AppID: "app", IdentityGeneration: 1}, Tenant: "tenant", ChatID: "chat", PrincipalID: "p"}
	card, err := workui.Card(view.ID, workui.Panel{Title: "existing"})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Within(ctx, func(tx *workstore.Tx) error { _, e := workui.Queue(tx, view, card); return e }); err != nil {
		t.Fatal(err)
	}
	r := &teamRuntime{store: store}
	if _, _, err = r.beginPersonalSourceFence(ctx, "s", "migration", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = store.Within(ctx, func(tx *workstore.Tx) error { _, e := workui.Queue(tx, view, card); return e }); err != nil {
		t.Fatalf("exact existing view replay: %v", err)
	}
	view.ID = "late"
	card, err = workui.Card(view.ID, workui.Panel{Title: "late"})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Within(ctx, func(tx *workstore.Tx) error { _, e := workui.Queue(tx, view, card); return e }); !errors.Is(err, workstore.ErrPersonalMigrationFenced) {
		t.Fatalf("late view: %v", err)
	}
}
