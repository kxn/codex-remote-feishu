package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"reflect"
)

func (t *Tx) GetTaskConversation(scopeID, taskID string) (work.TaskConversation, error) {
	var v work.TaskConversation
	var data string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM task_conversations WHERE scope_id=? AND task_id=?", scopeID, taskID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(data), &v)
	return v, err
}
func (t *Tx) PutTaskConversation(v work.TaskConversation, expected int64) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if expected < 0 || v.Revision != expected+1 {
		return ErrConflict
	}
	task, err := t.GetTask(v.TaskID)
	if err != nil {
		return err
	}
	if task.ScopeID != v.ScopeID {
		return ErrScopeDenied
	}
	scope, err := t.GetScope(v.ScopeID)
	if err != nil {
		return err
	}
	if scope.Kind == work.ScopeBootstrap {
		return ErrScopeDenied
	}
	if scope.Kind == work.ScopeProject {
		binding, e := t.GetProjectChannelBinding(v.Tenant, v.ChatID)
		if e != nil {
			return e
		}
		if !binding.Active || binding.ProjectID != scope.ProjectID || binding.BindingGeneration != v.ChannelGeneration {
			return fmt.Errorf("task conversation channel binding mismatch")
		}
	}
	if v.InheritedFromTaskID != "" {
		parent, e := t.GetTaskConversation(v.ScopeID, v.InheritedFromTaskID)
		if e != nil {
			return e
		}
		if parent.Tenant != v.Tenant || parent.ChatID != v.ChatID || parent.RootMessageID != v.RootMessageID {
			return fmt.Errorf("inherited conversation root mismatch")
		}
		dependencies, e := t.ListDependencies(v.InheritedFromTaskID)
		if e != nil {
			return e
		}
		linked := false
		for _, dependency := range dependencies {
			if dependency.DependsOnTaskID == v.TaskID {
				linked = true
			}
		}
		if !linked {
			return fmt.Errorf("conversation inheritance requires actual parent dependency")
		}
	}
	var result sql.Result
	if expected == 0 {
		result, err = t.tx.ExecContext(t.ctx, "INSERT INTO task_conversations(scope_id,task_id,revision,payload) VALUES(?,?,?,?) ON CONFLICT DO NOTHING", v.ScopeID, v.TaskID, v.Revision, mustJSON(v))
	} else {
		old, e := t.GetTaskConversation(v.ScopeID, v.TaskID)
		if e != nil {
			return e
		}
		if old.Tenant != v.Tenant || old.ChatID != v.ChatID || old.RootMessageID != v.RootMessageID || old.InheritedFromTaskID != v.InheritedFromTaskID {
			return fmt.Errorf("task business conversation is immutable")
		}
		result, err = t.tx.ExecContext(t.ctx, "UPDATE task_conversations SET revision=?,payload=? WHERE scope_id=? AND task_id=? AND revision=?", v.Revision, mustJSON(v), v.ScopeID, v.TaskID, expected)
	}
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrConflict
	}
	return nil
}
func (t *Tx) InheritTaskConversation(scopeID, parentTaskID, childTaskID string) error {
	parent, err := t.GetTaskConversation(scopeID, parentTaskID)
	if err != nil {
		return err
	}
	parent.TaskID = childTaskID
	parent.InheritedFromTaskID = parentTaskID
	parent.Revision = 1
	return t.PutTaskConversation(parent, 0)
}
func (t *Tx) GetPlatformMessageMapping(tenant, chatID, messageID string) (work.PlatformMessageMapping, error) {
	var v work.PlatformMessageMapping
	var data string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM platform_message_mappings WHERE tenant=? AND chat_id=? AND message_id=?", tenant, chatID, messageID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(data), &v)
	return v, err
}
func (t *Tx) PutPlatformMessageMapping(v work.PlatformMessageMapping) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if _, err := t.GetScope(v.ScopeID); err != nil {
		return err
	}
	if v.TaskID != "" {
		task, err := t.GetTask(v.TaskID)
		if err != nil {
			return err
		}
		if task.ScopeID != v.ScopeID {
			return ErrScopeDenied
		}
	}
	if v.IngressID != "" {
		raw, err := t.GetIngress(v.IngressID)
		if err != nil {
			return err
		}
		if raw.Envelope.Tenant != v.Tenant || raw.Envelope.ChatID != v.ChatID || raw.Envelope.MessageID != v.MessageID {
			return fmt.Errorf("mapping does not match actual SDK message")
		}
	} else {
		outbox, err := t.GetOutbox(v.OutboxID)
		if err != nil {
			return err
		}
		operation, err := t.GetOperation(v.OperationID)
		if err != nil {
			return err
		}
		if err = t.validatePublicationMappingActor(operation, v); err != nil {
			return err
		}
		if outbox.State != work.OutboxSent || outbox.ExternalMessageID != v.MessageID || outbox.ScopeID != v.ScopeID || outbox.Target != v.ChatID || outbox.OperationID != operation.ID || operation.ScopeID != v.ScopeID || operation.State != work.OperationConfirmed || operation.TargetGeneration != v.BotGeneration {
			return fmt.Errorf("mapping lacks matching real publication receipt")
		}
	}
	old, err := t.GetPlatformMessageMapping(v.Tenant, v.ChatID, v.MessageID)
	if err == nil {
		if !reflect.DeepEqual(old, v) {
			return ErrIdempotencyConflict
		}
		return nil
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, "INSERT INTO platform_message_mappings(tenant,chat_id,message_id,payload) VALUES(?,?,?,?)", v.Tenant, v.ChatID, v.MessageID, mustJSON(v))
	return err
}

func (t *Tx) GetTaskTopicReservation(scopeID, taskID string) (string, error) {
	var operationID string
	err := t.tx.QueryRowContext(t.ctx, "SELECT operation_id FROM task_topic_reservations WHERE scope_id=? AND task_id=?", scopeID, taskID).Scan(&operationID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return operationID, err
}
func (t *Tx) TryReserveTaskTopic(scopeID, taskID, operationID string) error {
	task, err := t.GetTask(taskID)
	if err != nil {
		return err
	}
	if task.ScopeID != scopeID {
		return ErrScopeDenied
	}
	operation, err := t.GetOperation(operationID)
	if err != nil {
		return err
	}
	if operation.ScopeID != scopeID || operation.Kind != "feishu.publish" || operation.State == work.OperationTerminal {
		return fmt.Errorf("topic reservation requires same-scope live publication")
	}
	if _, err = t.GetTaskConversation(scopeID, taskID); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	prior, err := t.GetTaskTopicReservation(scopeID, taskID)
	if err == nil {
		if prior == operationID {
			return nil
		}
		old, e := t.GetOperation(prior)
		if e != nil {
			return e
		}
		if old.State != work.OperationTerminal {
			return ErrConflict
		}
		_, e = t.tx.ExecContext(t.ctx, "UPDATE task_topic_reservations SET operation_id=? WHERE scope_id=? AND task_id=? AND operation_id=?", operationID, scopeID, taskID, prior)
		return e
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, "INSERT INTO task_topic_reservations(scope_id,task_id,operation_id) VALUES(?,?,?)", scopeID, taskID, operationID)
	return err
}

var ErrAmbiguousConversation = errors.New("business conversation has multiple independent root tasks")

func (t *Tx) FindTaskConversationByRoot(scopeID, tenant, chatID, rootMessageID string) (work.TaskConversation, error) {
	rows, err := t.tx.QueryContext(t.ctx, "SELECT payload FROM task_conversations WHERE scope_id=? AND json_extract(payload,'$.tenant')=? AND json_extract(payload,'$.chat_id')=? AND json_extract(payload,'$.root_message_id')=? AND coalesce(json_extract(payload,'$.inherited_from_task_id'),'')='' ORDER BY task_id", scopeID, tenant, chatID, rootMessageID)
	if err != nil {
		return work.TaskConversation{}, err
	}
	defer rows.Close()
	var result work.TaskConversation
	count := 0
	for rows.Next() {
		var data string
		if err = rows.Scan(&data); err != nil {
			return result, err
		}
		if err = json.Unmarshal([]byte(data), &result); err != nil {
			return result, err
		}
		count++
		if count > 1 {
			return work.TaskConversation{}, ErrAmbiguousConversation
		}
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if count == 0 {
		return result, ErrNotFound
	}
	return result, nil
}
