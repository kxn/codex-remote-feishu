package privatechat

import "context"

// CallbackPublicationEligible permits recovery only for an unapplied card
// publication with no later write already in flight for the same message.
func (j *Journal) CallbackPublicationEligible(ctx context.Context, o Owner, generation int64, id string, ordinal int, message string) (bool, error) {
	if !validOwner(o) || generation < 1 || id == "" || ordinal < 0 || message == "" {
		return false, ErrJournalState
	}
	var eligible bool
	err := j.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM private_effect e WHERE e.id=? AND e.tenant=? AND e.app_id=? AND e.gateway=? AND e.surface=? AND e.chat=? AND e.user_id=? AND e.generation=? AND e.lane='interactive' AND NOT EXISTS(SELECT 1 FROM private_effect_applied a WHERE a.effect_id=e.id) AND NOT EXISTS(SELECT 1 FROM private_effect n JOIN private_publication_ops op ON op.effect_id=n.id WHERE n.tenant=e.tenant AND n.app_id=e.app_id AND n.gateway=e.gateway AND n.surface=e.surface AND n.chat=e.chat AND n.user_id=e.user_id AND n.generation=e.generation AND (n.revision>e.revision OR (n.revision=e.revision AND n.ordinal>e.ordinal) OR (n.id=e.id AND op.ordinal>?)) AND op.state IN ('sending','confirmed','unknown') AND json_extract(op.body,'$.Operation.Kind')='update_card' AND json_extract(op.body,'$.Operation.MessageID')=?) AND NOT EXISTS(SELECT 1 FROM private_callback_response c JOIN ingress i ON i.seq=c.seq WHERE i.tenant=e.tenant AND i.app_id=e.app_id AND i.gateway=e.gateway AND i.surface=e.surface AND i.chat=e.chat AND i.user_id=e.user_id AND i.generation=e.generation AND c.revision>e.revision AND json_extract(c.body,'$.target_message_id')=? AND coalesce(json_extract(c.body,'$.view_token'),'')<>''))`, id, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, ordinal, message, message).Scan(&eligible)
	return eligible, err
}

// CallbackPublicationOwner reads the frozen owner of an issued effect; callers
// must independently verify the platform binding and callback operator.
func (j *Journal) CallbackPublicationOwner(ctx context.Context, id string) (Owner, int64, error) {
	var o Owner
	var generation int64
	err := j.db.QueryRowContext(ctx, `SELECT tenant,app_id,gateway,surface,chat,user_id,generation FROM private_effect WHERE id=? AND lane='interactive'`, id).Scan(&o.TenantID, &o.AppID, &o.GatewayID, &o.SurfaceID, &o.ChatID, &o.UserID, &generation)
	return o, generation, err
}
