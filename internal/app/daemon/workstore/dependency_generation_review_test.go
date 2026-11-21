package workstore

import (
	"context"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestReviewDependencySatisfactionDoesNotSurviveChildRequirementChange(t *testing.T) {
	store, _ := openTestStore(t)
	seedPersonal(t, store)
	requireTx(t, store, func(tx *Tx) error {
		parent := testTask("parent-versioned", "r")
		parent.State = work.TaskActive
		child := testTask("child-versioned", "expert")
		if err := tx.PutTask(parent, 0); err != nil {
			return err
		}
		if err := tx.PutTask(child, 0); err != nil {
			return err
		}
		source := work.SourceRef{Kind: "git_commit", ID: "old-result", Version: "old-sha", EvidenceRef: "old-review"}
		if err := tx.AddDependency(work.Dependency{TaskID: parent.ID, DependsOnTaskID: child.ID, Condition: work.DependencyIntegrated}); err != nil {
			return err
		}
		if err := tx.SatisfyDependency(work.Dependency{TaskID: parent.ID, DependsOnTaskID: child.ID, Condition: work.DependencyIntegrated, SatisfiedBy: &source}); err != nil {
			return err
		}
		child.Title = "New materially different requirement"
		child.RequirementsRevision++
		child.Revision++
		return tx.PutTask(child, 1)
	})
	err := store.Within(context.Background(), func(tx *Tx) error {
		parent, err := tx.GetTask("parent-versioned")
		if err != nil {
			return err
		}
		parent.State = work.TaskDone
		parent.Revision++
		return tx.PutTask(parent, 1)
	})
	if err == nil {
		t.Fatal("parent completed using a dependency conclusion for the child's previous requirements")
	}
}
