package workruntime

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// A rejected model selection is a failed private request, not unreported work.
// Only an exact before-inference failure may use this receipt. The original
// message remains durable, and processing a management copy never consumes its
// original business input or claims that the requested work succeeded.
func (c *Coordinator) preparePrivateRejectedTurnReport(tx *workstore.Tx, attempt work.ExecutionAttempt, request InspectionRequest, observation NativeObservation) error {
	if _, err := tx.GetProcessingReport(attempt.ID); err == nil {
		return nil
	} else if !errors.Is(err, workstore.ErrNotFound) {
		return err
	}
	scope, err := tx.GetScope(attempt.ScopeID)
	if err != nil {
		return err
	}
	if scope.Kind != work.ScopePersonal || attempt.TaskID != "" || request.Carrier.Backend != work.BackendCodex {
		return nil
	}
	wc, err := tx.GetContext(attempt.ContextID)
	if err != nil {
		return err
	}
	if wc.Purpose == work.PurposeReview || wc.TaskID != "" {
		return nil
	}
	event, err := automationTerminal(tx, request)
	if errors.Is(err, workstore.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if event.Status != "failed" || event.TurnCompletionOrigin != agentproto.TurnCompletionOriginRuntime || event.Problem == nil || event.Problem.Code != "modelNotFound" {
		return nil
	}
	turns, err := tx.ListNativeTurns(attempt.ID)
	if err != nil {
		return err
	}
	if len(turns) != 1 || turns[0].TurnID != request.TurnID {
		return nil
	}
	var probe RPCProbe
	if err = json.Unmarshal(observation.NativeEvidence, &probe); err != nil {
		return err
	}
	result, err := matchRPC(probe, "thread/read", request.SessionID)
	if err != nil {
		return err
	}
	var state struct {
		Thread struct {
			ID    string `json:"id"`
			Turns []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Error  struct {
					Code string `json:"codexErrorInfo"`
				} `json:"error"`
				Items *[]struct {
					Type string `json:"type"`
				} `json:"items"`
			} `json:"turns"`
		} `json:"thread"`
	}
	if err = json.Unmarshal(result, &state); err != nil {
		return err
	}
	if state.Thread.ID != request.SessionID {
		return nil
	}
	rejected := false
	for _, turn := range state.Thread.Turns {
		if turn.ID != request.TurnID {
			continue
		}
		if turn.Status != "failed" || turn.Error.Code != "modelNotFound" || turn.Items == nil {
			return nil
		}
		for _, item := range *turn.Items {
			if item.Type != "userMessage" {
				return nil
			}
		}
		rejected = true
	}
	if !rejected {
		return nil
	}
	ids, err := privateOutcomeHumanInputs(tx, attempt, true)
	if err != nil || len(ids) == 0 {
		return err
	}
	settled, err := privateOutcomeEffectsSettled(tx, attempt)
	if err != nil {
		return err
	}
	if !observation.provesStopped(request) || !settled {
		return fmt.Errorf("private rejected request still awaits exact quiescence and effect evidence")
	}
	// Older versions suppressed all management output, including failures. Replay
	// only this exact durable terminal event through the idempotent display owner
	// so an already-stuck request gets its real error when reconciliation recovers.
	if err = c.applyPrivateDisplay(tx, request.Carrier, request.Binding, event); err != nil {
		return err
	}
	source, err := c.automationSource(tx, request, event)
	if err != nil {
		return err
	}
	checkpoint := wc.Checkpoint
	if checkpoint == "" {
		checkpoint = "上次私人请求因模型配置被拒绝而未开始；没有执行请求的工作。请根据用户之后的模型选择和新输入继续。"
	}
	return tx.PutProcessingReport(workstore.ProcessingReport{AttemptID: attempt.ID, RunRef: attempt.RunRef, ProcessedInputIDs: ids, OutcomeRef: source.EvidenceRef, Checkpoint: checkpoint, WorkDisposition: "unknown", Revision: 1}, 0)
}
