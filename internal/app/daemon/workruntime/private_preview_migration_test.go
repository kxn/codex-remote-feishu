package workruntime

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	preview "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/preview"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestMigrationPreservedPreviewDoesNotBlockReconciliation(t *testing.T) {
	f := newFixture(t, work.BackendCodex, false, true)
	seedPrivatePreviewLane(t, f, t.TempDir())
	var calls atomic.Int32
	f.coordinator.options.PreviewPrivateFinal = func(context.Context, work.RoleBotBinding, preview.FinalBlockPreviewRequest) (preview.FinalBlockPreviewResult, error) {
		calls.Add(1)
		return preview.FinalBlockPreviewResult{}, nil
	}
	f.coordinator.options.HostID = "host"
	f.start()
	privateFinalEvent(f, "Open [file](./report.md) for the original answer.")
	if err := completePrivateTurn(f); err != nil {
		t.Fatal(err)
	}
	var id string
	f.tx(func(tx *workstore.Tx) error {
		ids, e := tx.ListPendingPrivatePreviews(8)
		if e != nil {
			return e
		}
		if len(ids) != 1 {
			t.Fatal("expected queued preview")
		}
		id = ids[0]
		v, e := tx.GetPrivateExecutionDisplay(id)
		if e != nil {
			return e
		}
		fence, e := tx.BeginPersonalMigrationFence(v.Binding.ScopeID, "migration", time.Now())
		if e != nil {
			return e
		}
		if _, e = tx.StartPersonalMigrationDrain(v.Binding.ScopeID, "migration", fence.Revision, time.Now()); e != nil {
			return e
		}
		return tx.PreservePersonalMigrationUnsent(v.Binding.ScopeID, "preview", id, v.Revision, time.Now())
	})
	f.tx(func(tx *workstore.Tx) error {
		pending, e := tx.HasPendingPrivatePreview("attempt")
		if e != nil {
			return e
		}
		if pending {
			t.Fatal("preserved preview still blocks")
		}
		return nil
	})
	if err := f.coordinator.RunPrivatePreviews(context.Background(), 8); err != nil {
		t.Fatal(err)
	}
	f.coordinator.WaitPrivatePreviews()
	if calls.Load() != 0 {
		t.Fatal("preserved preview was sent")
	}
	if err := f.coordinator.Reconcile(context.Background(), "attempt"); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		if _, e := tx.GetQuiescenceReceipt("attempt"); e != nil {
			return e
		}
		v, e := tx.GetPrivateExecutionDisplay(id)
		if e != nil {
			return e
		}
		if v.PreviewTerminal == nil || v.PreviewState != "queued" {
			t.Fatal("preview result was discarded")
		}
		return nil
	})
	f.tx(func(tx *workstore.Tx) error {
		v, e := tx.GetPrivateExecutionDisplay(id)
		if e != nil {
			return e
		}
		old := v.Revision
		v.PreviewState = "started"
		v.Revision++
		if e = tx.PutPrivateExecutionDisplay(v, old); e != nil {
			return e
		}
		pending, e := tx.HasPendingPrivatePreview("attempt")
		if e != nil {
			return e
		}
		if !pending {
			t.Fatal("started preview incorrectly treated as preserved")
		}
		return nil
	})
	if err := f.coordinator.RunPrivatePreviews(context.Background(), 8); err != nil {
		t.Fatal(err)
	}
	f.coordinator.WaitPrivatePreviews()
	f.tx(func(tx *workstore.Tx) error {
		v, e := tx.GetPrivateExecutionDisplay(id)
		if e != nil {
			return e
		}
		if v.PreviewState != "started" || v.PreviewTerminal == nil {
			t.Fatal("unknown started preview was discarded")
		}
		return nil
	})
}
