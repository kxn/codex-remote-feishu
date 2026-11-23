package privatechat

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

type effectWire struct {
	Version          int                               `json:"version"`
	Event            json.RawMessage                   `json:"event"`
	CommandInputs    *orchestrator.FrozenCommandInputs `json:"command_inputs,omitempty"`
	TargetInstanceID *string                           `json:"target_instance_id,omitempty"`
}

// EncodeEffectEvent keeps only the normalized flat event representation. The
// derived Payload interface is rebuilt at decode and is never a second source.
func EncodeEffectEvent(event eventcontract.Event) (json.RawMessage, error) {
	return encodeEffect(event, nil)
}
func encodeEffect(event eventcontract.Event, inputs *orchestrator.FrozenCommandInputs) (json.RawMessage, error) {
	return encodeEffectTarget(event, inputs, nil)
}
func encodeEffectTarget(event eventcontract.Event, inputs *orchestrator.FrozenCommandInputs, target *string) (json.RawMessage, error) {
	event = event.Normalized()
	if inputs != nil && (event.Command == nil || inputs.CommandID != event.Command.CommandID || inputs.SurfaceID != event.SurfaceSessionID) {
		return nil, fmt.Errorf("effect command input identity mismatch")
	}
	if target != nil && event.Command == nil || inputs != nil && target != nil && inputs.InstanceID != *target {
		return nil, fmt.Errorf("effect target identity mismatch")
	}
	event.Payload = nil
	raw, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	delete(fields, "Payload")
	flat, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	return json.Marshal(effectWire{Version: 1, Event: flat, CommandInputs: inputs, TargetInstanceID: target})
}
func DecodeEffectEvent(raw json.RawMessage) (eventcontract.Event, error) {
	event, _, err := DecodeStoredEffect(raw)
	return event, err
}

// DecodeStoredEffect returns the immutable input manifest alongside its event.
func DecodeStoredEffect(raw json.RawMessage) (eventcontract.Event, *orchestrator.FrozenCommandInputs, error) {
	event, inputs, _, err := DecodeStoredEffectTarget(raw)
	return event, inputs, err
}

// DecodeStoredEffectTarget exposes the instance frozen at command creation.
// A nil target denotes an older effect that requires explicit reconciliation.
func DecodeStoredEffectTarget(raw json.RawMessage) (eventcontract.Event, *orchestrator.FrozenCommandInputs, *string, error) {
	var wire effectWire
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return eventcontract.Event{}, nil, nil, err
	}
	if wire.Version != 1 || len(wire.Event) == 0 {
		return eventcontract.Event{}, nil, nil, fmt.Errorf("unsupported private effect format")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(wire.Event, &fields); err != nil {
		return eventcontract.Event{}, nil, nil, err
	}
	if _, ok := fields["Payload"]; ok {
		return eventcontract.Event{}, nil, nil, fmt.Errorf("private effect has derived payload")
	}
	var event eventcontract.Event
	if err := json.Unmarshal(wire.Event, &event); err != nil {
		return eventcontract.Event{}, nil, nil, err
	}
	event = event.Normalized()
	if wire.CommandInputs != nil && (event.Command == nil || wire.CommandInputs.CommandID != event.Command.CommandID || wire.CommandInputs.SurfaceID != event.SurfaceSessionID) {
		return eventcontract.Event{}, nil, nil, fmt.Errorf("effect command input identity mismatch")
	}
	if wire.TargetInstanceID != nil && event.Command == nil || wire.CommandInputs != nil && wire.TargetInstanceID != nil && wire.CommandInputs.InstanceID != *wire.TargetInstanceID {
		return eventcontract.Event{}, nil, nil, fmt.Errorf("effect target identity mismatch")
	}
	return event, wire.CommandInputs, wire.TargetInstanceID, nil
}
