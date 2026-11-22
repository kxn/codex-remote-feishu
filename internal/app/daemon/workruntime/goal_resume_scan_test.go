package workruntime

import (
	"fmt"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
)

func TestPendingGoalResumeScanDoesNotStarveAfterFirstBatch(t *testing.T) {
	f := idleKnownGoal(t, "paused")
	first := queueFrozenResume(t, f)
	f.tx(func(tx *workstore.Tx) error {
		for i := 0; i < 65; i++ {
			_, _, err := tx.QueueSessionGoalIntent(fmt.Sprintf("resume-intent-%03d", i), first.ContextID, first.Goal, workstore.PrivateActionSource{PersonalScopeID: first.ScopeID, CommandID: "resume-human"}, f.now)
			if err != nil {
				return err
			}
		}
		return nil
	})
	for i := 1; i <= 2; i++ {
		f.tx(func(tx *workstore.Tx) error {
			rejected, err := tx.CancelStaleSessionGoalResumes(f.now.Add(time.Duration(i) * time.Second))
			if len(rejected) != 0 {
				t.Fatal("valid pending goals were cancelled")
			}
			return err
		})
	}
	f.tx(func(tx *workstore.Tx) error {
		last, err := tx.GetSessionGoalIntent("resume-intent-064")
		if err != nil {
			return err
		}
		if last.LastCheckedAt.IsZero() {
			t.Fatal("first valid batch starved later pending goal")
		}
		return nil
	})
}
