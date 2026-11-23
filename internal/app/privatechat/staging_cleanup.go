package privatechat

import (
	"context"
	"database/sql"
	"fmt"
)

const stagingCleanupSchema = `
CREATE TABLE IF NOT EXISTS private_staging_cleanup (
 effect_id TEXT NOT NULL REFERENCES private_effect(id), path TEXT NOT NULL,
 event_id TEXT NOT NULL, message_id TEXT NOT NULL, sha256 TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('pending','done','retained')),
 PRIMARY KEY(effect_id,path));
`

type StagingCleanup struct {
	EffectID, Path, EventID, MessageID, SHA256 string
	Owner                                      Owner
	Generation                                 int64
}

// PrepareStagingCleanup derives the SDK event from the exact applied ingress
// that produced a confirmed local staging effect. Call before applying its
// receipt, so crash recovery can always recreate the pending cleanup work.
func (j *Journal) PrepareStagingCleanup(ctx context.Context, o Owner, generation int64, effectID, messageID string, files map[string]string) error {
	if !validOwner(o) || generation < 1 || effectID == "" || messageID == "" {
		return ErrJournalState
	}
	tx, e := j.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var eventID string
	e = tx.QueryRowContext(ctx, `SELECT i.event_id FROM private_effect e JOIN private_applied a ON a.revision=e.revision JOIN ingress i ON i.seq=a.seq WHERE e.id=? AND e.tenant=? AND e.app_id=? AND e.gateway=? AND e.surface=? AND e.chat=? AND e.user_id=? AND e.generation=? AND e.state='confirmed' AND i.tenant=e.tenant AND i.app_id=e.app_id AND i.gateway=e.gateway AND i.surface=e.surface AND i.chat=e.chat AND i.user_id=e.user_id AND i.generation=e.generation`, effectID, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&eventID)
	if e != nil {
		return e
	}
	for path, hash := range files {
		if path == "" || hash == "" {
			return ErrJournalState
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO private_staging_cleanup(effect_id,path,event_id,message_id,sha256,state) VALUES(?,?,?,?,?,'pending') ON CONFLICT(effect_id,path) DO NOTHING`, effectID, path, eventID, messageID, hash)
		if e != nil {
			return e
		}
		var savedEvent, savedMessage, savedHash string
		e = tx.QueryRowContext(ctx, `SELECT event_id,message_id,sha256 FROM private_staging_cleanup WHERE effect_id=? AND path=?`, effectID, path).Scan(&savedEvent, &savedMessage, &savedHash)
		if e != nil {
			return e
		}
		if savedEvent != eventID || savedMessage != messageID || savedHash != hash {
			return ErrJournalConflict
		}
	}
	return tx.Commit()
}
func (j *Journal) PendingStagingCleanup(ctx context.Context, limit int) ([]StagingCleanup, error) {
	if limit < 1 || limit > 128 {
		return nil, ErrJournalState
	}
	rows, e := j.db.QueryContext(ctx, `SELECT c.effect_id,c.path,c.event_id,c.message_id,c.sha256,e.tenant,e.app_id,e.gateway,e.surface,e.chat,e.user_id,e.generation FROM private_staging_cleanup c JOIN private_effect e ON e.id=c.effect_id WHERE c.state='pending' ORDER BY e.revision,e.ordinal,c.path LIMIT ?`, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []StagingCleanup
	for rows.Next() {
		var x StagingCleanup
		e = rows.Scan(&x.EffectID, &x.Path, &x.EventID, &x.MessageID, &x.SHA256, &x.Owner.TenantID, &x.Owner.AppID, &x.Owner.GatewayID, &x.Owner.SurfaceID, &x.Owner.ChatID, &x.Owner.UserID, &x.Generation)
		if e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (j *Journal) FinishStagingCleanup(ctx context.Context, work StagingCleanup, retained bool) error {
	state := "done"
	if retained {
		state = "retained"
	}
	result, e := j.db.ExecContext(ctx, `UPDATE private_staging_cleanup SET state=? WHERE effect_id=? AND path=? AND event_id=? AND message_id=? AND sha256=? AND state='pending'`, state, work.EffectID, work.Path, work.EventID, work.MessageID, work.SHA256)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return fmt.Errorf("staging cleanup: %w", sql.ErrNoRows)
	}
	return nil
}
