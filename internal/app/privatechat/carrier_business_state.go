package privatechat

import "context"

// CarrierHasUnsettledBusiness protects an old session from automatic resume
// while a business dispatch still lacks a durable outcome.
func (j *Journal) CarrierHasUnsettledBusiness(ctx context.Context, o Owner, generation int64, incarnation string) (bool, error) {
	if !validOwner(o) || generation < 1 || incarnation == "" {
		return false, ErrJournalState
	}
	var count int
	err := j.db.QueryRowContext(ctx, `SELECT count(*) FROM dispatch WHERE tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? AND carrier=? AND lane IN ('input','control') AND state IN ('prepared','sending','unknown','sent')`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, incarnation).Scan(&count)
	return count != 0, err
}
