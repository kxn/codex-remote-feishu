package privatechat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type settingsReconcileRef struct{ Version string }

// PendingSettingsReconciliations enumerates durable per-owner version tasks.
func (j *Journal) PendingSettingsReconciliations(ctx context.Context, o Owner, generation int64, limit int) ([]string, error) {
	if !validOwner(o) || generation < 1 || limit < 1 || limit > 64 {
		return nil, ErrJournalState
	}
	rows, err := j.db.QueryContext(ctx, `SELECT version FROM private_settings_reconcile WHERE tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? AND state='pending' ORDER BY rowid LIMIT ?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var versions []string
	for rows.Next() {
		var v string
		if err = rows.Scan(&v); err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

// ApplySettingsReconcile runs after the host reads and projects the current
// authoritative config outside its owner lock. The host must validate the
// target owner lease and provide its exact current config fingerprint.
func (p *Processor) ApplySettingsReconcile(ctx context.Context, o Owner, generation int64, version, current string, stillCurrent func() bool, guard ExecutionGuard) ([]EffectRecord, error) {
	unlockReference := p.journal.LockReferenceMutation()
	defer unlockReference()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return nil, ErrProcessorPoisoned
	}
	if !validOwner(o) || generation < 1 {
		return nil, ErrJournalState
	}
	if _, err := p.ensureOwner(ctx, o, generation); err != nil {
		return nil, err
	}
	var out []EffectRecord
	err := p.withSurfaceExecutionGuard(o.SurfaceID, guard, func() error {
		var e error
		out, e = p.applySettingsReconcileLocked(ctx, o, generation, version, current, stillCurrent)
		return e
	})
	return out, err
}

func (p *Processor) applySettingsReconcileLocked(ctx context.Context, o Owner, generation int64, version, current string, stillCurrent func() bool) ([]EffectRecord, error) {
	if !validOwner(o) || generation < 1 || version == "" || current == "" || stillCurrent == nil || !stillCurrent() {
		return nil, ErrJournalState
	}
	var status string
	var revision int64
	err := p.journal.db.QueryRowContext(ctx, `SELECT state,applied_revision FROM private_settings_reconcile WHERE version=? AND tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=?`, version, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&status, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrJournalConflict
	}
	if err != nil {
		return nil, err
	}
	if status == "applied" {
		return p.journal.processorEffects(ctx, o, generation, revision)
	}
	if status == "superseded" {
		return nil, nil
	}
	if status != "pending" {
		return nil, ErrJournalState
	}
	if current != version {
		_, err = p.journal.db.ExecContext(ctx, `UPDATE private_settings_reconcile SET state='superseded' WHERE version=? AND tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? AND state='pending'`, version, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation)
		return nil, err
	}
	state, err := p.ensureOwner(ctx, o, generation)
	if err != nil {
		return nil, err
	}
	if !stillCurrent() {
		return nil, ErrJournalConflict
	}
	events, err := p.app.Service().ReconcilePrivateBotSettings(o.SurfaceID)
	if err != nil {
		p.poisoned = true
		return nil, err
	}
	effects, err := p.freezeEffects(o, generation, "settings:"+version, events)
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
	if err = p.journal.commitCheckpointExtended(ctx, cp, state.revision, effects, 0, nil, nil, nil, nil, &settingsReconcileRef{Version: version}, nil, nil); err != nil {
		p.poisoned = true
		return nil, fmt.Errorf("%w: settings reconcile: %v", ErrProcessorPoisoned, err)
	}
	p.owners[o] = processorOwner{cp.Revision, cp.Cursor, generation}
	return p.journal.processorEffects(ctx, o, generation, cp.Revision)
}
