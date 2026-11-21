package workservice

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (s *Service) notifyDependentTaskOwners(tx *workstore.Tx, child work.Task) error {
	dependencies, err := tx.ListPendingDependentTasks(child.ID, work.DependencyResultAvailable)
	if err != nil {
		return err
	}
	for _, dependency := range dependencies {
		parent, err := tx.GetTask(dependency.TaskID)
		if err != nil {
			return err
		}
		if err = s.notifyDependentTaskOwner(tx, parent, child); err != nil {
			return err
		}
	}
	return nil
}

// Availability asks the responsible role to check this result and explicitly
// settle the dependency. Its coordination input must run outside the blocked
// parent workstream. Review, integration, and human gates remain independent.
func (s *Service) notifyDependentTaskOwner(tx *workstore.Tx, parent, child work.Task) error {
	if parent.State == work.TaskDone || parent.State == work.TaskCancelled {
		return nil
	}
	recipient := parent.OwnerRoleID
	if err := ensureMember(tx, parent.ScopeID, recipient); errors.Is(err, workauth.ErrUnauthorized) {
		scope, err := tx.GetScope(parent.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind == work.ScopeProject {
			project, err := tx.GetProject(scope.ProjectID)
			if err != nil {
				return err
			}
			recipient = project.LeaderRoleID
		}
	} else if err != nil {
		return err
	}
	if err := ensureEnrolledMember(tx, parent.ScopeID, recipient); errors.Is(err, workauth.ErrUnauthorized) {
		// A project without an eligible coordinator is already blocked by membership.
		return nil
	} else if err != nil {
		return err
	}
	now := s.now()
	identity := []string{parent.ID, child.ID, strconv.FormatInt(child.RequirementsRevision, 10),
		strconv.FormatInt(child.AssignmentGeneration, 10), recipient}
	if child.AvailableResultRevision > 0 {
		identity = append(identity, strconv.FormatInt(child.AvailableResultRevision, 10))
	}
	id := stableID("dependency-result", identity...)
	if _, err := tx.GetMessage(id); err == nil {
		return nil
	} else if !errors.Is(err, workstore.ErrNotFound) {
		return err
	}
	text := fmt.Sprintf("子任务已报告结果可用，请用 task.inspect 核验子任务及当前版本，确认适用后用 task.dependency.satisfy 结算 result_available，再继续父任务。此通知未满足依赖。\n父任务：%s\n子任务：%s\n子任务报告时版本：%d", parent.ID, child.ID, child.Revision)
	body := work.NewContent(id+":body", parent.ScopeID, "text/plain; charset=utf-8", []byte(text), now)
	if err := tx.PutContent(body); err != nil {
		return err
	}
	source := work.SourceRef{Kind: "task", ID: child.ID, Version: strconv.FormatInt(child.Revision, 10), EvidenceRef: body.ID}
	message := work.Message{ID: id, ScopeID: parent.ScopeID, ConversationID: stableID("dependency", parent.ID, child.ID),
		Source: work.Actor{Kind: work.ActorSystem, ID: "dependency-result"}, BodyRef: body.ID, DerivedFrom: &source, Revision: 1, CreatedAt: now}
	input := work.Inbox{ID: id + ":inbox", ScopeID: parent.ScopeID, RelatedTaskID: parent.ID, RecipientRoleID: recipient,
		MessageID: message.ID, EventID: "message:" + message.ID, State: work.InboxPending, ContentState: work.InputContentReady, AvailableAt: now, Revision: 1}
	_, _, err := tx.AcceptMessage(message, []work.Inbox{input})
	return err
}

func (s *Service) queueUnblockedTask(tx *workstore.Tx, task work.Task) error {
	dependencies, err := tx.ListDependencies(task.ID)
	if err != nil {
		return err
	}
	for _, dependency := range dependencies {
		if dependency.SatisfiedBy == nil {
			return nil
		}
	}
	id := stableID("dependency-cleared", task.ID, strconv.FormatInt(task.Revision, 10))
	now := s.now()
	return tx.PutInbox(work.Inbox{ID: id, ScopeID: task.ScopeID, TaskID: task.ID,
		RecipientRoleID: task.OwnerRoleID, EventID: "dependency-cleared:" + id,
		State: work.InboxPending, ContentState: work.InputContentReady, AvailableAt: now, Revision: 1}, 0)
}
