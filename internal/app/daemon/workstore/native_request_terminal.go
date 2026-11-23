package workstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const NativeRequestTerminalEvidenceType = "application/vnd.codex-remote.native-request-turn-terminal+json"

// A committed terminal turn closes its question, not the enclosing attempt or
// its resource/side-effect uncertainty. Only provably unregistered responses
// are locally terminated; registered unknown writes retain their own history.
func (t *Tx) CloseNativeRequestForTerminalTurn(id string, now time.Time) (bool, error) {
	request, err := t.GetNativeRequest(id)
	if err != nil {
		return false, err
	}
	if request.State != "open" && request.State != "response_pending" {
		return false, nil
	}
	turn, err := t.GetNativeTurn(request.CarrierIncarnation, request.NativeSessionID, request.NativeTurnID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if turn.Binding != request.Binding || turn.TerminalStatus == "" || turn.TerminalSequence == 0 {
		return false, nil
	}
	event, err := t.GetRuntimeEvent(request.CarrierIncarnation, turn.TerminalSequence)
	if err != nil {
		return false, err
	}
	carrier, err := t.GetCarrier(request.CarrierIncarnation)
	if err != nil {
		return false, err
	}
	var instance, storedHash string
	err = t.tx.QueryRowContext(t.ctx, "SELECT instance_id,payload_hash FROM runtime_receipts WHERE carrier_incarnation=? AND sequence=?", carrier.Incarnation, turn.TerminalSequence).Scan(&instance, &storedHash)
	if err != nil {
		return false, err
	}
	hash, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil {
		return false, err
	}
	if event.Kind != agentproto.EventTurnCompleted || event.Journal == nil || event.Journal.CarrierIncarnation != carrier.Incarnation || event.Journal.Sequence != turn.TerminalSequence || event.Seq != turn.TerminalSequence || event.Journal.PayloadHash != storedHash || hash != storedHash || instance != carrier.OriginInstanceID || event.ThreadID != request.NativeSessionID || event.TurnID != request.NativeTurnID || event.Status != turn.TerminalStatus || !carrier.Owner.SameOwner(request.Binding) || event.Execution != nil && *event.Execution != request.Binding {
		return false, ErrScopeDenied
	}
	raw, err := json.Marshal(struct {
		Carrier                        string
		Sequence                       uint64
		PayloadHash, RequestID, Status string
	}{carrier.Incarnation, turn.TerminalSequence, hash, request.ID, event.Status})
	if err != nil {
		return false, err
	}
	proof := work.NewContent(fmt.Sprintf("%s:turn-terminal:%d", request.ID, turn.TerminalSequence), request.ScopeID, NativeRequestTerminalEvidenceType, raw, now)
	if err = t.PutContent(proof); err != nil {
		return false, err
	}
	if request.ResponseOperationID != "" {
		op, err := t.GetOperation(request.ResponseOperationID)
		if err != nil {
			return false, err
		}
		if _, registered := t.GetNativeCommand(op.ID); errors.Is(registered, ErrNotFound) && op.Kind == "native.request_response" && op.ScopeID == request.ScopeID && op.ContextID == request.Binding.ContextID && (op.State == work.OperationUnknown || op.State == work.OperationSending || op.State == work.OperationRetryable) {
			unsent := op.TargetID == request.ID
			for _, step := range op.Steps {
				if step.ID == "native_response_unsent" && step.State == work.OperationConfirmed {
					if _, err = t.GetContent(op.ScopeID, step.Receipt); err != nil {
						return false, err
					}
					unsent = true
				}
			}
			if unsent {
				body, err := t.GetContent(op.ScopeID, op.RequestRef)
				if err != nil {
					return false, err
				}
				var command agentproto.Command
				if err = decodeNativeCanonical(body.Data, &command); err != nil {
					return false, err
				}
				canonical, err := json.Marshal(command)
				if err != nil {
					return false, err
				}
				if command.Kind != agentproto.CommandRequestRespond || command.CommandID != op.ID || command.Execution == nil || *command.Execution != request.Binding || command.Request.RequestID != request.NativeRequestID || command.Target.ThreadID != request.NativeSessionID || command.Target.TurnID != request.NativeTurnID || body.SHA256 != op.RequestHash || work.NewContent("proof", op.ScopeID, "application/json", canonical, now).SHA256 != op.RequestHash {
					return false, ErrScopeDenied
				}
				before := op.Revision
				op.Revision++
				op.State = work.OperationTerminal
				op.LastError = "native turn ended before the saved response was sent"
				op.NextAttemptAt = nil
				op.UpdatedAt = now.UTC()
				op.Steps = append(op.Steps, work.OperationStep{ID: "unsent_response_turn_ended", State: work.OperationConfirmed, Receipt: proof.ID})
				if _, _, err = t.PutOperation(op, before); err != nil {
					return false, err
				}
			}
		} else if registered != nil && !errors.Is(registered, ErrNotFound) {
			return false, registered
		}
	}
	before := request.Revision
	request.Revision++
	request.State = "stale"
	request.ResolvedEvidenceRef = proof.ID
	request.UpdatedAt = now.UTC()
	return true, t.PutNativeRequest(request, before)
}

func (t *Tx) CurrentNativeRequestView(scope, requestID, surface string) (UIView, error) {
	var view UIView
	var raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT payload FROM team_ui_views WHERE scope_id=? AND surface_key=? AND json_extract(payload,'$.kind')='native.request' AND json_extract(payload,'$.target_id')=? AND coalesce(json_extract(payload,'$.invalidated'),0)=0 ORDER BY rowid DESC LIMIT 1`, scope, surface, requestID).Scan(&raw)
	if err != nil {
		return view, err
	}
	err = json.Unmarshal([]byte(raw), &view)
	return view, err
}
