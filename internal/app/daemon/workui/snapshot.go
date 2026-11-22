package workui

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// PrepareSnapshot updates the confirmed status card, never a fixed navigation
// card or a human's menu. While a write is unresolved we leave the latest facts
// in business storage; the next poll regenerates them instead of queuing every
// intermediate state. Unknown writes retain the normal recovery path.
func PrepareSnapshot(tx *workstore.Tx, view *workstore.UIView, now time.Time, urgent bool) (bool, error) {
	views, err := tx.ListUIViews(view.ScopeID)
	if err != nil {
		return false, err
	}
	var latest *workstore.UIView
	for i := range views {
		old := &views[i]
		if old.SurfaceKey != view.SurfaceKey || old.Binding != view.Binding || old.Tenant != view.Tenant || old.ChatID != view.ChatID || old.ChannelGeneration != view.ChannelGeneration {
			continue
		}
		if latest == nil || old.CreatedAt.After(latest.CreatedAt) {
			latest = old
		}
	}
	if latest == nil {
		return true, nil
	}
	op, err := tx.GetOperation(latest.OperationID)
	if err != nil {
		return false, err
	}
	if op.State == work.OperationSending && now.Sub(op.UpdatedAt) < 2*time.Minute {
		return false, nil
	}
	if op.State == work.OperationUnknown || op.State == work.OperationSending {
		// Keep the uncertain write and its recovery record. New facts may
		// start a new status card; never patch the uncertain old message.
		return urgent || now.Sub(op.UpdatedAt) >= 30*time.Second, nil
	}
	if !urgent && op.State != work.OperationPrepared && op.State != work.OperationRetryable && now.Sub(op.UpdatedAt) < 30*time.Second {
		return false, nil
	}
	// A pending/rejected snapshot may be replaced by newer facts. Reuse its
	// last confirmed ancestor, which Queue invalidated for callback freshness.
	owner := *latest
	ownerOp := op
	for ownerOp.State != work.OperationConfirmed && owner.ReplacesViewID != "" {
		owner, err = tx.GetUIView(owner.ReplacesViewID)
		if err != nil {
			return false, err
		}
		ownerOp, err = tx.GetOperation(owner.OperationID)
		if err != nil {
			return false, err
		}
	}
	if ownerOp.State == work.OperationConfirmed && owner.ExternalMessageID != "" {
		origin := owner
		for origin.ReplacesViewID != "" {
			origin, err = tx.GetUIView(origin.ReplacesViewID)
			if err != nil {
				return false, err
			}
		}
		if now.Sub(origin.CreatedAt) < 13*24*time.Hour {
			view.ReplacesViewID = owner.ID
		}
	}
	return true, nil
}

// SnapshotReplacementAllowed relaxes callback invalidation only for an
// automatically refreshed project status whose displayed message is still
// confirmed. It cannot authorize private navigation or business action cards.
func SnapshotReplacementAllowed(tx *workstore.Tx, old, view workstore.UIView) (bool, error) {
	if old.Kind != "project.status" || view.Kind != "project.status" || old.SurfaceKey != "project.status:"+old.TargetID || view.SurfaceKey != old.SurfaceKey {
		return false, nil
	}
	views, err := tx.ListUIViews(view.ScopeID)
	if err != nil {
		return false, err
	}
	for _, candidate := range views {
		if candidate.ID == old.ID || candidate.SurfaceKey != old.SurfaceKey || candidate.CreatedAt.Before(old.CreatedAt) {
			continue
		}
		op, e := tx.GetOperation(candidate.OperationID)
		if e != nil {
			return false, e
		}
		if op.State == work.OperationSending || op.State == work.OperationUnknown || op.State == work.OperationConfirmed {
			return false, nil
		}
	}
	return true, nil
}
