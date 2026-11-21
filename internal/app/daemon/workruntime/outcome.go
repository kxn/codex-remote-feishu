package workruntime

import (
	"encoding/json"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (c *Coordinator) commitNativeOutcome(tx *workstore.Tx, attempt work.ExecutionAttempt, request InspectionRequest) error {
	if attempt.State == work.AttemptCompleted || attempt.State == work.AttemptFailed || attempt.State == work.AttemptInterrupted {
		return nil
	}
	turns, err := tx.ListNativeTurns(attempt.ID)
	if err != nil {
		return err
	}
	state := work.AttemptCompleted
	if len(turns) == 0 {
		state = work.AttemptFailed
	}
	for _, turn := range turns {
		if turn.TerminalStatus == "failed" {
			state = work.AttemptFailed
			break
		}
		if turn.TerminalStatus == "interrupted" {
			state = work.AttemptInterrupted
		}
	}
	if attempt.State == work.AttemptDispatchPending {
		expected := attempt.Revision
		attempt.Revision++
		attempt.State = work.AttemptRunning
		attempt.UpdatedAt = c.options.Clock()
		if err := tx.PutAttempt(attempt, expected); err != nil {
			return err
		}
	}
	ref := "native-terminal:" + request.Carrier.Incarnation + ":" + attempt.ID
	data, err := json.Marshal(turns)
	if err != nil {
		return err
	}
	if err := tx.PutContent(work.NewContent(ref, attempt.ScopeID, "application/json", data, c.options.Clock())); err != nil {
		return err
	}
	report, err := tx.GetProcessingReport(attempt.ID)
	if errors.Is(err, workstore.ErrNotFound) {
		if state != work.AttemptInterrupted {
			state = work.AttemptFailed
		}
		report = workstore.ProcessingReport{AttemptID: attempt.ID, RunRef: attempt.RunRef, OutcomeRef: "missing-processing-report:" + attempt.ID, Checkpoint: "原生执行已结束，但未收到业务处理报告；原输入保留，需负责人核对现场与原生历史后显式恢复。"}
		if err := tx.PutContent(work.NewContent(report.OutcomeRef, attempt.ScopeID, "text/plain", []byte(report.Checkpoint), c.options.Clock())); err != nil {
			return err
		}
		if err := tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: attempt.RunRef.WorkerID, ScopeID: attempt.ScopeID, AttemptID: attempt.ID, Reason: "missing_processing_report", CreatedAt: c.options.Clock()}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return tx.CommitOutcome(workstore.OutcomeRequest{AttemptID: attempt.ID, RunRef: attempt.RunRef, ExpectedAttemptRevision: attempt.Revision, ProcessedInputIDs: report.ProcessedInputIDs, OutcomeRef: report.OutcomeRef, Checkpoint: report.Checkpoint, TerminalState: state, TerminalEvidence: []work.SourceRef{{Kind: "native_terminal", ID: ref, Version: "1", EvidenceRef: ref}}, Quiescent: false, Now: c.options.Clock()})
}
