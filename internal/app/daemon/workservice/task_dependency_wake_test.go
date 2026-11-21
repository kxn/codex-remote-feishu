package workservice

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestRevisedChildResultWakesParentOncePerSubmission(t *testing.T) {
	service, token, run := lifecycleProjectService(t, false)
	ctx := t.Context()
	parent, err := service.AssignTask(ctx, token, run, AssignTaskRequest{Key: "parent", Title: "Review result", OwnerRoleID: "leader"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := service.AssignTask(ctx, token, run, AssignTaskRequest{Key: "child", Title: "Implement result", OwnerRoleID: "expert", ParentTaskID: parent.ObjectID, ParentRevision: parent.Revision, Condition: work.DependencyResultAvailable})
	if err != nil {
		t.Fatal(err)
	}
	transition := func(action func(TaskTransitionRequest) (Result, error), key string) {
		t.Helper()
		next, err := action(TaskTransitionRequest{Key: key, TaskID: child.ObjectID, ExpectedRevision: child.Revision})
		if err != nil {
			t.Fatal(err)
		}
		child = next
	}
	start := func(req TaskTransitionRequest) (Result, error) { return service.TaskStart(ctx, token, run, req) }
	check := func(req TaskTransitionRequest) (Result, error) { return service.TaskCheck(ctx, token, run, req) }
	assertNotices := func(expected int) {
		t.Helper()
		if err := service.store.Within(ctx, func(tx *workstore.Tx) error {
			inbox, err := tx.ListInbox("project-scope")
			if err != nil {
				return err
			}
			var count int
			for _, input := range inbox {
				if input.RelatedTaskID == parent.ObjectID && input.RecipientRoleID == "leader" {
					count++
				}
			}
			if count != expected {
				t.Fatalf("expected %d coordination inputs, got %d", expected, count)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	transition(start, "start")
	transition(check, "result-1")
	assertNotices(1)
	transition(check, "same-first-result")
	assertNotices(1)
	transition(start, "return-for-revision")
	transition(check, "result-2")
	assertNotices(2)
	transition(check, "same-second-result")
	assertNotices(2)
	transition(func(req TaskTransitionRequest) (Result, error) { return service.TaskDone(ctx, token, run, req) }, "done")
	assertNotices(2)
}
