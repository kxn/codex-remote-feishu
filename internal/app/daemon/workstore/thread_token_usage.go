package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type ThreadTokenUsageObservation struct {
	Usage      *agentproto.ThreadTokenUsage
	ObservedAt time.Time
}

func (t *Tx) RecordThreadTokenUsage(incarnation, thread string, usage *agentproto.ThreadTokenUsage, now time.Time) error {
	if incarnation == "" || thread == "" || usage == nil || now.IsZero() {
		return fmt.Errorf("exact carrier/thread usage observation required")
	}
	raw, err := json.Marshal(ThreadTokenUsageObservation{Usage: agentproto.CloneThreadTokenUsage(usage), ObservedAt: now})
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO native_thread_token_usage(incarnation,session_id,payload) VALUES(?,?,?) ON CONFLICT(incarnation,session_id) DO UPDATE SET payload=excluded.payload`, incarnation, thread, string(raw))
	return err
}
func (t *Tx) GetThreadTokenUsage(incarnation, thread string) (ThreadTokenUsageObservation, error) {
	var result ThreadTokenUsageObservation
	var raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT payload FROM native_thread_token_usage WHERE incarnation=? AND session_id=?`, incarnation, thread).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal([]byte(raw), &result)
	return result, err
}
