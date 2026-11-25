package privatechat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type DeadlineOwner struct {
	Owner      Owner
	Generation int64
}

// DeadlineOwnersPage enumerates persisted owners, keeping old generations out
// of the scheduler. The caller checks the active neutral binding before use.
func (j *Journal) DeadlineOwnersPage(ctx context.Context, limit, offset int) ([]DeadlineOwner, error) {
	if limit < 1 || limit > 128 || offset < 0 {
		return nil, ErrJournalState
	}
	rows, err := j.db.QueryContext(ctx, `SELECT tenant,app_id,gateway,surface,chat,user_id,generation FROM private_checkpoint c WHERE NOT EXISTS (SELECT 1 FROM private_checkpoint n WHERE n.tenant=c.tenant AND n.app_id=c.app_id AND n.gateway=c.gateway AND n.surface=c.surface AND n.chat=c.chat AND n.user_id=c.user_id AND n.generation>c.generation) ORDER BY tenant,app_id,gateway,surface,chat,user_id LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeadlineOwner
	for rows.Next() {
		var v DeadlineOwner
		if err = rows.Scan(&v.Owner.TenantID, &v.Owner.AppID, &v.Owner.GatewayID, &v.Owner.SurfaceID, &v.Owner.ChatID, &v.Owner.UserID, &v.Generation); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ApplyOwnerDeadline requires no native lease: it changes only the exact
// authenticated owner checkpoint and persists all resulting host effects.
func (p *Processor) ApplyOwnerDeadline(ctx context.Context, o Owner, generation int64, tickID string, now time.Time) ([]EffectRecord, error) {
	unlock := p.journal.LockReferenceMutation()
	defer unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	projectionCtx, projectionErr := beginProjection(ctx)
	if projectionErr != nil {
		return nil, projectionErr
	}
	ctx = projectionCtx
	if p.poisoned {
		return nil, ErrProcessorPoisoned
	}
	if !validOwner(o) || generation < 1 || tickID == "" || now.IsZero() {
		return nil, ErrJournalState
	}
	ref := tickRef{ID: "owner-deadline:" + tickID, At: now.UTC().Format(time.RFC3339Nano)}
	var revision int64
	var observed string
	err := p.journal.db.QueryRowContext(ctx, `SELECT revision,observed_at FROM private_tick_applied WHERE tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? AND tick_id=?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, ref.ID).Scan(&revision, &observed)
	if err == nil {
		if observed != ref.At {
			return nil, ErrJournalConflict
		}
		return p.journal.processorEffects(ctx, o, generation, revision)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	state, err := p.ensureOwner(ctx, o, generation)
	if err != nil {
		return nil, err
	}
	if !p.app.Service().PrivateOwnerDeadlineDue(o.SurfaceID, now) {
		return nil, nil
	}
	events := p.app.Service().TickPrivateOwnerDeadlines(o.SurfaceID, now)
	effects, err := p.freezeEffects(o, generation, ref.ID, events)
	if err != nil {
		p.poisoned = true
		return nil, err
	}
	core, err := p.app.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		p.poisoned = true
		return nil, err
	}
	cp := Checkpoint{Owner: o, Generation: generation, Revision: state.revision + 1, Cursor: state.cursor, Core: core}
	if err = p.journal.commitCheckpointExtended(ctx, cp, state.revision, effects, 0, nil, nil, nil, nil, nil, nil, &ref); err != nil {
		p.poisoned = true
		return nil, fmt.Errorf("%w: owner deadline: %v", ErrProcessorPoisoned, err)
	}
	p.owners[o] = processorOwner{cp.Revision, cp.Cursor, generation}
	return p.journal.processorEffects(ctx, o, generation, cp.Revision)
}

// ApplyOwnerLaunchExit commits an exact, host-verified process-exit receipt
// together with the failed launch projection and its durable stop effects.
func (p *Processor) ApplyOwnerLaunchExit(ctx context.Context, o Owner, generation int64, instanceID, receipt string, now time.Time) ([]EffectRecord, error) {
	unlock := p.journal.LockReferenceMutation()
	defer unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	projectionCtx, projectionErr := beginProjection(ctx)
	if projectionErr != nil {
		return nil, projectionErr
	}
	ctx = projectionCtx
	if p.poisoned {
		return nil, ErrProcessorPoisoned
	}
	if !validOwner(o) || generation < 1 || instanceID == "" || receipt == "" || now.IsZero() {
		return nil, ErrJournalState
	}
	ref := tickRef{ID: "owner-exit:" + receipt, At: now.UTC().Format(time.RFC3339Nano)}
	var revision int64
	var observed string
	err := p.journal.db.QueryRowContext(ctx, `SELECT revision,observed_at FROM private_tick_applied WHERE tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? AND tick_id=?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, ref.ID).Scan(&revision, &observed)
	if err == nil {
		return p.journal.processorEffects(ctx, o, generation, revision)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	current, err := p.ensureOwner(ctx, o, generation)
	if err != nil {
		return nil, err
	}
	events := p.app.Service().PrivateOwnerLaunchExited(o.SurfaceID, instanceID)
	if len(events) == 0 {
		return nil, nil
	}
	effects, err := p.freezeEffects(o, generation, ref.ID, events)
	if err != nil {
		p.poisoned = true
		return nil, err
	}
	core, err := p.app.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		p.poisoned = true
		return nil, err
	}
	cp := Checkpoint{Owner: o, Generation: generation, Revision: current.revision + 1, Cursor: current.cursor, Core: core}
	if err = p.journal.commitCheckpointExtended(ctx, cp, current.revision, effects, 0, nil, nil, nil, nil, nil, nil, &ref); err != nil {
		p.poisoned = true
		return nil, fmt.Errorf("%w: owner exit: %v", ErrProcessorPoisoned, err)
	}
	p.owners[o] = processorOwner{cp.Revision, cp.Cursor, generation}
	return p.journal.processorEffects(ctx, o, generation, cp.Revision)
}

func (p *Processor) OwnerPendingLaunchInstance(ctx context.Context, o Owner, generation int64) (string, error) {
	unlock := p.journal.LockReferenceMutation()
	defer unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return "", ErrProcessorPoisoned
	}
	if !validOwner(o) || generation < 1 {
		return "", ErrJournalState
	}
	if _, err := p.ensureOwner(ctx, o, generation); err != nil {
		return "", err
	}
	return p.app.Service().PrivatePendingLaunchInstance(o.SurfaceID), nil
}
