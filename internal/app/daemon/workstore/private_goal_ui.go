package workstore

import (
	"encoding/json"
	"reflect"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (t *Tx) ListPrivateGoalUI() ([]work.Operation, error) {
	return listRecords[work.Operation](t, "operations", ` WHERE id IN (SELECT o.id FROM operations o WHERE (kind='private.goal.intent-ui' OR (kind='native.user.goal' AND EXISTS (SELECT 1 FROM json_each(o.payload,'$.steps') p WHERE json_extract(p.value,'$.id')='ui_pending'))) AND NOT EXISTS (SELECT 1 FROM json_each(o.payload,'$.steps') s WHERE json_extract(s.value,'$.id')='ui_result') ORDER BY json_extract(o.payload,'$.updated_at'),id LIMIT 32)`)
}

// NativeGoalCommandResult exposes only the exact committed response registered
// to this command/session/owner. A previous Goal reply cannot finish a new UI.
func (t *Tx) NativeGoalCommandResult(commandID string) (agentproto.Event, error) {
	var event agentproto.Event
	command, err := t.GetNativeCommand(commandID)
	if err != nil {
		return event, err
	}
	if command.Kind != agentproto.CommandThreadGoalGet && command.Kind != agentproto.CommandThreadGoalSet && command.Kind != agentproto.CommandThreadGoalClear {
		return event, ErrScopeDenied
	}
	op, err := t.GetOperation(commandID)
	if err != nil {
		return event, err
	}
	if op.ExternalReceipt == "" {
		return event, ErrNotFound
	}
	body, err := t.GetContent(op.ScopeID, op.ExternalReceipt)
	if err != nil {
		return event, err
	}
	if err = json.Unmarshal(body.Data, &event); err != nil {
		return event, err
	}
	if event.Kind != agentproto.EventThreadGoalCommandResult || event.CommandID != commandID || event.Journal == nil || event.Journal.CarrierIncarnation != command.Incarnation || event.Execution == nil || *event.Execution != command.Binding || event.ThreadID != command.SessionID {
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
