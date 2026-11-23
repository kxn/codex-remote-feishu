package daemon

import (
	"context"
	"sync"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
)

// beginPersonalSourceActivity and beginPersonalSourceFence share the same lock:
// no new handler can pass its durable fence check after fence commit.
func (r *teamRuntime) beginPersonalSourceActivity(ctx context.Context, scope string) (func(), error) {
	r.personalMigrationMu.Lock()
	defer r.personalMigrationMu.Unlock()
	if err := r.store.Within(ctx, func(tx *workstore.Tx) error { return tx.AssertPersonalMigrationOpen(scope) }); err != nil {
		return nil, err
	}
	if r.personalMigrationActive == nil {
		r.personalMigrationActive = map[string]int{}
	}
	r.personalMigrationActive[scope]++
	var once sync.Once
	return func() {
		once.Do(func() {
			r.personalMigrationMu.Lock()
			defer r.personalMigrationMu.Unlock()
			r.personalMigrationActive[scope]--
			if r.personalMigrationActive[scope] == 0 {
				delete(r.personalMigrationActive, scope)
			}
			if r.personalMigrationChanged != nil {
				close(r.personalMigrationChanged)
				r.personalMigrationChanged = nil
			}
		})
	}, nil
}

func (r *teamRuntime) beginPersonalSourceFence(ctx context.Context, scope, id string, now time.Time) (workstore.PersonalMigrationFence, int, error) {
	r.personalMigrationMu.Lock()
	defer r.personalMigrationMu.Unlock()
	var fence workstore.PersonalMigrationFence
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		fence, e = tx.BeginPersonalMigrationFence(scope, id, now)
		return e
	})
	return fence, r.personalMigrationActive[scope], err
}

// This is process-local activity only. Zero after restart does not prove any
// interrupted external write settled; durable operations still need review.
func (r *teamRuntime) personalSourceActive(scope string) int {
	r.personalMigrationMu.Lock()
	defer r.personalMigrationMu.Unlock()
	return r.personalMigrationActive[scope]
}

// waitPersonalSourceActivities waits only for the callbacks tracked by this
// process. A restart still requires durable operation and host reconciliation.
func (r *teamRuntime) waitPersonalSourceActivities(ctx context.Context, scope string) error {
	for {
		r.personalMigrationMu.Lock()
		if r.personalMigrationActive[scope] == 0 {
			r.personalMigrationMu.Unlock()
			return nil
		}
		if r.personalMigrationChanged == nil {
			r.personalMigrationChanged = make(chan struct{})
		}
		changed := r.personalMigrationChanged
		r.personalMigrationMu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}
