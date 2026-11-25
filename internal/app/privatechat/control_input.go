package privatechat

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

// verifyControlInput binds synthesized prompt input to the exact authorized
// control ingress, never to the platform card being updated.
type controlInputQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func verifyControlInput(ctx context.Context, tx controlInputQuerier, o Owner, generation int64, eventID string, cursor int64, requireApplied bool) (int64, error) {
	if eventID == "" {
		return 0, ErrJournalState
	}
	var seq int64
	var payload []byte
	var applied int
	err := tx.QueryRowContext(ctx, `SELECT i.seq,i.payload,EXISTS(SELECT 1 FROM private_applied a WHERE a.seq=i.seq AND a.canceled_by IS NULL) FROM ingress i WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=? AND i.event_id=? AND i.lane='control'`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, eventID).Scan(&seq, &payload, &applied)
	if err != nil {
		return 0, fmt.Errorf("control input provenance: %w", err)
	}
	var action control.Action
	if json.Unmarshal(payload, &action) != nil || (action.Kind != control.ActionReviewApply && (action.Kind != control.ActionPlanProposalDecision || !control.PlanProposalExecutes(action.OptionID))) || action.ActorUserID != o.UserID || action.SurfaceSessionID != o.SurfaceID || (cursor > 0 && seq > cursor) || (requireApplied && applied != 1) {
		return 0, ErrJournalConflict
	}
	return seq, nil
}
