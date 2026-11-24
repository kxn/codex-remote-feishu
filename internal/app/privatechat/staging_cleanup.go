package privatechat

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

const stagingCleanupSchema = `
CREATE TABLE IF NOT EXISTS private_staging_cleanup (
 effect_id TEXT NOT NULL REFERENCES private_effect(id), path TEXT NOT NULL,
 event_id TEXT NOT NULL, message_id TEXT NOT NULL, sha256 TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('pending','done','retained')),
 PRIMARY KEY(effect_id,path));
CREATE TABLE IF NOT EXISTS private_canceled_staging_cleanup (
 ingress_seq INTEGER NOT NULL REFERENCES ingress(seq), path TEXT NOT NULL,
 message_id TEXT NOT NULL, state TEXT NOT NULL CHECK(state IN ('pending','done','retained')),
 PRIMARY KEY(ingress_seq,path));
`

type StagingCleanup struct {
	EffectID, Path, EventID, MessageID, SHA256 string
	Owner                                      Owner
	Generation                                 int64
	IngressSequence                            int64
}

// The canceled ingress itself is the only authority. Cleanup still validates
// the SDK manifest before removing any path.
func insertCanceledStagingCleanup(ctx context.Context, tx *sql.Tx, seq int64) error {
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT i.payload FROM ingress i JOIN private_applied a ON a.seq=i.seq WHERE i.seq=? AND a.canceled_by IS NOT NULL`, seq).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	var action control.Action
	if err = json.Unmarshal(raw, &action); err != nil {
		return err
	}
	if action.MessageID == "" {
		return nil
	}
	paths := map[string]bool{}
	if action.LocalPath != "" {
		paths[action.LocalPath] = true
	}
	for _, f := range action.Files {
		if f.LocalPath != "" {
			paths[f.LocalPath] = true
		}
	}
	for _, input := range action.Inputs {
		if input.Type == agentproto.InputLocalImage && input.Path != "" {
			paths[input.Path] = true
		}
	}
	for path := range paths {
		if _, err = tx.ExecContext(ctx, `INSERT INTO private_canceled_staging_cleanup(ingress_seq,path,message_id,state) VALUES(?,?,?,'pending') ON CONFLICT DO NOTHING`, seq, path, action.MessageID); err != nil {
			return err
		}
	}
	return nil
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
		if path == "" {
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
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if e = rows.Close(); e != nil {
		return nil, e
	}
	if len(out) >= limit {
		return out, nil
	}
	rows, e = j.db.QueryContext(ctx, `SELECT c.ingress_seq,c.path,i.event_id,c.message_id,i.tenant,i.app_id,i.gateway,i.surface,i.chat,i.user_id,i.generation FROM private_canceled_staging_cleanup c JOIN ingress i ON i.seq=c.ingress_seq JOIN private_applied a ON a.seq=i.seq AND a.canceled_by IS NOT NULL WHERE c.state='pending' ORDER BY i.seq,c.path LIMIT ?`, limit-len(out))
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var x StagingCleanup
		if e = rows.Scan(&x.IngressSequence, &x.Path, &x.EventID, &x.MessageID, &x.Owner.TenantID, &x.Owner.AppID, &x.Owner.GatewayID, &x.Owner.SurfaceID, &x.Owner.ChatID, &x.Owner.UserID, &x.Generation); e != nil {
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
	var result sql.Result
	var e error
	if work.IngressSequence != 0 {
		result, e = j.db.ExecContext(ctx, `UPDATE private_canceled_staging_cleanup SET state=? WHERE ingress_seq=? AND path=? AND message_id=? AND state='pending'`, state, work.IngressSequence, work.Path, work.MessageID)
	} else {
		result, e = j.db.ExecContext(ctx, `UPDATE private_staging_cleanup SET state=? WHERE effect_id=? AND path=? AND event_id=? AND message_id=? AND sha256=? AND state='pending'`, state, work.EffectID, work.Path, work.EventID, work.MessageID, work.SHA256)
	}
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
