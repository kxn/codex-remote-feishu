package workstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// NativeCommandPayload reads the one canonical operation request. The engine's
// native.dispatch envelope and direct system commands have explicit contracts;
// neither may be guessed from a current worker or a permissive JSON fallback.
func (t *Tx) NativeCommandPayload(operationID, incarnation string) (agentproto.Command, error) {
	var command agentproto.Command
	op, err := t.GetOperation(operationID)
	if err != nil {
		return command, err
	}
	body, err := t.GetContent(op.ScopeID, op.RequestRef)
	if err != nil {
		return command, err
	}
	if op.Kind == "native.dispatch" {
		var envelope struct {
			InstanceID         string             `json:"instance_id"`
			InstanceGeneration int64              `json:"instance_generation"`
			ProcessGeneration  int64              `json:"process_generation"`
			CarrierIncarnation string             `json:"carrier_incarnation"`
			Command            agentproto.Command `json:"command"`
		}
		if err = decodeNativeCanonical(body.Data, &envelope); err != nil {
			return command, err
		}
		if envelope.CarrierIncarnation != incarnation {
			return command, fmt.Errorf("dispatch envelope targets another incarnation")
		}
		carrier, e := t.GetCarrier(incarnation)
		if e != nil {
			return command, e
		}
		if envelope.InstanceID != carrier.OriginInstanceID || envelope.InstanceGeneration != carrier.InstanceGeneration || envelope.ProcessGeneration != carrier.ProcessGeneration || envelope.Command.Execution == nil || !carrier.Owner.SameOwner(*envelope.Command.Execution) {
			return command, fmt.Errorf("dispatch envelope differs from its registered carrier")
		}
		command = envelope.Command
	} else if err = decodeNativeCanonical(body.Data, &command); err != nil {
		return command, err
	}
	if command.CommandID != op.ID || command.Execution == nil || command.Execution.ScopeID != op.ScopeID || command.Execution.WorkerSessionID != op.TargetID || command.Execution.SessionGeneration != op.TargetGeneration || (op.ContextID != "" && op.ContextID != command.Execution.ContextID) {
		return command, fmt.Errorf("canonical native command operation ownership mismatch")
	}
	carrier, err := t.GetCarrier(incarnation)
	if err != nil {
		return command, err
	}
	if !carrier.Owner.SameOwner(*command.Execution) {
		return command, fmt.Errorf("canonical native command belongs to another carrier owner")
	}
	raw, err := json.Marshal(command)
	if err != nil {
		return command, err
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != op.RequestHash {
		return command, fmt.Errorf("canonical native command hash mismatch")
	}
	return command, nil
}

func decodeNativeCanonical(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("native canonical request must contain one JSON value")
	}
	return nil
}
