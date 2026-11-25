package privatechat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

type migrationResumeRef struct {
	ImportID, EffectID, PlanHash string
	Historical                   *historicalRecoveryRef
	Upgrade                      *upgradeRecoveryRef
	Connection                   *nativeConnectionRef
}

func (j *Journal) HasMigrationResumePlan(ctx context.Context, req MigrationImportRequest) (bool, error) {
	var count int
	err := j.db.QueryRowContext(ctx, `SELECT count(*) FROM private_migration_resume r JOIN private_migration_imports i ON i.import_id=r.import_id WHERE i.import_id=? AND i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=?`, req.ImportID, req.Owner.TenantID, req.Owner.AppID, req.Owner.GatewayID, req.Owner.SurfaceID, req.Owner.ChatID, req.Owner.UserID, req.Generation).Scan(&count)
	return count == 1, err
}

// MigrationResumeSelection distinguishes ordinary launches from an imported
// owner whose exact migration plan has not yet been bound to its core effect.
func (j *Journal) MigrationResumeSelection(ctx context.Context, owner Owner, generation int64, effectID string) (MigrationResumeIntent, bool, error) {
	var empty MigrationResumeIntent
	if !validOwner(owner) || generation < 1 || effectID == "" {
		return empty, false, ErrJournalState
	}
	var bound string
	err := j.db.QueryRowContext(ctx, `SELECT r.effect_id FROM private_migration_resume r JOIN private_migration_imports i ON i.import_id=r.import_id WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=?`, owner.TenantID, owner.AppID, owner.GatewayID, owner.SurfaceID, owner.ChatID, owner.UserID, generation).Scan(&bound)
	if errors.Is(err, sql.ErrNoRows) {
		return empty, false, nil
	}
	if err != nil {
		return empty, false, err
	}
	if bound == "" {
		return empty, true, ErrDeferred
	}
	if bound != effectID {
		return empty, false, nil
	}
	plan, err := j.MigrationResumePlan(ctx, owner, generation, effectID)
	return plan, true, err
}

// MigrationResumePlan returns the sealed-source plan only for its exact
// checkpoint-committed headless effect. An unbound plan grants no launch.
func (j *Journal) MigrationResumePlan(ctx context.Context, owner Owner, generation int64, effectID string) (MigrationResumeIntent, error) {
	var plan MigrationResumeIntent
	if !validOwner(owner) || generation < 1 || effectID == "" {
		return plan, ErrJournalState
	}
	var raw []byte
	var hash string
	err := j.db.QueryRowContext(ctx, `SELECT r.plan,r.plan_hash FROM private_migration_resume r JOIN private_migration_imports i ON i.import_id=r.import_id JOIN private_effect e ON e.id=r.effect_id WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=? AND r.effect_id=? AND e.tenant=i.tenant AND e.app_id=i.app_id AND e.gateway=i.gateway AND e.surface=i.surface AND e.chat=i.chat AND e.user_id=i.user_id AND e.generation=i.generation`, owner.TenantID, owner.AppID, owner.GatewayID, owner.SurfaceID, owner.ChatID, owner.UserID, generation, effectID).Scan(&raw, &hash)
	if err != nil {
		return plan, err
	}
	if migrationSHA(raw) != hash || json.Unmarshal(raw, &plan) != nil {
		return MigrationResumeIntent{}, ErrJournalConflict
	}
	return plan, nil
}

// PrepareMigrationResume freezes the ordinary core headless transition and its
// exact source plan in one target checkpoint transaction. The caller must
// first verify committed target activation and the sealed source.
func (p *Processor) PrepareMigrationResume(ctx context.Context, o Owner, generation int64) ([]EffectRecord, error) {
	unlockReference := p.journal.LockReferenceMutation()
	defer unlockReference()
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
	var importID, planHash, bound string
	var raw []byte
	err := p.journal.db.QueryRowContext(ctx, `SELECT r.import_id,r.plan,r.plan_hash,r.effect_id FROM private_migration_resume r JOIN private_migration_imports i ON i.import_id=r.import_id WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&importID, &raw, &planHash, &bound)
	if err != nil {
		return nil, err
	}
	var plan MigrationResumeIntent
	if migrationSHA(raw) != planHash || json.Unmarshal(raw, &plan) != nil {
		return nil, ErrJournalConflict
	}
	if bound != "" {
		effect, e := p.journal.LoadEffect(ctx, o, generation, bound)
		if e != nil {
			return nil, e
		}
		return []EffectRecord{effect}, nil
	}
	state, e := p.ensureOwner(ctx, o, generation)
	if e != nil {
		return nil, e
	}
	events, e := p.app.Service().PreparePrivateMigrationResume(o.SurfaceID, plan.ThreadID, plan.CWD, agentproto.Backend(plan.Backend), plan.ProfileID)
	if e != nil {
		p.poisoned = true
		return nil, e
	}
	effects, e := p.freezeEffects(o, generation, "migration-resume:"+importID, events)
	if e != nil {
		p.poisoned = true
		return nil, e
	}
	effectID := ""
	for i, event := range events {
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandStartHeadless {
			if effectID != "" {
				p.poisoned = true
				return nil, ErrJournalConflict
			}
			effectID = effects[i].ID
		}
	}
	if effectID == "" {
		p.poisoned = true
		return nil, ErrJournalState
	}
	core, e := p.app.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if e != nil {
		p.poisoned = true
		return nil, e
	}
	cp := Checkpoint{Owner: o, Generation: generation, Revision: state.revision + 1, Cursor: state.cursor, Core: core}
	ref := migrationResumeRef{ImportID: importID, EffectID: effectID, PlanHash: planHash}
	if e = p.journal.commitCheckpointExtended(ctx, cp, state.revision, effects, 0, nil, nil, nil, nil, nil, nil, nil, &ref); e != nil {
		p.poisoned = true
		return nil, e
	}
	p.owners[o] = processorOwner{cp.Revision, cp.Cursor, generation}
	return p.journal.processorEffects(ctx, o, generation, cp.Revision)
}

// RestoreMigrationHeadlessProjection matches the durable launch effect and
// source plan to the frozen core hold before reinstating its in-memory state.
func (p *Processor) RestoreMigrationHeadlessProjection(ctx context.Context, o Owner, generation int64, effectID string) error {
	unlockReference := p.journal.LockReferenceMutation()
	defer unlockReference()
	p.mu.Lock()
	defer p.mu.Unlock()
	projectionCtx, projectionErr := beginProjection(ctx)
	if projectionErr != nil {
		return projectionErr
	}
	ctx = projectionCtx
	if p.poisoned {
		return ErrProcessorPoisoned
	}
	plan, required, err := p.journal.MigrationResumeSelection(ctx, o, generation, effectID)
	if err != nil {
		return err
	}
	if !required {
		return ErrJournalState
	}
	effect, err := p.journal.LoadEffect(ctx, o, generation, effectID)
	if err != nil {
		return err
	}
	event, _, err := DecodeStoredEffect(effect.Effect.Body)
	if err != nil {
		return err
	}
	cmd := event.DaemonCommand
	if cmd == nil || cmd.Kind != control.DaemonCommandStartHeadless || cmd.ThreadID != plan.ThreadID || cmd.ThreadCWD != plan.CWD || string(cmd.Backend) != plan.Backend {
		return ErrJournalConflict
	}
	if _, err = p.ensureOwner(ctx, o, generation); err != nil {
		return err
	}
	return p.app.Service().RestorePrivateMigrationHeadlessProjection(o.SurfaceID, cmd.InstanceID, plan.ThreadID, plan.CWD, agentproto.Backend(plan.Backend))
}
