package runtimejournal

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

var ErrPrivateCompactNotIdle = errors.New("private compact requires verified idle native session")

// privateCompactIdleEvidence runs in the same transaction that freezes the
// frame watermark. A started native turn must have its matching terminal in
// the journal, whose sequence follows stdout observation order.
func privateCompactIdleEvidence(tx *sql.Tx, incarnation, thread string) error {
	if thread == "" {
		return ErrConflict
	}
	var outstanding int
	err := tx.QueryRow(`SELECT count(*) FROM events started WHERE started.incarnation=? AND json_extract(started.payload,'$.kind')=? AND json_extract(started.payload,'$.threadId')=? AND NOT EXISTS (SELECT 1 FROM events terminal WHERE terminal.incarnation=started.incarnation AND terminal.sequence>started.sequence AND json_extract(terminal.payload,'$.kind')=? AND json_extract(terminal.payload,'$.threadId')=? AND json_extract(terminal.payload,'$.turnId')=json_extract(started.payload,'$.turnId'))`, incarnation, string(agentproto.EventTurnStarted), thread, string(agentproto.EventTurnCompleted), thread).Scan(&outstanding)
	if err != nil {
		return err
	}
	if outstanding != 0 {
		return fmt.Errorf("compact requires a durable terminal for the previous native turn: %w", ErrPrivateCompactNotIdle)
	}
	var unproven int
	err = tx.QueryRow(`SELECT count(*) FROM frames f JOIN commands c ON c.id=f.command_id WHERE c.incarnation=? AND f.native_method='turn/start' AND f.stage IN ('child_write_started','written','unknown') AND COALESCE(json_extract(c.payload,'$.target.threadId'),'') IN (?, '') AND NOT EXISTS (SELECT 1 FROM events binding JOIN events terminal ON terminal.incarnation=binding.incarnation AND json_extract(terminal.payload,'$.kind')=? AND json_extract(terminal.payload,'$.threadId')=json_extract(binding.payload,'$.threadId') AND json_extract(terminal.payload,'$.turnId')=json_extract(binding.payload,'$.turnId') AND EXISTS (SELECT 1 FROM events started WHERE started.incarnation=terminal.incarnation AND started.sequence<terminal.sequence AND json_extract(started.payload,'$.kind')='turn.started' AND json_extract(started.payload,'$.threadId')=json_extract(terminal.payload,'$.threadId') AND json_extract(started.payload,'$.turnId')=json_extract(terminal.payload,'$.turnId')) WHERE binding.incarnation=c.incarnation AND json_extract(binding.payload,'$.kind')=? AND json_extract(binding.payload,'$.commandId')=c.id AND (COALESCE(json_extract(c.payload,'$.target.threadId'),'')='' OR json_extract(binding.payload,'$.threadId')=?) AND json_extract(binding.payload,'$.status')='accepted')`, incarnation, thread, string(agentproto.EventTurnCompleted), string(agentproto.EventNativeTurnBindingObserved), thread).Scan(&unproven)
	if err != nil {
		return err
	}
	if unproven != 0 {
		return fmt.Errorf("compact requires exact terminal evidence for every prior prompt write: %w", ErrPrivateCompactNotIdle)
	}
	return nil
}
