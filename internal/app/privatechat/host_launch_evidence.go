package privatechat

import (
	"context"
	"encoding/json"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

// ProveCancelledLaunchWithoutCarrier verifies a completed, applied local launch
// failure for this exact instance. The host must separately prove its carrier
// store has no record; an absent or unfinished launch is not proof.
func (j *Journal) ProveCancelledLaunchWithoutCarrier(ctx context.Context, o Owner, generation int64, instance string) error {
	if j == nil || !validOwner(o) || generation < 1 || instance == "" {
		return ErrJournalState
	}
	rows, err := j.db.QueryContext(ctx, `SELECT e.id,e.body,e.state,e.receipt,EXISTS(SELECT 1 FROM private_effect_applied a WHERE a.effect_id=e.id) FROM private_effect e WHERE e.tenant=? AND e.app_id=? AND e.gateway=? AND e.surface=? AND e.chat=? AND e.user_id=? AND e.generation=?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation)
	if err != nil {
		return err
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var id, state string
		var body, receipt []byte
		var applied int
		if err = rows.Scan(&id, &body, &state, &receipt, &applied); err != nil {
			return err
		}
		event, _, e := DecodeStoredEffect(body)
		if e != nil {
			return e
		}
		if event.DaemonCommand == nil || event.DaemonCommand.Kind != control.DaemonCommandStartHeadless || event.DaemonCommand.InstanceID != instance {
			continue
		}
		if found {
			return ErrJournalConflict
		}
		found = true
		var fact struct {
			Version              int
			EffectID, InstanceID string
			PID                  int
			Success              bool
			Problem              string
		}
		if state != "confirmed" || applied != 1 || json.Unmarshal(receipt, &fact) != nil || fact.Version != 1 || fact.EffectID != id || fact.InstanceID != instance || fact.Success || fact.PID != 0 || fact.Problem != "headless launch superseded" {
			return ErrDeferred
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if !found {
		return ErrDeferred
	}
	return nil
}
