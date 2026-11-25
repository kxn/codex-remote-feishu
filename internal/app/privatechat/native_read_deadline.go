package privatechat

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// NativeReadDeadline reads the deadline frozen with this owner's dispatch.
// Zero means this predates bounded observations; callers must not invent one.
func (j *Journal) NativeReadDeadline(ctx context.Context, o Owner, generation int64, dispatch string) (time.Time, error) {
	var ns int64
	err := j.db.QueryRowContext(ctx, `SELECT r.expires_ns FROM private_read_observations r JOIN dispatch d ON d.id=r.dispatch_id WHERE d.id=? AND d.tenant=? AND d.app_id=? AND d.gateway=? AND d.surface=? AND d.chat=? AND d.user_id=? AND d.generation=?`, dispatch, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&ns)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, ns), nil
}
