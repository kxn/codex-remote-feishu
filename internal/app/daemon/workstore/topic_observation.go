package workstore

import (
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type TopicReadRequest struct {
	Conversation work.TaskConversation `json:"conversation"`
	Binding      work.RoleBotBinding   `json:"binding"`
}
type TopicReadResult struct {
	MessageID string `json:"message_id"`
	ChatID    string `json:"chat_id"`
	Deleted   bool   `json:"deleted"`
}

// TaskTopicUnavailable accepts an actual SDK recall or a confirmed, exact
// same-Bot GetMessage result. Permission errors and missing responses are not
// proof that a message was deleted.
func (t *Tx) TaskTopicUnavailable(conversation work.TaskConversation) (bool, error) {
	if yes, err := t.IsPlatformMessageRecalled(conversation.Tenant, conversation.ChatID, conversation.RootMessageID); yes || err != nil {
		return yes, err
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT payload FROM operations WHERE scope_id=? AND json_extract(payload,'$.kind')='platform.topic.read' AND json_extract(payload,'$.target_id')=? AND json_extract(payload,'$.state')='confirmed'`, conversation.ScopeID, conversation.RootMessageID)
	if err != nil {
		return false, err
	}
	var observations []work.Operation
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return false, err
		}
		var op work.Operation
		if err = json.Unmarshal([]byte(raw), &op); err != nil {
			rows.Close()
			return false, err
		}
		observations = append(observations, op)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	for _, op := range observations {
		request, err := t.GetContent(op.ScopeID, op.RequestRef)
		if err != nil {
			return false, err
		}
		if request.SHA256 != op.RequestHash {
			return false, fmt.Errorf("topic read request hash mismatch")
		}
		var req TopicReadRequest
		if err = json.Unmarshal(request.Data, &req); err != nil {
			return false, err
		}
		if req.Conversation.Tenant != conversation.Tenant || req.Conversation.ChatID != conversation.ChatID || req.Conversation.ChannelGeneration != conversation.ChannelGeneration || req.Conversation.RootMessageID != conversation.RootMessageID || op.Actor != (work.Actor{Kind: work.ActorRole, ID: req.Binding.RoleID}) || op.TargetGeneration != req.Binding.IdentityGeneration {
			continue
		}
		result, err := t.GetContent(op.ScopeID, op.ExternalReceipt)
		if err != nil {
			return false, err
		}
		var observed TopicReadResult
		if err = json.Unmarshal(result.Data, &observed); err != nil {
			return false, err
		}
		if observed.Deleted && observed.MessageID == conversation.RootMessageID && observed.ChatID == conversation.ChatID {
			return true, nil
		}
	}
	return false, nil
}

func (t *Tx) LastTopicObservation(scopeID, root string) (string, error) {
	var value string
	err := t.tx.QueryRowContext(t.ctx, `SELECT coalesce(max(json_extract(payload,'$.created_at')),'') FROM operations WHERE scope_id=? AND json_extract(payload,'$.kind')='platform.topic.read' AND json_extract(payload,'$.target_id')=?`, scopeID, root).Scan(&value)
	return value, err
}
