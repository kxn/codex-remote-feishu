package workruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// A shutdown revocation cannot start business work. Its delivery remains
// unknown; only independently proved retirement lets the failed private input
// settle without waiting for that dead carrier to acknowledge the revocation.
func privateOutcomeRetiredShutdown(tx *workstore.Tx, op work.Operation, request InspectionRequest, observation NativeObservation) (bool, error) {
	if op.Actor != (work.Actor{Kind: work.ActorSystem, ID: "daemon-shutdown"}) || !observation.RetiredCarrierVerified || observation.RetirementEvidence == nil || !observation.provesStopped(request) {
		return false, nil
	}
	switch agentproto.CommandKind(op.Kind) {
	case agentproto.CommandThreadGoalClear, agentproto.CommandTurnInterrupt:
	default:
		return false, nil
	}
	evidence := observation.RetirementEvidence
	if evidence.NativeWriteWatermark != observation.NativeWriteWatermark || evidence.CanonicalCommandWatermark != observation.CanonicalCommandWatermark || evidence.InspectionSource != observation.Source || evidence.VerifiedHostID == "" || evidence.Process.PID <= 0 || evidence.VerifiedAt.IsZero() {
		return false, workstore.ErrConflict
	}
	if err := validateRetirementCommit(tx, request, observation); err != nil {
		return false, err
	}
	body, err := tx.GetContent(op.ScopeID, op.RequestRef)
	if err != nil {
		return false, err
	}
	if body.SHA256 != op.RequestHash {
		return false, workstore.ErrConflict
	}
	command, err := tx.NativeCommandPayload(op.ID, request.Carrier.Incarnation)
	if err != nil {
		return false, err
	}
	registered, err := tx.GetNativeCommand(op.ID)
	if err != nil {
		return false, err
	}
	if command.Kind != agentproto.CommandKind(op.Kind) || command.Execution == nil || *command.Execution != request.Binding || registered.Binding != request.Binding || registered.Incarnation != request.Carrier.Incarnation || registered.Kind != command.Kind || registered.SessionID != request.SessionID || command.Target.ThreadID != request.SessionID || command.Target.TurnID != "" && command.Target.TurnID != request.TurnID || command.Kind == agentproto.CommandThreadGoalClear && command.Target.TurnID != "" || command.Kind == agentproto.CommandTurnInterrupt && command.Target.TurnID != request.TurnID {
		return false, nil
	}
	identity, err := json.Marshal([]string{request.Carrier.Incarnation, request.Binding.AttemptID, string(command.Kind), command.Target.ThreadID, command.Target.TurnID})
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256(identity)
	return op.ID == "shutdown_"+hex.EncodeToString(sum[:20]) && op.IdempotencyKey == op.ID && op.ScopeID == request.Binding.ScopeID && op.ContextID == request.Binding.ContextID && op.TargetID == request.Binding.WorkerSessionID && op.TargetGeneration == request.Binding.SessionGeneration, nil
}
