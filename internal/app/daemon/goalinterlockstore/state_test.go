package goalinterlockstore

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func TestStoreRoundTripLeases(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFileName)
	store := NewStore(path)
	lease := orchestrator.GoalInterlockLease{
		InstanceID:           "inst-1",
		ThreadID:             "thread-1",
		Phase:                orchestrator.GoalInterlockDraining,
		Objective:            "ship it",
		TriggerSurfaceID:     "surface-1",
		ExternalMutationSeen: true,
		UpdatedAt:            time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC),
	}
	if err := store.ReplaceAll([]orchestrator.GoalInterlockLease{lease}); err != nil {
		t.Fatalf("ReplaceAll: %v", err)
	}

	loaded, err := LoadStore(path)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	leases := loaded.Leases()
	if len(leases) != 1 {
		t.Fatalf("expected one lease, got %#v", leases)
	}
	got := leases[0]
	if got.InstanceID != "inst-1" || got.ThreadID != "thread-1" || got.Phase != orchestrator.GoalInterlockDraining ||
		got.Objective != "ship it" || !got.ExternalMutationSeen {
		t.Fatalf("unexpected loaded lease: %#v", got)
	}
}

func TestStoreReplaceAllClearsMissingLeases(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFileName)
	store := NewStore(path)
	if err := store.ReplaceAll([]orchestrator.GoalInterlockLease{{
		InstanceID: "inst-1",
		ThreadID:   "thread-1",
		Phase:      orchestrator.GoalInterlockPausePending,
	}}); err != nil {
		t.Fatalf("ReplaceAll: %v", err)
	}
	if err := store.ReplaceAll(nil); err != nil {
		t.Fatalf("ReplaceAll nil: %v", err)
	}
	if leases := store.Leases(); len(leases) != 0 {
		t.Fatalf("expected cleared leases, got %#v", leases)
	}
}
