package workstore

import (
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// ReanchorRecalledConversation is the only root-changing write path. It needs
// an authenticated recall and a separately committed human reconciliation
// intent. All tasks sharing that actual topic move atomically; prior snapshots
// stay immutable in the audit operation's content.
func (t *Tx) ReanchorRecalledConversation(scopeID, taskID, auditID string, expected int64) error {
	audit, err := t.GetOperation(auditID)
	if err != nil {
		return err
	}
	old, err := t.GetTaskConversation(scopeID, taskID)
	if err != nil {
		return err
	}
	if old.Revision != expected {
		return ErrConflict
	}
	if audit.Kind != "task.topic.reanchor" || audit.ScopeID != scopeID || audit.TargetID != taskID || audit.State != work.OperationConfirmed || audit.Actor.Kind != work.ActorPrincipal || audit.ExternalReceipt == "" {
		return ErrScopeDenied
	}
	recalled, err := t.TaskTopicUnavailable(old)
	if err != nil {
		return err
	}
	if !recalled {
		return fmt.Errorf("topic root has no authenticated recall or exact deletion observation")
	}
	interaction, err := t.GetHumanInteraction(audit.IdempotencyKey)
	if err != nil {
		return err
	}
	if interaction.PrincipalID != audit.Actor.ID || interaction.Envelope.Tenant != old.Tenant || interaction.Envelope.ChatID != old.ChatID || interaction.Envelope.MessageID != audit.ExternalReceipt || interaction.Envelope.Values["action"] != "publication.reanchor" {
		return ErrScopeDenied
	}
	rows, err := t.tx.QueryContext(t.ctx, "SELECT payload FROM task_conversations WHERE scope_id=?", scopeID)
	if err != nil {
		return err
	}
	var previous []work.TaskConversation
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		var v work.TaskConversation
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			rows.Close()
			return err
		}
		if v.Tenant == old.Tenant && v.ChatID == old.ChatID && v.RootMessageID == old.RootMessageID {
			previous = append(previous, v)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	body, err := json.Marshal(previous)
	if err != nil {
		return err
	}
	content := work.NewContent(auditID+":previous-conversations", scopeID, "application/json", body, audit.CreatedAt)
	if err = t.PutContent(content); err != nil {
		return err
	}
	for _, v := range previous {
		rev := v.Revision
		v.Revision++
		v.RootMessageID = audit.ExternalReceipt
		result, e := t.tx.ExecContext(t.ctx, "UPDATE task_conversations SET revision=?,payload=? WHERE scope_id=? AND task_id=? AND revision=?", v.Revision, mustJSON(v), scopeID, v.TaskID, rev)
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return ErrConflict
		}
	}
	return nil
}
