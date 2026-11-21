package workservice

import (
	"errors"
	"fmt"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestCollaborationQueuesForUnavailableMemberButRejectsRemovedMember(t *testing.T) {
	s, token, run := lifecycleProjectService(t, false)
	setState := func(state work.ProjectMemberState) {
		t.Helper()
		if err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
			member, err := tx.GetProjectMember("project", "expert")
			if err != nil {
				return err
			}
			member.State = state
			member.Revision++
			return tx.PutProjectMember(member, member.Revision-1)
		}); err != nil {
			t.Fatal(err)
		}
	}
	setState(work.ProjectMemberUnavailable)
	task, err := s.AssignTask(t.Context(), token, run, AssignTaskRequest{Key: "unavailable-task", Title: "Preserve the request", OwnerRoleID: "expert"})
	if err != nil {
		t.Fatal(err)
	}
	message, err := s.SendMessage(t.Context(), token, run, SendMessageRequest{Key: "unavailable-message", RecipientRoleID: "expert",
		RelatedTaskID: task.ObjectID, ConversationID: "review-request", Body: "Please follow up when available."})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		inputs, err := tx.ListPendingInputs("project-scope", "expert")
		if err != nil {
			return err
		}
		if len(inputs) != 2 {
			return fmt.Errorf("expected assigned work and associated message: %+v", inputs)
		}
		for _, input := range inputs {
			if input.MessageID == message.ObjectID && input.RelatedTaskID == task.ObjectID && input.TaskID == "" {
				return nil
			}
		}
		return fmt.Errorf("related discussion became a task admission: %+v", inputs)
	}); err != nil {
		t.Fatal(err)
	}
	setState(work.ProjectMemberRemoved)
	if _, err = s.AssignTask(t.Context(), token, run, AssignTaskRequest{Key: "removed-task", Title: "Removed", OwnerRoleID: "expert"}); !errors.Is(err, workauth.ErrUnauthorized) {
		t.Fatalf("assignment to a removed role: %v", err)
	}
	if _, err = s.SendMessage(t.Context(), token, run, SendMessageRequest{Key: "removed-message", RecipientRoleID: "expert",
		ConversationID: "review-request", Body: "Do not admit this input."}); !errors.Is(err, workauth.ErrUnauthorized) {
		t.Fatalf("message to a removed role: %v", err)
	}
}

func TestCollaborationRejectsClosedAndCrossScopeExecutionTargets(t *testing.T) {
	s, token, run := lifecycleProjectService(t, false)
	task, err := s.AssignTask(t.Context(), token, run, AssignTaskRequest{Key: "open-task", Title: "Leader's task", OwnerRoleID: "leader"})
	if err != nil {
		t.Fatal(err)
	}
	req := SendMessageRequest{Key: "wrong-owner", RecipientRoleID: "expert", TaskID: task.ObjectID,
		ConversationID: "group", Body: "This task belongs to another worker."}
	if _, err = s.SendMessage(t.Context(), token, run, req); err == nil {
		t.Fatal("message targeted a task the receiving role does not own")
	}
	started, err := s.TaskStart(t.Context(), token, run, TaskTransitionRequest{Key: "start-task", TaskID: task.ObjectID, ExpectedRevision: task.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.TaskDone(t.Context(), token, run, TaskTransitionRequest{Key: "complete-task", TaskID: task.ObjectID, ExpectedRevision: started.Revision}); err != nil {
		t.Fatal(err)
	}
	req.Key, req.RecipientRoleID = "closed-task", "leader"
	if _, err = s.SendMessage(t.Context(), token, run, req); err == nil {
		t.Fatal("message tried to reopen completed work")
	}
	req.Key, req.TaskID, req.RelatedTaskID = "closed-discussion", "", task.ObjectID
	if _, err = s.SendMessage(t.Context(), token, run, req); err != nil {
		t.Fatalf("completed work should remain discussable: %v", err)
	}
	if err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		scope := work.WorkScope{ID: "private-leader", Kind: work.ScopePersonal, PrincipalID: "another-human", RoleID: "leader", Revision: 1}
		if err := tx.PutPrincipal(work.Principal{ID: scope.PrincipalID, DisplayName: "Other person", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutScope(scope, 0); err != nil {
			return err
		}
		other := work.Task{ID: "private-task", ScopeID: scope.ID, Title: "Private task", OwnerRoleID: scope.RoleID,
			RequirementsRevision: 1, AssignmentGeneration: 1, State: work.TaskTodo, Revision: 1}
		return tx.PutTask(other, 0)
	}); err != nil {
		t.Fatal(err)
	}
	req.Key, req.RelatedTaskID = "foreign-link", "private-task"
	if _, err = s.SendMessage(t.Context(), token, run, req); !errors.Is(err, workstore.ErrScopeDenied) {
		t.Fatalf("cross-scope task link: %v", err)
	}
	if _, err = s.PublishMessage(t.Context(), token, run, PublishMessageRequest{Key: "foreign-topic", TaskID: "private-task", Body: "Unsafe publication."}); !errors.Is(err, workauth.ErrUnauthorized) {
		t.Fatalf("cross-scope publication: %v", err)
	}
}
