package workingress

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
)

func TestTopicReplyUsesCurrentOwnerAndWaitingFeedbackUsesCoordinationStream(t *testing.T) {
	ingress, store, _ := ingressFixture(t, true)
	receive(t, ingress, rawMessage("leader", "root", "group", "group", "open", "user", "Initial business request"))
	process(t, ingress)
	txRun(t, store, func(tx *workstore.Tx) error {
		task := work.Task{ID: "task", ScopeID: "scope", Title: "Work", OwnerRoleID: "expert", RequirementsRevision: 1, AssignmentGeneration: 1, State: work.TaskTodo, Revision: 1}
		if err := tx.PutTask(task, 0); err != nil {
			return err
		}
		return tx.PutTaskConversation(work.TaskConversation{ScopeID: "scope", TaskID: "task", Tenant: "tenant", ChatID: "group", RootMessageID: "root", ChannelGeneration: 1, Revision: 1}, 0)
	})
	followup := rawMessage("leader", "reply", "group", "group", "open", "user", "Follow up in the same topic")
	followup.ParentID = "root"
	followup.RootID = "root"
	receive(t, ingress, followup)
	process(t, ingress)
	txRun(t, store, func(tx *workstore.Tx) error {
		mapping, err := tx.GetPlatformMessageMapping("tenant", "group", "reply")
		if err != nil {
			return err
		}
		if mapping.TaskID != "task" {
			t.Fatal("task conversation was ignored because original root arrived before task creation")
		}
		inputs, err := tx.ListInbox("scope")
		if err != nil {
			return err
		}
		found := false
		for _, input := range inputs {
			if input.TaskID == "task" {
				found = true
				if input.RecipientRoleID != "expert" {
					t.Fatal("reply went to observing bot instead of current task owner")
				}
			}
		}
		if !found {
			t.Fatal("no task reply input")
		}
		task, err := tx.GetTask("task")
		if err != nil {
			return err
		}
		task.State = work.TaskActive
		task.Revision++
		if err = tx.PutTask(task, 1); err != nil {
			return err
		}
		task.State = work.TaskWaiting
		task.Revision++
		return tx.PutTask(task, 2)
	})
	feedback := followup
	feedback.MessageID = "waiting-feedback"
	feedback.EventID = "waiting-event"
	feedback.RawContent = `{"text":"Please change the requirements while waiting"}`
	receive(t, ingress, feedback)
	process(t, ingress)
	txRun(t, store, func(tx *workstore.Tx) error {
		mapping, err := tx.GetPlatformMessageMapping("tenant", "group", "waiting-feedback")
		if err != nil {
			return err
		}
		inputs, err := tx.ListInbox("scope")
		if err != nil {
			return err
		}
		for _, input := range inputs {
			message, err := tx.GetMessage(input.MessageID)
			if err != nil {
				return err
			}
			if message.PlatformKey.MessageID == "waiting-feedback" {
				if input.TaskID != "" || input.RelatedTaskID != "task" || input.RecipientRoleID != "expert" || mapping.TaskID != "task" {
					t.Fatalf("waiting feedback deadlocked behind dependencies or lost topic: %+v", input)
				}
				return nil
			}
		}
		t.Fatal("waiting feedback lost")
		return nil
	})
}
func TestExplicitQuestionToAnotherExpertKeepsOriginalResponsibility(t *testing.T) {
	ingress, store, _ := ingressFixture(t, true)
	receive(t, ingress, rawMessage("leader", "root", "group", "group", "open", "user", "Original request"))
	process(t, ingress)
	txRun(t, store, func(tx *workstore.Tx) error {
		if err := tx.PutTask(work.Task{ID: "task", ScopeID: "scope", Title: "Original", OwnerRoleID: "leader", RequirementsRevision: 1, AssignmentGeneration: 1, State: work.TaskTodo, Revision: 1}, 0); err != nil {
			return err
		}
		return tx.PutTaskConversation(work.TaskConversation{ScopeID: "scope", TaskID: "task", Tenant: "tenant", ChatID: "group", RootMessageID: "root", ChannelGeneration: 1, Revision: 1}, 0)
	})
	question := rawMessage("leader", "question", "group", "group", "open", "user", "@_expert what do you think?")
	question.ParentID = "root"
	question.RootID = "root"
	question.Mentions = []work.RawMention{{Key: "@_expert", Type: "bot", IDs: work.SenderIDs{OpenID: "bot:expert"}, Name: "Expert"}}
	receive(t, ingress, question)
	process(t, ingress)
	txRun(t, store, func(tx *workstore.Tx) error {
		tasks, err := tx.ListTasks("scope")
		if err != nil {
			return err
		}
		if len(tasks) != 1 || tasks[0].OwnerRoleID != "leader" {
			t.Fatal("direct question transferred responsibility or created duplicate task")
		}
		inputs, err := tx.ListInbox("scope")
		if err != nil {
			return err
		}
		for _, input := range inputs {
			if input.RelatedTaskID == "task" {
				if input.TaskID != "" || input.RecipientRoleID != "expert" {
					t.Fatal("question became a task assignment")
				}
				return nil
			}
		}
		t.Fatal("expert did not receive related question")
		return nil
	})
}
