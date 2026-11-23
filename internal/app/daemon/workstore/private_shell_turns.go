package workstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// These queries return possible writes only to defer classification, never to
// infer an event's owner. ActualTurnID is assigned solely by a proven item.
func (t *Tx) HasUnassignedPrivateShellWrites(incarnation, session string) (bool, error) {
	var n int
	err := t.tx.QueryRowContext(t.ctx, `SELECT count(*) FROM private_shell_operations s JOIN native_commands c ON c.command_id=s.command_id WHERE c.incarnation=? AND json_extract(s.payload,'$.session_id')=? AND json_extract(s.payload,'$.state')<>'rejected' AND coalesce(json_extract(s.payload,'$.actual_turn_id'),'')=''`, incarnation, session).Scan(&n)
	return n > 0, err
}
func (t *Tx) PrivateShellOperationsForTurn(incarnation, session, turn string) ([]PrivateShellOperation, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT payload FROM private_shell_operations WHERE json_extract(payload,'$.incarnation')=? AND json_extract(payload,'$.session_id')=? AND json_extract(payload,'$.actual_turn_id')=? ORDER BY id`, incarnation, session, turn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []PrivateShellOperation
	for rows.Next() {
		var raw string
		var v PrivateShellOperation
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, rows.Err()
}
func (t *Tx) HasPendingPrivateShellTurns(incarnation string) (bool, error) {
	var n int
	err := t.tx.QueryRowContext(t.ctx, `SELECT count(*) FROM private_shell_operations s JOIN native_commands c ON c.command_id=s.command_id WHERE c.incarnation=? AND json_extract(s.payload,'$.state')<>'rejected' AND (coalesce(json_extract(s.payload,'$.actual_turn_id'),'')='' OR (json_type(s.payload,'$.turn_terminal_source') IS NULL AND NOT EXISTS(SELECT 1 FROM native_turns t WHERE t.carrier_incarnation=c.incarnation AND t.session_id=json_extract(s.payload,'$.session_id') AND t.turn_id=json_extract(s.payload,'$.actual_turn_id'))))`, incarnation).Scan(&n)
	return n > 0, err
}

func (t *Tx) RecordPrivateShellTurnBoundary(id string, event agentproto.Event, now time.Time) error {
	v, err := t.GetPrivateShellOperation(id)
	if err != nil {
		return err
	}
	if event.Journal == nil || event.Journal.CarrierIncarnation != v.Incarnation || event.ThreadID != v.SessionID || event.TurnID == "" || event.TurnID != v.ActualTurnID || v.ItemID == "" || v.ReceiptRef == "" || (event.Kind != agentproto.EventTurnStarted && event.Kind != agentproto.EventTurnCompleted) || (event.CommandID != "" && event.CommandID != v.CommandID) || (event.Execution != nil && *event.Execution != v.Execution) {
		return ErrScopeDenied
	}
	if _, e := t.GetNativeTurn(v.Incarnation, v.SessionID, v.ActualTurnID); e == nil {
		return ErrScopeDenied
	} else if !errors.Is(e, ErrNotFound) {
		return e
	}
	saved, err := t.GetRuntimeEvent(v.Incarnation, event.Journal.Sequence)
	if err != nil {
		return err
	}
	actual, err := agentproto.RuntimeEventPayloadHash(saved)
	if err != nil {
		return err
	}
	incoming, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil {
		return err
	}
	if saved.Journal == nil || *saved.Journal != *event.Journal || actual != incoming || incoming != event.Journal.PayloadHash {
		return ErrScopeDenied
	}
	native, err := t.GetNativeCommand(v.CommandID)
	if err != nil {
		return err
	}
	if native.Incarnation != v.Incarnation || native.Binding != v.Execution {
		return ErrScopeDenied
	}
	var previous *work.SourceRef
	if event.Kind == agentproto.EventTurnStarted {
		previous = v.TurnStartedSource
	} else {
		previous = v.TurnTerminalSource
	}
	if previous != nil {
		if event.Kind == agentproto.EventTurnCompleted {
			content, e := t.GetContent(v.ScopeID, previous.EvidenceRef)
			if e != nil {
				return e
			}
			var old agentproto.Event
			if e = json.Unmarshal(content.Data, &old); e != nil {
				return e
			}
			if old.Status != event.Status {
				return ErrConflict
			}
		}
		return t.ResolveQuarantine(v.Incarnation, event.Journal.Sequence)
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	content := work.NewContent(fmt.Sprintf("shell-turn:%s:%d", v.Incarnation, event.Journal.Sequence), v.ScopeID, "application/vnd.codex-remote.private-shell-turn+json", raw, now)
	if err = t.PutContent(content); err != nil {
		return err
	}
	source := &work.SourceRef{Kind: string(event.Kind), ID: content.ID, Version: event.Journal.PayloadHash, EvidenceRef: content.ID}
	before := v.Revision
	v.Revision++
	v.UpdatedAt = now
	if event.Kind == agentproto.EventTurnStarted {
		v.TurnStartedSource = source
	} else {
		if event.Status != "completed" && event.Status != "failed" && event.Status != "interrupted" {
			return ErrScopeDenied
		}
		v.TurnTerminalSource = source
	}
	if err = t.savePrivateShellOperation(v, before); err != nil {
		return err
	}
	return t.ResolveQuarantine(v.Incarnation, event.Journal.Sequence)
}
