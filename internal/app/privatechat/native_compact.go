package privatechat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

const nativeCompactSchema = `CREATE TABLE IF NOT EXISTS private_compact_operations (command_id TEXT PRIMARY KEY REFERENCES native_commands(command_id), incarnation TEXT NOT NULL, thread_id TEXT NOT NULL, turn_id TEXT NOT NULL DEFAULT '', proof_sequence INTEGER NOT NULL DEFAULT 0, after_sequence INTEGER NOT NULL DEFAULT 0, state TEXT NOT NULL CHECK(state IN ('pending','bound','completed','failed'))); CREATE UNIQUE INDEX IF NOT EXISTS private_one_active_compact ON private_compact_operations(incarnation) WHERE state IN ('pending','bound');`

func preparePrivateCompactTx(ctx context.Context, tx *sql.Tx, l *NativeLease, command agentproto.Command) error {
	if command.Kind == agentproto.CommandPromptSend || command.Kind == agentproto.CommandThreadCompactStart {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM private_compact_operations WHERE incarnation=? AND state IN ('pending','bound') AND command_id<>?`, l.native.CarrierIncarnation, command.CommandID).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return ErrDeferred
		}
	}
	if command.Kind != agentproto.CommandThreadCompactStart {
		return nil
	}
	if command.Target.ThreadID == "" || command.NativeBinding == nil || command.NativeBinding.Validate(true) != nil {
		return ErrJournalState
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO private_compact_operations(command_id,incarnation,thread_id,after_sequence,state) VALUES(?,?,?,COALESCE((SELECT max(sequence) FROM native_events WHERE incarnation=?),0),'pending') ON CONFLICT(command_id) DO NOTHING`, command.CommandID, l.native.CarrierIncarnation, command.Target.ThreadID, l.native.CarrierIncarnation)
	if err != nil {
		return err
	}
	var incarnation, thread string
	if err = tx.QueryRowContext(ctx, `SELECT incarnation,thread_id FROM private_compact_operations WHERE command_id=?`, command.CommandID).Scan(&incarnation, &thread); err != nil {
		return err
	}
	if incarnation != l.native.CarrierIncarnation || thread != command.Target.ThreadID {
		return ErrJournalConflict
	}
	return nil
}

// Bind only an actual native contextCompaction item to the one pending,
// successfully acknowledged compact on this exclusive carrier and session.
func observePrivateCompactTx(ctx context.Context, tx *sql.Tx, l *NativeLease, event agentproto.Event) error {
	if event.Kind == agentproto.EventNativeCommandRejected || event.Kind == agentproto.EventSystemError && event.Problem != nil && event.Problem.Operation == "thread.compact.start" && event.Problem.Stage == "thread_resume_response" {
		_, err := tx.ExecContext(ctx, `UPDATE private_compact_operations SET state='failed' WHERE incarnation=? AND command_id=? AND state='pending' AND EXISTS(SELECT 1 FROM native_commands n JOIN dispatch d ON d.id=n.dispatch_id WHERE n.command_id=private_compact_operations.command_id AND d.state='failed')`, l.native.CarrierIncarnation, event.CommandID)
		return err
	}
	if event.Kind == agentproto.EventNativeControlResult && event.Action == "thread.compact.start" {
		if event.Status == "failed" {
			_, err := tx.ExecContext(ctx, `UPDATE private_compact_operations SET state='failed' WHERE incarnation=? AND command_id=? AND state='pending' AND EXISTS(SELECT 1 FROM native_commands n JOIN dispatch d ON d.id=n.dispatch_id WHERE n.command_id=private_compact_operations.command_id AND d.state='failed')`, l.native.CarrierIncarnation, event.CommandID)
			return err
		}
		if event.Status == "accepted" {
			return bindPreviouslyObservedCompactTx(ctx, tx, l, event.CommandID)
		}
	}
	if (event.Kind == agentproto.EventItemStarted || event.Kind == agentproto.EventItemCompleted) && event.ItemKind == "context_compaction" && event.ThreadID != "" && event.TurnID != "" {
		return bindCompactItemTx(ctx, tx, l, event)
	}
	if event.Kind == agentproto.EventTurnCompleted && event.ThreadID != "" && event.TurnID != "" {
		if event.Status == "failed" || event.Status == "cancelled" || event.Status == "interrupted" {
			if err := bindFailedCompactTurnTx(ctx, tx, l, event); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `UPDATE private_compact_operations SET state='completed' WHERE incarnation=? AND thread_id=? AND turn_id=? AND state='bound'`, l.native.CarrierIncarnation, event.ThreadID, event.TurnID)
		return err
	}
	return nil
}
func bindCompactItemTx(ctx context.Context, tx *sql.Tx, l *NativeLease, event agentproto.Event) error {
	var command string
	err := tx.QueryRowContext(ctx, `SELECT c.command_id FROM private_compact_operations c JOIN native_commands n ON n.command_id=c.command_id JOIN dispatch d ON d.id=n.dispatch_id WHERE c.incarnation=? AND c.thread_id=? AND c.state IN ('pending','bound') AND d.state='accepted' AND c.after_sequence<?`, l.native.CarrierIncarnation, event.ThreadID, event.Seq).Scan(&command)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var turn string
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT turn_id,state FROM private_compact_operations WHERE command_id=?`, command).Scan(&turn, &state); err != nil {
		return err
	}
	if state == "bound" {
		if turn != event.TurnID {
			return ErrJournalConflict
		}
		return nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE private_compact_operations SET state='bound',turn_id=?,proof_sequence=? WHERE command_id=? AND state='pending'`, event.TurnID, event.Seq, command)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE private_compact_operations SET state='completed' WHERE command_id=? AND EXISTS(SELECT 1 FROM native_events WHERE incarnation=? AND json_extract(payload,'$.kind')=? AND json_extract(payload,'$.threadId')=? AND json_extract(payload,'$.turnId')=?)`, command, l.native.CarrierIncarnation, string(agentproto.EventTurnCompleted), event.ThreadID, event.TurnID)
	return err
}
func bindPreviouslyObservedCompactTx(ctx context.Context, tx *sql.Tx, l *NativeLease, command string) error {
	rows, err := tx.QueryContext(ctx, `SELECT e.payload FROM native_events e JOIN private_compact_operations c ON c.incarnation=e.incarnation AND c.thread_id=json_extract(e.payload,'$.threadId') WHERE c.command_id=? AND e.sequence>c.after_sequence AND json_extract(e.payload,'$.itemKind')='context_compaction' AND json_extract(e.payload,'$.kind') IN (?,?) ORDER BY e.sequence`, command, string(agentproto.EventItemStarted), string(agentproto.EventItemCompleted))
	if err != nil {
		return err
	}
	var events []agentproto.Event
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		var e agentproto.Event
		if err = json.Unmarshal(raw, &e); err != nil {
			rows.Close()
			return err
		}
		events = append(events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, e := range events {
		if err = bindCompactItemTx(ctx, tx, l, e); err != nil {
			return err
		}
	}
	terminalRows, err := tx.QueryContext(ctx, `SELECT e.payload FROM native_events e JOIN private_compact_operations c ON c.incarnation=e.incarnation AND c.thread_id=json_extract(e.payload,'$.threadId') WHERE c.command_id=? AND e.sequence>c.after_sequence AND json_extract(e.payload,'$.kind')=? AND json_extract(e.payload,'$.status') IN ('failed','cancelled','interrupted') ORDER BY e.sequence`, command, string(agentproto.EventTurnCompleted))
	if err != nil {
		return err
	}
	events = nil
	for terminalRows.Next() {
		var raw []byte
		if err = terminalRows.Scan(&raw); err != nil {
			terminalRows.Close()
			return err
		}
		var e agentproto.Event
		if err = json.Unmarshal(raw, &e); err != nil {
			terminalRows.Close()
			return err
		}
		events = append(events, e)
	}
	err = terminalRows.Err()
	terminalRows.Close()
	if err != nil {
		return err
	}
	for _, e := range events {
		if err = observePrivateCompactTx(ctx, tx, l, e); err != nil {
			return err
		}
	}
	return nil
}

// A failed/cancelled turn can lack a contextCompaction item. Only an exact
// native remote-origin turn-start observed after the unique fenced operation,
// followed by that same terminal turn, is sufficient to identify it.
func bindFailedCompactTurnTx(ctx context.Context, tx *sql.Tx, l *NativeLease, event agentproto.Event) error {
	var command string
	var after int64
	err := tx.QueryRowContext(ctx, `SELECT c.command_id,c.after_sequence FROM private_compact_operations c JOIN native_commands n ON n.command_id=c.command_id JOIN dispatch d ON d.id=n.dispatch_id WHERE c.incarnation=? AND c.thread_id=? AND c.state='pending' AND d.state='accepted'`, l.native.CarrierIncarnation, event.ThreadID).Scan(&command, &after)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var sequence sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT max(sequence) FROM native_events WHERE incarnation=? AND sequence>? AND sequence<? AND json_extract(payload,'$.kind')=? AND json_extract(payload,'$.threadId')=? AND json_extract(payload,'$.turnId')=? AND json_extract(payload,'$.initiator.kind')=? AND json_extract(payload,'$.initiator.surfaceSessionId')=?`, l.native.CarrierIncarnation, after, event.Seq, string(agentproto.EventTurnStarted), event.ThreadID, event.TurnID, string(agentproto.InitiatorRemoteSurface), l.surface.SurfaceID).Scan(&sequence)
	if err != nil {
		return err
	}
	if !sequence.Valid {
		return nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE private_compact_operations SET state='bound',turn_id=?,proof_sequence=? WHERE command_id=? AND state='pending'`, event.TurnID, event.Seq, command)
	return err
}

func (j *Journal) compactBindingForTurn(ctx context.Context, l *NativeLease, event agentproto.Event) (*agentproto.NativeTurnBinding, string, error) {
	if event.ThreadID == "" || event.TurnID == "" {
		return nil, "", nil
	}
	var raw []byte
	var commandID string
	err := j.db.QueryRowContext(ctx, `SELECT n.payload,c.command_id FROM private_compact_operations c JOIN native_commands n ON n.command_id=c.command_id JOIN dispatch d ON d.id=n.dispatch_id WHERE c.incarnation=? AND c.thread_id=? AND c.turn_id=? AND c.state IN ('bound','completed') AND d.state='accepted'`, l.native.CarrierIncarnation, event.ThreadID, event.TurnID).Scan(&raw, &commandID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	var command agentproto.Command
	if json.Unmarshal(raw, &command) != nil || command.Kind != agentproto.CommandThreadCompactStart || command.NativeBinding == nil || command.NativeBinding.Owner != l.native || command.NativeBinding.Validate(true) != nil || command.CommandID != commandID {
		return nil, "", ErrJournalConflict
	}
	return command.NativeBinding, commandID, nil
}
