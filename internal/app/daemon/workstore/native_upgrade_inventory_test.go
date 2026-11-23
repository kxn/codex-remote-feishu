package workstore

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestNativeUpgradeInventoryRejectsWorkerWithoutAttributableHost(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		worker, err := tx.GetWorker("w")
		if err != nil {
			return err
		}
		worker.Backend = work.BackendCodex
		worker.LocationID = ""
		worker.Revision++
		return tx.PutWorker(worker, worker.Revision-1)
	})
	err := s.Within(t.Context(), func(tx *Tx) error {
		_, err := tx.NativeUpgradeWorkers("host")
		return err
	})
	if err == nil {
		t.Fatal("missing worker host was silently omitted from maintenance inventory")
	}
}
