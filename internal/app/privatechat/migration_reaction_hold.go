package privatechat

import (
	"context"
	"encoding/json"
)

// HasUncertainMigratedReaction isolates only the exact old message whose
// source HTTP write has no receipt. Other private messages remain usable.
func (j *Journal) HasUncertainMigratedReaction(ctx context.Context, o Owner, generation int64, messageID string) (bool, error) {
	if !validOwner(o) || generation < 1 || messageID == "" {
		return false, ErrJournalState
	}
	rows, err := j.db.QueryContext(ctx, `SELECT r.payload,r.payload_hash FROM private_migration_records r JOIN private_migration_imports i ON i.import_id=r.import_id WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=? AND r.source_table='personal_migration_reaction_holds'`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var hash string
		if err = rows.Scan(&raw, &hash); err != nil {
			return false, err
		}
		var hold struct {
			Reaction struct {
				Message struct {
					Tenant string `json:"tenant"`
					Chat   string `json:"chat_id"`
					ID     string `json:"message_id"`
				} `json:"message"`
			} `json:"reaction_payload"`
		}
		if migrationSHA(raw) != hash || json.Unmarshal(raw, &hold) != nil || hold.Reaction.Message.Tenant == "" || hold.Reaction.Message.Chat == "" || hold.Reaction.Message.ID == "" {
			return false, ErrJournalConflict
		}
		if hold.Reaction.Message.Tenant == o.TenantID && hold.Reaction.Message.Chat == o.ChatID && hold.Reaction.Message.ID == messageID {
			return true, nil
		}
	}
	return false, rows.Err()
}
