package privatechat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const upgradeRecoverySchema = `CREATE TABLE IF NOT EXISTS private_upgrade_recovery (maintenance_id TEXT NOT NULL, old_incarnation TEXT NOT NULL, tenant TEXT NOT NULL, app_id TEXT NOT NULL, gateway TEXT NOT NULL, surface TEXT NOT NULL, chat TEXT NOT NULL, user_id TEXT NOT NULL, generation INTEGER NOT NULL, plan BLOB NOT NULL, plan_hash TEXT NOT NULL, effect_id TEXT NOT NULL, PRIMARY KEY(maintenance_id,old_incarnation)); CREATE TABLE IF NOT EXISTS private_upgrade_resumed (maintenance_id TEXT NOT NULL, old_incarnation TEXT NOT NULL, new_instance TEXT NOT NULL, revision INTEGER NOT NULL, PRIMARY KEY(maintenance_id,old_incarnation)); CREATE TABLE IF NOT EXISTS private_upgrade_paused (maintenance_id TEXT NOT NULL, old_incarnation TEXT NOT NULL, plan BLOB NOT NULL, plan_hash TEXT NOT NULL, PRIMARY KEY(maintenance_id,old_incarnation)); CREATE TABLE IF NOT EXISTS private_upgrade_hello (maintenance_id TEXT NOT NULL, old_incarnation TEXT NOT NULL, new_incarnation TEXT NOT NULL, revision INTEGER NOT NULL, PRIMARY KEY(maintenance_id,old_incarnation));`

// UpgradeRecoveryPlan freezes the previously authenticated owner and exact
// headless selection. It is not authority to create an arbitrary carrier.
type UpgradeRecoveryPlan struct {
	MaintenanceID            string
	MaintenanceEpoch         int64
	Owner                    Owner
	Generation               int64
	OldOwner                 agentproto.NativeOwner
	ThreadID, CWD, ProfileID string
	PreviousMode             state.DispatchMode
}

// UpgradeRecoverySelection only exposes an immutable plan paired with the
// exact committed private effect and owner, including after process restart.
func (j *Journal) UpgradeRecoverySelection(ctx context.Context, owner Owner, generation int64, effectID string) (UpgradeRecoveryPlan, bool, error) {
	var plan UpgradeRecoveryPlan
	var raw []byte
	var hash string
	err := j.db.QueryRowContext(ctx, `SELECT r.plan,r.plan_hash FROM private_upgrade_recovery r JOIN private_effect e ON e.id=r.effect_id WHERE r.tenant=? AND r.app_id=? AND r.gateway=? AND r.surface=? AND r.chat=? AND r.user_id=? AND r.generation=? AND r.effect_id=? AND e.tenant=r.tenant AND e.app_id=r.app_id AND e.gateway=r.gateway AND e.surface=r.surface AND e.chat=r.chat AND e.user_id=r.user_id AND e.generation=r.generation`, owner.TenantID, owner.AppID, owner.GatewayID, owner.SurfaceID, owner.ChatID, owner.UserID, generation, effectID).Scan(&raw, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return plan, false, nil
	}
	if err != nil {
		return plan, false, err
	}
	if migrationSHA(raw) != hash || json.Unmarshal(raw, &plan) != nil || plan.Owner != owner || plan.Generation != generation {
		return UpgradeRecoveryPlan{}, false, ErrJournalConflict
	}
	return plan, true, nil
}

func (j *Journal) UpgradeRecoveryEffectApplied(ctx context.Context, owner Owner, generation int64, effectID string) (bool, error) {
	var n int
	err := j.db.QueryRowContext(ctx, `SELECT count(*) FROM private_upgrade_recovery r JOIN private_effect e ON e.id=r.effect_id JOIN private_effect_applied a ON a.effect_id=e.id WHERE r.tenant=? AND r.app_id=? AND r.gateway=? AND r.surface=? AND r.chat=? AND r.user_id=? AND r.generation=? AND r.effect_id=? AND e.tenant=r.tenant AND e.app_id=r.app_id AND e.gateway=r.gateway AND e.surface=r.surface AND e.chat=r.chat AND e.user_id=r.user_id AND e.generation=r.generation`, owner.TenantID, owner.AppID, owner.GatewayID, owner.SurfaceID, owner.ChatID, owner.UserID, generation, effectID).Scan(&n)
	return n == 1, err
}

// UpgradeRecoveryAuthority holds the backend maintenance/stop proof while the
// processor commits the plan and core checkpoint. The callback is local-only.
type UpgradeRecoveryAuthority interface {
	WithUpgradeRecovery(context.Context, UpgradeRecoveryPlan, func() error) error
}

type UpgradePauseAuthority interface {
	WithUpgradePause(context.Context, UpgradeRecoveryPlan, func() error) error
}

type UpgradeReleaseAuthority interface {
	WithReleasedUpgrade(context.Context, UpgradeRecoveryPlan, string, func() error) error
}

type upgradeRecoveryRef struct {
	Plan             UpgradeRecoveryPlan
	EffectID         string
	NewInstanceID    string
	HelloIncarnation string
	Pause            bool
}

func (j *Journal) UpgradeRecoveryForCarrier(ctx context.Context, maintenanceID, incarnation string) (UpgradeRecoveryPlan, bool, error) {
	var plan UpgradeRecoveryPlan
	var raw []byte
	var hash string
	err := j.db.QueryRowContext(ctx, `SELECT plan,plan_hash FROM private_upgrade_recovery WHERE maintenance_id=? AND old_incarnation=?`, maintenanceID, incarnation).Scan(&raw, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return plan, false, nil
	}
	if err != nil {
		return plan, false, err
	}
	if migrationSHA(raw) != hash || json.Unmarshal(raw, &plan) != nil || plan.MaintenanceID != maintenanceID || plan.OldOwner.CarrierIncarnation != incarnation {
		return UpgradeRecoveryPlan{}, false, ErrJournalConflict
	}
	return plan, true, nil
}

func (j *Journal) UpgradePauseForCarrier(ctx context.Context, maintenanceID, incarnation string) (UpgradeRecoveryPlan, bool, error) {
	var plan UpgradeRecoveryPlan
	var raw []byte
	var hash string
	err := j.db.QueryRowContext(ctx, `SELECT plan,plan_hash FROM private_upgrade_paused WHERE maintenance_id=? AND old_incarnation=?`, maintenanceID, incarnation).Scan(&raw, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return plan, false, nil
	}
	if err != nil {
		return plan, false, err
	}
	if migrationSHA(raw) != hash || json.Unmarshal(raw, &plan) != nil || plan.MaintenanceID != maintenanceID || plan.OldOwner.CarrierIncarnation != incarnation {
		return UpgradeRecoveryPlan{}, false, ErrJournalConflict
	}
	return plan, true, nil
}

// PauseUpgradeDispatch freezes the old dispatch mode before draining starts.
func (p *Processor) PauseUpgradeDispatch(ctx context.Context, plan UpgradeRecoveryPlan, authority UpgradePauseAuthority) error {
	if authority == nil || !validOwner(plan.Owner) || plan.Generation <= 0 || plan.OldOwner.Validate() != nil || plan.MaintenanceID == "" || plan.MaintenanceEpoch <= 0 {
		return ErrJournalConflict
	}
	unlock := p.journal.LockReferenceMutation()
	defer unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return ErrProcessorPoisoned
	}
	return authority.WithUpgradePause(ctx, plan, func() error {
		ctx, projectionErr := beginProjection(ctx)
		if projectionErr != nil {
			return projectionErr
		}

		saved, exists, err := p.journal.UpgradePauseForCarrier(ctx, plan.MaintenanceID, plan.OldOwner.CarrierIncarnation)
		if err != nil {
			return err
		}
		if exists {
			if saved != plan {
				return ErrJournalConflict
			}
			return nil
		}
		owner, err := p.ensureOwner(ctx, plan.Owner, plan.Generation)
		if err != nil {
			return err
		}
		surface := p.app.Service().Surface(plan.Owner.SurfaceID)
		if surface == nil || surface.SelectedThreadID != plan.ThreadID {
			return ErrJournalConflict
		}
		previous, err := p.app.Service().SuspendPrivateUpgradeDispatch(plan.Owner.SurfaceID, plan.OldOwner.InstanceID)
		if err != nil {
			return err
		}
		if previous != plan.PreviousMode {
			p.poisoned = true
			return ErrJournalConflict
		}
		core, err := p.app.Service().ExportPrivateCheckpoint(plan.Owner.SurfaceID)
		if err != nil {
			p.poisoned = true
			return err
		}
		cp := Checkpoint{Owner: plan.Owner, Generation: plan.Generation, Revision: owner.revision + 1, Cursor: owner.cursor, Core: core}
		ref := migrationResumeRef{Upgrade: &upgradeRecoveryRef{Plan: plan, Pause: true}}
		if err = p.journal.commitCheckpointExtended(ctx, cp, owner.revision, nil, 0, nil, nil, nil, nil, nil, nil, nil, &ref); err != nil {
			p.poisoned = true
			return err
		}
		p.owners[plan.Owner] = processorOwner{cp.Revision, cp.Cursor, plan.Generation}
		return nil
	})
}

func (j *Journal) commitUpgradeRecovery(ctx context.Context, tx *sql.Tx, cp Checkpoint, ref upgradeRecoveryRef) error {
	if (!ref.Pause && ref.EffectID == "") || ref.Plan.Owner != cp.Owner || ref.Plan.Generation != cp.Generation || ref.Plan.MaintenanceID == "" || ref.Plan.MaintenanceEpoch <= 0 || ref.Plan.OldOwner.CarrierIncarnation == "" {
		return ErrJournalConflict
	}
	raw, err := json.Marshal(ref.Plan)
	if err != nil {
		return err
	}
	if ref.Pause {
		_, err = tx.ExecContext(ctx, `INSERT INTO private_upgrade_paused(maintenance_id,old_incarnation,plan,plan_hash) VALUES(?,?,?,?)`, ref.Plan.MaintenanceID, ref.Plan.OldOwner.CarrierIncarnation, raw, migrationSHA(raw))
		return err
	}
	if ref.HelloIncarnation != "" {
		result, err := tx.ExecContext(ctx, `INSERT INTO private_upgrade_hello(maintenance_id,old_incarnation,new_incarnation,revision) SELECT maintenance_id,old_incarnation,?,? FROM private_upgrade_recovery WHERE maintenance_id=? AND old_incarnation=? AND effect_id=? AND plan_hash=?`, ref.HelloIncarnation, cp.Revision, ref.Plan.MaintenanceID, ref.Plan.OldOwner.CarrierIncarnation, ref.EffectID, migrationSHA(raw))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrJournalConflict
		}
		return nil
	}
	if ref.NewInstanceID != "" {
		result, err := tx.ExecContext(ctx, `INSERT INTO private_upgrade_resumed(maintenance_id,old_incarnation,new_instance,revision) SELECT maintenance_id,old_incarnation,?,? FROM private_upgrade_recovery WHERE maintenance_id=? AND old_incarnation=? AND effect_id=? AND plan_hash=?`, ref.NewInstanceID, cp.Revision, ref.Plan.MaintenanceID, ref.Plan.OldOwner.CarrierIncarnation, ref.EffectID, migrationSHA(raw))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrJournalConflict
		}
		return nil
	}
	o := cp.Owner
	_, err = tx.ExecContext(ctx, `INSERT INTO private_upgrade_recovery(maintenance_id,old_incarnation,tenant,app_id,gateway,surface,chat,user_id,generation,plan,plan_hash,effect_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, ref.Plan.MaintenanceID, ref.Plan.OldOwner.CarrierIncarnation, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, cp.Generation, raw, migrationSHA(raw), ref.EffectID)
	return err
}

// PrepareUpgradeRecovery commits the real core transition, launch effect and
// exact maintenance plan atomically while the host holds its ownership proof.
func (p *Processor) PrepareUpgradeRecovery(ctx context.Context, plan UpgradeRecoveryPlan, authority UpgradeRecoveryAuthority) ([]EffectRecord, error) {
	if authority == nil || !validOwner(plan.Owner) || plan.Generation <= 0 || plan.OldOwner.Validate() != nil || plan.MaintenanceID == "" || plan.MaintenanceEpoch <= 0 {
		return nil, ErrJournalConflict
	}
	unlockReference := p.journal.LockReferenceMutation()
	defer unlockReference()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return nil, ErrProcessorPoisoned
	}
	var out []EffectRecord
	err := authority.WithUpgradeRecovery(ctx, plan, func() error {
		ctx, projectionErr := beginProjection(ctx)
		if projectionErr != nil {
			return projectionErr
		}

		var existing string
		var raw []byte
		var hash string
		err := p.journal.db.QueryRowContext(ctx, `SELECT effect_id,plan,plan_hash FROM private_upgrade_recovery WHERE maintenance_id=? AND old_incarnation=?`, plan.MaintenanceID, plan.OldOwner.CarrierIncarnation).Scan(&existing, &raw, &hash)
		if err == nil {
			var saved UpgradeRecoveryPlan
			if migrationSHA(raw) != hash || json.Unmarshal(raw, &saved) != nil || saved != plan {
				return ErrJournalConflict
			}
			effect, e := p.journal.LoadEffect(ctx, plan.Owner, plan.Generation, existing)
			if e != nil {
				return e
			}
			out = []EffectRecord{effect}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		state, err := p.ensureOwner(ctx, plan.Owner, plan.Generation)
		if err != nil {
			return err
		}
		paused, exists, err := p.journal.UpgradePauseForCarrier(ctx, plan.MaintenanceID, plan.OldOwner.CarrierIncarnation)
		if err != nil {
			return err
		}
		if !exists || paused != plan {
			return ErrJournalConflict
		}
		events, err := p.app.Service().PreparePrivateUpgradeResume(orchestrator.PrivateUpgradeResume{SurfaceID: plan.Owner.SurfaceID, OldInstanceID: plan.OldOwner.InstanceID, ThreadID: plan.ThreadID, CWD: plan.CWD, Backend: plan.OldOwner.Backend, ProfileID: plan.ProfileID, HistoryNamespace: plan.OldOwner.HistoryNamespace})
		if err != nil {
			p.poisoned = true
			return err
		}
		effects, err := p.freezeEffects(plan.Owner, plan.Generation, "upgrade-recovery:"+plan.MaintenanceID+":"+plan.OldOwner.CarrierIncarnation, events)
		if err != nil {
			p.poisoned = true
			return err
		}
		effectID := ""
		for i, event := range events {
			if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandStartHeadless {
				if effectID != "" {
					p.poisoned = true
					return ErrJournalConflict
				}
				effectID = effects[i].ID
			}
		}
		if effectID == "" {
			p.poisoned = true
			return ErrJournalState
		}
		core, err := p.app.Service().ExportPrivateCheckpoint(plan.Owner.SurfaceID)
		if err != nil {
			p.poisoned = true
			return err
		}
		cp := Checkpoint{Owner: plan.Owner, Generation: plan.Generation, Revision: state.revision + 1, Cursor: state.cursor, Core: core}
		ref := migrationResumeRef{Upgrade: &upgradeRecoveryRef{Plan: plan, EffectID: effectID}}
		if err = p.journal.commitCheckpointExtended(ctx, cp, state.revision, effects, 0, nil, nil, nil, nil, nil, nil, nil, &ref); err != nil {
			p.poisoned = true
			return err
		}
		p.owners[plan.Owner] = processorOwner{cp.Revision, cp.Cursor, plan.Generation}
		out, err = p.journal.processorEffects(ctx, plan.Owner, plan.Generation, cp.Revision)
		return err
	})
	return out, err
}

func (p *Processor) RestoreUpgradeHeadlessProjection(ctx context.Context, owner Owner, generation int64, effectID string) error {
	unlock := p.journal.LockReferenceMutation()
	defer unlock()
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
	plan, required, err := p.journal.UpgradeRecoverySelection(ctx, owner, generation, effectID)
	if err != nil {
		return err
	}
	if !required {
		return ErrJournalState
	}
	effect, err := p.journal.LoadEffect(ctx, owner, generation, effectID)
	if err != nil {
		return err
	}
	event, _, err := DecodeStoredEffect(effect.Effect.Body)
	if err != nil {
		return err
	}
	cmd := event.DaemonCommand
	if cmd == nil || cmd.Kind != control.DaemonCommandStartHeadless || cmd.ThreadID != plan.ThreadID || cmd.ThreadCWD != plan.CWD || cmd.Backend != plan.OldOwner.Backend {
		return ErrJournalConflict
	}
	if _, err = p.ensureOwner(ctx, owner, generation); err != nil {
		return err
	}
	if surface := p.app.Service().Surface(owner.SurfaceID); surface != nil && surface.AttachedInstanceID == cmd.InstanceID {
		instance := p.app.Service().Instance(cmd.InstanceID)
		if instance != nil && instance.Online && instance.Managed {
			return nil
		}
	}
	req := orchestrator.PrivateUpgradeResume{SurfaceID: owner.SurfaceID, OldInstanceID: plan.OldOwner.InstanceID, ThreadID: plan.ThreadID, CWD: plan.CWD, Backend: plan.OldOwner.Backend, ProfileID: plan.ProfileID, HistoryNamespace: plan.OldOwner.HistoryNamespace}
	return p.app.Service().RestorePrivateUpgradeHeadlessProjection(req, cmd.InstanceID)
}

// ResumeUpgradeRecovery restores the frozen dispatch mode only after the
// backend gate has durably released and the exact replacement is online.
func (p *Processor) ResumeUpgradeRecovery(ctx context.Context, plan UpgradeRecoveryPlan, newInstance string, authority UpgradeReleaseAuthority) ([]EffectRecord, error) {
	if authority == nil || newInstance == "" {
		return nil, ErrJournalConflict
	}
	unlock := p.journal.LockReferenceMutation()
	defer unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return nil, ErrProcessorPoisoned
	}
	var out []EffectRecord
	err := authority.WithReleasedUpgrade(ctx, plan, newInstance, func() error {
		ctx, projectionErr := beginProjection(ctx)
		if projectionErr != nil {
			return projectionErr
		}

		var effectID, existing string
		var raw []byte
		var hash string
		err := p.journal.db.QueryRowContext(ctx, `SELECT r.effect_id,r.plan,r.plan_hash,coalesce(done.new_instance,'') FROM private_upgrade_recovery r LEFT JOIN private_upgrade_resumed done ON done.maintenance_id=r.maintenance_id AND done.old_incarnation=r.old_incarnation WHERE r.maintenance_id=? AND r.old_incarnation=?`, plan.MaintenanceID, plan.OldOwner.CarrierIncarnation).Scan(&effectID, &raw, &hash, &existing)
		if err != nil {
			return err
		}
		var saved UpgradeRecoveryPlan
		if migrationSHA(raw) != hash || json.Unmarshal(raw, &saved) != nil || saved != plan {
			return ErrJournalConflict
		}
		if existing != "" {
			if existing != newInstance {
				return ErrJournalConflict
			}
			return nil
		}
		state, err := p.ensureOwner(ctx, plan.Owner, plan.Generation)
		if err != nil {
			return err
		}
		events, err := p.app.Service().ResumePrivateUpgradeDispatch(plan.Owner.SurfaceID, newInstance, plan.PreviousMode)
		if err != nil {
			return err
		}
		effects, err := p.freezeEffects(plan.Owner, plan.Generation, "upgrade-resumed:"+plan.MaintenanceID+":"+plan.OldOwner.CarrierIncarnation, events)
		if err != nil {
			p.poisoned = true
			return err
		}
		core, err := p.app.Service().ExportPrivateCheckpoint(plan.Owner.SurfaceID)
		if err != nil {
			p.poisoned = true
			return err
		}
		cp := Checkpoint{Owner: plan.Owner, Generation: plan.Generation, Revision: state.revision + 1, Cursor: state.cursor, Core: core}
		ref := migrationResumeRef{Upgrade: &upgradeRecoveryRef{Plan: plan, EffectID: effectID, NewInstanceID: newInstance}}
		if err = p.journal.commitCheckpointExtended(ctx, cp, state.revision, effects, 0, nil, nil, nil, nil, nil, nil, nil, &ref); err != nil {
			p.poisoned = true
			return err
		}
		p.owners[plan.Owner] = processorOwner{cp.Revision, cp.Cursor, plan.Generation}
		out, err = p.journal.processorEffects(ctx, plan.Owner, plan.Generation, cp.Revision)
		return err
	})
	return out, err
}
