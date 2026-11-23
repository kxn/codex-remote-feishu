package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPersonalSourceFenceWaitsForInFlightCallbacks(t *testing.T) {
	ctx := t.Context()
	store, err := workstore.Open(ctx, filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Within(ctx, func(tx *workstore.Tx) error {
		if e := tx.PutRole(work.Role{ID: "r", Name: "R", Description: "work", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutPrincipal(work.Principal{ID: "p", DisplayName: "P", Revision: 1}, 0); e != nil {
			return e
		}
		return tx.PutScope(work.WorkScope{ID: "s", Kind: work.ScopePersonal, PrincipalID: "p", RoleID: "r", Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	r := &teamRuntime{store: store}
	end, err := r.beginPersonalSourceActivity(ctx, "s")
	if err != nil {
		t.Fatal(err)
	}
	_, active, err := r.beginPersonalSourceFence(ctx, "s", "migration", time.Now())
	if err != nil || active != 1 {
		t.Fatalf("fence: active=%d err=%v", active, err)
	}
	if _, err = r.beginPersonalSourceActivity(ctx, "s"); !errors.Is(err, workstore.ErrPersonalMigrationFenced) {
		t.Fatalf("new callback admitted: %v", err)
	}
	short, cancel := context.WithTimeout(ctx, time.Millisecond)
	defer cancel()
	if !errors.Is(r.waitPersonalSourceActivities(short, "s"), context.DeadlineExceeded) {
		t.Fatal("wait returned before callback ended")
	}
	end()
	if err = r.waitPersonalSourceActivities(ctx, "s"); err != nil {
		t.Fatal(err)
	}
}
