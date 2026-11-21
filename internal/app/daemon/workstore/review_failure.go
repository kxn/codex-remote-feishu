package workstore

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"time"
)

type ReviewFailureReason string

const (
	ReviewFailed             ReviewFailureReason = "failed"
	ReviewInterrupted        ReviewFailureReason = "interrupted"
	ReviewMissingFinalResult ReviewFailureReason = "missing_final_result"
)
const SchemaReviewFailuresV1 = `CREATE TABLE review_failure_receipts(attempt_id TEXT PRIMARY KEY REFERENCES attempts(id),reason TEXT NOT NULL,source TEXT NOT NULL CHECK(json_valid(source)));`

func (t *Tx) CompleteReviewFailure(attemptID string, terminal work.SourceRef, reason ReviewFailureReason) error {
	return t.atomic(func() error {
		attempt, err := t.GetAttempt(attemptID)
		if err != nil {
			return err
		}
		wc, err := t.GetContext(attempt.ContextID)
		if err != nil {
			return err
		}
		if wc.Purpose != work.PurposeReview || wc.Review == nil || attempt.TaskID != "" {
			return ErrScopeDenied
		}
		event, err := t.reviewEvent(attempt, terminal)
		if err != nil {
			return err
		}
		if event.Kind != agentproto.EventTurnCompleted || event.TurnCompletionOrigin != agentproto.TurnCompletionOriginRuntime {
			return ErrScopeDenied
		}
		switch reason {
		case ReviewFailed:
			if event.Status != "failed" || attempt.State != work.AttemptFailed {
				return ErrScopeDenied
			}
		case ReviewInterrupted:
			if event.Status != "interrupted" || attempt.State != work.AttemptInterrupted {
				return ErrScopeDenied
			}
		case ReviewMissingFinalResult:
			if event.Status != "completed" || attempt.State != work.AttemptFailed {
				return ErrScopeDenied
			}
		default:
			return fmt.Errorf("invalid review failure reason")
		}
		if _, err = t.GetReviewResult(attemptID); err == nil {
			return ErrConflict
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		var count int
		if err = t.tx.QueryRowContext(t.ctx, "SELECT count(*) FROM quiescence_receipts WHERE attempt_id=?", attemptID).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return ErrConflict
		}
		occupied, err := t.WorkerHasOccupancy(attempt.RunRef.WorkerID)
		if err != nil {
			return err
		}
		if occupied {
			return ErrConflict
		}
		var oldReason, oldSource string
		e := t.tx.QueryRowContext(t.ctx, "SELECT reason,source FROM review_failure_receipts WHERE attempt_id=?", attemptID).Scan(&oldReason, &oldSource)
		if e == nil {
			if oldReason != string(reason) || oldSource != mustJSON(terminal) {
				return ErrIdempotencyConflict
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		source, err := t.GetContext(wc.Review.SourceContextID)
		if err != nil {
			return err
		}
		if source.ScopeID != wc.ScopeID || source.RoleID != wc.RoleID {
			return ErrScopeDenied
		}
		now := time.Now().UTC()
		id := "review-failure:" + attemptID
		text := "这次只读审查没有完成。原工作没有被修改，也没有生成验收结论；可以重新使用 /review 发起审查。"
		if reason == ReviewMissingFinalResult {
			text = "这次只读审查已停止，但没有收到完整评审正文。没有生成验收结论；可以重新使用 /review 重试。"
		}
		body := work.NewContent(id+":body", wc.ScopeID, "text/plain", []byte(text), now)
		if err = t.PutContent(body); err != nil {
			return err
		}
		message := work.Message{ID: id, ScopeID: wc.ScopeID, ConversationID: source.ID, Source: work.Actor{Kind: work.ActorSystem, ID: "review-failure"}, DerivedFrom: &terminal, BodyRef: body.ID, Revision: 1, CreatedAt: now}
		input := work.Inbox{ID: id + ":input", ScopeID: wc.ScopeID, RecipientRoleID: source.RoleID, RecipientContextID: source.ID, EventID: id, MessageID: message.ID, State: work.InboxPending, Revision: 1, AvailableAt: now}
		if _, _, err = t.AcceptMessage(message, []work.Inbox{input}); err != nil {
			return err
		}
		_, err = t.tx.ExecContext(t.ctx, "INSERT INTO review_failure_receipts(attempt_id,reason,source) VALUES(?,?,?)", attemptID, reason, mustJSON(terminal))
		return err
	})
}
