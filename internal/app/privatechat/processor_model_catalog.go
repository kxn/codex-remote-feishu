package privatechat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

// ApplyPrivateModelCatalog commits a trusted host observation and its page
// effect together. Identical observations never write another checkpoint.
func (p *Processor) ApplyPrivateModelCatalog(ctx context.Context, o Owner, generation int64, effectID, tracking, profile, fingerprint, namespace string, revision int64, catalog *agentproto.ModelCatalogSnapshot, problem string) ([]EffectRecord, error) {
	unlock := p.journal.LockReferenceMutation()
	defer unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return nil, ErrProcessorPoisoned
	}
	current, err := p.ensureOwner(ctx, o, generation)
	if err != nil {
		return nil, err
	}
	effect, err := p.journal.LoadEffect(ctx, o, generation, effectID)
	if err != nil {
		return nil, err
	}
	event, _, err := DecodeStoredEffect(effect.Effect.Body)
	if err != nil {
		return nil, err
	}
	if event.DaemonCommand == nil || event.DaemonCommand.Kind != control.DaemonCommandPrivateModelCatalogQuery || event.SurfaceSessionID != o.SurfaceID {
		return nil, ErrJournalConflict
	}
	var intent struct{ TrackingKey, ProfileID, Fingerprint string }
	if json.Unmarshal([]byte(event.DaemonCommand.Text), &intent) != nil || intent.TrackingKey != tracking || intent.ProfileID != profile || intent.Fingerprint != fingerprint {
		return nil, ErrJournalConflict
	}
	raw, err := json.Marshal(struct {
		Revision  int64
		Namespace string
		Catalog   *agentproto.ModelCatalogSnapshot
		Problem   string
	}{revision, namespace, catalog, problem})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	events, changed := p.app.Service().ProjectPrivateModelCatalog(o.SurfaceID, effectID, tracking, hash, profile, fingerprint, namespace, revision, catalog, problem)
	if !changed {
		return nil, nil
	}
	effects, err := p.freezeEffects(o, generation, "model-catalog:"+effectID+":"+hash, events)
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
	if err = p.journal.commitCheckpoint(ctx, cp, current.revision, effects, 0, nil); err != nil {
		p.poisoned = true
		return nil, fmt.Errorf("%w: model catalog: %v", ErrProcessorPoisoned, err)
	}
	p.owners[o] = processorOwner{cp.Revision, cp.Cursor, generation}
	return p.journal.processorEffects(ctx, o, generation, cp.Revision)
}
