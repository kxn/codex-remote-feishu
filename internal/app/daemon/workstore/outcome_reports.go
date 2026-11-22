package workstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// ProcessingReport records handled inputs. Usually agent-reported, it may also
// be a strictly verified private native-conversation receipt (unknown business
// disposition); this never substitutes for project task acceptance. It does
// not assert that a native process stopped, release occupancy, or accept work
// on behalf of a human. Runtime reconciliation commits the final Outcome.
type ProcessingReport struct {
	WorkDisposition   string      `json:"work_disposition,omitempty"`
	AttemptID         string      `json:"attempt_id"`
	RunRef            work.RunRef `json:"run_ref"`
	ProcessedInputIDs []string    `json:"processed_input_ids"`
	OutcomeRef        string      `json:"outcome_ref"`
	Checkpoint        string      `json:"checkpoint"`
	Revision          int64       `json:"revision"`
}

func (t *Tx) PutProcessingReport(report ProcessingReport, expected int64) error {
	switch report.WorkDisposition {
	case "", "unknown", "complete", "unfinished":
	default:
		return fmt.Errorf("unsupported work disposition")
	}
	if report.Revision != expected+1 || expected < 0 {
		return ErrConflict
	}
	attempt, err := t.GetAttempt(report.AttemptID)
	if err != nil {
		return err
	}
	attempt, err = t.effectiveAttemptInputs(attempt)
	if err != nil {
		return err
	}
	if attempt.RunRef != report.RunRef {
		return fmt.Errorf("processing report run mismatch")
	}
	if attempt.State != work.AttemptRunning && attempt.State != work.AttemptReconciling {
		return fmt.Errorf("processing report needs unfinished attempt")
	}
	if _, err = t.GetContent(attempt.ScopeID, report.OutcomeRef); err != nil {
		return err
	}
	inputs := make([]work.Inbox, 0, len(report.ProcessedInputIDs))
	for _, id := range report.ProcessedInputIDs {
		input, e := t.GetInbox(id)
		if e != nil {
			return e
		}
		if e = t.ValidateSessionGoalInputConsumption(input); e != nil {
			return e
		}
		inputs = append(inputs, input)
	}
	if err = work.ValidateInputConsumption(attempt, inputs, report.ProcessedInputIDs); err != nil {
		return err
	}
	body, err := json.Marshal(report)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	if expected == 0 {
		old, e := t.GetProcessingReport(report.AttemptID)
		if e == nil {
			oldBody, _ := json.Marshal(old)
			if string(oldBody) == string(body) {
				return nil
			}
			return ErrConflict
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		_, err = t.tx.ExecContext(t.ctx, "INSERT INTO outcome_reports(attempt_id,request_hash,payload) VALUES(?,?,?)", report.AttemptID, hash, string(body))
		return err
	}
	result, err := t.tx.ExecContext(t.ctx, "UPDATE outcome_reports SET request_hash=?,payload=? WHERE attempt_id=? AND json_extract(payload,'$.revision')=?", hash, string(body), report.AttemptID, expected)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}

func (t *Tx) GetProcessingReport(attemptID string) (ProcessingReport, error) {
	var report ProcessingReport
	var payload string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM outcome_reports WHERE attempt_id=?", attemptID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return report, ErrNotFound
	}
	if err != nil {
		return report, err
	}
	err = json.Unmarshal([]byte(payload), &report)
	return report, err
}
