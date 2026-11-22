package workpublish

import (
	"encoding/json"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// A final closes its producing attempt's progress. A new authenticated attempt
// may start another segment; an old asynchronous update cannot resurrect one.
func (p *Publisher) progressObsolete(tx *workstore.Tx, scope work.WorkScope, op work.Operation, projection workservice.MessageProjection) (bool, error) {
	if projection.Purpose != "progress" || (scope.Kind != work.ScopeProject && scope.Kind != work.ScopeBootstrap) {
		return false, nil
	}
	_, stream, _, err := scheduleIdentity(tx, scope, op, projection)
	if err != nil {
		return false, err
	}
	records, err := tx.ListRoleProgressPublications(scope.ID, projection.RoleID, projection.Tenant, projection.ChatID, projection.TaskID, projection.RootMessageID)
	if err != nil {
		return false, err
	}
	for _, record := range records {
		item, candidate, body := record.Outbox, record.Operation, record.Content
		if item.OperationID == op.ID || item.State == work.OutboxTerminal {
			continue
		}

		var next workservice.MessageProjection
		if err = json.Unmarshal(body.Data, &next); err != nil {
			return false, err
		}
		if next.Purpose != "progress" && next.Purpose != "final" {
			continue
		}
		_, other, _, err := scheduleIdentity(tx, scope, candidate, next)
		if err != nil {
			return false, err
		}
		if other != stream {
			continue
		}
		sameAttempt := projection.Authority != nil && next.Authority != nil && projection.Authority.AttemptID == next.Authority.AttemptID
		newer := candidate.CreatedAt.After(op.CreatedAt) || candidate.CreatedAt.Equal(op.CreatedAt) && candidate.ID > op.ID
		if !newer && !(next.Purpose == "final" && sameAttempt) {
			continue
		}
		if err = workservice.ValidatePublication(tx, candidate, next); err != nil {
			if errors.Is(err, workservice.ErrPublicationDenied) {
				continue
			}
			return false, err
		}
		if next.Purpose == "final" && (newer || sameAttempt) {
			return true, nil
		}
		if next.Purpose == "progress" && newer {
			return true, nil
		}
	}
	return false, nil
}
