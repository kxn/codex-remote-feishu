package workingress

import (
	"fmt"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateStopCutoffFencesLateClaimAndRequeuesOnlyNewerInput(t *testing.T) {
	for _, mixed := range []bool{true, false} {
		t.Run(fmt.Sprintf("mixed_old_and_new_%t", mixed), func(t *testing.T) {
			ingress, store, _ := ingressFixture(t, false)
			old := rawMessage("leader", "queued-before-stop", "dm", "p2p", "open-person", "person", "原有待办")
			receive(t, ingress, old)
			process(t, ingress)
			stop := rawMessage("leader", "delayed-stop", "dm", "p2p", "open-person", "person", "/stop")
			stop.ReceivedAt = old.ReceivedAt.Add(time.Second)
			source, nav := privateCommandSourceFromIngress(t, ingress, store, stop)
			newer := rawMessage("leader", "input-after-stop", "dm", "p2p", "open-person", "person", "新的请求")
			newer.ReceivedAt = stop.ReceivedAt.Add(time.Second)
			receive(t, ingress, newer)
			process(t, ingress)

			var worker work.WorkerSession
			var run work.ExecutionAttempt
			var oldInputID, newInputID string
			txRun(t, store, func(tx *workstore.Tx) error {
				items, err := tx.ListPendingInputs(nav.PersonalScopeID, "leader")
				if err != nil || len(items) != 2 {
					t.Fatalf("expected both actual private SDK inputs before the delayed Stop handler: %+v %v", items, err)
				}
				for _, item := range items {
					message, e := tx.GetMessage(item.MessageID)
					if e != nil {
						return e
					}
					switch message.PlatformKey.MessageID {
					case old.MessageID:
						oldInputID = item.ID
					case newer.MessageID:
						newInputID = item.ID
					}
				}
				worker = work.WorkerSession{ID: "cutoff-worker", ScopeID: nav.PersonalScopeID, ContextID: nav.FrontContextID, RoleID: "leader", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "instance", InstanceGeneration: 1, ProcessGeneration: 1, CarrierIncarnation: "carrier", LocationID: "cutoff-location", LocationGeneration: 1, Revision: 1}
				if err = tx.PutLocation(work.ExecutionLocation{ID: worker.LocationID, HostID: "host", Cwd: t.TempDir(), Generation: 1, Revision: 1}, 0); err != nil {
					return err
				}
				if err = tx.PutWorker(worker, 0); err != nil {
					return err
				}
				// The scheduler wins after the stop's real received timestamp.
				run = legacyStopClaim(worker, newInputID, "cutoff", newer.ReceivedAt.Add(time.Second))
				if mixed {
					run.InputIDs = append([]string{oldInputID}, run.InputIDs...)
				}
				if err = tx.ClaimAttempt(legacyStopClaimRequest(worker, run)); err != nil {
					return err
				}
				run.State = work.AttemptRunning
				run.Revision++
				return tx.PutAttempt(run, run.Revision-1)
			})
			service := privateTestService(t, store)
			if _, err := service.TrustedPrivateStop(t.Context(), workservice.TrustedPrivateStopRequest{Key: "delayed-stop", Source: source, ExpectedNavRevision: nav.Revision}); err != nil {
				t.Fatal(err)
			}
			txRun(t, store, func(tx *workstore.Tx) error {
				marked, err := tx.PrivateAttemptStopped(run)
				if err != nil || marked != mixed {
					t.Fatalf("received cutoff classified the late claim incorrectly: marked=%t want=%t %v", marked, mixed, err)
				}
				wantNew := work.InboxClaimed
				if mixed {
					if err = settleLegacyPrivateStop(tx, run, run.CreatedAt.Add(time.Second), false); err != nil {
						return err
					}
					wantNew = work.InboxPending
				}
				previous, err := tx.GetInbox(oldInputID)
				if err != nil || previous.State != work.InboxCancelled {
					t.Fatalf("old SDK input survived the stop: %+v %v", previous, err)
				}
				fresh, err := tx.GetInbox(newInputID)
				if err != nil || fresh.State != wantNew {
					t.Fatalf("newer SDK input lost its admission: %+v %v", fresh, err)
				}
				if !mixed {
					return nil
				}
				if fresh.ClaimedAttemptID != "" {
					t.Fatalf("mixed stopped attempt still owns the requeued new input: %+v", fresh)
				}
				pending, err := tx.ListPendingInputs(nav.PersonalScopeID, worker.RoleID)
				if err != nil || len(pending) != 1 || pending[0].ID != newInputID {
					t.Fatalf("new SDK input did not requeue exactly once: %+v %v", pending, err)
				}
				worker, err = tx.GetWorker(worker.ID)
				if err != nil {
					return err
				}
				next := legacyStopClaim(worker, newInputID, "next", run.CreatedAt.Add(2*time.Second))
				return tx.ClaimAttempt(legacyStopClaimRequest(worker, next))
			})
		})
	}
}
