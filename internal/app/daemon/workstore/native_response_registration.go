package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// The human decision and native send are separate transactions. Recheck the
// exact request here so a terminal receipt between them prevents a stale write.
func (t *Tx) ValidateNativeResponseRegistration(command agentproto.Command, incarnation string) error {
	var raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT payload FROM native_ui_requests WHERE json_extract(payload,'$.response_operation_id')=?`, command.CommandID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrScopeDenied
	}
	if err != nil {
		return err
	}
	var request NativeRequest
	if err = json.Unmarshal([]byte(raw), &request); err != nil {
		return err
	}
	if command.Kind != agentproto.CommandRequestRespond || command.Execution == nil || *command.Execution != request.Binding || request.CarrierIncarnation != incarnation || request.State != "response_pending" || request.NativeRequestID != command.Request.RequestID || command.Target.ThreadID != request.NativeSessionID || command.Target.TurnID != request.NativeTurnID {
		return fmt.Errorf("native request has ended or its response ownership changed")
	}
	attempt, err := t.ValidateNativeBinding(request.Binding)
	if err != nil {
		return err
	}
	if attempt.State != work.AttemptRunning {
		return fmt.Errorf("native request execution is no longer running")
	}
	turn, err := t.GetNativeTurn(incarnation, request.NativeSessionID, request.NativeTurnID)
	if err != nil {
		return err
	}
	if turn.Binding != request.Binding || turn.TerminalStatus != "" {
		return fmt.Errorf("native request turn has ended or changed")
	}
	gates, err := t.ListPauseGates(request.ScopeID)
	if err != nil {
		return err
	}
	wc, err := t.GetContext(attempt.ContextID)
	if err != nil {
		return err
	}
	allowed, err := work.GatesAllowRunForPurpose(request.ScopeID, attempt.TaskID, attempt.ContextID, wc.Purpose, gates)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("execution is paused; response has not been sent")
	}
	return nil
}
