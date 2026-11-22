package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// A model catalog request can be queued before a wrapper completes bootstrap.
// It never starts a session or work. Such a request must not permanently pin a
// carrier whose pre-Hello failure and exact process exit have been proved.
// All other commands retain the conservative startup recovery boundary.
func (t *Tx) CarrierHasStartupBlockingCommands(incarnation string) (bool, error) {
	var exists bool
	err := t.tx.QueryRowContext(t.ctx, `SELECT EXISTS(SELECT 1 FROM native_commands WHERE incarnation=? AND COALESCE(json_extract(payload,'$.kind'),'')<>?)`, incarnation, agentproto.CommandModelList).Scan(&exists)
	return exists, err
}

func (t *Tx) NativeCommandWatermark(commandID string) (int64, error) {
	var value int64
	err := t.tx.QueryRowContext(t.ctx, `SELECT rowid FROM native_commands WHERE command_id=?`, commandID).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return value, err
}
func (t *Tx) AssertNoEffectfulCommandsAfter(incarnation string, watermark int64) error {
	if watermark <= 0 {
		return fmt.Errorf("canonical command watermark required")
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT payload FROM native_commands WHERE incarnation=? AND rowid>?`, incarnation, watermark)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return err
		}
		var command NativeCommand
		if err = json.Unmarshal([]byte(raw), &command); err != nil {
			return err
		}
		switch command.Kind {
		case agentproto.CommandRuntimeInspect, agentproto.CommandModelList, agentproto.CommandThreadGoalGet, agentproto.CommandProcessExit, agentproto.CommandTurnInterrupt, agentproto.CommandThreadGoalClear:
		default:
			return fmt.Errorf("effectful canonical command follows the quiescence proof")
		}
	}
	return rows.Err()
}
