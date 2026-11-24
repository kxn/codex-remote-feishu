package privatechat

import (
	"context"
	"database/sql"
	"errors"
)

// LegacyStageRouteDiscarded checks the effect's immutable applied ingress
// against subsequently successful route-discard controls for the same owner.
// Missing or ambiguous provenance cannot authorize replay of a legacy stage.
func (j *Journal) LegacyStageRouteDiscarded(ctx context.Context, effectID string) (bool, error) {
	if j == nil || effectID == "" {
		return true, ErrJournalState
	}
	rows, err := j.db.QueryContext(ctx, `SELECT i.seq,i.origin_router,i.origin_seq FROM private_effect e JOIN private_applied a ON a.revision=e.revision AND a.canceled_by IS NULL JOIN ingress i ON i.seq=a.seq WHERE e.id=? AND i.tenant=e.tenant AND i.app_id=e.app_id AND i.gateway=e.gateway AND i.surface=e.surface AND i.chat=e.chat AND i.user_id=e.user_id AND i.generation=e.generation AND i.lane='input'`, effectID)
	if err != nil {
		return true, err
	}
	var source int64
	var router string
	var origin int64
	count := 0
	for rows.Next() {
		if err = rows.Scan(&source, &router, &origin); err != nil {
			rows.Close()
			return true, err
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return true, err
	}
	if count != 1 {
		return true, nil
	}
	var discarded bool
	err = j.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM private_effect e JOIN ingress c ON c.tenant=e.tenant AND c.app_id=e.app_id AND c.gateway=e.gateway AND c.surface=e.surface AND c.chat=e.chat AND c.user_id=e.user_id AND c.generation=e.generation JOIN private_applied a ON a.seq=c.seq AND a.route_discard=1 WHERE e.id=? AND ((?<>'' AND c.origin_router=? AND c.origin_seq>?) OR ((?='' OR c.origin_router='') AND c.seq>?) OR (?<>'' AND c.origin_router<>'' AND c.origin_router<>? AND c.seq>?)))`, effectID, router, router, origin, router, source, router, router, source).Scan(&discarded)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	return discarded, err
}
