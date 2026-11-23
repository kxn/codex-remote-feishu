package workstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// RepairUnregisteredNativeResponseTarget is the sole exception for the legacy
// response metadata bug. The actual command bytes/ID/human answer never change.
// Production transport registers before writing; this mismatched target could
// not register, and absence from native_commands proves this local failure.
func (t *Tx) RepairUnregisteredNativeResponseTarget(operationID, requestID string, now time.Time) (bool, error) {
	op, err := t.GetOperation(operationID)
	if err != nil {
		return false, err
	}
	request, err := t.GetNativeRequest(requestID)
	if err != nil {
		return false, err
	}
	if now.IsZero() || op.Kind != "native.request_response" || op.State != work.OperationUnknown || op.TargetID != request.ID || op.TargetID == request.Binding.WorkerSessionID || request.State != "response_pending" || request.ResponseOperationID != op.ID || op.ScopeID != request.ScopeID || op.ContextID != request.Binding.ContextID || op.TargetGeneration != request.Binding.SessionGeneration || op.Actor.Kind != work.ActorPrincipal {
		return false, nil
	}
	if _, err = t.GetNativeCommand(op.ID); err == nil {
		return false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return false, err
	}
	body, err := t.GetContent(op.ScopeID, op.RequestRef)
	if err != nil {
		return false, err
	}
	if body.SHA256 != op.RequestHash {
		return false, ErrScopeDenied
	}
	var command agentproto.Command
	if err = decodeNativeCanonical(body.Data, &command); err != nil {
		return false, err
	}
	if command.CommandID != op.ID || command.Kind != agentproto.CommandRequestRespond || command.Execution == nil || *command.Execution != request.Binding || command.Execution.ValidateExecution() != nil || command.Origin.UserID != op.Actor.ID || command.Origin.MessageID == "" || command.Target.ThreadID != request.NativeSessionID || command.Target.TurnID != request.NativeTurnID || command.Request.RequestID != request.NativeRequestID || command.Request.Response == nil {
		return false, ErrScopeDenied
	}
	canonical, err := json.Marshal(command)
	if err != nil {
		return false, err
	}
	if work.NewContent("proof", op.ScopeID, "application/json", canonical, now).SHA256 != op.RequestHash {
		return false, ErrScopeDenied
	}
	human, err := t.GetHumanInteraction(command.Origin.MessageID)
	if err != nil {
		return false, err
	}
	if human.PrincipalID != op.Actor.ID || human.Gateway.RoleID != request.Binding.RoleID || human.Gateway.BotGeneration != request.Binding.BotGeneration || human.State != "pending" && human.State != "handled" || human.State == "handled" && human.ResultRef != op.ID {
		return false, ErrScopeDenied
	}
	viewID, _ := human.Envelope.Values["team_view"].(string)
	view, err := t.GetUIView(viewID)
	if err != nil {
		return false, err
	}
	if view.Kind != "native.request" || view.TargetID != request.ID || view.ScopeID != request.ScopeID || view.PrincipalID != human.PrincipalID || view.Binding.RoleID != human.Gateway.RoleID || view.Binding.AppID != human.Gateway.AppID || view.Binding.GatewayRef != human.Gateway.GatewayRef || view.Binding.IdentityGeneration != human.Gateway.BotGeneration {
		return false, ErrScopeDenied
	}
	decisionBody, err := t.GetContent(op.ScopeID, human.ID+":decision")
	if err != nil {
		return false, err
	}
	var decision struct {
		RequestID          string
		ExpectedRevision   int64
		PrincipalID        string
		EvidenceRef        string
		FollowupHumanInput string
		Response           agentproto.Request
	}
	if err = decodeNativeCanonical(decisionBody.Data, &decision); err != nil {
		return false, err
	}
	if decision.ExpectedRevision != view.TargetRevision || request.Revision != decision.ExpectedRevision+1 {
		return false, ErrScopeDenied
	}
	decision.Response.RequestID = request.NativeRequestID
	response, err := json.Marshal(decision.Response)
	if err != nil {
		return false, err
	}
	actual, err := json.Marshal(command.Request)
	if err != nil {
		return false, err
	}
	if decision.RequestID != request.ID || decision.PrincipalID != human.PrincipalID || decision.EvidenceRef != human.ID || !bytes.Equal(response, actual) {
		return false, ErrScopeDenied
	}
	proofRaw, err := json.Marshal(struct {
		OperationID, RequestID, PreviousTargetID, WorkerID, RequestHash, HumanEvidence string
		PreviousRevision                                                               int64
		VerifiedAt                                                                     time.Time
	}{op.ID, request.ID, op.TargetID, request.Binding.WorkerSessionID, op.RequestHash, human.ID, op.Revision, now.UTC()})
	if err != nil {
		return false, err
	}
	proof := work.NewContent(fmt.Sprintf("%s:unregistered-target-repair:%d", op.ID, op.Revision), op.ScopeID, "application/json", proofRaw, now)
	if err = t.PutContent(proof); err != nil {
		return false, err
	}
	previous := op.Revision
	op.TargetID = request.Binding.WorkerSessionID
	op.State = work.OperationRetryable
	op.Revision++
	op.UpdatedAt = now.UTC()
	op.LastError = "local response registration target repaired; command was not sent"
	op.Steps = append(op.Steps, work.OperationStep{ID: "local_registration_target_repair", State: work.OperationConfirmed, Receipt: proof.ID})
	if err = op.Validate(); err != nil {
		return false, err
	}
	raw, err := json.Marshal(op)
	if err != nil {
		return false, err
	}
	result, err := t.tx.ExecContext(t.ctx, "UPDATE operations SET revision=?,payload=? WHERE id=? AND revision=? AND NOT EXISTS(SELECT 1 FROM native_commands WHERE command_id=?)", op.Revision, string(raw), op.ID, previous, op.ID)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if count != 1 {
		return false, ErrConflict
	}
	return true, nil
}
