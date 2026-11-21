package workstore

import (
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
)

func reviewedTask() work.Task {
	v := testTask("task", "r")
	v.Requirements = []work.TaskRequirementRef{{ID: "req", ScopeID: "s", Source: evidenceSource(), AdoptedBy: "p", RequiredEvidence: []string{"review"}}}
	return v
}
func reviewEvidence() work.RequirementEvidence {
	return work.RequirementEvidence{ID: "review", ScopeID: "s", TaskID: "task", RequirementsRevision: 1, AssignmentGeneration: 1, RequirementID: "req", RequirementVersion: "1", Kind: "review", Source: evidenceSource(), AcceptedBy: work.Actor{Kind: work.ActorRole, ID: "expert"}}
}
func TestRequirementEvidenceSurvivesStateChanges(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		task := reviewedTask()
		if err := tx.PutTask(task, 0); err != nil {
			return err
		}
		task.State = work.TaskActive
		task.Revision++
		if err := tx.PutTask(task, 1); err != nil {
			return err
		}
		if err := tx.PutEvidence(reviewEvidence()); err != nil {
			return err
		}
		task.State = work.TaskChecking
		task.Revision++
		if err := tx.PutTask(task, 2); err != nil {
			return err
		}
		task.State = work.TaskDone
		task.Revision++
		return tx.PutTask(task, 3)
	})
	requireTx(t, s, func(tx *Tx) error {
		task, err := tx.GetTask("task")
		if err != nil {
			return err
		}
		if task.Revision != 4 || task.RequirementsRevision != 1 || task.State != work.TaskDone {
			t.Fatalf("state transitions damaged requirements: %+v", task)
		}
		return nil
	})
}
func TestRequirementsChangeInvalidatesOldEvidence(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		task := reviewedTask()
		if err := tx.PutTask(task, 0); err != nil {
			return err
		}
		if err := tx.PutEvidence(reviewEvidence()); err != nil {
			return err
		}
		task.Title = "new requirement"
		task.Revision++
		if err := tx.PutTask(task, 1); err == nil {
			t.Fatal("semantic change without new requirements revision")
		}
		task.RequirementsRevision++
		if err := tx.PutTask(task, 1); err != nil {
			return err
		}
		task.State = work.TaskActive
		task.Revision++
		if err := tx.PutTask(task, 2); err != nil {
			return err
		}
		task.State = work.TaskDone
		task.Revision++
		if err := tx.PutTask(task, 3); err == nil {
			t.Fatal("old review accepted revised requirements")
		}
		return nil
	})
}
func TestOldAssignmentOutcomeOnlyArchivesEvidence(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.PutTask(reviewedTask(), 0); err != nil {
			return err
		}
		input := testInput("input", "r", "c-r")
		input.TaskID = "task"
		return tx.PutInbox(input, 0)
	})
	claim := testClaim()
	claim.Attempt.TaskID = "task"
	claim.Attempt.FrozenRequirementsRevision = 1
	claim.Attempt.FrozenAssignmentGeneration = 1
	requireTx(t, s, func(tx *Tx) error { return tx.ClaimAttempt(claim) })
	markRunning(t, s)
	requireTx(t, s, func(tx *Tx) error {
		task, err := tx.GetTask("task")
		if err != nil {
			return err
		}
		task.OwnerRoleID = "expert"
		task.AssignmentGeneration++
		task.Revision++
		return tx.PutTask(task, 1)
	})
	outcome := testOutcome(claim)
	outcome.TaskEvidence = []work.RequirementEvidence{reviewEvidence()}
	outcome.FollowupInbox = []work.Inbox{testInput("followup", "expert", "")}
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.CommitOutcome(outcome); err == nil {
			t.Fatal("old assignment advanced followup work")
		}
		return nil
	})
	outcome.FollowupInbox = nil
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.CommitOutcome(outcome); err != nil {
			return err
		}
		context, err := tx.GetContext("c-r")
		if err != nil {
			return err
		}
		if context.Checkpoint != "" {
			t.Fatal("stale assignment overwrote checkpoint")
		}
		task, err := tx.GetTask("task")
		if err != nil {
			return err
		}
		evidence, err := tx.ListEvidence(task.ID)
		if err != nil {
			return err
		}
		if len(evidence) != 1 {
			t.Fatal("historical evidence lost")
		}
		if err = work.ValidateTaskCompletion(task, evidence); err == nil {
			t.Fatal("old owner's evidence satisfied new assignment")
		}
		return nil
	})
}
func TestPersonalScopeAndMemoryAreRolePrivate(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		for _, role := range []string{"r", "expert"} {
			if err := tx.PutScope(work.WorkScope{ID: "personal-" + role, Kind: work.ScopePersonal, PrincipalID: "p", RoleID: role, Revision: 1}, 0); err != nil {
				return err
			}
		}
		if err := tx.PutContext(work.WorkContext{ID: "leak", ScopeID: "personal-r", RoleID: "expert", Workstream: "main", Revision: 1}, 0); err == nil {
			t.Fatal("cross-role personal context accepted")
		}
		scope, err := tx.GetScope("personal-r")
		if err != nil {
			return err
		}
		scope.RoleID = "expert"
		scope.Revision++
		if err = tx.PutScope(scope, 1); err == nil {
			t.Fatal("personal owner silently rebound")
		}
		return nil
	})
	if err := (work.MemoryScope{Kind: work.MemoryPersonal, PrincipalID: "p"}).Validate(); err == nil {
		t.Fatal("personal memory missing role")
	}
	if err := (work.MemoryScope{Kind: work.MemoryPersonal, PrincipalID: "p", RoleID: "r"}).Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestBindingGenerationsKeepHistory(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		binding := work.ProjectChannelBinding{ProjectID: "project", Tenant: "tenant", ChatID: "chat", Purpose: "primary", BindingGeneration: 1, Active: true}
		if err := tx.PutProjectChannelBinding(binding, 0); err != nil {
			return err
		}
		if err := tx.PutProjectChannelBinding(binding, 0); !errors.Is(err, ErrConflict) {
			t.Fatalf("stale binding CAS %v", err)
		}
		binding.Active = false
		binding.BindingGeneration++
		if err := tx.PutProjectChannelBinding(binding, 1); err != nil {
			return err
		}
		if _, err := tx.GetChannelProject("tenant", "chat"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("inactive channel routed: %v", err)
		}
		history, err := tx.ListProjectChannelBindings("project")
		if err != nil {
			return err
		}
		if len(history) != 2 || history[0].Active || history[1].Active {
			t.Fatalf("binding history %+v", history)
		}
		return nil
	})
}
