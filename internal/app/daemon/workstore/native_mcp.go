package workstore

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (t *Tx) NativeMCPCommandResult(commandID string) (agentproto.Event, error) {
	var event agentproto.Event
	command, err := t.GetNativeCommand(commandID)
	if err != nil {
		return event, err
	}
	if command.Kind != agentproto.CommandRuntimeMCPRead && command.Kind != agentproto.CommandMCPOAuthLogin {
		return event, fmt.Errorf("command is not an MCP query or login")
	}
	op, err := t.GetOperation(commandID)
	if err != nil {
		return event, err
	}
	if op.ExternalReceipt == "" {
		return event, ErrNotFound
	}
	content, err := t.GetContent(op.ScopeID, op.ExternalReceipt)
	if err != nil {
		return event, err
	}
	if err = json.Unmarshal(content.Data, &event); err != nil {
		return event, err
	}
	if event.Journal == nil || event.CommandID != commandID || event.Journal.CarrierIncarnation != command.Incarnation || event.Execution == nil || *event.Execution != command.Binding {
		return event, ErrScopeDenied
	}
	persisted, err := t.GetRuntimeEvent(command.Incarnation, event.Journal.Sequence)
	if err != nil {
		return event, err
	}
	if !reflect.DeepEqual(persisted, event) {
		return event, ErrScopeDenied
	}
	return event, nil
}
