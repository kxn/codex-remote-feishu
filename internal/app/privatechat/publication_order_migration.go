package privatechat

import (
	"context"
	"database/sql"

	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

// Called inside OpenJournal's migration transaction. Only immutable effects
// and their original display chains contribute; neither checkpoints nor current
// UI state can establish the ownership of a historical publication.
func (j *Journal) backfillPublicationOrder(ctx context.Context) error {
	var after int64
	for {
		rows, err := j.db.QueryContext(ctx, `SELECT e.rowid,e.id,e.tenant,e.app_id,e.gateway,e.surface,e.chat,e.user_id,e.generation,e.body,c.chain_key FROM private_effect e LEFT JOIN private_display_chain c ON c.effect_id=e.id WHERE e.rowid>? AND e.lane='interactive' AND NOT EXISTS(SELECT 1 FROM private_publication_order k WHERE k.effect_id=e.id) ORDER BY e.rowid LIMIT 128`, after)
		if err != nil {
			return err
		}
		type record struct {
			row        int64
			id         string
			owner      Owner
			generation int64
			body       []byte
			chain      sql.NullString
		}
		var batch []record
		for rows.Next() {
			var r record
			if err = rows.Scan(&r.row, &r.id, &r.owner.TenantID, &r.owner.AppID, &r.owner.GatewayID, &r.owner.SurfaceID, &r.owner.ChatID, &r.owner.UserID, &r.generation, &r.body, &r.chain); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		for _, r := range batch {
			after = r.row
			var scopes []string
			if event, err := DecodeEffectEvent(r.body); err == nil {
				scopes = orchestrator.FrozenPrivatePublicationOrderScopes(event)
			}
			if r.chain.Valid {
				scopes = append(scopes, "display:"+r.chain.String)
			}
			for _, key := range publicationOrderKeys(r.owner, r.generation, scopes) {
				if _, err := j.db.ExecContext(ctx, `INSERT OR IGNORE INTO private_publication_order(effect_id,order_key) VALUES(?,?)`, r.id, key); err != nil {
					return err
				}
			}
		}
	}
}
