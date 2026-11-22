package workruntime

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// A correlated thread/turn-start rejection is a failed request before a native
// turn exists. This does not apply to an absent result, an ambiguous write, or
// an existing session whose explicit resume rejection needs checkpoint recovery.
func (c *Coordinator) preparePrivateStartRejectionReport(tx *workstore.Tx, attempt *work.ExecutionAttempt, request InspectionRequest, observation NativeObservation) error {
	if _, err := tx.GetProcessingReport(attempt.ID); err == nil {
		return nil
	} else if !errors.Is(err, workstore.ErrNotFound) {
		return err
	}
	if request.ResumeRejected || request.TurnID != "" || request.SubjectCommandID != attempt.CommandID {
		return nil
	}
	ids, err := privateOutcomeHumanInputs(tx, *attempt, true)
	if err != nil || len(ids) == 0 {
		return err
	}
	turns, err := tx.ListNativeTurns(attempt.ID)
	if err != nil || len(turns) != 0 {
		return err
	}
	registered, err := tx.GetNativeCommand(attempt.CommandID)
	if err != nil {
		return err
	}
	if registered.Incarnation != request.Carrier.Incarnation || registered.Binding != request.Binding || registered.SessionID != request.SessionID || registered.TerminalRejection == nil {
		return nil
	}
	canonical, err := tx.NativeCommandPayload(attempt.CommandID, registered.Incarnation)
	if err != nil {
		return err
	}
	if canonical.Kind != agentproto.CommandPromptSend || canonical.Target.InternalHelper {
		return nil
	}
	resumeRejected, err := tx.IsNativeResumeRejection(registered)
	if err != nil || resumeRejected {
		return err
	}
	event := *registered.TerminalRejection
	if event.Kind != agentproto.EventTurnCompleted || event.CommandID != attempt.CommandID || event.RequestID == "" || event.Status != "failed" || event.TurnID != "" || event.Journal == nil || event.Journal.CarrierIncarnation != registered.Incarnation || (event.Execution != nil && *event.Execution != registered.Binding) {
		return nil
	}
	switch event.TurnCompletionOrigin {
	case agentproto.TurnCompletionOriginThreadStartRejected:
		if canonical.Target.EffectivePromptExecutionMode() != agentproto.PromptExecutionModeStartNew || canonical.Target.ThreadID != "" || event.ThreadID != "" {
			return nil
		}
	case agentproto.TurnCompletionOriginTurnStartRejected:
		if event.ThreadID == "" || event.ThreadID != request.SessionID {
			return nil
		}
	default:
		return nil
	}
	persisted, err := tx.GetRuntimeEvent(registered.Incarnation, event.Journal.Sequence)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(persisted, event) {
		return nil
	}
	// The independent inspection must carry this exact native RPC rejection as
	// its native-domain proof, together with continuation and host inventories.
	var nativeProof agentproto.Event
	if err = json.Unmarshal(observation.NativeEvidence, &nativeProof); err != nil || !reflect.DeepEqual(nativeProof, event) {
		return nil
	}
	if !observation.provesStopped(request) {
		return fmt.Errorf("private rejected start awaits full native, continuation and background stop proof")
	}
	settled, err := privateOutcomeEffectsSettled(tx, *attempt, request, observation)
	if err != nil {
		return err
	}
	if !settled {
		return fmt.Errorf("private rejected start still has unresolved business effects")
	}
	if err = c.applyPrivateDisplay(tx, request.Carrier, request.Binding, event); err != nil {
		return err
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	evidence := work.NewContent("private-start-rejection:"+attempt.ID, attempt.ScopeID, "application/json", raw, c.options.Clock())
	if err = tx.PutContent(evidence); err != nil {
		return err
	}
	wc, err := tx.GetContext(attempt.ContextID)
	if err != nil {
		return err
	}
	checkpoint := wc.Checkpoint
	if checkpoint == "" {
		checkpoint = "上次私人请求在原生开始执行前被拒绝，没有执行本次请求的工作；原文仍保留，请根据用户的新输入继续。"
	}
	if attempt.State == work.AttemptDispatchPending {
		next := *attempt
		next.State = work.AttemptRunning
		next.Revision++
		next.UpdatedAt = c.options.Clock()
		if err = tx.PutAttempt(next, attempt.Revision); err != nil {
			return err
		}
		*attempt = next
	}
	return tx.PutProcessingReport(workstore.ProcessingReport{AttemptID: attempt.ID, RunRef: attempt.RunRef, ProcessedInputIDs: ids, OutcomeRef: evidence.ID, Checkpoint: checkpoint, WorkDisposition: "unknown", Revision: 1}, 0)
}
