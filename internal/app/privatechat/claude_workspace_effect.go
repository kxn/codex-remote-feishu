package privatechat

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

// EarlierClaudeWorkspaceEffectPending orders writes to the same legacy store
// key across all private owners, using durable insertion order.
func (j *Journal) EarlierClaudeWorkspaceEffectPending(ctx context.Context, o Owner, generation int64, id, key string) (bool, error) {
	if !validOwner(o) || generation < 1 || id == "" || key == "" {
		return false, ErrJournalState
	}
	var pending int
	err := j.db.QueryRowContext(ctx, `SELECT count(*) FROM private_effect current JOIN private_effect prior ON prior.rowid < current.rowid WHERE current.id=? AND current.tenant=? AND current.app_id=? AND current.gateway=? AND current.surface=? AND current.chat=? AND current.user_id=? AND current.generation=? AND json_extract(prior.body,'$.event.DaemonCommand.Kind')=? AND json_extract(json_extract(prior.body,'$.event.DaemonCommand.Text'),'$.Key')=? AND prior.state<>'confirmed'`, id, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, control.DaemonCommandPrivateClaudeWorkspace, key).Scan(&pending)
	return pending != 0, err
}

func (j *Journal) restoreClaudeWorkspaceOverlay(ctx context.Context, app *App, o Owner, generation int64) error {
	rows, err := j.db.QueryContext(ctx, `SELECT e.body FROM private_effect e LEFT JOIN private_effect_applied a ON a.effect_id=e.id WHERE e.tenant=? AND e.app_id=? AND e.gateway=? AND e.surface=? AND e.chat=? AND e.user_id=? AND e.generation=? AND json_extract(e.body,'$.event.DaemonCommand.Kind')=? AND a.effect_id IS NULL ORDER BY e.rowid`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, control.DaemonCommandPrivateClaudeWorkspace)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return err
		}
		event, _, err := DecodeStoredEffect(raw)
		if err != nil {
			return err
		}
		var change orchestrator.PrivateClaudeSnapshotChange
		if event.DaemonCommand == nil || event.SurfaceSessionID != o.SurfaceID || json.Unmarshal([]byte(event.DaemonCommand.Text), &change) != nil || change.SurfaceID != o.SurfaceID || change.Key == "" || change.Token == "" {
			return fmt.Errorf("invalid private Claude workspace effect")
		}
		app.Service().SetPrivateClaudeSnapshotOverlay(change)
	}
	return rows.Err()
}
