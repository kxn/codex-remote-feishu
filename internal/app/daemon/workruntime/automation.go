package workruntime

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func automationTerminal(tx *workstore.Tx, request InspectionRequest) (agentproto.Event, error) {
	turn, err := tx.GetNativeTurn(request.Carrier.Incarnation, request.SessionID, request.TurnID)
	if err != nil {
		return agentproto.Event{}, err
	}
	if turn.TerminalSequence == 0 {
		return agentproto.Event{}, workstore.ErrNotFound
	}
	return tx.GetRuntimeEvent(request.Carrier.Incarnation, turn.TerminalSequence)
}

func (c *Coordinator) automationSource(tx *workstore.Tx, request InspectionRequest, event agentproto.Event) (work.SourceRef, error) {
	id := fmt.Sprintf("native-automation:%s:%d", request.Carrier.Incarnation, event.Journal.Sequence)
	raw, err := json.Marshal(event)
	if err != nil {
		return work.SourceRef{}, err
	}
	if err = tx.PutContent(work.NewContent(id, request.Binding.ScopeID, "application/json", raw, c.options.Clock())); err != nil {
		return work.SourceRef{}, err
	}
	return work.SourceRef{Kind: string(event.Kind), ID: id, Version: event.Journal.PayloadHash, EvidenceRef: id}, nil
}

func (c *Coordinator) prepareAutomaticFailureReport(tx *workstore.Tx, attempt work.ExecutionAttempt, request InspectionRequest, observation NativeObservation) error {
	if _, err := tx.GetProcessingReport(attempt.ID); err == nil {
		return nil
	} else if !errors.Is(err, workstore.ErrNotFound) {
		return err
	}
	event, err := automationTerminal(tx, request)
	if errors.Is(err, workstore.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	eligible, err := tx.CanAutoContinueFailure(attempt.ID, event)
	if err != nil {
		return err
	}
	if !eligible {
		return nil
	}
	if gate, e := tx.GetRuntimeGate(attempt.RunRef.WorkerID); e == nil && gate.RecoveryReason != "" {
		return nil
	} else if e != nil && !errors.Is(e, workstore.ErrNotFound) {
		return e
	}
	if !observation.provesStopped(request) {
		return fmt.Errorf("retryable native failure waits for complete background quiescence")
	}
	if err = tx.AssertAutomationEffectsSettled(attempt.ID); err != nil {
		return err
	}
	source, err := c.automationSource(tx, request, event)
	if err != nil {
		return err
	}
	wc, err := tx.GetContext(attempt.ContextID)
	if err != nil {
		return err
	}
	// This is a system failure receipt. It makes no claim that the agent handled
	// the business inputs; those remain pending for the bounded recovery episode.
	return tx.PutProcessingReport(workstore.ProcessingReport{AttemptID: attempt.ID, RunRef: attempt.RunRef, ProcessedInputIDs: nil, OutcomeRef: source.EvidenceRef, Checkpoint: wc.Checkpoint, WorkDisposition: "unfinished", Revision: 1}, 0)
}

func (c *Coordinator) queueAutomaticFollowup(tx *workstore.Tx, attemptID string, request InspectionRequest) error {
	event, err := automationTerminal(tx, request)
	if errors.Is(err, workstore.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	kind := work.AutomationWhip
	if event.Status == "failed" {
		kind = work.AutomationContinue
	} else if event.Status != "completed" {
		return nil
	}
	source, err := c.automationSource(tx, request, event)
	if err != nil {
		return err
	}
	_, _, err = tx.QueueAutomationFollowup(attemptID, kind, source, c.options.Clock())
	if errors.Is(err, workstore.ErrAutomationNotApplicable) || errors.Is(err, workstore.ErrNotFound) {
		return nil
	}
	return err
}
