package workingress

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
	"testing"
)

func TestTopicReplyAfterOwnerExitGoesToLeaderWithoutChangingTask(t *testing.T) {
	for _, kind := range []string{"removed", "inactive", "unavailable"} {
		t.Run(kind, func(t *testing.T) {
			ingress, store, _ := ingressFixture(t, true)
			receive(t, ingress, rawMessage("leader", "root", "group", "group", "open", "user", "Original request"))
			process(t, ingress)
			txRun(t, store, func(tx *workstore.Tx) error {
				if err := tx.PutTask(work.Task{ID: "task", ScopeID: "scope", Title: "Original", OwnerRoleID: "expert", RequirementsRevision: 1, AssignmentGeneration: 1, State: work.TaskTodo, Revision: 1}, 0); err != nil {
					return err
				}
				if err := tx.PutTaskConversation(work.TaskConversation{ScopeID: "scope", TaskID: "task", Tenant: "tenant", ChatID: "group", RootMessageID: "root", ChannelGeneration: 1, Revision: 1}, 0); err != nil {
					return err
				}
				if kind == "inactive" {
					scope, e := tx.GetScope("scope")
					if e != nil {
						return e
					}
					m, e := tx.GetTeamMember(scope.TeamID, "expert")
					if e != nil {
						return e
					}
					old := m.Revision
					m.Revision++
					m.Active = false
					return tx.PutTeamMember(m, old)
				}
				m, e := tx.GetProjectMember("project", "expert")
				if e != nil {
					return e
				}
				old := m.Revision
				m.Revision++
				if kind == "removed" {
					m.State = work.ProjectMemberRemoved
				} else {
					m.State = work.ProjectMemberUnavailable
				}
				return tx.PutProjectMember(m, old)
			})
			raw := rawMessage("leader", "after-exit", "group", "group", "open", "user", "Please clarify this task")
			raw.ParentID = "root"
			raw.RootID = "root"
			receipt := receive(t, ingress, raw)
			process(t, ingress)
			txRun(t, store, func(tx *workstore.Tx) error {
				items, e := tx.ListInbox("scope")
				if e != nil {
					return e
				}
				found := false
				for _, v := range items {
					m, e := tx.GetMessage(v.MessageID)
					if e != nil {
						return e
					}
					if m.PlatformKey.MessageID != "after-exit" {
						continue
					}
					found = true
					if kind == "unavailable" {
						if v.RecipientRoleID != "expert" || v.TaskID != "task" {
							t.Fatalf("temporary unavailability rerouted: %+v", v)
						}
					} else if v.RecipientRoleID != "leader" || v.TaskID != "" || v.RelatedTaskID != "task" {
						t.Fatalf("wrong exit coordination: %+v", v)
					}
					if kind != "unavailable" {
						body, e := tx.GetContent("scope", m.BodyRef)
						if e != nil {
							return e
						}
						if !strings.Contains(string(body.Data), "原任务负责人") {
							t.Fatal("coordination lost ownership explanation")
						}
					}
				}
				if !found {
					t.Fatal("real SDK topic reply was not durably accepted")
				}
				task, e := tx.GetTask("task")
				if e != nil {
					return e
				}
				if task.OwnerRoleID != "expert" || task.State != work.TaskTodo || task.AssignmentGeneration != 1 || task.Revision != 1 {
					t.Fatalf("routing mutated task: %+v", task)
				}
				saved, e := tx.GetIngress(receipt.IngressID)
				if e == nil && saved.State != work.IngressProcessed {
					t.Fatalf("input still retrying: %+v", saved)
				}
				return e
			})
		})
	}
}

func TestFinishedTopicKeepsHistoricalTaskWhenMentionFallsBack(t *testing.T) {
	for _, multiple := range []bool{false, true} {
		t.Run(map[bool]string{false: "removed_mention", true: "multiple_mentions"}[multiple], func(t *testing.T) {
			ingress, store, _ := ingressFixture(t, true)
			receive(t, ingress, rawMessage("leader", "root", "group", "group", "open", "user", "Original request"))
			process(t, ingress)
			txRun(t, store, func(tx *workstore.Tx) error {
				task := work.Task{ID: "finished", ScopeID: "scope", Title: "Old task", OwnerRoleID: "expert", RequirementsRevision: 1, AssignmentGeneration: 1, State: work.TaskTodo, Revision: 1}
				if e := tx.PutTask(task, 0); e != nil {
					return e
				}
				task.State = work.TaskCancelled
				task.Revision++
				if e := tx.PutTask(task, 1); e != nil {
					return e
				}
				if e := tx.PutTaskConversation(work.TaskConversation{ScopeID: "scope", TaskID: task.ID, Tenant: "tenant", ChatID: "group", RootMessageID: "root", ChannelGeneration: 1, Revision: 1}, 0); e != nil {
					return e
				}
				m, e := tx.GetProjectMember("project", "expert")
				if e != nil {
					return e
				}
				m.State = work.ProjectMemberRemoved
				m.Revision++
				return tx.PutProjectMember(m, 1)
			})
			raw := rawMessage("leader", "followup", "group", "group", "open", "user", "@_expert follow up")
			raw.ParentID = "root"
			raw.Mentions = []work.RawMention{{Key: "@_expert", Type: "bot", IDs: work.SenderIDs{OpenID: "bot:expert"}}}
			if multiple {
				raw.Mentions = append(raw.Mentions, work.RawMention{Key: "@_leader", Type: "bot", IDs: work.SenderIDs{OpenID: "bot:leader"}})
			}
			receive(t, ingress, raw)
			process(t, ingress)
			txRun(t, store, func(tx *workstore.Tx) error {
				inputs, e := tx.ListInbox("scope")
				if e != nil {
					return e
				}
				for _, v := range inputs {
					m, e := tx.GetMessage(v.MessageID)
					if e != nil {
						return e
					}
					if m.PlatformKey.MessageID == "followup" {
						if v.TaskID != "" || v.RelatedTaskID != "finished" || v.RecipientRoleID != "leader" {
							t.Fatalf("historical relation lost: %+v", v)
						}
						return nil
					}
				}
				t.Fatal("missing followup")
				return nil
			})
		})
	}
}
