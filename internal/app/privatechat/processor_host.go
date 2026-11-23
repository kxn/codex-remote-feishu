package privatechat

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

type hostEffectRef struct{ ID, ReceiptDigest, SettingsVersion string }

// HostReceiptReducer is trusted local computation over a confirmed, already
// verified and durable delivery receipt. It must not do I/O or send effects.
type HostReceiptReducer interface {
	ReduceHostReceipt(*App, Effect, json.RawMessage) ([]eventcontract.Event, error)
}

// HostSettingsFanout returns a confirmed authoritative config version to
// reconcile independently across the remaining private owners of that Bot.
type HostSettingsFanout interface {
	SettingsFanout(Effect, json.RawMessage) (string, error)
}

// ApplyHostReceipt projects one durable confirmed receipt exactly once. The
// receipt marker, checkpoint and subsequent effects commit atomically.
func (p *Processor) ApplyHostReceipt(ctx context.Context, o Owner, generation int64, id string, reducer HostReceiptReducer) ([]EffectRecord, error) {
	return p.applyHostReceipt(ctx, o, generation, id, reducer, false)
}

// ApplyLocalHostReceipt projects only a receipt durably confirmed by the local
// command runner. It shares the same atomic applied marker as publications.
func (p *Processor) ApplyLocalHostReceipt(ctx context.Context, o Owner, generation int64, id string, reducer HostReceiptReducer) ([]EffectRecord, error) {
	return p.applyHostReceipt(ctx, o, generation, id, reducer, true)
}

func (p *Processor) applyHostReceipt(ctx context.Context, o Owner, generation int64, id string, reducer HostReceiptReducer, local bool) ([]EffectRecord, error) {
	unlockReference := p.journal.LockReferenceMutation()
	defer unlockReference()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return nil, ErrProcessorPoisoned
	}
	if !validOwner(o) || generation < 1 || id == "" || reducer == nil {
		return nil, ErrJournalState
	}
	var effect Effect
	effect.ID = id
	var receipt []byte
	err := p.journal.db.QueryRowContext(ctx, `SELECT lane,body,receipt FROM private_effect WHERE id=? AND tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? AND state='confirmed'`, id, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&effect.Lane, &effect.Body, &receipt)
	if err != nil {
		return nil, err
	}
	if local {
		var recorded []byte
		if err = p.journal.db.QueryRowContext(ctx, `SELECT receipt FROM private_local_receipts WHERE effect_id=?`, id).Scan(&recorded); err != nil {
			return nil, err
		}
		if !bytes.Equal(recorded, receipt) {
			return nil, ErrJournalConflict
		}
	} else {
		publication, err := p.journal.LoadPublication(ctx, o, generation, id)
		if err != nil {
			return nil, err
		}
		var delivered PublicationReceipt
		if err = json.Unmarshal(receipt, &delivered); err != nil {
			return nil, err
		}
		if delivered.Version != 1 || delivered.EffectID != id || delivered.PlanHash != publication.PlanHash || !bytes.Equal(delivered.Plan, publication.Plan) || delivered.CompletedAt.IsZero() || len(delivered.Results) != len(publication.Operations) {
			return nil, ErrJournalConflict
		}
		for i, op := range publication.Operations {
			result := delivered.Results[i]
			if op.State != "confirmed" || result.Ordinal != op.Ordinal || !bytes.Equal(result.Operation, op.Body) || !bytes.Equal(result.Receipt, op.Receipt) {
				return nil, ErrJournalConflict
			}
		}
	}
	hash := sha256.Sum256(receipt)
	ref := hostEffectRef{ID: id, ReceiptDigest: hex.EncodeToString(hash[:])}
	var prior int64
	var digest string
	err = p.journal.db.QueryRowContext(ctx, `SELECT revision,receipt_digest FROM private_effect_applied WHERE effect_id=?`, id).Scan(&prior, &digest)
	if err == nil {
		if digest != ref.ReceiptDigest {
			return nil, ErrJournalConflict
		}
		return p.journal.processorEffects(ctx, o, generation, prior)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	state, err := p.ensureOwner(ctx, o, generation)
	if err != nil {
		return nil, err
	}
	events, err := reducer.ReduceHostReceipt(p.app, effect, receipt)
	if err != nil {
		if errors.Is(err, ErrDeferred) {
			return nil, err
		}
		p.poisoned = true
		return nil, fmt.Errorf("%w: host receipt: %v", ErrProcessorPoisoned, err)
	}
	if fanout, ok := reducer.(HostSettingsFanout); ok {
		ref.SettingsVersion, err = fanout.SettingsFanout(effect, receipt)
		if err != nil {
			p.poisoned = true
			return nil, err
		}
	}
	effects, err := p.freezeEffects(o, "host:"+id, events)
	if err != nil {
		p.poisoned = true
		return nil, fmt.Errorf("%w: host effects: %v", ErrProcessorPoisoned, err)
	}
	core, err := p.app.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		p.poisoned = true
		return nil, fmt.Errorf("%w: host snapshot: %v", ErrProcessorPoisoned, err)
	}
	cp := Checkpoint{Owner: o, Generation: generation, Revision: state.revision + 1, Cursor: state.cursor, Core: core}
	if err = p.journal.commitCheckpointExtended(ctx, cp, state.revision, effects, 0, nil, nil, &ref, nil, nil, nil, nil); err != nil {
		p.poisoned = true
		return nil, fmt.Errorf("%w: host commit: %v", ErrProcessorPoisoned, err)
	}
	p.owners[o] = processorOwner{cp.Revision, cp.Cursor, generation}
	return p.journal.processorEffects(ctx, o, generation, cp.Revision)
}
