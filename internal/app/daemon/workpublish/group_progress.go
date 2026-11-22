package workpublish

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Resolve the exact previously confirmed progress card under the same frozen
// identity and stream. This is an operation-owned display; model input never
// supplies a platform message ID to patch.
func (p *Publisher) resolveGroupProgress(tx *workstore.Tx, scope work.WorkScope, op work.Operation, job *publication, recovery bool) error {
	if job.Projection.Purpose != "progress" || (scope.Kind != work.ScopeProject && scope.Kind != work.ScopeBootstrap) || job.Projection.UIViewID != "" {
		return nil
	}
	for _, step := range op.Steps {
		if step.ID == "send_projection" {
			// resolveTopic will restore the immutable payload. It must not choose a
			// newer card for a retry, especially after an unknown PATCH.
			return nil
		}
	}
	if recovery {
		return fmt.Errorf("progress recovery has no frozen payload")
	}
	_, stream, _, err := scheduleIdentity(tx, scope, op, job.Projection)
	if err != nil {
		return err
	}
	records, err := tx.ListRoleProgressPublications(scope.ID, job.Projection.RoleID, job.Projection.Tenant, job.Projection.ChatID, job.Projection.TaskID, job.Projection.RootMessageID)
	if err != nil {
		return err
	}
	var previous work.Operation
	var previousProjection workservice.MessageProjection
	var messageID string
	var previousState work.OutboxState
	for _, record := range records {
		item, candidate, body := record.Outbox, record.Operation, record.Content
		if item.OperationID == op.ID || (item.State != work.OutboxSent && item.State != work.OutboxSending && item.State != work.OutboxUnknown) {
			continue
		}

		var projection workservice.MessageProjection
		if err = json.Unmarshal(body.Data, &projection); err != nil {
			return err
		}
		if projection.Purpose != "progress" && projection.Purpose != "final" {
			continue
		}
		_, candidateStream, _, err := scheduleIdentity(tx, scope, candidate, projection)
		if err != nil {
			return err
		}
		if stream != candidateStream || projection.BotGeneration != job.Projection.BotGeneration || projection.AppID != job.Projection.AppID || projection.GatewayRef != job.Projection.GatewayRef || projection.ChannelGeneration != job.Projection.ChannelGeneration {
			continue
		}
		if previous.ID == "" || candidate.CreatedAt.After(previous.CreatedAt) || candidate.CreatedAt.Equal(previous.CreatedAt) && candidate.ID > previous.ID {
			previous, previousProjection, messageID, previousState = candidate, projection, item.ExternalMessageID, item.State
		}
	}
	if previousState == work.OutboxSending && p.now().Sub(previous.UpdatedAt) < 2*time.Minute {
		return workstore.ErrConflict
	}
	if previousState == work.OutboxUnknown || previousState == work.OutboxSending {
		// A changed snapshot starts a new segment. The old uncertain write
		// remains available for observation and is never replayed over it.
		if p.now().Sub(previous.UpdatedAt) < groupSnapshotInterval {
			return workstore.ErrConflict
		}
		messageID = ""
	}
	if previousState == work.OutboxSent && previousProjection.Purpose == "progress" && previousProjection.Body == job.Projection.Body {
		job.TerminalReason = "progress snapshot is unchanged"
		return nil
	}
	// A completed result closes the preceding progress segment. A later task
	// update starts a fresh card; never rewrite a delivered final answer.
	if previous.ID != "" && previousProjection.Purpose == "progress" && messageID != "" && p.now().Sub(previous.CreatedAt) < 13*24*time.Hour {
		// Look up the original creation timestamp, not the newest PATCH timestamp.
		oldest := previous.CreatedAt
		for _, record := range records {
			if record.Outbox.State == work.OutboxSent && record.Outbox.ExternalMessageID == messageID && record.Operation.CreatedAt.Before(oldest) {
				oldest = record.Operation.CreatedAt
			}
		}
		if p.now().Sub(oldest) < 13*24*time.Hour {
			job.Projection.ReplaceMessageID = messageID
		}
	}
	card, err := workservice.GroupProgressCard(job.Projection.Body)
	if err != nil {
		return err
	}
	job.Projection.Card = card
	return nil
}
