package workruntime

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestRetirementCommitRejectsReconnectAfterProof(t *testing.T) {
	f, request := retiredInspectionFixture(t, "stopped", false)
	observation, err := VerifyRetiredInspection(t.Context(), f.store, f.coordinator.options.StateDir, f.coordinator.options.HostID, request)
	if err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		return tx.SetCarrierConnected(request.Carrier.Incarnation, true)
	})
	err = f.store.Within(t.Context(), func(tx *workstore.Tx) error {
		return validateRetirementCommit(tx, request, observation)
	})
	if err == nil {
		t.Fatal("carrier reconnected after proof but settlement was allowed")
	}
	f.tx(func(tx *workstore.Tx) error {
		input, err := tx.GetInbox("input")
		if err != nil {
			return err
		}
		occupied, err := tx.HasCarrierOccupancy(f.carrier, "attempt")
		if err != nil {
			return err
		}
		if !occupied || input.State != work.InboxClaimed {
			t.Fatal("rejected retirement changed occupancy or consumed input")
		}
		return nil
	})
}
