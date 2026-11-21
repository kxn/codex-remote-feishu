package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const SchemaReviewResultsV1 = `CREATE TABLE IF NOT EXISTS review_results(attempt_id TEXT PRIMARY KEY REFERENCES attempts(id),payload TEXT NOT NULL CHECK(json_valid(payload)));`

func (t *Tx) GetReviewResult(attemptID string) (work.ReviewResult, error) {
	var v work.ReviewResult
	var raw string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM review_results WHERE attempt_id=?", attemptID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v)
	return v, err
}

// RecordReviewResult is used only by the trusted runtime receipt transaction.
// Its text must exactly match committed final native message events.
func (t *Tx) RecordReviewResult(attemptID string, terminal work.SourceRef, finalText string, textSources []work.SourceRef) (result work.ReviewResult, created bool, err error) {
	err = t.atomic(func() error {
		attempt, e := t.GetAttempt(attemptID)
		if e != nil {
			return e
		}
		wc, e := t.GetContext(attempt.ContextID)
		if e != nil {
			return e
		}
		if wc.Purpose != work.PurposeReview || wc.Review == nil || attempt.TaskID != "" {
			return ErrScopeDenied
		}
		event, e := t.reviewEvent(attempt, terminal)
		if e != nil {
			return e
		}
		if event.Kind != agentproto.EventTurnCompleted || event.Status != "completed" || event.TurnCompletionOrigin != agentproto.TurnCompletionOriginRuntime {
			return fmt.Errorf("review requires a real completed native turn")
		}
		var text strings.Builder
		var previous uint64
		if len(textSources) == 0 {
			return fmt.Errorf("review needs exact final-message evidence")
		}
		for _, source := range textSources {
			part, e := t.reviewEvent(attempt, source)
			if e != nil {
				return e
			}
			if part.Kind != agentproto.EventItemCompleted || (part.ItemKind != "agent_message" && part.ItemKind != "agentMessage" && part.ItemKind != "assistantMessage" && part.ItemKind != "assistant_message") || part.ItemID == "" || part.ThreadID != event.ThreadID || part.TurnID != event.TurnID || part.Journal.Sequence <= previous || part.Journal.Sequence > event.Journal.Sequence {
				return fmt.Errorf("review text is not an ordered final native message")
			}
			previous = part.Journal.Sequence
			original, ok := part.Metadata["text"].(string)
			if !ok {
				return fmt.Errorf("native final message has no original text")
			}
			text.WriteString(original)
		}
		if text.String() != finalText || strings.TrimSpace(finalText) == "" {
			return fmt.Errorf("review body differs from original native final text")
		}
		old, e := t.GetReviewResult(attemptID)
		if e == nil {
			body, e := t.GetContent(attempt.ScopeID, old.BodyRef)
			if e != nil {
				return e
			}
			if string(body.Data) != finalText || !reflect.DeepEqual(old.Terminal, terminal) || !reflect.DeepEqual(old.TextSources, textSources) {
				return ErrIdempotencyConflict
			}
			result = old
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		now := time.Now().UTC()
		body := work.NewContent("review-result:"+attempt.ID, attempt.ScopeID, "text/markdown", []byte(finalText), now)
		if e = t.PutContent(body); e != nil {
			return e
		}
		result = work.ReviewResult{AttemptID: attempt.ID, ContextID: wc.ID, ScopeID: wc.ScopeID, RunRef: attempt.RunRef, Spec: *wc.Review, BodyRef: body.ID, Terminal: terminal, TextSources: textSources, Revision: 1, CreatedAt: now}
		if e = t.PutProcessingReport(ProcessingReport{AttemptID: attempt.ID, RunRef: attempt.RunRef, ProcessedInputIDs: attempt.InputIDs, OutcomeRef: body.ID, Checkpoint: wc.Checkpoint, Revision: 1}, 0); e != nil {
			return e
		}
		raw, e := json.Marshal(result)
		if e != nil {
			return e
		}
		_, e = t.tx.ExecContext(t.ctx, "INSERT INTO review_results(attempt_id,payload) VALUES(?,?)", attempt.ID, string(raw))
		created = e == nil
		return e
	})
	return
}

func (t *Tx) reviewEvent(attempt work.ExecutionAttempt, source work.SourceRef) (agentproto.Event, error) {
	var event agentproto.Event
	if source.Validate() != nil {
		return event, ErrScopeDenied
	}
	body, err := t.GetContent(attempt.ScopeID, source.EvidenceRef)
	if err != nil {
		return event, err
	}
	if err = json.Unmarshal(body.Data, &event); err != nil {
		return event, err
	}
	if event.Journal == nil || source.Kind != string(event.Kind) || source.Version != event.Journal.PayloadHash {
		return event, ErrScopeDenied
	}
	persisted, err := t.GetRuntimeEvent(event.Journal.CarrierIncarnation, event.Journal.Sequence)
	if err != nil {
		return event, err
	}
	if !reflect.DeepEqual(persisted, event) {
		return event, ErrScopeDenied
	}
	turn, err := t.GetNativeTurn(event.Journal.CarrierIncarnation, event.ThreadID, event.TurnID)
	if err != nil {
		return event, err
	}
	if turn.Binding.AttemptID != attempt.ID || turn.Binding.RunRef != attempt.RunRef.RunID || turn.Binding.WorkerSessionID != attempt.RunRef.WorkerID || turn.Binding.SessionGeneration != attempt.RunRef.Generation {
		return event, ErrScopeDenied
	}
	return event, nil
}

// CompleteReview delivers the result once, after the normal runtime outcome
// and independently proven quiescence have released this review's resources.
func (t *Tx) CompleteReview(attemptID string) error {
	return t.atomic(func() error {
		result, err := t.GetReviewResult(attemptID)
		if err != nil {
			return err
		}
		if result.Delivered {
			return nil
		}
		attempt, err := t.GetAttempt(attemptID)
		if err != nil {
			return err
		}
		if attempt.State != work.AttemptCompleted {
			return ErrConflict
		}
		var count int
		if err = t.tx.QueryRowContext(t.ctx, "SELECT count(*) FROM quiescence_receipts WHERE attempt_id=?", attemptID).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return ErrConflict
		}
		for _, table := range []string{"carrier_occupancy", "resource_occupancy", "task_occupancy"} {
			if err = t.tx.QueryRowContext(t.ctx, "SELECT count(*) FROM "+table+" WHERE attempt_id=?", attemptID).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				return ErrConflict
			}
		}
		source, err := t.GetContext(result.Spec.SourceContextID)
		if err != nil {
			return err
		}
		if source.ScopeID != result.ScopeID || source.Purpose == work.PurposeReview || source.Purpose == work.PurposeControl {
			return ErrScopeDenied
		}
		now := time.Now().UTC()
		derived := result.Terminal
		message := work.Message{ID: "review-result-message:" + attemptID, ScopeID: source.ScopeID, ConversationID: source.ID, Source: work.Actor{Kind: work.ActorSystem, ID: "review-result"}, BodyRef: result.BodyRef, DerivedFrom: &derived, Revision: 1, CreatedAt: now}
		input := work.Inbox{ID: "review-result-input:" + attemptID, ScopeID: source.ScopeID, RecipientRoleID: source.RoleID, RecipientContextID: source.ID, EventID: "review.result:" + attemptID, MessageID: message.ID, State: work.InboxPending, Revision: 1, AvailableAt: now}
		if _, _, err = t.AcceptMessage(message, []work.Inbox{input}); err != nil {
			return err
		}
		result.Delivered = true
		result.Revision++
		raw, err := json.Marshal(result)
		if err != nil {
			return err
		}
		_, err = t.tx.ExecContext(t.ctx, "UPDATE review_results SET payload=? WHERE attempt_id=?", string(raw), attemptID)
		return err
	})
}
