package privatechat

import "context"

// RecentEventIDs returns only this authenticated owner's durable ingress keys.
// The host must verify the corresponding immutable SDK record before using it
// as an identity witness; entries here are not themselves platform authority.
func (j *Journal) RecentEventIDs(ctx context.Context, o Owner, generation int64, limit int) ([]string, error) {
	if !validOwner(o) || generation < 1 || limit < 1 || limit > 1000 {
		return nil, ErrJournalState
	}
	rows, err := j.db.QueryContext(ctx, `SELECT event_id FROM ingress WHERE tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? ORDER BY seq DESC LIMIT ?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
