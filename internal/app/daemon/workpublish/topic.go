package workpublish

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// A queued publication identifies its business task before that task has an
// external topic root. Reserve the first creation and freeze the exact resolved
// platform payload before sending. Recovery always reuses that frozen payload.
func (p *Publisher) resolveTopic(tx *workstore.Tx, op *work.Operation, job *publication) error {
	for _, step := range op.Steps {
		if step.ID != "send_projection" {
			continue
		}
		body, err := tx.GetContent(op.ScopeID, step.Receipt)
		if err != nil {
			return err
		}
		var frozen workservice.MessageProjection
		if err = json.Unmarshal(body.Data, &frozen); err != nil {
			return err
		}
		original := job.Projection
		if frozen.RoleID != original.RoleID || frozen.BotGeneration != original.BotGeneration || frozen.AppID != original.AppID || frozen.GatewayRef != original.GatewayRef || frozen.Tenant != original.Tenant || frozen.ChatID != original.ChatID || frozen.ChannelGeneration != original.ChannelGeneration || frozen.TaskID != original.TaskID || frozen.Body != original.Body {
			return fmt.Errorf("resolved publication escaped its original intent")
		}
		job.Projection = frozen
		return nil
	}
	if job.Projection.TaskID != "" {
		if err := tx.TryReserveTaskTopic(op.ScopeID, job.Projection.TaskID, op.ID); err != nil {
			return err
		}
		conversation, err := tx.GetTaskConversation(op.ScopeID, job.Projection.TaskID)
		if err == nil {
			if conversation.Tenant != job.Projection.Tenant || conversation.ChatID != job.Projection.ChatID || conversation.ChannelGeneration != job.Projection.ChannelGeneration {
				return fmt.Errorf("task topic belongs to a different channel generation")
			}
			if job.Projection.RootMessageID != "" && job.Projection.RootMessageID != conversation.RootMessageID {
				return fmt.Errorf("publication root differs from existing task topic")
			}
			job.Projection.RootMessageID = conversation.RootMessageID
			if job.Projection.ReplyToMessageID == "" {
				job.Projection.ReplyToMessageID = conversation.RootMessageID
			}
		} else if !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
	}
	raw, err := json.Marshal(job.Projection)
	if err != nil {
		return err
	}
	body := work.NewContent(op.ID+"_send_projection", op.ScopeID, "application/json", raw, p.now())
	if err = tx.PutContent(body); err != nil {
		return err
	}
	op.Steps = append(op.Steps, work.OperationStep{ID: "send_projection", State: work.OperationConfirmed, Receipt: body.ID})
	return nil
}
