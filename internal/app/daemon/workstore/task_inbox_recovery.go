package workstore

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Only the two known legacy producers may change a persisted pending target.
// Claimed and terminal inputs retain their original runtime evidence.
func (t *Tx) repairLegacyTaskInboxBindings() error {
	adoptions, err := listRecords[PrivateInstructionAdoption](t, "private_instruction_adoptions", ` WHERE EXISTS (
		SELECT 1 FROM inbox AS i JOIN contexts AS c ON c.id=i.context_id
		WHERE i.id=json_extract(private_instruction_adoptions.payload,'$.inbox_id')
		  AND i.state='pending' AND json_extract(c.payload,'$.task_id') IS NULL)`)
	if err != nil {
		return err
	}
	for _, adoption := range adoptions {
		if err = t.repairPrivateAdoptionTarget(adoption); err != nil {
			return err
		}
	}
	notices, err := listRecords[work.Inbox](t, "inbox", ` WHERE state='pending' AND task_id IS NULL AND event_id GLOB 'memory-correction:*'
		AND EXISTS (SELECT 1 FROM contexts AS c WHERE c.id=inbox.context_id AND json_extract(c.payload,'$.task_id') IS NOT NULL)
		AND EXISTS (SELECT 1 FROM messages AS m WHERE m.id=inbox.message_id
		  AND json_extract(m.payload,'$.source.kind')='system' AND json_extract(m.payload,'$.source.id')='memory-correction')`)
	if err != nil {
		return err
	}
	for _, notice := range notices {
		context, err := t.GetContext(notice.RecipientContextID)
		if err != nil {
			return err
		}
		task, err := t.GetTask(context.TaskID)
		if err != nil {
			return err
		}
		if task.ScopeID != notice.ScopeID || task.OwnerRoleID != notice.RecipientRoleID || task.State == work.TaskDone || task.State == work.TaskCancelled {
			continue
		}
		notice.TaskID = task.ID
		notice.Revision++
		if err = t.record("inbox", notice.ID, notice.Revision, notice.Revision-1, notice); err != nil {
			return err
		}
	}
	return nil
}

func (t *Tx) repairPrivateAdoptionTarget(adoption PrivateInstructionAdoption) error {
	input, err := t.GetInbox(adoption.InboxID)
	if err != nil {
		return err
	}
	if input.ScopeID != adoption.ProjectScopeID || input.TaskID != adoption.TaskID || input.MessageID != adoption.MessageID || input.RecipientContextID != adoption.ProjectContextID || input.EventID != adoption.ID || !strings.HasPrefix(adoption.ID, "private-adoption:") {
		return nil
	}
	task, err := t.GetTask(adoption.TaskID)
	if err != nil {
		return err
	}
	if task.ScopeID != input.ScopeID || task.OwnerRoleID != input.RecipientRoleID || task.State == work.TaskDone || task.State == work.TaskCancelled {
		return nil
	}
	candidates, err := listRecords[work.WorkContext](t, "contexts", " WHERE scope_id=? AND role_id=? AND workstream=?", task.ScopeID, task.OwnerRoleID, "task:"+task.ID)
	if err != nil {
		return err
	}
	context := privateAdoptionTaskContext(adoption.ID+":context", task, task.CreatedAt)
	if len(candidates) == 0 {
		if err = t.PutContext(context, 0); err != nil {
			return err
		}
	} else {
		context = candidates[0]
		if context.TaskID != task.ID {
			return ErrScopeDenied
		}
	}
	input.RecipientContextID = context.ID
	input.Revision++
	if err = t.record("inbox", input.ID, input.Revision, input.Revision-1, input); err != nil {
		return err
	}
	adoption.ProjectContextID = context.ID
	_, err = t.tx.ExecContext(t.ctx, "UPDATE private_instruction_adoptions SET payload=? WHERE id=?", mustJSON(adoption), adoption.ID)
	return err
}
