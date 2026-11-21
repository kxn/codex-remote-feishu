package workstore

import (
	"database/sql"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// PendingReviewPresentations includes only outcomes whose dedicated runtime
// transaction already proved quiescence. The UI is independent of whether the
// source conversation can currently run another model turn.
func (t *Tx) PendingReviewPresentations(limit int) ([]string, error) {
	if limit < 1 || limit > 100 {
		limit = 32
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT attempt_id FROM (SELECT attempt_id FROM review_results WHERE json_extract(payload,'$.delivered')=1 UNION SELECT attempt_id FROM review_failure_receipts) r WHERE NOT EXISTS(SELECT 1 FROM team_ui_views v WHERE v.id='review-presentation:'||r.attempt_id) AND NOT EXISTS(SELECT 1 FROM operations o WHERE o.id='review-presentation-unavailable:'||r.attempt_id) ORDER BY attempt_id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// A changed Bot identity cannot silently inherit a historical return address.
// Preserve that presentation failure without starving newer ready results.
func (t *Tx) MarkReviewPresentationUnavailable(attemptID, reason string) error {
	attempt, err := t.GetAttempt(attemptID)
	if err != nil {
		return err
	}
	wc, err := t.GetContext(attempt.ContextID)
	if err != nil {
		return err
	}
	if wc.Purpose != work.PurposeReview {
		return ErrScopeDenied
	}
	id := "review-presentation-unavailable:" + attemptID
	body := work.NewContent(id+":reason", attempt.ScopeID, "text/plain", []byte(reason), attempt.UpdatedAt)
	if err = t.PutContent(body); err != nil {
		return err
	}
	_, _, err = t.PutOperation(work.Operation{ID: id, ScopeID: attempt.ScopeID, Actor: work.Actor{Kind: work.ActorSystem, ID: "review-presenter"}, Kind: "review.presentation.unavailable", TargetID: attemptID, TargetGeneration: 1, IdempotencyKey: id, RequestHash: body.SHA256, RequestRef: body.ID, State: work.OperationTerminal, LastError: reason, Revision: 1}, 0)
	return err
}
func (t *Tx) LatestReviewPresentation(contextID string) (string, error) {
	var id string
	err := t.tx.QueryRowContext(t.ctx, `SELECT a.id FROM attempts a WHERE json_extract(a.payload,'$.context_id')=? AND (EXISTS(SELECT 1 FROM review_results r WHERE r.attempt_id=a.id AND json_extract(r.payload,'$.delivered')=1) OR EXISTS(SELECT 1 FROM review_failure_receipts f WHERE f.attempt_id=a.id)) ORDER BY a.rowid DESC LIMIT 1`, contextID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}
