package workhost

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type settingsFunc func(context.Context, work.WorkScope, work.Role, work.WorkContext) (LaunchSettings, error)

func (f settingsFunc) ResolveLaunch(ctx context.Context, s work.WorkScope, r work.Role, c work.WorkContext) (LaunchSettings, error) {
	return f(ctx, s, r, c)
}

func TestHostPoolCountsUnobservedLaunchesAndPreservesManagementReserve(t *testing.T) {
	var calls atomic.Int32
	h, store := hostFixture(t, func(relayruntime.HeadlessLaunchOptions) (int, error) { return int(calls.Add(1)) + 100, nil })
	h.config.MaxLiveCarriers = 3
	h.config.ControlReservedCarriers = 1
	ctx := context.Background()
	seedPoolContexts(t, store, "one", "two", "business-overflow")
	for _, id := range []string{"one", "two"} {
		if err := h.Prepare(ctx, workengine.WorkerNeed{ScopeID: "scope", RoleID: "role", ContextID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.Prepare(ctx, workengine.WorkerNeed{ScopeID: "scope", RoleID: "role", ContextID: "business-overflow"}); !errors.Is(err, ErrNativePoolCapacity) {
		t.Fatalf("business stole reserved capacity: %v", err)
	}
	if err := store.Within(ctx, func(tx *workstore.Tx) error {
		return tx.PutContext(work.WorkContext{ID: "management", ScopeID: "scope", RoleID: "role", Workstream: "management", ControlTargetContextID: "one", Purpose: work.PurposeControl, Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.Prepare(ctx, workengine.WorkerNeed{ScopeID: "scope", RoleID: "role", ContextID: "management"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Within(ctx, func(tx *workstore.Tx) error {
		count, e := tx.CountNativePoolCarriers("local")
		if count != 3 {
			t.Fatalf("pool=%d", count)
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("launches=%d", calls.Load())
	}
}

func TestHostPreparationIndependentContextsBoundConcurrencyAndCancelWaiting(t *testing.T) {
	h, store := hostFixture(t, func(relayruntime.HeadlessLaunchOptions) (int, error) { return 100, nil })
	seedPoolContexts(t, store, "slow-one", "slow-two", "cancelled")
	h.launchSlots = make(chan struct{}, 2)
	original := h.config.Settings
	entered := make(chan string, 3)
	release := make(chan struct{})
	h.config.Settings = settingsFunc(func(ctx context.Context, s work.WorkScope, r work.Role, c work.WorkContext) (LaunchSettings, error) {
		entered <- c.ID
		select {
		case <-release:
		case <-ctx.Done():
			return LaunchSettings{}, ctx.Err()
		}
		return original.ResolveLaunch(ctx, s, r, c)
	})
	done := make(chan error, 2)
	for _, id := range []string{"slow-one", "slow-two"} {
		go func(id string) {
			done <- h.Prepare(context.Background(), workengine.WorkerNeed{ScopeID: "scope", RoleID: "role", ContextID: id})
		}(id)
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("different context blocked behind global preparation lock")
		}
	}
	waiting, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.Prepare(waiting, workengine.WorkerNeed{ScopeID: "scope", RoleID: "role", ContextID: "cancelled"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting preparation ignored cancellation: %v", err)
	}
	select {
	case id := <-entered:
		t.Fatalf("exceeded preparation concurrency: %s", id)
	default:
	}
	close(release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func seedPoolContexts(t *testing.T, store *workstore.Store, ids ...string) {
	t.Helper()
	if err := store.Within(context.Background(), func(tx *workstore.Tx) error {
		for _, id := range ids {
			if err := tx.PutContext(work.WorkContext{ID: id, ScopeID: "scope", RoleID: "role", Workstream: id, Revision: 1}, 0); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
