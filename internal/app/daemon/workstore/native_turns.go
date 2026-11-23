package workstore

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type CarrierRegistration struct {
	Incarnation        string                      `json:"incarnation"`
	OriginInstanceID   string                      `json:"origin_instance_id"`
	Owner              agentproto.ExecutionBinding `json:"owner"`
	Backend            work.Backend                `json:"backend"`
	InstanceGeneration int64                       `json:"instance_generation"`
	ProcessGeneration  int64                       `json:"process_generation"`
	LocationGeneration int64                       `json:"location_generation"`
}

func (v CarrierRegistration) Matches(worker work.WorkerSession) bool {
	return worker.State != work.WorkerRetired && v.Incarnation == worker.CarrierIncarnation && v.OriginInstanceID == worker.InstanceID && v.Backend == worker.Backend && v.InstanceGeneration == worker.InstanceGeneration && v.ProcessGeneration == worker.ProcessGeneration && v.LocationGeneration == worker.LocationGeneration && v.Owner.WorkerSessionID == worker.ID && v.Owner.ScopeID == worker.ScopeID && v.Owner.ContextID == worker.ContextID && v.Owner.RoleID == worker.RoleID && v.Owner.SessionGeneration == worker.Generation && v.Owner.ControllerGeneration == worker.ControllerGeneration
}

func (v CarrierRegistration) MatchesPhysical(worker work.WorkerSession) bool {
	worker.ControllerGeneration = v.Owner.ControllerGeneration
	return v.Matches(worker)
}

func (t *Tx) RegisterCarrier(v CarrierRegistration) error {
	if v.Incarnation == "" || v.OriginInstanceID == "" || v.Owner.AttemptID != "" || v.Owner.RunRef != "" || v.Owner.ValidateOwner() != nil || v.InstanceGeneration < 1 || v.ProcessGeneration < 1 {
		return fmt.Errorf("invalid carrier registration")
	}
	worker, err := t.GetWorker(v.Owner.WorkerSessionID)
	if err != nil {
		return err
	}
	if !v.Matches(worker) {
		return fmt.Errorf("carrier does not match frozen worker generations")
	}
	old, err := t.GetCarrier(v.Incarnation)
	if err == nil {
		if !reflect.DeepEqual(old, v) {
			return ErrIdempotencyConflict
		}
		return nil
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO carrier_registrations(incarnation,worker_id,instance_id,payload) VALUES(?,?,?,?)`, v.Incarnation, v.Owner.WorkerSessionID, v.OriginInstanceID, mustJSON(v))
	return err
}

func (t *Tx) GetCarrier(incarnation string) (CarrierRegistration, error) {
	var v CarrierRegistration
	var raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT payload FROM carrier_registrations WHERE incarnation=?`, incarnation).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v)
	return v, err
}

func (t *Tx) ListCarriersForInstance(instanceID string) ([]CarrierRegistration, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT payload FROM carrier_registrations WHERE instance_id=? ORDER BY incarnation`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []CarrierRegistration
	for rows.Next() {
		var raw string
		var v CarrierRegistration
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

func (t *Tx) SetCarrierConnected(incarnation string, connected bool) error {
	result, err := t.tx.ExecContext(t.ctx, `UPDATE carrier_registrations SET connected=? WHERE incarnation=?`, connected, incarnation)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNotFound
	}
	return nil
}

type NativeCommand struct {
	TerminalRejection *agentproto.Event           `json:"terminal_rejection,omitempty"`
	Ack               *agentproto.CommandAck      `json:"ack,omitempty"`
	CommandID         string                      `json:"command_id"`
	Incarnation       string                      `json:"incarnation"`
	Binding           agentproto.ExecutionBinding `json:"binding"`
	Kind              agentproto.CommandKind      `json:"kind"`
	RequestRef        string                      `json:"request_ref"`
	SessionID         string                      `json:"session_id"`
}

// IsNativeResumeRejection distinguishes exact pre-turn rejection of a
// registered resume from an absent result or an ambiguous write.
func (t *Tx) IsNativeResumeRejection(command NativeCommand) (bool, error) {
	canonical, err := t.NativeCommandPayload(command.CommandID, command.Incarnation)
	if err != nil {
		return false, err
	}
	if canonical.Kind != agentproto.CommandPromptSend || canonical.Target.EffectivePromptExecutionMode() != agentproto.PromptExecutionModeResumeExisting || canonical.Target.ThreadID == "" {
		return false, nil
	}
	if command.TerminalRejection != nil && command.TerminalRejection.TurnCompletionOrigin == agentproto.TurnCompletionOriginThreadResumeRejected {
		return true, nil
	}
	ack := command.Ack
	return ack != nil && !ack.Accepted && ack.RuntimeReceipt != nil && ack.RuntimeReceipt.Stage == "prepared" && ack.RuntimeReceipt.PreparedFrames == 0, nil
}

// RegisterNativeCommand indexes the canonical operation request; it never
// creates a second command payload or independent execution authority.
func (t *Tx) RegisterNativeCommand(incarnation string, command agentproto.Command) (NativeCommand, error) {
	v := NativeCommand{}
	ownerOnly := command.IsOwnerOnlyRuntimeCommand()
	if command.Execution == nil || (ownerOnly && command.Execution.ValidateOwner() != nil) || (!ownerOnly && command.Execution.ValidateExecution() != nil) {
		return v, fmt.Errorf("command requires execution binding")
	}
	operation, err := t.GetOperation(command.CommandID)
	if err != nil {
		return v, err
	}
	privateShell := ownerOnly && command.Kind == agentproto.CommandThreadShellCommand && command.ShellCommand.OperationID != ""
	if privateShell {
		if err = t.ValidatePrivateShellCommand(command, incarnation); err != nil {
			return v, err
		}
	}
	if ownerOnly && operation.Actor.Kind != work.ActorSystem && !privateShell {
		return v, fmt.Errorf("owner-only inspection requires trusted runtime actor")
	}
	if operation.RequestRef == "" || operation.ScopeID != command.Execution.ScopeID || operation.TargetID != command.Execution.WorkerSessionID || operation.TargetGeneration != command.Execution.SessionGeneration {
		return v, fmt.Errorf("command operation ownership mismatch")
	}
	canonical, err := t.NativeCommandPayload(command.CommandID, incarnation)
	if err != nil {
		return v, err
	}
	canonicalJSON, err := json.Marshal(canonical)
	if err != nil {
		return v, err
	}
	incomingJSON, err := json.Marshal(command)
	if err != nil {
		return v, err
	}
	if !bytes.Equal(canonicalJSON, incomingJSON) {
		return v, ErrIdempotencyConflict
	}
	carrier, err := t.GetCarrier(incarnation)
	if err != nil {
		return v, err
	}
	if !carrier.Owner.SameOwner(*command.Execution) {
		return v, fmt.Errorf("command carrier mismatch")
	}
	if !ownerOnly {
		if _, err := t.ValidateNativeBinding(*command.Execution); err != nil {
			return v, err
		}
	}
	v = NativeCommand{CommandID: command.CommandID, Incarnation: incarnation, Binding: *command.Execution, Kind: command.Kind, RequestRef: operation.RequestRef, SessionID: command.Target.ThreadID}
	old, err := t.GetNativeCommand(command.CommandID)
	if err == nil {
		oldAck := old.Ack
		oldRejection := old.TerminalRejection
		old.TerminalRejection = nil
		old.Ack = nil
		if !reflect.DeepEqual(old, v) {
			return v, ErrIdempotencyConflict
		}
		old.Ack = oldAck
		old.TerminalRejection = oldRejection
		return old, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return v, err
	}
	var attemptID any = v.Binding.AttemptID
	if ownerOnly {
		attemptID = nil
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO native_commands(command_id,incarnation,attempt_id,payload) VALUES(?,?,?,?)`, v.CommandID, incarnation, attemptID, mustJSON(v))
	return v, err
}

func (t *Tx) RecordNativeRejection(commandID string, event agentproto.Event) error {
	value, err := t.GetNativeCommand(commandID)
	if err != nil {
		return err
	}
	if event.CommandID != commandID || event.RequestID == "" || event.Status != "failed" {
		return fmt.Errorf("native rejection requires precise command RPC proof")
	}
	if value.TerminalRejection == nil {
		value.TerminalRejection = &event
	}
	_, err = t.tx.ExecContext(t.ctx, `UPDATE native_commands SET payload=? WHERE command_id=?`, mustJSON(value), commandID)
	return err
}

func (t *Tx) RecordNativeAck(commandID string, ack agentproto.CommandAck) error {
	value, err := t.GetNativeCommand(commandID)
	if err != nil {
		return err
	}
	if ack.CommandID != commandID {
		return fmt.Errorf("native acknowledgement command mismatch")
	}
	value.Ack = &ack
	_, err = t.tx.ExecContext(t.ctx, `UPDATE native_commands SET payload=? WHERE command_id=?`, mustJSON(value), commandID)
	return err
}

func (t *Tx) GetNativeCommand(commandID string) (NativeCommand, error) {
	var v NativeCommand
	var raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT payload FROM native_commands WHERE command_id=?`, commandID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v)
	return v, err
}

func (t *Tx) ResolveNativeCommand(incarnation, commandID string) (NativeCommand, error) {
	if v, err := t.GetNativeCommand(commandID); err == nil {
		if v.Incarnation != incarnation {
			return NativeCommand{}, ErrScopeDenied
		}
		return v, nil
	} else if !errors.Is(err, ErrNotFound) {
		return v, err
	}
	command, err := t.NativeCommandPayload(commandID, incarnation)
	if err != nil {
		return NativeCommand{}, err
	}
	return t.RegisterNativeCommand(incarnation, command)
}

func (t *Tx) ValidateNativeBinding(binding agentproto.ExecutionBinding) (work.ExecutionAttempt, error) {
	if err := binding.ValidateExecution(); err != nil {
		return work.ExecutionAttempt{}, err
	}
	attempt, err := t.GetAttempt(binding.AttemptID)
	if err != nil {
		return attempt, err
	}
	if attempt.ContextID != binding.ContextID || attempt.ScopeID != binding.ScopeID || attempt.RunRef.WorkerID != binding.WorkerSessionID || attempt.RunRef.Generation != binding.SessionGeneration || attempt.RunRef.RunID != binding.RunRef {
		return attempt, fmt.Errorf("native attempt binding mismatch")
	}
	context, err := t.GetContext(attempt.ContextID)
	if err != nil {
		return attempt, err
	}
	if context.RoleID != binding.RoleID {
		return attempt, fmt.Errorf("native role mismatch")
	}
	return attempt, nil
}

type NativeTurn struct {
	ContinuationAuthorization string                      `json:"continuation_authorization,omitempty"`
	AuthorizationSequence     uint64                      `json:"authorization_sequence,omitempty"`
	CarrierIncarnation        string                      `json:"carrier_incarnation"`
	SessionID                 string                      `json:"session_id"`
	TurnID                    string                      `json:"turn_id"`
	Binding                   agentproto.ExecutionBinding `json:"binding"`
	Started                   bool                        `json:"started"`
	TerminalStatus            string                      `json:"terminal_status,omitempty"`
	TerminalSequence          uint64                      `json:"terminal_sequence,omitempty"`
	LastSequence              uint64                      `json:"last_sequence"`
	UpdatedAt                 time.Time                   `json:"updated_at"`
}

func (t *Tx) PutNativeTurn(v NativeTurn) error {
	if v.CarrierIncarnation == "" || v.SessionID == "" || v.TurnID == "" || v.UpdatedAt.IsZero() {
		return fmt.Errorf("native turn identity required")
	}
	carrier, err := t.GetCarrier(v.CarrierIncarnation)
	if err != nil {
		return err
	}
	if !carrier.Owner.SameOwner(v.Binding) {
		return fmt.Errorf("native turn outside carrier owner")
	}
	if _, err := t.ValidateNativeBinding(v.Binding); err != nil {
		return err
	}
	old, err := t.GetNativeTurn(v.CarrierIncarnation, v.SessionID, v.TurnID)
	if err == nil {
		if old.Binding != v.Binding {
			return ErrIdempotencyConflict
		}
		if old.TerminalStatus != "" && v.TerminalStatus != "" && old.TerminalStatus != v.TerminalStatus {
			return ErrIdempotencyConflict
		}
		if old.TerminalStatus != "" {
			v.TerminalStatus, v.TerminalSequence = old.TerminalStatus, old.TerminalSequence
		}
		if old.ContinuationAuthorization != "" {
			v.ContinuationAuthorization = old.ContinuationAuthorization
			v.AuthorizationSequence = old.AuthorizationSequence
		}
		v.Started = v.Started || old.Started
		if old.LastSequence > v.LastSequence {
			v.LastSequence = old.LastSequence
		}
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO native_turns(carrier_incarnation,session_id,turn_id,attempt_id,payload) VALUES(?,?,?,?,?) ON CONFLICT(carrier_incarnation,session_id,turn_id) DO UPDATE SET payload=excluded.payload`, v.CarrierIncarnation, v.SessionID, v.TurnID, v.Binding.AttemptID, mustJSON(v))
	return err
}

func (t *Tx) GetNativeTurn(incarnation, sessionID, turnID string) (NativeTurn, error) {
	var v NativeTurn
	var raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT payload FROM native_turns WHERE carrier_incarnation=? AND session_id=? AND turn_id=?`, incarnation, sessionID, turnID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v)
	return v, err
}

func (t *Tx) ListNativeTurns(attemptID string) ([]NativeTurn, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT payload FROM native_turns WHERE attempt_id=? ORDER BY carrier_incarnation,session_id,turn_id LIMIT 256`, attemptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []NativeTurn
	for rows.Next() {
		var raw string
		var v NativeTurn
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, rows.Err()
}
