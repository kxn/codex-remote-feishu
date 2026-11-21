package workstore

import (
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

func TestRemovedMemberCanSettleOldRunButCannotClaimAgain(t *testing.T) {
	for _, kind := range []string{"project_removed", "team_inactive"} {
		t.Run(kind, func(t *testing.T) {
			s, _ := openTestStore(t)
			seedPersonal(t, s)
			claim := seedRunningAttempt(t, s)
			requireTx(t, s, func(tx *Tx) error {
				if kind == "project_removed" {
					member, e := tx.GetProjectMember("project", "r")
					if e != nil {
						return e
					}
					before := member.Revision
					member.Revision++
					member.State = work.ProjectMemberRemoved
					if e = tx.PutProjectMember(member, before); e != nil {
						return e
					}
				} else {
					member, e := tx.GetTeamMember("team", "r")
					if e != nil {
						return e
					}
					before := member.Revision
					member.Revision++
					member.Active = false
					if e = tx.PutTeamMember(member, before); e != nil {
						return e
					}
				}
				outcome := testOutcome(claim)
				outcome.Quiescent = false
				if kind == "team_inactive" {
					outcome.ProcessedInputIDs = nil
				}
				if e := tx.CommitOutcome(outcome); e != nil {
					return e
				}
				worker, e := tx.GetWorker("w")
				if e != nil {
					return e
				}
				if worker.State != work.WorkerRecovering {
					t.Fatal("membership removal released unverified native execution")
				}
				input, e := tx.GetInbox("input")
				if e != nil {
					return e
				}
				expected := work.InboxProcessed
				if kind == "team_inactive" {
					expected = work.InboxPending
				}
				if input.State != expected {
					t.Fatal("old inputs could not retain their actual processing disposition")
				}
				proof := work.QuiescenceEvidence{RunRef: claim.Attempt.RunRef, InstanceID: worker.InstanceID, InstanceGeneration: worker.InstanceGeneration, ProcessGeneration: worker.ProcessGeneration, CarrierIncarnation: worker.CarrierIncarnation, NativeStopped: true, ContinuationStopped: true, BackgroundWorkStopped: true, Source: work.SourceRef{Kind: "trusted_runtime_inspection", ID: "three-domain-fixture", Version: "1", EvidenceRef: "body"}, ObservedAt: time.Now().UTC()}
				if e = tx.ReconcileQuiescence(ReconcileQuiescenceRequest{Evidence: proof, ExpectedWorkerRevision: worker.Revision}); e != nil {
					return e
				}
				worker, e = tx.GetWorker("w")
				if e != nil {
					return e
				}
				if e = tx.PutInbox(testInput("new-work", "r", "c-r"), 0); e == nil {
					t.Fatal("removed member accepted a new assignment")
				}
				if kind == "team_inactive" {
					next := testClaim()
					next.Attempt.ID = "successor"
					next.Attempt.RunRef.RunID = "successor-run"
					next.Attempt.CommandID = "successor-command"
					next.StartOperation = testOperation("successor-command")
					next.ExpectedWorkerRevision = worker.Revision
					if e = tx.ClaimAttempt(next); e == nil {
						t.Fatal("historical pending input bypassed membership fence")
					}
				}
				return nil
			})
		})
	}
}
